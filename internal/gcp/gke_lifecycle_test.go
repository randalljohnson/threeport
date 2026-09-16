// Tests for the GKE lifecycle adapter. They drive the real adapter against an
// in-process httptest stub of the threeport API (via internal/machinetest) plus
// a small fake provider.InfraProvider; no GCP, Pulumi, or NATS infrastructure is
// touched.
//
// Every package-level identifier is gke-prefixed and there is no TestMain, so
// GCE tests sharing this package do not collide.
package gcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	logr "github.com/go-logr/logr"
	nats "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	notif "github.com/threeport/threeport/internal/gcp/notif"
	machinetest "github.com/threeport/threeport/internal/machinetest"
	"github.com/threeport/threeport/internal/provider"
	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	encryption "github.com/threeport/threeport/pkg/encryption/v0"
	kube "github.com/threeport/threeport/pkg/kube/v0"
	notifications "github.com/threeport/threeport/pkg/notifications/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

const (
	// gkeTestInstanceID is the default API ID used by single-instance test cases.
	gkeTestInstanceID uint = 42
	// gkeTestInstanceName is the default name used by single-instance test cases.
	gkeTestInstanceName = "gke-test-instance"
	// gkeTestDefinitionID is the GKE definition ID referenced by test instances.
	gkeTestDefinitionID uint = 9
	// gkeTestProviderID is the GCP provider ID referenced by test instances.
	gkeTestProviderID uint = 5
	// gkeTestKriID is the linked kubernetes runtime instance ID used by connection tests.
	gkeTestKriID uint = 77
)

// gkeFakeInfra is a provider.InfraProvider that is not *provider.KubernetesRuntimeInfraGKE.
// It drives OnCreateConfirmed's wrong-type branch and supplies SaveCreateOutputs'
// unused infra argument.
type gkeFakeInfra struct{}

func (gkeFakeInfra) DeployInfra() error                      { return nil }
func (gkeFakeInfra) DestroyInfra() error                     { return nil }
func (gkeFakeInfra) SetStackState(*datatypes.JSON) error     { return nil }
func (gkeFakeInfra) GetStackState() (*datatypes.JSON, error) { return nil, nil }

// gkePublishedMessage is one JetStream Publish call captured by gkeFakeJetStream.
type gkePublishedMessage struct {
	subject string
	data    []byte
}

// gkeFakeJetStream embeds nats.JetStreamContext and overrides Publish so tests
// can drive both the publish-success and publish-error paths. All other
// interface methods are inherited from the embedded nil and must not be called.
type gkeFakeJetStream struct {
	nats.JetStreamContext
	mu         sync.Mutex
	published  []gkePublishedMessage
	publishErr error
}

// Publish records the subject and body, or returns the configured error.
func (f *gkeFakeJetStream) Publish(subject string, data []byte, _ ...nats.PubOpt) (*nats.PubAck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.publishErr != nil {
		return nil, f.publishErr
	}
	f.published = append(f.published, gkePublishedMessage{subject: subject, data: data})
	return &nats.PubAck{}, nil
}

// messages returns a copy of the recorded Publish calls.
func (f *gkeFakeJetStream) messages() []gkePublishedMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]gkePublishedMessage, len(f.published))
	copy(out, f.published)
	return out
}

// gkeCapturedPatch is one recorded PATCH path and body.
type gkeCapturedPatch struct {
	path string
	body []byte
}

// gkePatchRecorder collects PATCH requests across server goroutines.
type gkePatchRecorder struct {
	mu      sync.Mutex
	patches []gkeCapturedPatch
}

// add stores a captured PATCH body for the given path.
func (rec *gkePatchRecorder) add(path string, body []byte) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.patches = append(rec.patches, gkeCapturedPatch{path: path, body: body})
}

// snapshot returns a copy of the recorded PATCH requests.
func (rec *gkePatchRecorder) snapshot() []gkeCapturedPatch {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make([]gkeCapturedPatch, len(rec.patches))
	copy(out, rec.patches)
	return out
}

// gkeParsePathID extracts the trailing numeric ID from a request path.
func gkeParsePathID(path string) (uint, bool) {
	idx := strings.LastIndex(path, "/")
	if idx < 0 || idx == len(path)-1 {
		return 0, false
	}
	id, err := strconv.ParseUint(path[idx+1:], 10, 64)
	if err != nil {
		return 0, false
	}
	return uint(id), true
}

// gkeStubReconciler builds a controller.Reconciler pointed at the stub.
func gkeStubReconciler(api *machinetest.APIStub) *controller.Reconciler {
	return &controller.Reconciler{APIClient: api.Client, APIServer: api.Addr}
}

// gkeNewLifecycle constructs the adapter against the stub for a given instance.
func gkeNewLifecycle(api *machinetest.APIStub, inst *v0.GcpGkeKubernetesRuntimeInstance) *gkeLifecycle {
	log := logr.Discard()
	return newGkeLifecycleProvider(gkeStubReconciler(api), inst, &log)
}

