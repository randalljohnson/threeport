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

	util "github.com/threeport/threeport/pkg/util/v0"
)

// GKE credential threading is per instance, never process-global. Two
// concurrent creates for different service accounts authenticate with
// their own JSON; a process-global credential would race across them.

// serviceAccountJSON returns a GCP service account key JSON with
// clientEmail in the client_email field.
func serviceAccountJSON(t *testing.T, clientEmail string) string {
	t.Helper()
	// generate an RSA private key
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	// encode the key as PKCS8 PEM
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type: "PRIVATE KEY",
		Bytes: func() []byte {
			der, err := x509.MarshalPKCS8PrivateKey(key)
			require.NoError(t, err)
			return der
		}(),
	})
	// fill the service account key document
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
	// marshal the document to JSON
	b, err := json.Marshal(creds)
	require.NoError(t, err)
	return string(b)
}

// emailFromCredentialsJSON returns the client_email field from credentials JSON.
func emailFromCredentialsJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var parsed struct {
		ClientEmail string `json:"client_email"`
	}
	require.NoError(t, json.Unmarshal(raw, &parsed))
	return parsed.ClientEmail
}

// TestGKEClientOptions_ThreadsPerInstanceCredentials covers that service
// account JSON threads one credentials option and empty threads none.
func TestGKEClientOptions_ThreadsPerInstanceCredentials(t *testing.T) {
	// build an instance with service account JSON and one without
	withCreds := &KubernetesRuntimeInfraGKE{
		ServiceAccountCredentials: serviceAccountJSON(t, "a@test-project.iam.gserviceaccount.com"),
	}
	withoutCreds := &KubernetesRuntimeInfraGKE{}

	// collect options from the instance with credentials
	base := withCreds.gcpClientOptions()
	assert.Len(t, base, 1, "an instance with credentials threads one credentials option")

	// collect options from the instance without credentials
	none := withoutCreds.gcpClientOptions()
	assert.Empty(t, none, "an instance without credentials threads no credentials option and falls back to ADC")
}

// TestGKETokenSource_ConcurrentCredentialsDoNotBleed covers concurrent token
// source builds from two instances with distinct service account JSON.
func TestGKETokenSource_ConcurrentCredentialsDoNotBleed(t *testing.T) {
	const (
		emailA = "account-a@test-project.iam.gserviceaccount.com"
		emailB = "account-b@test-project.iam.gserviceaccount.com"
	)
	// build two instances with distinct service account emails
	instanceA := &KubernetesRuntimeInfraGKE{ServiceAccountCredentials: serviceAccountJSON(t, emailA)}
	instanceB := &KubernetesRuntimeInfraGKE{ServiceAccountCredentials: serviceAccountJSON(t, emailB)}

	const scope = "https://www.googleapis.com/auth/cloud-platform"
	ctx := context.Background()

	// run both token source builds concurrently
	const rounds = 50
	var wg sync.WaitGroup
	errs := make(chan error, rounds*2)

	build := func(inst *KubernetesRuntimeInfraGKE, wantEmail string) {
		defer wg.Done()
		// build the instance token source
		ts, err := inst.tokenSource(ctx, scope)
		if err != nil {
			errs <- fmt.Errorf("token source build failed: %w", err)
			return
		}
		// parse the instance JSON independently
		creds, err := google.CredentialsFromJSON(ctx, []byte(inst.ServiceAccountCredentials), scope)
		if err != nil {
			errs <- fmt.Errorf("credentials parse failed: %w", err)
			return
		}
		// reject a client_email that does not match this instance
		if got := emailFromCredentialsJSON(t, creds.JSON); got != wantEmail {
			errs <- fmt.Errorf("credentials bled: got %q want %q", got, wantEmail)
			return
		}
		// reject a nil token source
		if ts == nil {
			errs <- fmt.Errorf("nil token source for %s", wantEmail)
		}
	}

	// launch one paired build per round
	for i := 0; i < rounds; i++ {
		wg.Add(2)
		go build(instanceA, emailA)
		go build(instanceB, emailB)
	}
	// wait for every build then drain errors
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

// Re-entry after CreationConfirmed is set skips post-creation work and
// confirmation. Post-creation work writes a connection token and sets
// the runtime instance unreconciled.

// TestHandleInfraCreate_CompleteBranch_ReentryConfirmsOnce covers
// complete-create re-entry running post-creation work and confirmation once.
func TestHandleInfraCreate_CompleteBranch_ReentryConfirmsOnce(t *testing.T) {
	acked := util.Ptr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	fl := newFakeLifecycle(
		// pass 1 initial fetch: acked, not confirmed
		&ReconciliationSnapshot{CreationAcknowledged: acked},
		// pass 1 pre-confirm re-check: still not confirmed, do the work
		&ReconciliationSnapshot{CreationAcknowledged: acked},
		// pass 2 initial fetch: still unconfirmed at the top-level guard
		&ReconciliationSnapshot{CreationAcknowledged: acked},
		// pass 2 pre-confirm re-check: confirmation visible, skip the work
		&ReconciliationSnapshot{
			CreationAcknowledged: acked,
			CreationConfirmed:    util.Ptr(time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC)),
		},
	)
	fl.setCreateComplete(true)
	fl.setInfra(newFakeInfra())

	// run the complete-create path once
	requeue, err := HandleInfraCreate(fl, newTestLogger())
	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 1, fl.callCount("OnCreateConfirmed"))
	assert.Equal(t, 1, fl.callCount("ConfirmCreation"))

	// re-enter: re-check sees confirmation and skips a second token write
	requeue, err = HandleInfraCreate(fl, newTestLogger())
	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 1, fl.callCount("OnCreateConfirmed"), "post-creation work must run once across re-entry")
	assert.Equal(t, 1, fl.callCount("ConfirmCreation"), "confirmation must run once across re-entry")
	assert.Equal(t, 1, fl.callCount("BuildInfra"), "infra must be built once across re-entry")
}

