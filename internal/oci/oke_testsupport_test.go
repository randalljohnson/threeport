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

// All package-level test identifiers in this file are prefixed oke/oci so a
// sibling worktree can later add _test.go files to this package without
// collisions. No TestMain is defined here for the same reason.

// okeNewAPIStub returns the shared machinetest API stub under an
// oke-prefixed name.
func okeNewAPIStub(t *testing.T) *machinetest.APIStub {
	t.Helper()
	return machinetest.NewAPIStub(t)
}

// okeNewEncryptionKey returns a fresh AES-256 key under an oke-prefixed name.
func okeNewEncryptionKey(t *testing.T) string {
	t.Helper()
	return machinetest.NewEncryptionKey(t)
}

// okeWriteResponse writes data in the response envelope the threeport client
// helpers expect.
func okeWriteResponse(t *testing.T, w http.ResponseWriter, status int, data []apiserver_lib.Object) {
	t.Helper()
	machinetest.WriteResponse(t, w, status, data)
}

// okeFakeJetStream satisfies nats.JetStreamContext by embedding the
// interface and overriding only Publish(). Any other method panics on the
// nil embedded interface, which is desirable: the lifecycle methods under
// test must never touch JetStream beyond publishing.
type okeFakeJetStream struct {
	nats.JetStreamContext

	mu         sync.Mutex
	subjects   []string
	payloads   [][]byte
	publishErr error
}

func (f *okeFakeJetStream) Publish(subj string, data []byte, opts ...nats.PubOpt) (*nats.PubAck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.publishErr != nil {
		return nil, f.publishErr
	}
	f.subjects = append(f.subjects, subj)
	f.payloads = append(f.payloads, append([]byte(nil), data...))
	return &nats.PubAck{}, nil
}

// published returns copies of the recorded subjects and payloads.
func (f *okeFakeJetStream) published() ([]string, [][]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	subjects := append([]string(nil), f.subjects...)
	payloads := append([][]byte(nil), f.payloads...)
	return subjects, payloads
}

// okeReconciler builds a controller.Reconciler pointed at the API stub.
func okeReconciler(api *machinetest.APIStub, key string, js nats.JetStreamContext) *controller.Reconciler {
	return &controller.Reconciler{
		APIClient:        api.Client,
		APIServer:        api.Addr,
		EncryptionKey:    key,
		JetStreamContext: js,
	}
}

// okeInstance builds an OKE runtime instance with ID and Name set. Both are
// dereferenced by the entry points and the lifecycle constructor, so every
// fixture must carry them. Tests set additional fields directly.
func okeInstance(id uint, name string) *v0.OciOkeKubernetesRuntimeInstance {
	return &v0.OciOkeKubernetesRuntimeInstance{
		Common:   v0.Common{ID: util.Ptr(id)},
		Instance: v0.Instance{Name: util.Ptr(name)},
	}
}

// okeInstancePath returns the API path for an OKE runtime instance ID.
func okeInstancePath(id uint) string {
	return fmt.Sprintf("%s/%d", v0.PathOciOkeKubernetesRuntimeInstances, id)
}

// okeRequestRecorder captures PATCH bodies sent to the API stub so tests can
// assert which reconciliation fields a lifecycle method persisted.
type okeRequestRecorder struct {
	mu     sync.Mutex
	bodies [][]byte
}

// patchBodies returns the recorded PATCH bodies as strings.
func (rec *okeRequestRecorder) patchBodies() []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	bodies := make([]string, len(rec.bodies))
	for i, b := range rec.bodies {
		bodies[i] = string(b)
	}
	return bodies
}

// okeServeInstance registers a handler at the instance's path that serves
// the instance on GET and echoes the patched object on PATCH, recording each
// PATCH body for assertion.
func okeServeInstance(t *testing.T, api *machinetest.APIStub, inst *v0.OciOkeKubernetesRuntimeInstance) *okeRequestRecorder {
	t.Helper()
	rec := &okeRequestRecorder{}
	api.Mux.HandleFunc(okeInstancePath(*inst.ID), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			okeWriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{*inst})
		case http.MethodPatch:
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

// okeServeGet registers a GET-only handler serving obj at path.
func okeServeGet(t *testing.T, api *machinetest.APIStub, path string, obj apiserver_lib.Object) {
	t.Helper()
	api.Mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		okeWriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{obj})
	})
}

// okeServeError registers a handler returning the given status with an empty
// response envelope so the client error path parses cleanly.
func okeServeError(t *testing.T, api *machinetest.APIStub, path string, status int) {
	t.Helper()
	api.Mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		okeWriteResponse(t, w, status, nil)
	})
}

// okeForbidAPIRequests registers a catch-all handler that fails the test if
// any API request arrives. OCI-boundary tests use it to prove the method
// under test returns at the local failure before issuing any API call.
func okeForbidAPIRequests(t *testing.T, api *machinetest.APIStub) {
	t.Helper()
	api.Mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API request: %s %s", r.Method, r.URL.Path)
		okeWriteResponse(t, w, http.StatusInternalServerError, nil)
	})
}

// okeLocalFailConfigProvider returns a config provider that every OCI client
// constructor rejects LOCALLY: the empty region fails the SDK's
// configuration-validity region check and the non-PEM key fails its PEM
// decode check, both before any socket is opened. Tenancy, user, and
// fingerprint are non-empty dummies so the forced failure lands
// deterministically on the region/PEM checks rather than an earlier
// empty-field error. Every test that touches a concrete OCI method MUST use
// this provider: the SDK calls run with context.Background() and no timeout,
// so a syntactically valid config would dial the real OCI endpoint and hang.
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

// okeLocalFailInfra builds an OKE infra whose concrete OCI methods all fail
// locally at client construction, guaranteeing no network dial.
func okeLocalFailInfra(name string) *provider.KubernetesRuntimeInfraOKE {
	return &provider.KubernetesRuntimeInfraOKE{
		PulumiWorkspace: provider.PulumiWorkspace{RuntimeInstanceName: name},
		Region:          "us-ashburn-1",
		ConfigProvider:  okeLocalFailConfigProvider(),
	}
}

// okeProvider builds an OCI provider record whose PrivateKey decrypts
// successfully but yields a non-PEM plaintext, so the infra build passes the
// decrypt phase and then fails LOCALLY at OCI identity client construction
// without reaching the network.
func okeProvider(t *testing.T, id uint, encryptionKey string) *v0.OciProvider {
	t.Helper()
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

// okeDefinition builds an OKE definition carrying the worker-node fields the
// infra build dereferences.
func okeDefinition(id uint) *v0.OciOkeKubernetesRuntimeDefinition {
	return &v0.OciOkeKubernetesRuntimeDefinition{
		Common:                 v0.Common{ID: util.Ptr(id)},
		Definition:             v0.Definition{Name: util.Ptr("test-oke-definition")},
		WorkerNodeShape:        util.Ptr("VM.Standard.E4.Flex"),
		WorkerNodeInitialCount: util.Ptr(int32(2)),
	}
}
