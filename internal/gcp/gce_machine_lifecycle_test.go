package gcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	logr "github.com/go-logr/logr"
	nats "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	"github.com/threeport/threeport/internal/provider"
	machine "github.com/threeport/threeport/internal/provider/machine"
	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	encryption "github.com/threeport/threeport/pkg/encryption/v0"
)

// gceTestInstanceID is the default API ID used by single-instance test cases.
const gceTestInstanceID uint = 42

// gceTestInstanceName is the default name used by single-instance test cases.
const gceTestInstanceName = "gce-test-instance"

// gceTestProviderID is the GCP provider ID referenced by test instances.
const gceTestProviderID uint = 7

// gceTestDefinitionID is the GCE definition ID referenced by test instances.
const gceTestDefinitionID uint = 11

// gceTestMachineRuntimeInstanceID is the married machine runtime instance ID
// referenced by test instances.
const gceTestMachineRuntimeInstanceID uint = 19

// gceAPIStub is an httptest.Server plus the client and address threeport
// client helpers expect. PATCH bodies are recorded keyed by path.
type gceAPIStub struct {
	server        *httptest.Server
	mux           *http.ServeMux
	client        *http.Client
	addr          string
	mu            sync.Mutex
	patches       map[string][][]byte
	encryptionKey string
}

// gceNewAPIStub returns a gceAPIStub with an empty mux. Addr drops the
// http:// scheme because the threeport client prepends one.
func gceNewAPIStub(t *testing.T) *gceAPIStub {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	key, err := encryption.GenerateKey()
	require.NoError(t, err)
	return &gceAPIStub{
		server:        srv,
		mux:           mux,
		client:        &http.Client{},
		addr:          strings.TrimPrefix(srv.URL, "http://"),
		patches:       make(map[string][][]byte),
		encryptionKey: key,
	}
}

// gceReconciler builds a controller.Reconciler pointed at the stub.
func (s *gceAPIStub) gceReconciler() *controller.Reconciler {
	return &controller.Reconciler{
		APIClient:     s.client,
		APIServer:     s.addr,
		EncryptionKey: s.encryptionKey,
	}
}

// gceRecordPatch appends a captured PATCH body under path.
func (s *gceAPIStub) gceRecordPatch(path string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.patches[path] = append(s.patches[path], body)
}

// gceLastPatch returns the most recently captured PATCH body for the path.
func (s *gceAPIStub) gceLastPatch(t *testing.T, path string) v0.GcpGceMachineRuntimeInstance {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	bodies := s.patches[path]
	require.NotEmpty(t, bodies, "expected at least one PATCH to %s", path)
	var updated v0.GcpGceMachineRuntimeInstance
	require.NoError(t, json.Unmarshal(bodies[len(bodies)-1], &updated))
	return updated
}

// gceWriteResponse marshals data into an apiserver_lib.Response envelope and
// writes it with status.
func gceWriteResponse(t *testing.T, w http.ResponseWriter, status int, data []apiserver_lib.Object) {
	t.Helper()
	body, err := json.Marshal(apiserver_lib.Response{Data: data})
	require.NoError(t, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// gceInstancePath returns the GET/PATCH path for an instance ID.
func gceInstancePath(id uint) string {
	return fmt.Sprintf("%s/%d", v0.PathGcpGceMachineRuntimeInstances, id)
}

// gceProviderPath returns the GET path for a GCP provider ID.
func gceProviderPath(id uint) string {
	return fmt.Sprintf("%s/%d", v0.PathGcpProviders, id)
}

// gceHandleInstance registers a handler that returns get on GET and records
// the body on PATCH.
func (s *gceAPIStub) gceHandleInstance(t *testing.T, id uint, get *v0.GcpGceMachineRuntimeInstance) {
	t.Helper()
	path := gceInstancePath(id)
	s.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			gceWriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{get})
		case http.MethodPatch:
			body, _ := io.ReadAll(r.Body)
			s.gceRecordPatch(path, body)
			var updated v0.GcpGceMachineRuntimeInstance
			require.NoError(t, json.Unmarshal(body, &updated))
			updated.ID = gcePtr(id)
			gceWriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{&updated})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

// gceHandleInstance500 registers an instance handler that always returns 500.
func (s *gceAPIStub) gceHandleInstance500(t *testing.T, id uint) {
	t.Helper()
	s.mux.HandleFunc(gceInstancePath(id), func(w http.ResponseWriter, r *http.Request) {
		gceWriteResponse(t, w, http.StatusInternalServerError, []apiserver_lib.Object{})
	})
}

// gceHandleProvider registers a GET handler for a GCP provider path.
func (s *gceAPIStub) gceHandleProvider(t *testing.T, id uint, provider *v0.GcpProvider) {
	t.Helper()
	s.mux.HandleFunc(gceProviderPath(id), func(w http.ResponseWriter, r *http.Request) {
		gceWriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{provider})
	})
}

// gceHandleProvider500 registers a GCP provider handler that returns 500.
func (s *gceAPIStub) gceHandleProvider500(t *testing.T, id uint) {
	t.Helper()
	s.mux.HandleFunc(gceProviderPath(id), func(w http.ResponseWriter, r *http.Request) {
		gceWriteResponse(t, w, http.StatusInternalServerError, []apiserver_lib.Object{})
	})
}