// A delete racing a create that has not yet acknowledged must not stall.
// Create aborts before writing a fresh acknowledgement; a fresh ack would
// trip the delete cross-replica guard and requeue every 60s until stale.

// TestHandleInfraCreate_DeleteRacesCreate_NoFreshAck covers a create that
// observes a scheduled delete and writes no ack, so delete can launch.
func TestHandleInfraCreate_DeleteRacesCreate_NoFreshAck(t *testing.T) {
	fl := newFakeLifecycle(
		// initial fetch: new create, nothing acked
		&ReconciliationSnapshot{},
		// pre-acknowledge re-check: a delete landed in the race window
		&ReconciliationSnapshot{
			DeletionScheduled: util.Ptr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
	)
	fi := newFakeInfra()
	fl.setInfra(fi)

	// run create against the scheduled delete
	requeue, err := HandleInfraCreate(fl, newTestLogger())

	// create must abort without ack, build, or a deploy goroutine
	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 0, fl.callCount("AckCreation"), "no fresh acknowledgement may be written when a delete is already scheduled")
	assert.Equal(t, 0, fl.callCount("BuildInfra"), "infra must not be built once the delete is observed")
	assert.Equal(t, 0, fi.deployCallCount(), "the create goroutine must not launch")
	assert.Equal(t, int64(0), inFlightCount())

	// with no create ack, delete acknowledges and launches instead of requeueing
	dfi := newFakeInfra()
	dfi.setDestroy(infraBlock, nil)
	cfg := testLifecycleConfig()
	cfg.SemaphoreCapacity = 1
	restoreCfg := setLifecycleConfig(cfg)
	t.Cleanup(restoreCfg)

	// run delete with only DeletionScheduled set, no create ack
	dl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: util.Ptr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
	})
	dl.setInfra(dfi)

	dRequeue, dErr := HandleInfraDelete(dl, newTestLogger())
	// delete launches instead of requeueing on the cross-replica guard
	require.NoError(t, dErr)
	assert.Equal(t, int64(300), dRequeue, "delete proceeds to launch rather than requeueing at 60s")
	assert.Equal(t, 1, dl.callCount("AckDeletion"), "delete acknowledges instead of stalling on the cross-replica guard")

	// wait for the destroy goroutine, then drain it
	require.Eventually(t, func() bool {
		return dfi.destroyCallCount() == 1
	}, 5*time.Second, 5*time.Millisecond, "destroy goroutine never launched")
	dfi.releaseDestroy()
	require.Eventually(t, func() bool {
		return inFlightCount() == 0 && len(infraSemaphore) == 0
	}, 5*time.Second, 5*time.Millisecond, "in-flight delete did not drain")
}