// gkeTestInstance returns a minimal GKE instance with ID and name set.
func gkeTestInstance(id uint, name string) *v0.GcpGkeKubernetesRuntimeInstance {
	return &v0.GcpGkeKubernetesRuntimeInstance{
		Common:   v0.Common{ID: util.Ptr(id)},
		Instance: v0.Instance{Name: util.Ptr(name)},
	}
}

// gkeServeInstances registers a subtree handler for the GKE instance endpoint:
// GET returns inst, PATCH records the body and echoes it back. Non-OK statuses
// force the corresponding error paths.
func gkeServeInstances(
	t *testing.T,
	api *machinetest.APIStub,
	inst *v0.GcpGkeKubernetesRuntimeInstance,
	rec *gkePatchRecorder,
	getStatus int,
	patchStatus int,
) {
	t.Helper()
	api.Mux.HandleFunc(v0.PathGcpGkeKubernetesRuntimeInstances+"/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if getStatus != http.StatusOK {
				machinetest.WriteResponse(t, w, getStatus, nil)
				return
			}
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{*inst})
		case http.MethodPatch:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if rec != nil {
				rec.add(r.URL.Path, body)
			}
			if patchStatus != http.StatusOK {
				machinetest.WriteResponse(t, w, patchStatus, nil)
				return
			}
			var updated v0.GcpGkeKubernetesRuntimeInstance
			if err := json.Unmarshal(body, &updated); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if id, ok := gkeParsePathID(r.URL.Path); ok {
				updated.ID = util.Ptr(id)
			}
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{updated})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

// gkeServeKubeRuntimeInstances registers a subtree handler for the kubernetes
// runtime instance endpoint, GET and PATCH, mirroring gkeServeInstances.
func gkeServeKubeRuntimeInstances(
	t *testing.T,
	api *machinetest.APIStub,
	kri *v0.KubernetesRuntimeInstance,
	rec *gkePatchRecorder,
	getStatus int,
	patchStatus int,
) {
	t.Helper()
	api.Mux.HandleFunc(v0.PathKubernetesRuntimeInstances+"/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if getStatus != http.StatusOK {
				machinetest.WriteResponse(t, w, getStatus, nil)
				return
			}
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{*kri})
		case http.MethodPatch:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if rec != nil {
				rec.add(r.URL.Path, body)
			}
			if patchStatus != http.StatusOK {
				machinetest.WriteResponse(t, w, patchStatus, nil)
				return
			}
			var updated v0.KubernetesRuntimeInstance
			if err := json.Unmarshal(body, &updated); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if id, ok := gkeParsePathID(r.URL.Path); ok {
				updated.ID = util.Ptr(id)
			}
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{updated})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

// gkeServeDefinition registers a GET-only subtree handler for the GKE
// definition endpoint.
func gkeServeDefinition(t *testing.T, api *machinetest.APIStub, def *v0.GcpGkeKubernetesRuntimeDefinition, status int) {
	t.Helper()
	api.Mux.HandleFunc(v0.PathGcpGkeKubernetesRuntimeDefinitions+"/", func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			machinetest.WriteResponse(t, w, status, nil)
			return
		}
		machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{*def})
	})
}

// gkeServeProvider registers a GET-only subtree handler for the GCP provider
// endpoint.
func gkeServeProvider(t *testing.T, api *machinetest.APIStub, prov *v0.GcpProvider, status int) {
	t.Helper()
	api.Mux.HandleFunc(v0.PathGcpProviders+"/", func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			machinetest.WriteResponse(t, w, status, nil)
			return
		}
		machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{*prov})
	})
}

// gkeRunUpdateMethod exercises one reconciliation-update method: a success run
// asserting the PATCH path and body, and a 500 run asserting the error propagates.
func gkeRunUpdateMethod(
	t *testing.T,
	call func(g *gkeLifecycle) error,
	check func(t *testing.T, patched v0.GcpGkeKubernetesRuntimeInstance),
) {
	t.Helper()

	t.Run("success", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		rec := &gkePatchRecorder{}
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		gkeServeInstances(t, api, inst, rec, http.StatusOK, http.StatusOK)

		require.NoError(t, call(gkeNewLifecycle(api, inst)))

		patches := rec.snapshot()
		require.Len(t, patches, 1)
		assert.Equal(t, fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeInstances, gkeTestInstanceID), patches[0].path)
		var patched v0.GcpGkeKubernetesRuntimeInstance
		require.NoError(t, json.Unmarshal(patches[0].body, &patched))
		check(t, patched)
	})

	t.Run("patch error", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusInternalServerError)

		require.Error(t, call(gkeNewLifecycle(api, inst)))
	})
}

