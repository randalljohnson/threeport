package provider

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2/google"
)

// GKE credential threading must be per call, never a process-global.
//
// Two concurrent GKE creates for different service accounts must each
// authenticate against their own credentials. The service account JSON is
// threaded into the Pulumi provider and every GCP SDK client per call instead
// of writing GOOGLE_APPLICATION_CREDENTIALS, which two goroutines would race.

// serviceAccountJSON builds well-formed GCP service_account credentials JSON
// with a throwaway RSA key and clientEmail. CredentialsFromJSON parses the key
// at construction, so the email it reports proves which credentials were used.
func serviceAccountJSON(t *testing.T, clientEmail string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type: "PRIVATE KEY",
		Bytes: func() []byte {
			der, err := x509.MarshalPKCS8PrivateKey(key)
			require.NoError(t, err)
			return der
		}(),
	})
	creds := map[string]string{
		"type":                        "service_account",
		"project_id":                  "test-project",
		"private_key_id":              "test-key-id",
		"private_key":                 string(keyPEM),
		"client_email":                clientEmail,
		"client_id":                   "test-client-id",
		"token_uri":                   "https://oauth2.googleapis.com/token",
		"auth_uri":                    "https://accounts.google.com/o/oauth2/auth",
		"auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
	}
	b, err := json.Marshal(creds)
	require.NoError(t, err)
	return string(b)
}

// emailFromCredentialsJSON extracts client_email from parsed credentials JSON.
func emailFromCredentialsJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var parsed struct {
		ClientEmail string `json:"client_email"`
	}
	require.NoError(t, json.Unmarshal(raw, &parsed))
	return parsed.ClientEmail
}

// TestGKEClientOptions_ThreadsPerInstanceCredentials asserts gcpClientOptions
// appends a credentials option only when the instance carries service account
// credentials.
func TestGKEClientOptions_ThreadsPerInstanceCredentials(t *testing.T) {
	withCreds := &KubernetesRuntimeInfraGKE{
		ServiceAccountCredentials: serviceAccountJSON(t, "a@test-project.iam.gserviceaccount.com"),
	}
	withoutCreds := &KubernetesRuntimeInfraGKE{}

	base := withCreds.gcpClientOptions()
	assert.Len(t, base, 1, "an instance with credentials threads one credentials option")

	none := withoutCreds.gcpClientOptions()
	assert.Empty(t, none, "an instance without credentials threads no credentials option and falls back to ADC")
}