// gcePtr returns a pointer to its argument.
func gcePtr[T any](v T) *T {
	return &v
}

// gceBaseInstance returns a minimal valid instance referencing the test
// provider and definition.
func gceBaseInstance(id uint, name string) *v0.GcpGceMachineRuntimeInstance {
	return &v0.GcpGceMachineRuntimeInstance{
		Common:                           v0.Common{ID: gcePtr(id)},
		Instance:                         v0.Instance{Name: gcePtr(name)},
		GcpProviderID:                    gcePtr(gceTestProviderID),
		GcpGceMachineRuntimeDefinitionID: gcePtr(gceTestDefinitionID),
	}
}

// gceBaseDefinition returns a GCE definition carrying the fields
// buildGceMachineInfra copies onto infra.
func gceBaseDefinition() *v0.GcpGceMachineRuntimeDefinition {
	return &v0.GcpGceMachineRuntimeDefinition{
		Common:      v0.Common{ID: gcePtr(gceTestDefinitionID)},
		Definition:  v0.Definition{Name: gcePtr("gce-test-definition")},
		MachineType: gcePtr("e2-medium"),
		ImageID:     gcePtr("debian-12"),
	}
}

// gceDefinitionPath returns the GET path for a GCE definition ID.
func gceDefinitionPath(id uint) string {
	return fmt.Sprintf("%s/%d", v0.PathGcpGceMachineRuntimeDefinitions, id)
}

// gceHandleDefinition registers a GET handler for a GCE definition path.
func (s *gceAPIStub) gceHandleDefinition(t *testing.T, id uint, def *v0.GcpGceMachineRuntimeDefinition) {
	t.Helper()
	s.mux.HandleFunc(gceDefinitionPath(id), func(w http.ResponseWriter, r *http.Request) {
		gceWriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{def})
	})
}

// gceHandleDefinition500 registers a GCE definition handler that returns 500.
func (s *gceAPIStub) gceHandleDefinition500(t *testing.T, id uint) {
	t.Helper()
	s.mux.HandleFunc(gceDefinitionPath(id), func(w http.ResponseWriter, r *http.Request) {
		gceWriteResponse(t, w, http.StatusInternalServerError, []apiserver_lib.Object{})
	})
}

// gceMachineRuntimeInstancePath returns the GET/PATCH path for a machine
// runtime instance ID.
func gceMachineRuntimeInstancePath(id uint) string {
	return fmt.Sprintf("%s/%d", v0.PathMachineRuntimeInstances, id)
}

// gceHandleMachineRuntimeInstance registers a handler that returns get on GET
// and records the body on PATCH.
func (s *gceAPIStub) gceHandleMachineRuntimeInstance(t *testing.T, id uint, get *v0.MachineRuntimeInstance) {
	t.Helper()
	path := gceMachineRuntimeInstancePath(id)
	s.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			gceWriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{get})
		case http.MethodPatch:
			body, _ := io.ReadAll(r.Body)
			s.gceRecordPatch(path, body)
			var updated v0.MachineRuntimeInstance
			require.NoError(t, json.Unmarshal(body, &updated))
			updated.ID = gcePtr(id)
			gceWriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{&updated})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

// gceLastMachineRuntimeInstancePatch returns the most recently captured PATCH
// body for the machine runtime instance path.
func (s *gceAPIStub) gceLastMachineRuntimeInstancePatch(t *testing.T, path string) v0.MachineRuntimeInstance {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	bodies := s.patches[path]
	require.NotEmpty(t, bodies, "expected at least one PATCH to %s", path)
	var updated v0.MachineRuntimeInstance
	require.NoError(t, json.Unmarshal(bodies[len(bodies)-1], &updated))
	return updated
}

// gceBaseMachineRuntimeInstance returns a married machine runtime instance.
func gceBaseMachineRuntimeInstance(id uint, name string) *v0.MachineRuntimeInstance {
	return &v0.MachineRuntimeInstance{
		Common:   v0.Common{ID: gcePtr(id)},
		Instance: v0.Instance{Name: gcePtr(name)},
	}
}

// gceBaseProvider returns a GCP provider with project ID set.
func gceBaseProvider() *v0.GcpProvider {
	return &v0.GcpProvider{
		Common:                    v0.Common{ID: gcePtr(gceTestProviderID)},
		Name:                      gcePtr("test-provider"),
		ProjectID:                 gcePtr("test-project"),
		ServiceAccountCredentials: nil,
	}
}

// gceFakeInfra is a provider.InfraProvider that is not *machine.GceMachineInfra.
type gceFakeInfra struct{}

func (gceFakeInfra) DeployInfra() error                      { return nil }
func (gceFakeInfra) DestroyInfra() error                     { return nil }
func (gceFakeInfra) SetStackState(_ *datatypes.JSON) error   { return nil }
func (gceFakeInfra) GetStackState() (*datatypes.JSON, error) { return nil, nil }

var _ provider.InfraProvider = gceFakeInfra{}

// gceFakeJetStream is a nats.JetStreamContext that records Publish subjects
// and returns a configured error. Other methods on the embedded nil must not be called.
type gceFakeJetStream struct {
	nats.JetStreamContext
	err      error
	subjects []string
}