// TestGkeLifecycleGetReconciliation covers field mapping, the nil-CreationFailed
// guard, and a GET error from the stub.
func TestGkeLifecycleGetReconciliation(t *testing.T) {
	t.Run("maps all fields", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		ackTime := time.Now().UTC().Add(-1 * time.Minute)
		confirmTime := time.Now().UTC().Add(-2 * time.Minute)
		delSchedTime := time.Now().UTC().Add(-3 * time.Minute)
		delAckTime := time.Now().UTC().Add(-4 * time.Minute)
		delConfirmTime := time.Now().UTC().Add(-5 * time.Minute)
		inventory := datatypes.JSON([]byte(`{"cluster":"x"}`))
		inst.CreationAcknowledged = &ackTime
		inst.CreationConfirmed = &confirmTime
		inst.CreationFailed = util.Ptr(true)
		inst.DeletionScheduled = &delSchedTime
		inst.DeletionAcknowledged = &delAckTime
		inst.DeletionConfirmed = &delConfirmTime
		inst.ResourceInventory = &inventory
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)

		snap, err := gkeNewLifecycle(api, inst).GetReconciliation()
		require.NoError(t, err)

		require.NotNil(t, snap.CreationAcknowledged)
		assert.WithinDuration(t, ackTime, *snap.CreationAcknowledged, time.Second)
		require.NotNil(t, snap.CreationConfirmed)
		assert.WithinDuration(t, confirmTime, *snap.CreationConfirmed, time.Second)
		assert.True(t, snap.CreationFailed)
		require.NotNil(t, snap.DeletionScheduled)
		assert.WithinDuration(t, delSchedTime, *snap.DeletionScheduled, time.Second)
		require.NotNil(t, snap.DeletionAcknowledged)
		assert.WithinDuration(t, delAckTime, *snap.DeletionAcknowledged, time.Second)
		require.NotNil(t, snap.DeletionConfirmed)
		assert.WithinDuration(t, delConfirmTime, *snap.DeletionConfirmed, time.Second)
		require.NotNil(t, snap.ResourceInventory)
		assert.JSONEq(t, `{"cluster":"x"}`, string(*snap.ResourceInventory))
	})

	t.Run("nil creation failed maps to false", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)

		snap, err := gkeNewLifecycle(api, inst).GetReconciliation()
		require.NoError(t, err)
		assert.False(t, snap.CreationFailed)
	})

	t.Run("get error", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		gkeServeInstances(t, api, inst, nil, http.StatusInternalServerError, http.StatusOK)

		_, err := gkeNewLifecycle(api, inst).GetReconciliation()
		assert.ErrorContains(t, err, "failed to get latest GKE instance")
	})
}

// gkeBuildFixtures returns an instance, definition, and provider wired together
// with valid required fields for infra-build tests.
func gkeBuildFixtures() (*v0.GcpGkeKubernetesRuntimeInstance, *v0.GcpGkeKubernetesRuntimeDefinition, *v0.GcpProvider) {
	inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
	inst.Region = util.Ptr("us-central1")
	inst.GcpGkeKubernetesRuntimeDefinitionID = util.Ptr(gkeTestDefinitionID)
	inst.GcpProviderID = util.Ptr(gkeTestProviderID)
	def := &v0.GcpGkeKubernetesRuntimeDefinition{
		Common:                      v0.Common{ID: util.Ptr(gkeTestDefinitionID)},
		DefaultNodeGroupInitialSize: util.Ptr(3),
	}
	prov := &v0.GcpProvider{
		Common:    v0.Common{ID: util.Ptr(gkeTestProviderID)},
		ProjectID: util.Ptr("proj-x"),
	}
	return inst, def, prov
}

// TestGkeLifecycleBuildInfra covers field projection, credential gates, and
// fetch errors for BuildInfra.
func TestGkeLifecycleBuildInfra(t *testing.T) {
	t.Run("happy path projects all fields", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst, def, prov := gkeBuildFixtures()
		// seed the credential encrypted with a fresh key so BuildInfra decrypts it
		key, err := encryption.GenerateKey()
		require.NoError(t, err)
		enc, err := encryption.Encrypt(key, `{"type":"service_account"}`)
		require.NoError(t, err)
		prov.ServiceAccountCredentials = util.Ptr(enc)
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)
		gkeServeDefinition(t, api, def, http.StatusOK)
		gkeServeProvider(t, api, prov, http.StatusOK)

		// wire the matching key into the reconciler so the decrypt succeeds
		lc := gkeNewLifecycle(api, inst)
		lc.r.EncryptionKey = key
		infra, err := lc.BuildInfra()
		require.NoError(t, err)

		infraGKE, ok := infra.(*provider.KubernetesRuntimeInfraGKE)
		require.True(t, ok)
		assert.Equal(t, gkeTestInstanceName, infraGKE.RuntimeInstanceName)
		assert.Equal(t, "proj-x", infraGKE.ProjectID)
		assert.Equal(t, "us-central1", infraGKE.Region)
		assert.Equal(t, int32(3), infraGKE.WorkerNodeInitialCount)
		assert.Equal(t, `{"type":"service_account"}`, infraGKE.ServiceAccountCredentials)
	})

	t.Run("nil credentials rejected", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst, def, prov := gkeBuildFixtures()
		// reject before DeployInfra so an empty key never reaches the Pulumi provider
		prov.ServiceAccountCredentials = nil
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)
		gkeServeDefinition(t, api, def, http.StatusOK)
		gkeServeProvider(t, api, prov, http.StatusOK)

		_, err := gkeNewLifecycle(api, inst).BuildInfra()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no service account credentials")
	})

	t.Run("empty credentials rejected", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst, def, prov := gkeBuildFixtures()
		// empty credentials must fail the same gate as nil
		prov.ServiceAccountCredentials = util.Ptr("")
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)
		gkeServeDefinition(t, api, def, http.StatusOK)
		gkeServeProvider(t, api, prov, http.StatusOK)

		_, err := gkeNewLifecycle(api, inst).BuildInfra()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no service account credentials")
	})

	t.Run("instance fetch error", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst, _, _ := gkeBuildFixtures()
		gkeServeInstances(t, api, inst, nil, http.StatusInternalServerError, http.StatusOK)

		_, err := gkeNewLifecycle(api, inst).BuildInfra()
		assert.ErrorContains(t, err, "failed to get GKE instance for infra build")
	})

	t.Run("definition fetch error", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst, def, _ := gkeBuildFixtures()
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)
		gkeServeDefinition(t, api, def, http.StatusInternalServerError)

		_, err := gkeNewLifecycle(api, inst).BuildInfra()
		assert.ErrorContains(t, err, "failed to get GKE definition")
	})

	t.Run("provider fetch error", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst, def, prov := gkeBuildFixtures()
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)
		gkeServeDefinition(t, api, def, http.StatusOK)
		gkeServeProvider(t, api, prov, http.StatusInternalServerError)

		_, err := gkeNewLifecycle(api, inst).BuildInfra()
		assert.ErrorContains(t, err, "failed to retrieve GCP provider by ID")
	})

	t.Run("missing definition id rejected", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst, _, _ := gkeBuildFixtures()
		inst.GcpGkeKubernetesRuntimeDefinitionID = nil
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)

		_, err := gkeNewLifecycle(api, inst).BuildInfra()
		assert.ErrorContains(t, err, "GcpGkeKubernetesRuntimeDefinitionID")
	})
}