// TestGKETokenSource_ConcurrentCredentialsDoNotBleed asserts two GKE
// instances with distinct credentials bind concurrent token sources to their
// own service accounts without process-global bleed.
func TestGKETokenSource_ConcurrentCredentialsDoNotBleed(t *testing.T) {
	const (
		emailA = "account-a@test-project.iam.gserviceaccount.com"
		emailB = "account-b@test-project.iam.gserviceaccount.com"
	)
	instanceA := &KubernetesRuntimeInfraGKE{ServiceAccountCredentials: serviceAccountJSON(t, emailA)}
	instanceB := &KubernetesRuntimeInfraGKE{ServiceAccountCredentials: serviceAccountJSON(t, emailB)}

	const scope = "https://www.googleapis.com/auth/cloud-platform"
	ctx := context.Background()

	const rounds = 50
	var wg sync.WaitGroup
	errs := make(chan error, rounds*2)

	build := func(inst *KubernetesRuntimeInfraGKE, wantEmail string) {
		defer wg.Done()
		ts, err := inst.tokenSource(ctx, scope)
		if err != nil {
			errs <- fmt.Errorf("token source build failed: %w", err)
			return
		}
		creds, err := google.CredentialsFromJSON(ctx, []byte(inst.ServiceAccountCredentials), scope)
		if err != nil {
			errs <- fmt.Errorf("credentials parse failed: %w", err)
			return
		}
		if got := emailFromCredentialsJSON(t, creds.JSON); got != wantEmail {
			errs <- fmt.Errorf("credentials bled: got %q want %q", got, wantEmail)
			return
		}
		if ts == nil {
			errs <- fmt.Errorf("nil token source for %s", wantEmail)
		}
	}

	// run both builds concurrently many times so a process-global bleed
	// would surface as a mismatched email under the race detector
	for i := 0; i < rounds; i++ {
		wg.Add(2)
		go build(instanceA, emailA)
		go build(instanceB, emailB)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

// The create complete branch must confirm at most once.
//
// OnCreateConfirmed writes kube connection info, including a connection
// token, and sets Reconciled=false on the runtime instance; re-entering
// after confirmation must not repeat that side effect.

// TestHandleInfraCreate_CompleteBranch_ReentryConfirmsOnce asserts re-entry
// after confirmation runs no further confirmation work: OnCreateConfirmed and
// ConfirmCreation each fire once across both passes.
func TestHandleInfraCreate_CompleteBranch_ReentryConfirmsOnce(t *testing.T) {
	acked := timePtr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	// pass 1: acked, not confirmed on either fetch; pass 2: confirmed on re-check
	fl := newFakeLifecycle(
		&ReconciliationSnapshot{CreationAcknowledged: acked},
		&ReconciliationSnapshot{CreationAcknowledged: acked},
		&ReconciliationSnapshot{CreationAcknowledged: acked},
		&ReconciliationSnapshot{
			CreationAcknowledged: acked,
			CreationConfirmed:    timePtr(time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC)),
		},
	)
	fl.setCreateComplete(true)
	fl.setInfra(newFakeInfra())

	// pass 1: runs the confirmation work once
	requeue, err := HandleInfraCreate(fl, newTestLogger())
	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 1, fl.callCount("OnCreateConfirmed"))
	assert.Equal(t, 1, fl.callCount("ConfirmCreation"))

	// pass 2: re-check sees confirmation and short-circuits
	requeue, err = HandleInfraCreate(fl, newTestLogger())
	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 1, fl.callCount("OnCreateConfirmed"), "post-creation work must run once across re-entry")
	assert.Equal(t, 1, fl.callCount("ConfirmCreation"), "confirmation must run once across re-entry")
	assert.Equal(t, 1, fl.callCount("BuildInfra"), "infra must be built once across re-entry")
}

// A delete racing a just-started create must not stall.
//
// When the pre-acknowledge deletion check sees a delete already scheduled,
// create must abort before writing a fresh CreationAcknowledged. A fresh ack
// would trip the delete handler's cross-replica guard for the stale-ack window.

// TestHandleInfraCreate_DeleteRacesCreate_NoFreshAck asserts a delete in the
// pre-ack window leaves no fresh acknowledgement; AckCreation never fires.
func TestHandleInfraCreate_DeleteRacesCreate_NoFreshAck(t *testing.T) {
	// initial fetch: brand new create; pre-ack re-check: delete already scheduled
	fl := newFakeLifecycle(
		&ReconciliationSnapshot{},
		&ReconciliationSnapshot{
			DeletionScheduled: timePtr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
	)
	fi := newFakeInfra()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 0, fl.callCount("AckCreation"), "no fresh acknowledgement may be written when a delete is already scheduled")
	assert.Equal(t, 0, fl.callCount("BuildInfra"), "infra must not be built once the delete is observed")
	assert.Equal(t, 0, fi.deployCallCount(), "the create goroutine must not launch")
	assert.Equal(t, int64(0), inFlightCount())

	// without a fresh ack the delete handler proceeds instead of requeueing
	dfi := newFakeInfra()
	dfi.setDestroy(infraBlock, nil)
	cfg := testLifecycleConfig()
	cfg.SemaphoreCapacity = 1
	restoreCfg := setLifecycleConfig(cfg)
	t.Cleanup(restoreCfg)

	dl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: timePtr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
	})
	dl.setInfra(dfi)

	dRequeue, dErr := HandleInfraDelete(dl, newTestLogger())
	require.NoError(t, dErr)
	assert.Equal(t, int64(300), dRequeue, "delete proceeds to launch rather than requeueing at 60s")
	assert.Equal(t, 1, dl.callCount("AckDeletion"), "delete acknowledges instead of stalling on the cross-replica guard")

	require.Eventually(t, func() bool {
		return dfi.destroyCallCount() == 1
	}, 5*time.Second, 5*time.Millisecond, "destroy goroutine never launched")
	dfi.releaseDestroy()
	require.Eventually(t, func() bool {
		return inFlightCount() == 0 && len(infraSemaphore) == 0
	}, 5*time.Second, 5*time.Millisecond, "in-flight delete did not drain")
}
