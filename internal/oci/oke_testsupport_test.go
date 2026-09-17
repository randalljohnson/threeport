package oci

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/stretchr/testify/require"

	"github.com/threeport/threeport/internal/machinetest"
	"github.com/threeport/threeport/internal/provider"
	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	encryption "github.com/threeport/threeport/pkg/encryption/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// Package-level test identifiers here are prefixed oke so a sibling _test.go
// can land in this package without collisions. This file defines no TestMain
// for the same reason.

// okeNewAPIStub returns a machinetest API stub under an oke-prefixed name.
func okeNewAPIStub(t *testing.T) *machinetest.APIStub {
	t.Helper()
	return machinetest.NewAPIStub(t)
}

// okeNewEncryptionKey returns a fresh AES-256 key under an oke-prefixed name.
func okeNewEncryptionKey(t *testing.T) string {
	t.Helper()
	return machinetest.NewEncryptionKey(t)
}

// okeWriteResponse writes data in the Response envelope the threeport client expects.
func okeWriteResponse(t *testing.T, w http.ResponseWriter, status int, data []apiserver_lib.Object) {
	t.Helper()
	machinetest.WriteResponse(t, w, status, data)
}

// okeFakeJetStream is a JetStreamContext that records Publish calls.
// Other methods panic on the nil embedded interface, so a lifecycle
// method that touches JetStream beyond Publish fails the test.
type okeFakeJetStream struct {
	nats.JetStreamContext

	mu         sync.Mutex
	subjects   []string
	payloads   [][]byte
	publishErr error
}

// Publish records subj and a copy of data, or returns publishErr when set.
func (f *okeFakeJetStream) Publish(subj string, data []byte, opts ...nats.PubOpt) (*nats.PubAck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// fail the publish when a test injected an error
	if f.publishErr != nil {
		return nil, f.publishErr
	}
	// record subject and payload copy
	f.subjects = append(f.subjects, subj)
	f.payloads = append(f.payloads, append([]byte(nil), data...))
	return &nats.PubAck{}, nil
}

// published returns copies of the recorded subjects and payloads.
func (f *okeFakeJetStream) published() ([]string, [][]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// copy recorded slices for the caller
	subjects := append([]string(nil), f.subjects...)
	payloads := append([][]byte(nil), f.payloads...)
	return subjects, payloads
}

// okeReconciler returns a Reconciler pointed at api, key, and js.
func okeReconciler(api *machinetest.APIStub, key string, js nats.JetStreamContext) *controller.Reconciler {
	return &controller.Reconciler{
		APIClient:        api.Client,
		APIServer:        api.Addr,
		EncryptionKey:    key,
		JetStreamContext: js,
	}
}

// okeInstance returns an OKE runtime instance with ID and Name set.
// Entry points and newOkeLifecycleProvider dereference both.
func okeInstance(id uint, name string) *v0.OciOkeKubernetesRuntimeInstance {
	return &v0.OciOkeKubernetesRuntimeInstance{
		Common:   v0.Common{ID: util.Ptr(id)},
		Instance: v0.Instance{Name: util.Ptr(name)},
	}
}

// okeInstancePath returns the API path for an OKE instance id.
func okeInstancePath(id uint) string {
	return fmt.Sprintf("%s/%d", v0.PathOciOkeKubernetesRuntimeInstances, id)
}

// okeRequestRecorder is a collector of PATCH bodies on the instance path.
// Tests assert which reconciliation fields a lifecycle method persisted.
type okeRequestRecorder struct {
	mu     sync.Mutex
	bodies [][]byte
}

// patchBodies returns recorded PATCH bodies as strings.
func (rec *okeRequestRecorder) patchBodies() []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	// stringify recorded bodies
	bodies := make([]string, len(rec.bodies))
	for i, b := range rec.bodies {
		bodies[i] = string(b)
	}
	return bodies
}