// TestGkeBuildInfraFieldValidation asserts each required field is validated
// before dereference so malformed API objects produce errors, not panics.
func TestGkeBuildInfraFieldValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(inst *v0.GcpGkeKubernetesRuntimeInstance, def *v0.GcpGkeKubernetesRuntimeDefinition, prov *v0.GcpProvider)
		errPart string
		// The flag that registers the provider endpoint; ProjectID is checked only after fetch
		servePrv bool
	}{
		{
			name: "nil provider id",
			mutate: func(inst *v0.GcpGkeKubernetesRuntimeInstance, _ *v0.GcpGkeKubernetesRuntimeDefinition, _ *v0.GcpProvider) {
				inst.GcpProviderID = nil
			},
			errPart: "GcpProviderID",
		},
		{
			name: "nil instance name",
			mutate: func(inst *v0.GcpGkeKubernetesRuntimeInstance, _ *v0.GcpGkeKubernetesRuntimeDefinition, _ *v0.GcpProvider) {
				inst.Name = nil
			},
			errPart: "Name",
		},
		{
			name: "nil region",
			mutate: func(inst *v0.GcpGkeKubernetesRuntimeInstance, _ *v0.GcpGkeKubernetesRuntimeDefinition, _ *v0.GcpProvider) {
				inst.Region = nil
			},
			errPart: "Region",
		},
		{
			name: "nil node group initial size",
			mutate: func(_ *v0.GcpGkeKubernetesRuntimeInstance, def *v0.GcpGkeKubernetesRuntimeDefinition, _ *v0.GcpProvider) {
				def.DefaultNodeGroupInitialSize = nil
			},
			errPart: "DefaultNodeGroupInitialSize",
		},
		{
			name: "nil project id",
			mutate: func(_ *v0.GcpGkeKubernetesRuntimeInstance, _ *v0.GcpGkeKubernetesRuntimeDefinition, prov *v0.GcpProvider) {
				prov.ProjectID = nil
			},
			errPart:  "ProjectID",
			servePrv: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := machinetest.NewAPIStub(t)
			inst, def, prov := gkeBuildFixtures()
			tt.mutate(inst, def, prov)
			if tt.servePrv {
				gkeServeProvider(t, api, prov, http.StatusOK)
			}

			log := logr.Discard()
			infra, err := buildGkeInfra(gkeStubReconciler(api), inst, def, &log)
			require.Error(t, err)
			assert.Nil(t, infra)
			assert.ErrorContains(t, err, tt.errPart)
		})
	}
}

