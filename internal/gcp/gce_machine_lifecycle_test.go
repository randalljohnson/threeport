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

// gceAPIStub wraps an httptest.Server with the http.Client and base address the
// threeport client helpers expect, and records every PATCH body keyed by path.
type gceAPIStub struct {
	server        *httptest.Server
	mux           *http.ServeMux
	client        *http.Client
	addr          string
	mu            sync.Mutex
	patches       map[string][][]byte
	encryptionKey string
}

// gceNewAPIStub returns a gceAPIStub with an empty mux. The addr has the
// "http://" scheme stripped because the threeport client helpers prepend a
// scheme themselves, and the bare *http.Client keeps the scheme check resolving
// to "http://" against this plain HTTP server.
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

// gceRecordPatch stores a captured PATCH body for the given path.
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
// writes it with the given status; the threeport client expects this shape.
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

// gceHandleInstance registers a handler for an instance path that returns the
// supplied object on GET and records the body on PATCH (echoing it back).
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

// gceHandleGcpNetworks registers a handler for the GcpNetworks collection path
// that returns an empty list on GET (no existing networks for any query) and
// echoes the created object back on POST, so BuildInfra's ensureGcpNetwork call
// resolves cleanly without hitting an unstubbed endpoint.
func (s *gceAPIStub) gceHandleGcpNetworks(t *testing.T) {
	t.Helper()
	s.mux.HandleFunc(v0.PathGcpNetworks, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			gceWriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{})
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var created v0.GcpNetwork
			require.NoError(t, json.Unmarshal(body, &created))
			created.ID = gcePtr(uint(101))
			gceWriteResponse(t, w, http.StatusCreated, []apiserver_lib.Object{&created})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

// gceBaseInstance returns a minimal valid instance referencing the test
// provider and definition, with the given ID and name.
func gceBaseInstance(id uint, name string) *v0.GcpGceMachineRuntimeInstance {
	return &v0.GcpGceMachineRuntimeInstance{
		Common:                           v0.Common{ID: gcePtr(id)},
		Instance:                         v0.Instance{Name: gcePtr(name)},
		GcpProviderID:                    gcePtr(gceTestProviderID),
		GcpGceMachineRuntimeDefinitionID: gcePtr(gceTestDefinitionID),
	}
}

// gceBaseDefinition returns a GCE definition carrying the provisioning
// template fields read by buildGceMachineInfra.
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

// gceMachineRuntimeInstancePath returns the GET/PATCH path for a machine runtime
// instance ID.
func gceMachineRuntimeInstancePath(id uint) string {
	return fmt.Sprintf("%s/%d", v0.PathMachineRuntimeInstances, id)
}

// gceHandleMachineRuntimeInstance registers a handler for a machine runtime
// instance path that returns the supplied object on GET and records the body on
// PATCH (echoing it back).
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

// gceBaseMachineRuntimeInstance returns a married machine runtime instance with
// the given ID and name.
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

// gceFakeInfra is a provider.InfraProvider that is NOT *machine.GceMachineInfra,
// used to exercise the wrong-concrete-type branch of SaveCreateOutputs.
type gceFakeInfra struct{}

func (gceFakeInfra) DeployInfra() error                      { return nil }
func (gceFakeInfra) DestroyInfra() error                     { return nil }
func (gceFakeInfra) SetStackState(_ *datatypes.JSON) error   { return nil }
func (gceFakeInfra) GetStackState() (*datatypes.JSON, error) { return nil, nil }

// gceFakeInfra satisfies provider.InfraProvider so SaveCreateOutputs accepts it
// at the call site and exercises the wrong-concrete-type branch.
var _ provider.InfraProvider = gceFakeInfra{}

// gceFakeJetStream embeds nats.JetStreamContext and overrides Publish so tests
// can drive both the publish-success and publish-error paths. All other
// interface methods are inherited from the embedded nil and must not be called.
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
		// buildGceMachineInfra resolves the shared VPC network for the
		// (provider, zone) tuple; stub the collection path so it lands cleanly.
		s.gceHandleGcpNetworks(t)

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
		// stub the shared-network collection so ensureGcpNetwork lands cleanly
		s.gceHandleGcpNetworks(t)

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

func TestGceLifecycleIsCreateComplete(t *testing.T) {
	// NOTE: every inventory below must be valid JSON because it round-trips
	// through the API stub's response marshalling (datatypes.JSON refuses to
	// marshal invalid bytes). The empty-string and whitespace-only raw-DB
	// states cannot arrive as a JSON API response, so they are covered by the
	// nil case and the trimmed-empty cases here.
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

// gceJSON returns a pointer to a datatypes.JSON from the given string.
func gceJSON(s string) *datatypes.JSON {
	j := datatypes.JSON([]byte(s))
	return &j
}

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

func TestGceSaveCreateOutputsWrongConcreteTypeReturnsError(t *testing.T) {
	s := gceNewAPIStub(t)
	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	state := datatypes.JSON([]byte(`{}`))

	err := g.SaveCreateOutputs(gceFakeInfra{}, &state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected *machine.GceMachineInfra")
}

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

// gceFakeOrphanCloud is an orphanReclaimCloud whose presence, probe errors,
// delete errors, and post-delete lingering are configurable, so the reclaim
// logic is driven through every branch without a live compute API. It records
// probe and delete counts so a test can assert the probe-delete-reprobe shape.
type gceFakeOrphanCloud struct {
	instancePresent  bool
	firewallPresent  bool
	instanceProbeErr error
	firewallProbeErr error
	instanceDelErr   error
	firewallDelErr   error
	instanceLingers  bool
	firewallLingers  bool
	instanceProbes   int
	firewallProbes   int
	instanceDeletes  int
	firewallDeletes  int
}

func (f *gceFakeOrphanCloud) instanceExists() (bool, error) {
	f.instanceProbes++
	if f.instanceProbeErr != nil {
		return false, f.instanceProbeErr
	}
	return f.instancePresent, nil
}

func (f *gceFakeOrphanCloud) deleteInstance() error {
	f.instanceDeletes++
	if f.instanceDelErr != nil {
		return f.instanceDelErr
	}
	if !f.instanceLingers {
		f.instancePresent = false
	}
	return nil
}

func (f *gceFakeOrphanCloud) firewallExists() (bool, error) {
	f.firewallProbes++
	if f.firewallProbeErr != nil {
		return false, f.firewallProbeErr
	}
	return f.firewallPresent, nil
}

func (f *gceFakeOrphanCloud) deleteFirewall() error {
	f.firewallDeletes++
	if f.firewallDelErr != nil {
		return f.firewallDelErr
	}
	if !f.firewallLingers {
		f.firewallPresent = false
	}
	return nil
}

func TestGceLifecycleOnDeleteConfirmedRejectsWrongInfraType(t *testing.T) {
	// the post-destroy reclaim needs the concrete GCE infra to address the VM;
	// a nil or foreign infra must refuse rather than confirm a deletion it
	// cannot validate against the cloud
	s := gceNewAPIStub(t)
	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))

	// a nil infra is rejected
	require.Error(t, g.OnDeleteConfirmed(nil))
	// a foreign concrete type is rejected with a message naming the wanted type
	err := g.OnDeleteConfirmed(gceFakeInfra{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected *machine.GceMachineInfra")
}

func TestGceNewComputeOrphanReclaimCloudRejectsMissingCoordinates(t *testing.T) {
	// building the reclaim client against an infra missing its addressing fields
	// must fail before any cloud call, so a false not-found cannot read as gone
	gceInfra := machine.NewGceMachineInfra("")
	gceInfra.ProjectID = ""
	gceInfra.Zone = ""

	_, err := newComputeOrphanReclaimCloud(gceInfra)
	require.Error(t, err)
	// the accumulated error names every missing coordinate
	assert.Contains(t, err.Error(), "ProjectID")
	assert.Contains(t, err.Error(), "Zone")
	assert.Contains(t, err.Error(), "RuntimeInstanceName")
}

func TestReclaimOrphansNoSurvivorsConfirms(t *testing.T) {
	// the common no-drift path: neither resource present, so the reclaim probes
	// once each, deletes nothing, and returns nil to let deletion confirm
	cloud := &gceFakeOrphanCloud{}
	require.NoError(t, reclaimOrphans(cloud))
	assert.Equal(t, 1, cloud.instanceProbes)
	assert.Equal(t, 1, cloud.firewallProbes)
	assert.Equal(t, 0, cloud.instanceDeletes)
	assert.Equal(t, 0, cloud.firewallDeletes)
}

func TestReclaimOrphansDeletesSurvivingInstance(t *testing.T) {
	// an instance the destroy abandoned is deleted, then re-probed gone, so the
	// reclaim returns nil and lets deletion confirm
	cloud := &gceFakeOrphanCloud{instancePresent: true}
	require.NoError(t, reclaimOrphans(cloud))
	// the instance is deleted once and probed before and after the delete
	assert.Equal(t, 1, cloud.instanceDeletes)
	assert.Equal(t, 2, cloud.instanceProbes)
}

func TestReclaimOrphansDeletesSurvivingFirewall(t *testing.T) {
	// a firewall the destroy abandoned is deleted and re-probed gone
	cloud := &gceFakeOrphanCloud{firewallPresent: true}
	require.NoError(t, reclaimOrphans(cloud))
	assert.Equal(t, 1, cloud.firewallDeletes)
	assert.Equal(t, 2, cloud.firewallProbes)
}

func TestReclaimOrphansRejectsDeleteFailure(t *testing.T) {
	// a delete that errors surfaces so the teardown requeues instead of
	// confirming a deletion that left the instance live
	cloud := &gceFakeOrphanCloud{instancePresent: true, instanceDelErr: fmt.Errorf("delete denied")}
	err := reclaimOrphans(cloud)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete orphaned instance")
}

func TestReclaimOrphansRejectsResourceLingeringAfterDelete(t *testing.T) {
	// a delete the cloud accepts but that leaves the resource present is caught
	// by the re-probe and returns an error so the teardown retries
	cloud := &gceFakeOrphanCloud{instancePresent: true, instanceLingers: true}
	err := reclaimOrphans(cloud)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "still present after delete")
}

func TestReclaimOrphansRejectsProbeFailure(t *testing.T) {
	// a probe that errors surfaces and no delete is attempted, so a transient
	// cloud failure requeues rather than confirming on incomplete information
	cloud := &gceFakeOrphanCloud{instanceProbeErr: fmt.Errorf("api down")}
	err := reclaimOrphans(cloud)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to probe instance")
	assert.Equal(t, 0, cloud.instanceDeletes)
}

func TestGceLifecycleOnCreateConfirmedWritesHostnameSSHOntoMarriedInstance(t *testing.T) {
	// stub a fully provisioned GCE instance carrying the married runtime instance
	// ID and the connection fields persisted earlier from the VM
	s := gceNewAPIStub(t)
	latest := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
	latest.MachineRuntimeInstanceID = gcePtr(gceTestMachineRuntimeInstanceID)
	latest.ExternalIP = gcePtr("203.0.113.7")
	latest.SSHUser = gcePtr("threeport")
	latest.SSHKey = gcePtr("PRIVATE-KEY")
	s.gceHandleInstance(t, gceTestInstanceID, latest)
	// stub the married machine runtime instance the connection fields copy onto
	s.gceHandleMachineRuntimeInstance(t, gceTestMachineRuntimeInstanceID, gceBaseMachineRuntimeInstance(gceTestMachineRuntimeInstanceID, gceTestInstanceName))

	// run post-create confirmation against the stubbed instance
	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	require.NoError(t, g.OnCreateConfirmed(nil))

	// inspect the PATCH sent to the married machine runtime instance
	patch := s.gceLastMachineRuntimeInstancePatch(t, gceMachineRuntimeInstancePath(gceTestMachineRuntimeInstanceID))
	// confirm the external IP lands on the hostname
	require.NotNil(t, patch.Hostname)
	assert.Equal(t, "203.0.113.7", *patch.Hostname)
	// confirm the ssh user carries across
	require.NotNil(t, patch.SSHUser)
	assert.Equal(t, "threeport", *patch.SSHUser)
	// confirm the ssh key carries across
	require.NotNil(t, patch.SSHKey)
	assert.Equal(t, "PRIVATE-KEY", *patch.SSHKey)
	// confirm reconciled is cleared so the workload ssh install runs next
	require.NotNil(t, patch.Reconciled)
	assert.False(t, *patch.Reconciled)
}

func TestGceLifecycleOnCreateConfirmedNilMarriedIDReturnsError(t *testing.T) {
	// stub a GCE instance with no married machine runtime instance ID
	s := gceNewAPIStub(t)
	latest := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
	latest.MachineRuntimeInstanceID = nil
	s.gceHandleInstance(t, gceTestInstanceID, latest)

	// run post-create confirmation against the unmarried instance
	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	err := g.OnCreateConfirmed(nil)
	// confirm the missing married ID surfaces as an error naming the absent field
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MachineRuntimeInstanceID")
}

func TestGceLifecycleOnCreateConfirmedInstanceGETErrorWraps(t *testing.T) {
	// stub the GCE instance GET to fail
	s := gceNewAPIStub(t)
	s.gceHandleInstance500(t, gceTestInstanceID)

	// run post-create confirmation while the instance fetch is failing
	g := gceNewLifecycle(s, gceBaseInstance(gceTestInstanceID, gceTestInstanceName))
	err := g.OnCreateConfirmed(nil)
	// confirm the fetch failure is wrapped with context naming the failed lookup
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get GCE instance for machine runtime update")
}

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

func TestGceInstanceCreatedRequeuesWhenAckedNotStale(t *testing.T) {
	s := gceNewAPIStub(t)
	latest := gceBaseInstance(gceTestInstanceID, gceTestInstanceName)
	latest.CreationConfirmed = nil
	latest.CreationFailed = gcePtr(false)
	latest.CreationAcknowledged = gcePtr(time.Now().UTC())
	// inventory nil so IsCreateComplete is false; reaches the non-stale requeue.
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

// TestGceLifecycleConcurrentInstancesNoRace constructs N adapters for distinct
// instance IDs and drives their stateless methods concurrently. The adapter
// holds no shared mutable state: there is no data race under -race and no
// cross-instance bleed (each goroutine's PATCH carries its own ID). Semaphore-
// cap, goroutine-leak, and stale-ack behavior live in the shared handler and
// are not asserted here.
func TestGceLifecycleConcurrentInstancesNoRace(t *testing.T) {
	const n = 200
	s := gceNewAPIStub(t)
	// register a handler per distinct instance ID, returning that ID on GET and
	// recording PATCH bodies under its own path.
	for i := 0; i < n; i++ {
		id := uint(1000 + i)
		s.gceHandleInstance(t, id, gceBaseInstance(id, fmt.Sprintf("gce-%d", id)))
	}

	var wg sync.WaitGroup
	errCh := make(chan error, n)
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

	// each instance path captured its own PATCHes; the client carries the ID in
	// the URL (stripped from the body), so a recorded PATCH on every distinct
	// path proves each goroutine targeted its own instance with no bleed.
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < n; i++ {
		id := uint(1000 + i)
		bodies := s.patches[gceInstancePath(id)]
		// AckCreation, SaveState, ConfirmCreation each issue one PATCH.
		assert.Len(t, bodies, 3, "instance %d should have exactly its own 3 PATCHes", id)
	}
}