// Publish records the subject and returns the configured error.
func (f *gceFakeJetStream) Publish(subj string, _ []byte, _ ...nats.PubOpt) (*nats.PubAck, error) {
	f.subjects = append(f.subjects, subj)
	if f.err != nil {
		return nil, f.err
	}
	return &nats.PubAck{}, nil
}

// gceNewLifecycle constructs the adapter against the stub for a given instance.
func gceNewLifecycle(s *gceAPIStub, instance *v0.GcpGceMachineRuntimeInstance) *gceMachineLifecycle {
	log := logr.Discard()
	return newGceMachineLifecycleProvider(s.gceReconciler(), instance, &log)
}

// TestGceLifecycleGetReconciliation covers GetReconciliation snapshot mapping,
// a nil CreationFailed as false, and a wrapped GET 500.
func TestGceLifecycleGetReconciliation(t *testing.T) {
	t.Run("happy mapping", func(t *testing.T) {
		s := gceNewAPIStub(t)
		now := time.Now().UTC()
		inventory := datatypes.JSON([]byte(`{"a":1}`))
		latest := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
		latest.CreationAcknowledged = gcePtr(now)
		latest.CreationConfirmed = gcePtr(now)
		latest.CreationFailed = gcePtr(true)
		latest.DeletionScheduled = gcePtr(now)
		latest.DeletionAcknowledged = gcePtr(now)
		latest.DeletionConfirmed = gcePtr(now)
		latest.ResourceInventory = &inventory
		s.gceHandleInstance(t, gceTestInstanceID, latest)

		g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
		snap, err := g.GetReconciliation()
		require.NoError(t, err)
		assert.True(t, snap.CreationFailed)
		assert.NotNil(t, snap.CreationAcknowledged)
		assert.NotNil(t, snap.CreationConfirmed)
		assert.NotNil(t, snap.DeletionScheduled)
		assert.NotNil(t, snap.DeletionAcknowledged)
		assert.NotNil(t, snap.DeletionConfirmed)
		require.NotNil(t, snap.ResourceInventory)
		assert.JSONEq(t, `{"a":1}`, string(*snap.ResourceInventory))
	})

	t.Run("nil CreationFailed reads false", func(t *testing.T) {
		s := gceNewAPIStub(t)
		latest := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
		latest.CreationFailed = nil
		s.gceHandleInstance(t, gceTestInstanceID, latest)

		g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
		snap, err := g.GetReconciliation()
		require.NoError(t, err)
		assert.False(t, snap.CreationFailed)
	})

	t.Run("GET 500 wraps error", func(t *testing.T) {
		s := gceNewAPIStub(t)
		s.gceHandleInstance500(t, gceTestInstanceID)

		g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
		_, err := g.GetReconciliation()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get latest GCE instance")
	})
}