// TestGkeLifecycleIsCreateComplete covers inventory edge cases and a GET error.
func TestGkeLifecycleIsCreateComplete(t *testing.T) {
	// empty- and whitespace-only inventory bytes are not representable in the
	// JSON response envelope, so those forms cannot reach the adapter over
	// the wire; the nil and padded cases cover their semantics
	tests := []struct {
		name      string
		inventory *string
		want      bool
	}{
		{name: "nil inventory", inventory: nil, want: false},
		{name: "empty object", inventory: util.Ptr("{}"), want: false},
		{name: "null literal", inventory: util.Ptr("null"), want: false},
		{name: "populated inventory", inventory: util.Ptr(`{"a":1}`), want: true},
		{name: "whitespace padded empty object", inventory: util.Ptr(" {} "), want: false},
		{name: "newline padded empty object", inventory: util.Ptr("\t{}\n"), want: false},
		{name: "empty array", inventory: util.Ptr("[]"), want: false},
		{name: "quoted null string", inventory: util.Ptr(`"null"`), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := machinetest.NewAPIStub(t)
			inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
			if tt.inventory != nil {
				inv := datatypes.JSON([]byte(*tt.inventory))
				inst.ResourceInventory = &inv
			}
			gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)

			complete, err := gkeNewLifecycle(api, inst).IsCreateComplete()
			require.NoError(t, err)
			assert.Equal(t, tt.want, complete)
		})
	}

	t.Run("get error", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		gkeServeInstances(t, api, inst, nil, http.StatusInternalServerError, http.StatusOK)

		_, err := gkeNewLifecycle(api, inst).IsCreateComplete()
		assert.ErrorContains(t, err, "failed to check GKE creation status")
	})
}

// TestGkeLifecycleOnCreateConfirmedWrongInfraType rejects a non-GKE infra
// provider with a descriptive error instead of panicking on the type assert.
func TestGkeLifecycleOnCreateConfirmedWrongInfraType(t *testing.T) {
	api := machinetest.NewAPIStub(t)
	inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)

	err := gkeNewLifecycle(api, inst).OnCreateConfirmed(gkeFakeInfra{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "expected a GKE infra provider")
}

// gkeValidConnection returns complete kube connection info for connection tests.
func gkeValidConnection() *kube.KubeConnectionInfo {
	return &kube.KubeConnectionInfo{
		APIEndpoint:     "https://10.0.0.1",
		CACertificate:   "ca-pem",
		Token:           "tok",
		TokenExpiration: time.Now().UTC().Add(time.Hour),
	}
}

// TestGkeLifecycleUpdateKubeConnection drives updateKubeRuntimeConnection
// directly, bypassing GetConnection, covering the PATCH body and every error path.
func TestGkeLifecycleUpdateKubeConnection(t *testing.T) {
	t.Run("success patches linked kubernetes runtime instance", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		rec := &gkePatchRecorder{}
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		inst.KubernetesRuntimeInstanceID = util.Ptr(gkeTestKriID)
		kri := &v0.KubernetesRuntimeInstance{
			Common:   v0.Common{ID: util.Ptr(gkeTestKriID)},
			Instance: v0.Instance{Name: util.Ptr("kri")},
		}
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)
		gkeServeKubeRuntimeInstances(t, api, kri, rec, http.StatusOK, http.StatusOK)

		conn := gkeValidConnection()
		require.NoError(t, gkeNewLifecycle(api, inst).updateKubeRuntimeConnection(conn))

		patches := rec.snapshot()
		require.Len(t, patches, 1)
		assert.Equal(t, fmt.Sprintf("%s/%d", v0.PathKubernetesRuntimeInstances, gkeTestKriID), patches[0].path)
		var patched v0.KubernetesRuntimeInstance
		require.NoError(t, json.Unmarshal(patches[0].body, &patched))
		require.NotNil(t, patched.APIEndpoint)
		assert.Equal(t, conn.APIEndpoint, *patched.APIEndpoint)
		require.NotNil(t, patched.CACertificate)
		assert.Equal(t, conn.CACertificate, *patched.CACertificate)
		require.NotNil(t, patched.ConnectionToken)
		assert.Equal(t, conn.Token, *patched.ConnectionToken)
		require.NotNil(t, patched.ConnectionTokenExpiration)
		assert.WithinDuration(t, conn.TokenExpiration, *patched.ConnectionTokenExpiration, time.Second)
		// confirm reconciled is cleared so the linked runtime reconciler runs next
		require.NotNil(t, patched.Reconciled)
		assert.False(t, *patched.Reconciled)
	})

	t.Run("gke instance fetch error", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		gkeServeInstances(t, api, inst, nil, http.StatusInternalServerError, http.StatusOK)

		err := gkeNewLifecycle(api, inst).updateKubeRuntimeConnection(gkeValidConnection())
		assert.ErrorContains(t, err, "failed to get GKE instance for connection update")
	})

	t.Run("nil kubernetes runtime instance id rejected", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		inst.KubernetesRuntimeInstanceID = nil
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)

		err := gkeNewLifecycle(api, inst).updateKubeRuntimeConnection(gkeValidConnection())
		assert.ErrorContains(t, err, "KubernetesRuntimeInstanceID")
	})

	t.Run("linked instance fetch error", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		inst.KubernetesRuntimeInstanceID = util.Ptr(gkeTestKriID)
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)
		gkeServeKubeRuntimeInstances(t, api, nil, nil, http.StatusInternalServerError, http.StatusOK)

		err := gkeNewLifecycle(api, inst).updateKubeRuntimeConnection(gkeValidConnection())
		assert.ErrorContains(t, err, "failed to get kubernetes runtime instance")
	})

	t.Run("patch error", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		inst.KubernetesRuntimeInstanceID = util.Ptr(gkeTestKriID)
		kri := &v0.KubernetesRuntimeInstance{
			Common:   v0.Common{ID: util.Ptr(gkeTestKriID)},
			Instance: v0.Instance{Name: util.Ptr("kri")},
		}
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)
		gkeServeKubeRuntimeInstances(t, api, kri, nil, http.StatusOK, http.StatusInternalServerError)

		err := gkeNewLifecycle(api, inst).updateKubeRuntimeConnection(gkeValidConnection())
		assert.ErrorContains(t, err, "failed to update kubernetes runtime instance with kube connection info")
	})

	t.Run("incomplete connection info rejected", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		rec := &gkePatchRecorder{}
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		inst.KubernetesRuntimeInstanceID = util.Ptr(gkeTestKriID)
		kri := &v0.KubernetesRuntimeInstance{
			Common:   v0.Common{ID: util.Ptr(gkeTestKriID)},
			Instance: v0.Instance{Name: util.Ptr("kri")},
		}
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)
		gkeServeKubeRuntimeInstances(t, api, kri, rec, http.StatusOK, http.StatusOK)

		conn := gkeValidConnection()
		conn.APIEndpoint = ""
		err := gkeNewLifecycle(api, inst).updateKubeRuntimeConnection(conn)
		require.Error(t, err)
		assert.ErrorContains(t, err, "incomplete kube connection info")
		assert.Empty(t, rec.snapshot(), "incomplete connection info must not be written to the runtime instance")
	})
}