// okeServeInstance serves GET of inst and PATCH that echoes the body with inst.ID.
func okeServeInstance(t *testing.T, api *machinetest.APIStub, inst *v0.OciOkeKubernetesRuntimeInstance) *okeRequestRecorder {
	t.Helper()
	rec := &okeRequestRecorder{}
	// serve GET of inst and PATCH echo at the instance path
	api.Mux.HandleFunc(okeInstancePath(*inst.ID), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			okeWriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{*inst})
		case http.MethodPatch:
			// record body, echo unmarshaled instance with original ID
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			rec.mu.Lock()
			rec.bodies = append(rec.bodies, body)
			rec.mu.Unlock()
			var updated v0.OciOkeKubernetesRuntimeInstance
			require.NoError(t, json.Unmarshal(body, &updated))
			updated.ID = inst.ID
			okeWriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{updated})
		default:
			t.Errorf("unexpected method %s on %s", r.Method, r.URL.Path)
		}
	})
	return rec
}

// okeServeGet serves GET of obj at path and fails on any other method.
func okeServeGet(t *testing.T, api *machinetest.APIStub, path string, obj apiserver_lib.Object) {
	t.Helper()
	// serve GET of obj at path
	api.Mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		okeWriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{obj})
	})
}

// okeServeError serves status with an empty Response envelope at path.
func okeServeError(t *testing.T, api *machinetest.APIStub, path string, status int) {
	t.Helper()
	// serve empty envelope with status
	api.Mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		okeWriteResponse(t, w, status, nil)
	})
}

// okeForbidAPIRequests fails the test on any request that reaches the mux root.
func okeForbidAPIRequests(t *testing.T, api *machinetest.APIStub) {
	t.Helper()
	// fail the test on any request that reaches "/"
	api.Mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API request: %s %s", r.Method, r.URL.Path)
		okeWriteResponse(t, w, http.StatusInternalServerError, nil)
	})
}

// okeLocalFailConfigProvider returns dummy OCI credentials that fail client construction.
// IsConfigurationProviderValid (oci-go-sdk/v65@v65.101.0 common/configuration.go)
// rejects an empty Region() before PrivateRSAKey(); tenancy, user, and fingerprint
// are non-empty so that check is the first failure. not-a-pem-key would fail PEM
// decode if Region were set. Construction never opens a socket.
func okeLocalFailConfigProvider() common.ConfigurationProvider {
	return common.NewRawConfigurationProvider(
		"dummy-tenancy",
		"dummy-user",
		"",
		"dummy-fingerprint",
		"not-a-pem-key",
		nil,
	)
}

// okeLocalFailInfra returns OKE infra whose OCI clients fail at construction.
func okeLocalFailInfra(name string) *provider.KubernetesRuntimeInfraOKE {
	return &provider.KubernetesRuntimeInfraOKE{
		PulumiWorkspace: provider.PulumiWorkspace{RuntimeInstanceName: name},
		Region:          "us-ashburn-1",
		ConfigProvider:  okeLocalFailConfigProvider(),
	}
}

// okeProvider returns an OciProvider whose PrivateKey decrypts to not-a-pem-key.
// buildOkeInfra then fails locally at identity client construction.
func okeProvider(t *testing.T, id uint, encryptionKey string) *v0.OciProvider {
	t.Helper()
	// encrypt dummy PEM so decrypt succeeds and identity-client construction fails
	encryptedKey, err := encryption.Encrypt(encryptionKey, "not-a-pem-key")
	require.NoError(t, err)
	return &v0.OciProvider{
		Common:          v0.Common{ID: util.Ptr(id)},
		Name:            util.Ptr("test-oci-provider"),
		UserOCID:        util.Ptr("dummy-user"),
		TenancyOCID:     util.Ptr("dummy-tenancy"),
		CompartmentOCID: util.Ptr("dummy-compartment"),
		DefaultRegion:   util.Ptr("us-phoenix-1"),
		KeyFingerprint:  util.Ptr("dummy-fingerprint"),
		PrivateKey:      util.Ptr(encryptedKey),
	}
}

// okeDefinition returns an OKE definition with the worker-node fields buildOkeInfra dereferences.
func okeDefinition(id uint) *v0.OciOkeKubernetesRuntimeDefinition {
	return &v0.OciOkeKubernetesRuntimeDefinition{
		Common:                 v0.Common{ID: util.Ptr(id)},
		Definition:             v0.Definition{Name: util.Ptr("test-oke-definition")},
		WorkerNodeShape:        util.Ptr("VM.Standard.E4.Flex"),
		WorkerNodeInitialCount: util.Ptr(int32(2)),
	}
}