// TestGceLifecycleBuildInfra covers BuildInfra field copy, decrypt, and GET
// failures for instance, provider, and definition.
func TestGceLifecycleBuildInfra(t *testing.T) {
	t.Run("happy path populates fields", func(t *testing.T) {
		s := gceNewAPIStub(t)
		latest := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
		latest.Region = gcePtr("us-central1")
		latest.Zone = gcePtr("us-central1-a")
		latest.NetworkID = gcePtr("default")
		latest.SSHUser = gcePtr("threeport")
		latest.SSHSourceRanges = gcePtr([]string{"10.0.0.0/8", "192.168.0.0/16"})
		s.gceHandleInstance(t, gceTestInstanceID, latest)
		prov := gceBaseProvider()
		enc, err := encryption.Encrypt(s.encryptionKey, "creds-json")
		require.NoError(t, err)
		prov.ServiceAccountCredentials = gcePtr(enc)
		s.gceHandleProvider(t, gceTestProviderID, prov)
		s.gceHandleDefinition(t, gceTestDefinitionID, gceBaseDefinition())

		g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
		infra, err := g.BuildInfra()
		require.NoError(t, err)
		gceInfra, ok := infra.(*machine.GceMachineInfra)
		require.True(t, ok)
		assert.Equal(t, "test-project", gceInfra.ProjectID)
		assert.Equal(t, "us-central1", gceInfra.Region)
		assert.Equal(t, "us-central1-a", gceInfra.Zone)
		assert.Equal(t, "e2-medium", gceInfra.MachineType)
		assert.Equal(t, "debian-12", gceInfra.ImageID)
		assert.Equal(t, "default", gceInfra.NetworkID)
		assert.Equal(t, "threeport", gceInfra.SSHUser)
		assert.Equal(t, []string{"10.0.0.0/8", "192.168.0.0/16"}, gceInfra.SSHSourceRanges)
		assert.Equal(t, "creds-json", gceInfra.ServiceAccountCredentials)
	})

	t.Run("instance GET fails", func(t *testing.T) {
		s := gceNewAPIStub(t)
		s.gceHandleInstance500(t, gceTestInstanceID)

		g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
		_, err := g.BuildInfra()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get GCE instance for infra build")
	})

	t.Run("provider GET fails", func(t *testing.T) {
		s := gceNewAPIStub(t)
		s.gceHandleInstance(t, gceTestInstanceID, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
		s.gceHandleProvider500(t, gceTestProviderID)

		g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
		_, err := g.BuildInfra()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to retrieve GCP provider by ID")
	})

	t.Run("empty service account credentials left empty", func(t *testing.T) {
		s := gceNewAPIStub(t)
		s.gceHandleInstance(t, gceTestInstanceID, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
		prov := gceBaseProvider()
		prov.ServiceAccountCredentials = gcePtr("")
		s.gceHandleProvider(t, gceTestProviderID, prov)
		s.gceHandleDefinition(t, gceTestDefinitionID, gceBaseDefinition())

		g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
		infra, err := g.BuildInfra()
		require.NoError(t, err)
		gceInfra := infra.(*machine.GceMachineInfra)
		assert.Equal(t, "", gceInfra.ServiceAccountCredentials)
	})

	t.Run("nil SSHSourceRanges left empty", func(t *testing.T) {
		s := gceNewAPIStub(t)
		latest := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
		latest.SSHSourceRanges = nil
		s.gceHandleInstance(t, gceTestInstanceID, latest)
		s.gceHandleProvider(t, gceTestProviderID, gceBaseProvider())
		s.gceHandleDefinition(t, gceTestDefinitionID, gceBaseDefinition())

		g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
		infra, err := g.BuildInfra()
		require.NoError(t, err)
		gceInfra := infra.(*machine.GceMachineInfra)
		assert.Nil(t, gceInfra.SSHSourceRanges)
	})

	t.Run("definition GET fails", func(t *testing.T) {
		s := gceNewAPIStub(t)
		s.gceHandleInstance(t, gceTestInstanceID, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
		s.gceHandleProvider(t, gceTestProviderID, gceBaseProvider())
		s.gceHandleDefinition500(t, gceTestDefinitionID)

		g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
		_, err := g.BuildInfra()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to retrieve GCE machine runtime definition by ID")
	})
}

// TestGceBuildInfraNilRequiredFields covers buildGceMachineInfra errors for
// nil GcpProviderID, ProjectID, and GcpGceMachineRuntimeDefinitionID.
func TestGceBuildInfraNilRequiredFields(t *testing.T) {
	t.Run("nil GcpProviderID returns clean error", func(t *testing.T) {
		s := gceNewAPIStub(t)
		instance := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
		instance.GcpProviderID = nil

		_, err := buildGceMachineInfra(s.gceReconciler(), instance, gceDiscardLog())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GcpProviderID")
	})

	t.Run("nil provider ProjectID returns clean error", func(t *testing.T) {
		s := gceNewAPIStub(t)
		prov := gceBaseProvider()
		prov.ProjectID = nil
		s.gceHandleProvider(t, gceTestProviderID, prov)

		_, err := buildGceMachineInfra(s.gceReconciler(), gceBaseInstance(gceTestInstanceID, gceTestInstanceName), gceDiscardLog())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ProjectID")
	})

	t.Run("nil GcpGceMachineRuntimeDefinitionID returns clean error", func(t *testing.T) {
		s := gceNewAPIStub(t)
		s.gceHandleProvider(t, gceTestProviderID, gceBaseProvider())
		instance := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
		instance.GcpGceMachineRuntimeDefinitionID = nil

		_, err := buildGceMachineInfra(s.gceReconciler(), instance, gceDiscardLog())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GcpGceMachineRuntimeDefinitionID")
	})
}

// gceDiscardLog returns a discard logger pointer for direct builder calls.
func gceDiscardLog() *logr.Logger {
	log := logr.Discard()
	return &log
}

// TestGceLifecycleIsCreateComplete covers IsCreateComplete inventory cases
// and a wrapped GET error.
func TestGceLifecycleIsCreateComplete(t *testing.T) {
	// inventory bytes must be valid JSON: the stub marshals them through
	// datatypes.JSON, which refuses invalid bytes
	cases := []struct {
		name      string
		inventory *datatypes.JSON
		want      bool
	}{
		{"nil inventory", nil, false},
		{"empty object", gceJSON("{}"), false},
		{"json null literal", gceJSON("null"), false},
		{"populated object", gceJSON(`{"a":1}`), true},
		{"padded empty object", gceJSON(" {} "), false},
		{"tabbed empty object", gceJSON("\t{}\n"), false},
		{"empty array", gceJSON("[]"), false},
		{"quoted null", gceJSON(`"null"`), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := gceNewAPIStub(t)
			latest := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
			latest.ResourceInventory = tc.inventory
			s.gceHandleInstance(t, gceTestInstanceID, latest)

			g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
			got, err := g.IsCreateComplete()
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("GET error", func(t *testing.T) {
		s := gceNewAPIStub(t)
		s.gceHandleInstance500(t, gceTestInstanceID)

		g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
		_, err := g.IsCreateComplete()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to check GCE creation status")
	})
}

// gceJSON returns a pointer to a datatypes.JSON from s.
func gceJSON(s string) *datatypes.JSON {
	j := datatypes.JSON([]byte(s))
	return &j
}

// TestGceSaveCreateOutputsWritesHostnameIPKey covers SaveCreateOutputs writing
// hostname, external IP, SSH key, and inventory onto the instance PATCH.
func TestGceSaveCreateOutputsWritesHostnameIPKey(t *testing.T) {
	s := gceNewAPIStub(t)
	s.gceHandleInstance(t, gceTestInstanceID, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))

	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	gceInfra := machine.NewGceMachineInfra(gceTestInstanceName)
	gceInfra.SetCreateOutputs("vm.example", "203.0.113.7", "PRIVATE-KEY")
	state := datatypes.JSON([]byte(`{"checkpoint":{}}`))

	require.NoError(t, g.SaveCreateOutputs(gceInfra, &state))

	patch := s.gceLastPatch(t, gceInstancePath(gceTestInstanceID))
	require.NotNil(t, patch.Hostname)
	assert.Equal(t, "vm.example", *patch.Hostname)
	require.NotNil(t, patch.ExternalIP)
	assert.Equal(t, "203.0.113.7", *patch.ExternalIP)
	require.NotNil(t, patch.SSHKey)
	assert.Equal(t, "PRIVATE-KEY", *patch.SSHKey)
	require.NotNil(t, patch.ResourceInventory)
	assert.JSONEq(t, `{"checkpoint":{}}`, string(*patch.ResourceInventory))
}

// TestGceSaveCreateOutputsUpdateErrorWraps covers SaveCreateOutputs wrapping a
// PATCH 500.
func TestGceSaveCreateOutputsUpdateErrorWraps(t *testing.T) {
	s := gceNewAPIStub(t)
	s.gceHandleInstance500(t, gceTestInstanceID)

	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	gceInfra := machine.NewGceMachineInfra(gceTestInstanceName)
	gceInfra.SetCreateOutputs("h", "ip", "k")
	state := datatypes.JSON([]byte(`{}`))

	err := g.SaveCreateOutputs(gceInfra, &state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update GCE instance with create outputs")
}

// TestGceSaveCreateOutputsWrongConcreteTypeReturnsError covers SaveCreateOutputs
// rejecting a provider.InfraProvider that is not *machine.GceMachineInfra.
func TestGceSaveCreateOutputsWrongConcreteTypeReturnsError(t *testing.T) {
	s := gceNewAPIStub(t)
	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	state := datatypes.JSON([]byte(`{}`))

	err := g.SaveCreateOutputs(gceFakeInfra{}, &state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected *machine.GceMachineInfra")
}

// TestGceLifecycleAckCreation covers AckCreation setting CreationAcknowledged
// and clearing CreationFailed, and a PATCH error.
func TestGceLifecycleAckCreation(t *testing.T) {
	t.Run("sets acknowledged and clears failed", func(t *testing.T) {
		s := gceNewAPIStub(t)
		s.gceHandleInstance(t, gceTestInstanceID, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))

		g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
		require.NoError(t, g.AckCreation())

		patch := s.gceLastPatch(t, gceInstancePath(gceTestInstanceID))
		require.NotNil(t, patch.CreationAcknowledged)
		assert.WithinDuration(t, time.Now().UTC(), *patch.CreationAcknowledged, time.Minute)
		require.NotNil(t, patch.CreationFailed)
		assert.False(t, *patch.CreationFailed)
	})

	t.Run("PATCH error propagates", func(t *testing.T) {
		s := gceNewAPIStub(t)
		s.gceHandleInstance500(t, gceTestInstanceID)

		g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
		require.Error(t, g.AckCreation())
	})
}

// TestGceLifecycleRefreshCreationAck covers RefreshCreationAck writing
// CreationAcknowledged and a PATCH error.
func TestGceLifecycleRefreshCreationAck(t *testing.T) {
	s := gceNewAPIStub(t)
	s.gceHandleInstance(t, gceTestInstanceID, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))

	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.NoError(t, g.RefreshCreationAck())
	patch := s.gceLastPatch(t, gceInstancePath(gceTestInstanceID))
	require.NotNil(t, patch.CreationAcknowledged)

	s500 := gceNewAPIStub(t)
	s500.gceHandleInstance500(t, gceTestInstanceID)
	g500 := gceNewLifecycle(s500, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.Error(t, g500.RefreshCreationAck())
}

// TestGceLifecycleSetCreationFailed covers SetCreationFailed writing
// CreationFailed true and a PATCH error.
func TestGceLifecycleSetCreationFailed(t *testing.T) {
	s := gceNewAPIStub(t)
	s.gceHandleInstance(t, gceTestInstanceID, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))

	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.NoError(t, g.SetCreationFailed())
	patch := s.gceLastPatch(t, gceInstancePath(gceTestInstanceID))
	require.NotNil(t, patch.CreationFailed)
	assert.True(t, *patch.CreationFailed)

	s500 := gceNewAPIStub(t)
	s500.gceHandleInstance500(t, gceTestInstanceID)
	g500 := gceNewLifecycle(s500, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.Error(t, g500.SetCreationFailed())
}

// TestGceLifecycleConfirmCreation covers ConfirmCreation writing Reconciled
// and CreationConfirmed, and a PATCH error.
func TestGceLifecycleConfirmCreation(t *testing.T) {
	s := gceNewAPIStub(t)
	s.gceHandleInstance(t, gceTestInstanceID, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))

	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.NoError(t, g.ConfirmCreation())
	patch := s.gceLastPatch(t, gceInstancePath(gceTestInstanceID))
	require.NotNil(t, patch.Reconciled)
	assert.True(t, *patch.Reconciled)
	require.NotNil(t, patch.CreationConfirmed)

	s500 := gceNewAPIStub(t)
	s500.gceHandleInstance500(t, gceTestInstanceID)
	g500 := gceNewLifecycle(s500, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.Error(t, g500.ConfirmCreation())
}

// TestGceLifecycleAckDeletion covers AckDeletion writing DeletionAcknowledged
// and a PATCH error.
func TestGceLifecycleAckDeletion(t *testing.T) {
	s := gceNewAPIStub(t)
	s.gceHandleInstance(t, gceTestInstanceID, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))

	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.NoError(t, g.AckDeletion())
	patch := s.gceLastPatch(t, gceInstancePath(gceTestInstanceID))
	require.NotNil(t, patch.DeletionAcknowledged)

	s500 := gceNewAPIStub(t)
	s500.gceHandleInstance500(t, gceTestInstanceID)
	g500 := gceNewLifecycle(s500, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.Error(t, g500.AckDeletion())
}

// TestGceLifecycleRefreshDeletionAck covers RefreshDeletionAck writing
// DeletionAcknowledged and a PATCH error.
func TestGceLifecycleRefreshDeletionAck(t *testing.T) {
	s := gceNewAPIStub(t)
	s.gceHandleInstance(t, gceTestInstanceID, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))

	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.NoError(t, g.RefreshDeletionAck())
	patch := s.gceLastPatch(t, gceInstancePath(gceTestInstanceID))
	require.NotNil(t, patch.DeletionAcknowledged)

	s500 := gceNewAPIStub(t)
	s500.gceHandleInstance500(t, gceTestInstanceID)
	g500 := gceNewLifecycle(s500, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.Error(t, g500.RefreshDeletionAck())
}

// TestGceLifecycleConfirmDeletion covers ConfirmDeletion writing
// DeletionConfirmed and a PATCH error.
func TestGceLifecycleConfirmDeletion(t *testing.T) {
	s := gceNewAPIStub(t)
	s.gceHandleInstance(t, gceTestInstanceID, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))

	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.NoError(t, g.ConfirmDeletion())
	patch := s.gceLastPatch(t, gceInstancePath(gceTestInstanceID))
	require.NotNil(t, patch.DeletionConfirmed)

	s500 := gceNewAPIStub(t)
	s500.gceHandleInstance500(t, gceTestInstanceID)
	g500 := gceNewLifecycle(s500, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.Error(t, g500.ConfirmDeletion())
}

// TestGceLifecycleSaveState covers SaveState writing ResourceInventory and a
// PATCH error.
func TestGceLifecycleSaveState(t *testing.T) {
	s := gceNewAPIStub(t)
	s.gceHandleInstance(t, gceTestInstanceID, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))

	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	state := datatypes.JSON([]byte(`{"deployment":{}}`))
	require.NoError(t, g.SaveState(&state))
	patch := s.gceLastPatch(t, gceInstancePath(gceTestInstanceID))
	require.NotNil(t, patch.ResourceInventory)
	assert.JSONEq(t, `{"deployment":{}}`, string(*patch.ResourceInventory))

	s500 := gceNewAPIStub(t)
	s500.gceHandleInstance500(t, gceTestInstanceID)
	g500 := gceNewLifecycle(s500, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.Error(t, g500.SaveState(&state))
}

// TestGceLifecycleClearInventory covers ClearInventory writing "{}" as
// ResourceInventory and a PATCH error.
func TestGceLifecycleClearInventory(t *testing.T) {
	s := gceNewAPIStub(t)
	s.gceHandleInstance(t, gceTestInstanceID, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))

	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.NoError(t, g.ClearInventory())
	patch := s.gceLastPatch(t, gceInstancePath(gceTestInstanceID))
	require.NotNil(t, patch.ResourceInventory)
	assert.Equal(t, "{}", string(*patch.ResourceInventory))

	s500 := gceNewAPIStub(t)
	s500.gceHandleInstance500(t, gceTestInstanceID)
	g500 := gceNewLifecycle(s500, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.Error(t, g500.ClearInventory())
}

// TestGceLifecycleOnDeleteConfirmed covers OnDeleteConfirmed returning nil.
func TestGceLifecycleOnDeleteConfirmed(t *testing.T) {
	s := gceNewAPIStub(t)
	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	assert.NoError(t, g.OnDeleteConfirmed(nil))
}

// TestGceLifecycleOnCreateConfirmedWritesHostnameSSHOntoMarriedInstance covers
// OnCreateConfirmed writing ExternalIP onto Hostname with SSHUser, SSHKey, and Reconciled false.
func TestGceLifecycleOnCreateConfirmedWritesHostnameSSHOntoMarriedInstance(t *testing.T) {
	s := gceNewAPIStub(t)
	latest := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
	latest.MachineRuntimeInstanceID = gcePtr(gceTestMachineRuntimeInstanceID)
	latest.ExternalIP = gcePtr("203.0.113.7")
	latest.SSHUser = gcePtr("threeport")
	latest.SSHKey = gcePtr("PRIVATE-KEY")
	s.gceHandleInstance(t, gceTestInstanceID, latest)
	s.gceHandleMachineRuntimeInstance(t, gceTestMachineRuntimeInstanceID, gceBaseMachineRuntimeInstance(gceTestMachineRuntimeInstanceID, gceTestInstanceName))

	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.NoError(t, g.OnCreateConfirmed(nil))

	// inspect the PATCH sent to the married machine runtime instance
	patch := s.gceLastMachineRuntimeInstancePatch(t, gceMachineRuntimeInstancePath(gceTestMachineRuntimeInstanceID))
	require.NotNil(t, patch.Hostname)
	assert.Equal(t, "203.0.113.7", *patch.Hostname)
	require.NotNil(t, patch.SSHUser)
	assert.Equal(t, "threeport", *patch.SSHUser)
	require.NotNil(t, patch.SSHKey)
	assert.Equal(t, "PRIVATE-KEY", *patch.SSHKey)
	require.NotNil(t, patch.Reconciled)
	assert.False(t, *patch.Reconciled)
}

// TestGceLifecycleOnCreateConfirmedNilMarriedIDReturnsError covers
// OnCreateConfirmed rejecting a nil MachineRuntimeInstanceID.
func TestGceLifecycleOnCreateConfirmedNilMarriedIDReturnsError(t *testing.T) {
	s := gceNewAPIStub(t)
	latest := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
	latest.MachineRuntimeInstanceID = nil
	s.gceHandleInstance(t, gceTestInstanceID, latest)

	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	err := g.OnCreateConfirmed(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MachineRuntimeInstanceID")
}

// TestGceLifecycleOnCreateConfirmedInstanceGETErrorWraps covers
// OnCreateConfirmed wrapping a GCE instance GET 500.
func TestGceLifecycleOnCreateConfirmedInstanceGETErrorWraps(t *testing.T) {
	s := gceNewAPIStub(t)
	s.gceHandleInstance500(t, gceTestInstanceID)

	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	err := g.OnCreateConfirmed(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get GCE instance for machine runtime update")
}

// TestGceLifecyclePublishCreateNotification covers PublishCreateNotification
// publishing gcpGceMachineRuntimeInstance.create and wrapping a NATS error.
func TestGceLifecyclePublishCreateNotification(t *testing.T) {
	t.Run("publish success", func(t *testing.T) {
		s := gceNewAPIStub(t)
		r := s.gceReconciler()
		js := &gceFakeJetStream{}
		r.JetStreamContext = js
		log := logr.Discard()
		g := newGceMachineLifecycleProvider(r, gceBaseInstance(gceTestInstanceID, gceTestInstanceName), &log)

		require.NoError(t, g.PublishCreateNotification())
		assert.Equal(t, []string{"gcpGceMachineRuntimeInstance.create"}, js.subjects)
	})

	t.Run("publish error wraps", func(t *testing.T) {
		s := gceNewAPIStub(t)
		r := s.gceReconciler()
		js := &gceFakeJetStream{err: fmt.Errorf("nats down")}
		r.JetStreamContext = js
		log := logr.Discard()
		g := newGceMachineLifecycleProvider(r, gceBaseInstance(gceTestInstanceID, gceTestInstanceName), &log)

		err := g.PublishCreateNotification()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to publish create notification")
	})
}

// TestGceLifecyclePublishDeleteNotification covers PublishDeleteNotification
// publishing gcpGceMachineRuntimeInstance.delete and wrapping a NATS error.
func TestGceLifecyclePublishDeleteNotification(t *testing.T) {
	t.Run("publish success", func(t *testing.T) {
		s := gceNewAPIStub(t)
		r := s.gceReconciler()
		js := &gceFakeJetStream{}
		r.JetStreamContext = js
		log := logr.Discard()
		g := newGceMachineLifecycleProvider(r, gceBaseInstance(gceTestInstanceID, gceTestInstanceName), &log)

		require.NoError(t, g.PublishDeleteNotification())
		assert.Equal(t, []string{"gcpGceMachineRuntimeInstance.delete"}, js.subjects)
	})

	t.Run("publish error wraps", func(t *testing.T) {
		s := gceNewAPIStub(t)
		r := s.gceReconciler()
		js := &gceFakeJetStream{err: fmt.Errorf("nats down")}
		r.JetStreamContext = js
		log := logr.Discard()
		g := newGceMachineLifecycleProvider(r, gceBaseInstance(gceTestInstanceID, gceTestInstanceName), &log)

		err := g.PublishDeleteNotification()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to publish delete notification")
	})
}

// TestGceInstanceCreatedConfirmedNoop covers v0GcpGceMachineRuntimeInstanceCreated
// returning delay 0 when CreationConfirmed is already set.
func TestGceInstanceCreatedConfirmedNoop(t *testing.T) {
	s := gceNewAPIStub(t)
	latest := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
	latest.CreationConfirmed = gcePtr(time.Now().UTC())
	s.gceHandleInstance(t, gceTestInstanceID, latest)

	log := logr.Discard()
	delay, err := v0GcpGceMachineRuntimeInstanceCreated(
		s.gceReconciler(),
		gceBaseInstance(gceTestInstanceID, gceTestInstanceName),
		&log,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// TestGceInstanceCreatedRequeuesWhenAckedNotStale covers
// v0GcpGceMachineRuntimeInstanceCreated returning delay 120 for a fresh ack.
func TestGceInstanceCreatedRequeuesWhenAckedNotStale(t *testing.T) {
	s := gceNewAPIStub(t)
	latest := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
	latest.CreationConfirmed = nil
	latest.CreationFailed = gcePtr(false)
	latest.CreationAcknowledged = gcePtr(time.Now().UTC())
	latest.ResourceInventory = nil
	s.gceHandleInstance(t, gceTestInstanceID, latest)

	log := logr.Discard()
	delay, err := v0GcpGceMachineRuntimeInstanceCreated(
		s.gceReconciler(),
		gceBaseInstance(gceTestInstanceID, gceTestInstanceName),
		&log,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(120), delay)
}

// TestGceInstanceUpdatedNoop covers v0GcpGceMachineRuntimeInstanceUpdated
// returning delay 0.
func TestGceInstanceUpdatedNoop(t *testing.T) {
	s := gceNewAPIStub(t)
	log := logr.Discard()
	delay, err := v0GcpGceMachineRuntimeInstanceUpdated(
		s.gceReconciler(),
		gceBaseInstance(gceTestInstanceID, gceTestInstanceName),
		&log,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// TestGceInstanceDeletedConfirmedNoop covers v0GcpGceMachineRuntimeInstanceDeleted
// returning delay 0 when DeletionConfirmed is already set.
func TestGceInstanceDeletedConfirmedNoop(t *testing.T) {
	s := gceNewAPIStub(t)
	latest := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
	latest.DeletionScheduled = gcePtr(time.Now().UTC())
	latest.DeletionConfirmed = gcePtr(time.Now().UTC())
	s.gceHandleInstance(t, gceTestInstanceID, latest)

	log := logr.Discard()
	delay, err := v0GcpGceMachineRuntimeInstanceDeleted(
		s.gceReconciler(),
		gceBaseInstance(gceTestInstanceID, gceTestInstanceName),
		&log,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// TestGceInstanceDeletedRequeuesWhenCreateInProgress covers
// v0GcpGceMachineRuntimeInstanceDeleted returning delay 60 while create is acked.
func TestGceInstanceDeletedRequeuesWhenCreateInProgress(t *testing.T) {
	s := gceNewAPIStub(t)
	latest := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
	latest.DeletionScheduled = gcePtr(time.Now().UTC())
	latest.DeletionConfirmed = nil
	latest.CreationConfirmed = nil
	latest.CreationAcknowledged = gcePtr(time.Now().UTC())
	s.gceHandleInstance(t, gceTestInstanceID, latest)

	log := logr.Discard()
	delay, err := v0GcpGceMachineRuntimeInstanceDeleted(
		s.gceReconciler(),
		gceBaseInstance(gceTestInstanceID, gceTestInstanceName),
		&log,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(60), delay)
}

// TestGceInstanceDeletedNotScheduled covers v0GcpGceMachineRuntimeInstanceDeleted
// erroring when DeletionScheduled is nil.
func TestGceInstanceDeletedNotScheduled(t *testing.T) {
	s := gceNewAPIStub(t)
	latest := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
	latest.DeletionScheduled = nil
	s.gceHandleInstance(t, gceTestInstanceID, latest)

	log := logr.Discard()
	_, err := v0GcpGceMachineRuntimeInstanceDeleted(
		s.gceReconciler(),
		gceBaseInstance(gceTestInstanceID, gceTestInstanceName),
		&log,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deletion notification received but not scheduled")
}

// TestGceLifecycleConcurrentInstancesNoRace covers concurrent adapters for
// distinct instance IDs with no cross-instance PATCH bleed.
func TestGceLifecycleConcurrentInstancesNoRace(t *testing.T) {
	const n = 200
	s := gceNewAPIStub(t)
	// register a GET/PATCH handler per instance ID
	for i := 0; i < n; i++ {
		id := uint(1000 + i)
		s.gceHandleInstance(t, id, gceBaseInstance(id, fmt.Sprintf("gce-%d", id)))
	}

	var wg sync.WaitGroup
	errCh := make(chan error, n)
	// run GetReconciliation, AckCreation, SaveState, ConfirmCreation per ID
	for i := 0; i < n; i++ {
		id := uint(1000 + i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			g := gceNewLifecycle(s, gceBaseInstance(id, fmt.Sprintf("gce-%d", id)))
			if _, err := g.GetReconciliation(); err != nil {
				errCh <- err
				return
			}
			if err := g.AckCreation(); err != nil {
				errCh <- err
				return
			}
			st := datatypes.JSON([]byte(`{"deployment":{}}`))
			if err := g.SaveState(&st); err != nil {
				errCh <- err
				return
			}
			if err := g.ConfirmCreation(); err != nil {
				errCh <- err
				return
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// check each path recorded AckCreation, SaveState, and ConfirmCreation
	for i := 0; i < n; i++ {
		id := uint(1000 + i)
		bodies := s.patches[gceInstancePath(id)]
		assert.Len(t, bodies, 3, "instance %d should have exactly its own 3 PATCHes", id)
	}
}