// TestGkeLifecycleSaveCreateOutputs asserts ResourceInventory is PATCHed from the create state.
func TestGkeLifecycleSaveCreateOutputs(t *testing.T) {
	state := datatypes.JSON([]byte(`{"deployment":{"resources":[{"urn":"a"}]}}`))
	gkeRunUpdateMethod(t,
		func(g *gkeLifecycle) error { return g.SaveCreateOutputs(gkeFakeInfra{}, &state) },
		func(t *testing.T, patched v0.GcpGkeKubernetesRuntimeInstance) {
			require.NotNil(t, patched.ResourceInventory)
			assert.JSONEq(t, string(state), string(*patched.ResourceInventory))
		},
	)
}

// TestGkeLifecycleOnDeleteConfirmed asserts the post-delete hook is a no-op.
func TestGkeLifecycleOnDeleteConfirmed(t *testing.T) {
	api := machinetest.NewAPIStub(t)
	inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
	assert.NoError(t, gkeNewLifecycle(api, inst).OnDeleteConfirmed(nil))
}

// TestGkeLifecycleAckCreation asserts CreationAcknowledged is set and CreationFailed cleared.
func TestGkeLifecycleAckCreation(t *testing.T) {
	gkeRunUpdateMethod(t,
		func(g *gkeLifecycle) error { return g.AckCreation() },
		func(t *testing.T, patched v0.GcpGkeKubernetesRuntimeInstance) {
			require.NotNil(t, patched.CreationAcknowledged)
			assert.WithinDuration(t, time.Now().UTC(), *patched.CreationAcknowledged, 10*time.Second)
			require.NotNil(t, patched.CreationFailed)
			assert.False(t, *patched.CreationFailed)
			assert.Nil(t, patched.CreationConfirmed)
		},
	)
}

// TestGkeLifecycleRefreshCreationAck asserts only CreationAcknowledged is refreshed.
func TestGkeLifecycleRefreshCreationAck(t *testing.T) {
	gkeRunUpdateMethod(t,
		func(g *gkeLifecycle) error { return g.RefreshCreationAck() },
		func(t *testing.T, patched v0.GcpGkeKubernetesRuntimeInstance) {
			require.NotNil(t, patched.CreationAcknowledged)
			assert.WithinDuration(t, time.Now().UTC(), *patched.CreationAcknowledged, 10*time.Second)
			assert.Nil(t, patched.CreationFailed)
			assert.Nil(t, patched.CreationConfirmed)
		},
	)
}

// TestGkeLifecycleSetCreationFailed asserts CreationFailed is set true alone.
func TestGkeLifecycleSetCreationFailed(t *testing.T) {
	gkeRunUpdateMethod(t,
		func(g *gkeLifecycle) error { return g.SetCreationFailed() },
		func(t *testing.T, patched v0.GcpGkeKubernetesRuntimeInstance) {
			require.NotNil(t, patched.CreationFailed)
			assert.True(t, *patched.CreationFailed)
			assert.Nil(t, patched.CreationAcknowledged)
			assert.Nil(t, patched.CreationConfirmed)
		},
	)
}

// TestGkeLifecycleConfirmCreation asserts CreationConfirmed and Reconciled are set.
func TestGkeLifecycleConfirmCreation(t *testing.T) {
	gkeRunUpdateMethod(t,
		func(g *gkeLifecycle) error { return g.ConfirmCreation() },
		func(t *testing.T, patched v0.GcpGkeKubernetesRuntimeInstance) {
			require.NotNil(t, patched.Reconciled)
			assert.True(t, *patched.Reconciled)
			require.NotNil(t, patched.CreationConfirmed)
			assert.WithinDuration(t, time.Now().UTC(), *patched.CreationConfirmed, 10*time.Second)
			assert.Nil(t, patched.CreationAcknowledged)
		},
	)
}

// TestGkeLifecycleAckDeletion asserts DeletionAcknowledged is set.
func TestGkeLifecycleAckDeletion(t *testing.T) {
	gkeRunUpdateMethod(t,
		func(g *gkeLifecycle) error { return g.AckDeletion() },
		func(t *testing.T, patched v0.GcpGkeKubernetesRuntimeInstance) {
			require.NotNil(t, patched.DeletionAcknowledged)
			assert.WithinDuration(t, time.Now().UTC(), *patched.DeletionAcknowledged, 10*time.Second)
			assert.Nil(t, patched.DeletionConfirmed)
		},
	)
}

// TestGkeLifecycleRefreshDeletionAck asserts only DeletionAcknowledged is refreshed.
func TestGkeLifecycleRefreshDeletionAck(t *testing.T) {
	gkeRunUpdateMethod(t,
		func(g *gkeLifecycle) error { return g.RefreshDeletionAck() },
		func(t *testing.T, patched v0.GcpGkeKubernetesRuntimeInstance) {
			require.NotNil(t, patched.DeletionAcknowledged)
			assert.WithinDuration(t, time.Now().UTC(), *patched.DeletionAcknowledged, 10*time.Second)
			assert.Nil(t, patched.DeletionConfirmed)
		},
	)
}

// TestGkeLifecycleConfirmDeletion asserts DeletionConfirmed is set.
func TestGkeLifecycleConfirmDeletion(t *testing.T) {
	gkeRunUpdateMethod(t,
		func(g *gkeLifecycle) error { return g.ConfirmDeletion() },
		func(t *testing.T, patched v0.GcpGkeKubernetesRuntimeInstance) {
			require.NotNil(t, patched.DeletionConfirmed)
			assert.WithinDuration(t, time.Now().UTC(), *patched.DeletionConfirmed, 10*time.Second)
			assert.Nil(t, patched.DeletionAcknowledged)
		},
	)
}

// TestGkeLifecycleSaveState asserts ResourceInventory is PATCHed from intermediate state.
func TestGkeLifecycleSaveState(t *testing.T) {
	state := datatypes.JSON([]byte(`{"checkpoint":{"latest":{"resources":[{"urn":"b"}]}}}`))
	gkeRunUpdateMethod(t,
		func(g *gkeLifecycle) error { return g.SaveState(&state) },
		func(t *testing.T, patched v0.GcpGkeKubernetesRuntimeInstance) {
			require.NotNil(t, patched.ResourceInventory)
			assert.JSONEq(t, string(state), string(*patched.ResourceInventory))
		},
	)
}

// TestGkeLifecycleClearInventory asserts ResourceInventory is cleared to {}.
func TestGkeLifecycleClearInventory(t *testing.T) {
	gkeRunUpdateMethod(t,
		func(g *gkeLifecycle) error { return g.ClearInventory() },
		func(t *testing.T, patched v0.GcpGkeKubernetesRuntimeInstance) {
			require.NotNil(t, patched.ResourceInventory)
			assert.JSONEq(t, `{}`, string(*patched.ResourceInventory))
		},
	)
}

// TestGkeLifecyclePublishCreateNotification covers the create subject and a publish error.
func TestGkeLifecyclePublishCreateNotification(t *testing.T) {
	t.Run("success publishes to create subject", func(t *testing.T) {
		js := &gkeFakeJetStream{}
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		log := logr.Discard()
		g := newGkeLifecycleProvider(&controller.Reconciler{JetStreamContext: js}, inst, &log)

		require.NoError(t, g.PublishCreateNotification())

		msgs := js.messages()
		require.Len(t, msgs, 1)
		assert.Equal(t, notif.GcpGkeKubernetesRuntimeInstanceCreateSubject, msgs[0].subject)
		var n notifications.Notification
		require.NoError(t, json.Unmarshal(msgs[0].data, &n))
		assert.EqualValues(t, notifications.NotificationOperationCreated, n.Operation)
	})

	t.Run("publish error", func(t *testing.T) {
		js := &gkeFakeJetStream{publishErr: fmt.Errorf("nats down")}
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		log := logr.Discard()
		g := newGkeLifecycleProvider(&controller.Reconciler{JetStreamContext: js}, inst, &log)

		assert.ErrorContains(t, g.PublishCreateNotification(), "failed to publish create notification")
	})
}

// TestGkeLifecyclePublishDeleteNotification covers the delete subject and a publish error.
func TestGkeLifecyclePublishDeleteNotification(t *testing.T) {
	t.Run("success publishes to delete subject", func(t *testing.T) {
		js := &gkeFakeJetStream{}
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		log := logr.Discard()
		g := newGkeLifecycleProvider(&controller.Reconciler{JetStreamContext: js}, inst, &log)

		require.NoError(t, g.PublishDeleteNotification())

		msgs := js.messages()
		require.Len(t, msgs, 1)
		assert.Equal(t, notif.GcpGkeKubernetesRuntimeInstanceDeleteSubject, msgs[0].subject)
		var n notifications.Notification
		require.NoError(t, json.Unmarshal(msgs[0].data, &n))
		assert.EqualValues(t, notifications.NotificationOperationDeleted, n.Operation)
	})

	t.Run("publish error", func(t *testing.T) {
		js := &gkeFakeJetStream{publishErr: fmt.Errorf("nats down")}
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		log := logr.Discard()
		g := newGkeLifecycleProvider(&controller.Reconciler{JetStreamContext: js}, inst, &log)

		assert.ErrorContains(t, g.PublishDeleteNotification(), "failed to publish delete notification")
	})
}

// TestGkeInstanceUpdatedNoop asserts the Updated reconciler returns delay 0.
func TestGkeInstanceUpdatedNoop(t *testing.T) {
	inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
	log := logr.Discard()
	delay, err := v0GcpGkeKubernetesRuntimeInstanceUpdated(&controller.Reconciler{}, inst, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// TestGkeInstanceCreatedConfirmedNoop asserts Created short-circuits once CreationConfirmed is set.
func TestGkeInstanceCreatedConfirmedNoop(t *testing.T) {
	api := machinetest.NewAPIStub(t)
	inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
	inst.CreationConfirmed = util.Ptr(time.Now().UTC())
	gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)

	log := logr.Discard()
	delay, err := v0GcpGkeKubernetesRuntimeInstanceCreated(gkeStubReconciler(api), inst, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// TestGkeInstanceDeletedConfirmedNoop asserts Deleted short-circuits once DeletionConfirmed is set.
func TestGkeInstanceDeletedConfirmedNoop(t *testing.T) {
	api := machinetest.NewAPIStub(t)
	inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
	inst.DeletionScheduled = util.Ptr(time.Now().UTC())
	inst.DeletionConfirmed = util.Ptr(time.Now().UTC())
	gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)

	log := logr.Discard()
	delay, err := v0GcpGkeKubernetesRuntimeInstanceDeleted(gkeStubReconciler(api), inst, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// TestGkeInstanceDeletedNotScheduled rejects a delete notification without DeletionScheduled.
func TestGkeInstanceDeletedNotScheduled(t *testing.T) {
	api := machinetest.NewAPIStub(t)
	inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
	gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)

	log := logr.Discard()
	delay, err := v0GcpGkeKubernetesRuntimeInstanceDeleted(gkeStubReconciler(api), inst, &log)
	assert.Equal(t, int64(0), delay)
	assert.ErrorContains(t, err, "deletion notification received but not scheduled")
}

// TestGkeLifecycleConcurrentInstancesNoRace constructs N adapters for distinct
// instance IDs and drives their stateless methods concurrently. The adapter
// holds no shared mutable state: there is no data race under -race and no
// cross-instance bleed (each goroutine's PATCH carries its own ID). Semaphore-
// cap, goroutine-leak, and stale-ack behavior live in the shared handler and
// are not asserted here.
func TestGkeLifecycleConcurrentInstancesNoRace(t *testing.T) {
	const instanceCount = 200

	api := machinetest.NewAPIStub(t)
	rec := &gkePatchRecorder{}
	// register one handler for all instance IDs, returning each ID on GET and
	// recording PATCH bodies under its own path
	api.Mux.HandleFunc(v0.PathGcpGkeKubernetesRuntimeInstances+"/", func(w http.ResponseWriter, r *http.Request) {
		id, ok := gkeParsePathID(r.URL.Path)
		if !ok {
			http.Error(w, "bad instance id", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodGet:
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{
				*gkeTestInstance(id, fmt.Sprintf("gke-%d", id)),
			})
		case http.MethodPatch:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			rec.add(r.URL.Path, body)
			var updated v0.GcpGkeKubernetesRuntimeInstance
			if err := json.Unmarshal(body, &updated); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			updated.ID = util.Ptr(id)
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{updated})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	var wg sync.WaitGroup
	errCh := make(chan error, instanceCount*4)
	for i := 0; i < instanceCount; i++ {
		id := uint(i + 1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			inst := gkeTestInstance(id, fmt.Sprintf("gke-%d", id))
			g := gkeNewLifecycle(api, inst)
			if _, err := g.GetReconciliation(); err != nil {
				errCh <- err
				return
			}
			if err := g.AckCreation(); err != nil {
				errCh <- err
				return
			}
			state := datatypes.JSON([]byte(fmt.Sprintf(`{"id":%d}`, id)))
			if err := g.SaveState(&state); err != nil {
				errCh <- err
				return
			}
			if err := g.ConfirmCreation(); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	// every goroutine PATCHed three times, and each saved state carries the ID
	// from its own URL path; a mismatch would indicate cross-instance bleed
	patches := rec.snapshot()
	assert.Len(t, patches, instanceCount*3)
	for _, p := range patches {
		id, ok := gkeParsePathID(p.path)
		require.True(t, ok)
		var patched v0.GcpGkeKubernetesRuntimeInstance
		require.NoError(t, json.Unmarshal(p.body, &patched))
		if patched.ResourceInventory != nil {
			assert.JSONEq(t, fmt.Sprintf(`{"id":%d}`, id), string(*patched.ResourceInventory))
		}
	}
}
