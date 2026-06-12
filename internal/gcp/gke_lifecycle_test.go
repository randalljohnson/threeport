// Tests for the GKE lifecycle adapter in the observe-and-requeue model. They
// drive the real adapter against an in-process httptest stub of the threeport
// API (via internal/machinetest) plus a small fake provider.InfraProvider; no
// GCP, Pulumi, or NATS infrastructure is touched.
//
// Coverage contract (every reachable branch in the two hand-written adapter
// files in the observe model): GetReconciliation field mapping + nil-
// CreationFailed guard + GET error; BuildInfra / buildGkeInfra projection, each
// fetch error, the credentials gate, and nil-required-field validation;
// UpdateReconciliation single-PATCH body for the create-confirm, provisioning,
// failure, and delete-confirm snapshots, plus its error path; OnDeleteConfirmed
// wrong-infra-type guard; the two publish methods (success subject + publish
// error); the reconciler entry points (Created confirmed-noop, Updated noop,
// Deleted scheduled-and-confirmed noop, Deleted not-scheduled error); and
// N-instance concurrency under -race.
//
// Accepted gaps: OnCreateConfirmed and OnDeleteConfirmed reach GCP SDK clients
// (connection fetch, service-account cleanup) that need real auth and a live
// cluster, so only their type-guard and wiring are exercised here; the
// provisioning/refresh/requeue/no-double-kick behavior lives in
// internal/provider and is asserted there.
//
// Same-package collision rule (a sibling GCE adapter may add tests to this
// package): every package-level identifier here is gke-prefixed and there is no
// TestMain.
package gcp

import (
	"context"
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
	notifications "github.com/threeport/threeport/pkg/notifications/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

const (
	gkeTestInstanceID   uint = 42
	gkeTestInstanceName      = "gke-test-instance"
	gkeTestDefinitionID uint = 9
	gkeTestProviderID   uint = 5
	gkeTestKriID        uint = 77
)

// gkeFakeInfra is a non-GKE provider.InfraProvider used to drive the
// wrong-infra-type guard. It satisfies the observe-model interface with
// no-op methods.
type gkeFakeInfra struct{}

func (gkeFakeInfra) Observe(context.Context) (provider.Observation, error) {
	return provider.Observation{}, nil
}
func (gkeFakeInfra) Apply(context.Context) error             { return nil }
func (gkeFakeInfra) Destroy(context.Context) error           { return nil }
func (gkeFakeInfra) SetStackState(*datatypes.JSON) error     { return nil }
func (gkeFakeInfra) GetStackState() (*datatypes.JSON, error) { return nil, nil }

// gkePublishedMessage is one message captured by the fake jetstream context.
type gkePublishedMessage struct {
	subject string
	data    []byte
}

// gkeFakeJetStream stubs nats.JetStreamContext by embedding the interface
// (nil) and overriding only Publish, the one method the adapter calls.
type gkeFakeJetStream struct {
	nats.JetStreamContext
	mu         sync.Mutex
	published  []gkePublishedMessage
	publishErr error
}

func (f *gkeFakeJetStream) Publish(subject string, data []byte, _ ...nats.PubOpt) (*nats.PubAck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.publishErr != nil {
		return nil, f.publishErr
	}
	f.published = append(f.published, gkePublishedMessage{subject: subject, data: data})
	return &nats.PubAck{}, nil
}

func (f *gkeFakeJetStream) messages() []gkePublishedMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]gkePublishedMessage, len(f.published))
	copy(out, f.published)
	return out
}

// gkeCapturedPatch is one recorded PATCH request.
type gkeCapturedPatch struct {
	path string
	body []byte
}

// gkePatchRecorder collects PATCH requests across server goroutines.
type gkePatchRecorder struct {
	mu      sync.Mutex
	patches []gkeCapturedPatch
}

func (rec *gkePatchRecorder) add(path string, body []byte) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.patches = append(rec.patches, gkeCapturedPatch{path: path, body: body})
}

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

// gkeStubReconciler points a real Reconciler at the API stub.
func gkeStubReconciler(api *machinetest.APIStub) *controller.Reconciler {
	return &controller.Reconciler{APIClient: api.Client, APIServer: api.Addr}
}

// gkeNewLifecycle constructs the adapter under test against the stub API.
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

func TestGkeLifecycleGetReconciliation(t *testing.T) {
	t.Run("maps lifecycle fields", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		confirmTime := time.Now().UTC().Add(-2 * time.Minute)
		delSchedTime := time.Now().UTC().Add(-3 * time.Minute)
		delConfirmTime := time.Now().UTC().Add(-5 * time.Minute)
		inventory := datatypes.JSON([]byte(`{"cluster":"x"}`))
		inst.CreationConfirmed = &confirmTime
		inst.CreationFailed = util.Ptr(true)
		inst.Reconciled = util.Ptr(true)
		inst.DeletionScheduled = &delSchedTime
		inst.DeletionConfirmed = &delConfirmTime
		inst.ResourceInventory = &inventory
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)

		snap, err := gkeNewLifecycle(api, inst).GetReconciliation()
		require.NoError(t, err)

		assert.True(t, snap.Reconciled)
		require.NotNil(t, snap.CreationConfirmed)
		assert.WithinDuration(t, confirmTime, *snap.CreationConfirmed, time.Second)
		assert.True(t, snap.CreationFailed)
		require.NotNil(t, snap.DeletionScheduled)
		assert.WithinDuration(t, delSchedTime, *snap.DeletionScheduled, time.Second)
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

func TestGkeLifecycleBuildInfra(t *testing.T) {
	t.Run("happy path projects all fields", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst, def, prov := gkeBuildFixtures()
		prov.ServiceAccountCredentials = util.Ptr(`{"type":"service_account"}`)
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)
		gkeServeDefinition(t, api, def, http.StatusOK)
		gkeServeProvider(t, api, prov, http.StatusOK)

		infra, err := gkeNewLifecycle(api, inst).BuildInfra()
		require.NoError(t, err)

		infraGKE, ok := infra.(*provider.KubernetesRuntimeInfraGKE)
		require.True(t, ok)
		assert.Equal(t, gkeTestInstanceName, infraGKE.RuntimeInstanceName)
		assert.Equal(t, "proj-x", infraGKE.ProjectID)
		assert.Equal(t, "us-central1", infraGKE.Region)
		assert.Equal(t, int32(3), infraGKE.WorkerNodeInitialCount)
		assert.Equal(t, `{"type":"service_account"}`, infraGKE.ServiceAccountCredentials)
	})

	t.Run("nil credentials leave field empty", func(t *testing.T) {
		api := machinetest.NewAPIStub(t)
		inst, def, prov := gkeBuildFixtures()
		prov.ServiceAccountCredentials = nil
		gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)
		gkeServeDefinition(t, api, def, http.StatusOK)
		gkeServeProvider(t, api, prov, http.StatusOK)

		infra, err := gkeNewLifecycle(api, inst).BuildInfra()
		require.NoError(t, err)
		assert.Empty(t, infra.(*provider.KubernetesRuntimeInfraGKE).ServiceAccountCredentials)
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
}

// gkeUpdateError installs a PATCH-500 instance handler and returns the adapter,
// for asserting the single-write error path of UpdateReconciliation.
func gkeUpdateError(t *testing.T) *gkeLifecycle {
	t.Helper()
	api := machinetest.NewAPIStub(t)
	inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
	gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusInternalServerError)
	return gkeNewLifecycle(api, inst)
}

// TestGkeLifecycleUpdateReconciliation covers the single-PATCH write that
// replaced the per-field setters: the adapter sends exactly one PATCH to the
// instance endpoint carrying the snapshot's lifecycle fields, the boolean
// reconciliation flags are always written, and a PATCH error propagates.
func TestGkeLifecycleUpdateReconciliation(t *testing.T) {
	patchFor := func(t *testing.T, snap provider.ReconciliationSnapshot) v0.GcpGkeKubernetesRuntimeInstance {
		t.Helper()
		api := machinetest.NewAPIStub(t)
		rec := &gkePatchRecorder{}
		inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
		gkeServeInstances(t, api, inst, rec, http.StatusOK, http.StatusOK)

		require.NoError(t, gkeNewLifecycle(api, inst).UpdateReconciliation(snap))

		patches := rec.snapshot()
		require.Len(t, patches, 1, "UpdateReconciliation must issue exactly one PATCH")
		assert.Equal(t,
			fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeInstances, gkeTestInstanceID),
			patches[0].path,
		)
		var patched v0.GcpGkeKubernetesRuntimeInstance
		require.NoError(t, json.Unmarshal(patches[0].body, &patched))
		return patched
	}

	t.Run("create confirmation snapshot", func(t *testing.T) {
		now := time.Now().UTC()
		inventory := datatypes.JSON([]byte(`{"deployment":{"resources":[{"urn":"a"}]}}`))
		patched := patchFor(t, provider.ReconciliationSnapshot{
			ResourceInventory: &inventory,
			CreationConfirmed: &now,
			Reconciled:        true,
			CreationFailed:    false,
		})

		require.NotNil(t, patched.Reconciled)
		assert.True(t, *patched.Reconciled)
		require.NotNil(t, patched.CreationFailed)
		assert.False(t, *patched.CreationFailed)
		require.NotNil(t, patched.CreationConfirmed)
		assert.WithinDuration(t, now, *patched.CreationConfirmed, time.Second)
		require.NotNil(t, patched.ResourceInventory)
		assert.JSONEq(t, string(inventory), string(*patched.ResourceInventory))
	})

	t.Run("provisioning state snapshot", func(t *testing.T) {
		inventory := datatypes.JSON([]byte(`{"deployment":{"resources":[{"urn":"b"}]}}`))
		patched := patchFor(t, provider.ReconciliationSnapshot{
			ResourceInventory: &inventory,
		})

		require.NotNil(t, patched.ResourceInventory)
		assert.JSONEq(t, string(inventory), string(*patched.ResourceInventory))
		// an in-progress write carries the zero reconciliation flags
		require.NotNil(t, patched.Reconciled)
		assert.False(t, *patched.Reconciled)
		require.NotNil(t, patched.CreationFailed)
		assert.False(t, *patched.CreationFailed)
		assert.Nil(t, patched.CreationConfirmed)
	})

	t.Run("failure snapshot", func(t *testing.T) {
		patched := patchFor(t, provider.ReconciliationSnapshot{
			CreationFailed: true,
		})
		require.NotNil(t, patched.CreationFailed)
		assert.True(t, *patched.CreationFailed)
	})

	t.Run("delete confirmation snapshot", func(t *testing.T) {
		now := time.Now().UTC()
		cleared := datatypes.JSON([]byte("{}"))
		patched := patchFor(t, provider.ReconciliationSnapshot{
			ResourceInventory: &cleared,
			DeletionConfirmed: &now,
		})
		require.NotNil(t, patched.DeletionConfirmed)
		assert.WithinDuration(t, now, *patched.DeletionConfirmed, time.Second)
		require.NotNil(t, patched.ResourceInventory)
		assert.JSONEq(t, "{}", string(*patched.ResourceInventory))
	})

	t.Run("patch error", func(t *testing.T) {
		err := gkeUpdateError(t).UpdateReconciliation(provider.ReconciliationSnapshot{})
		assert.ErrorContains(t, err, "failed to update GKE reconciliation state")
	})
}

// TestGkeLifecycleOnDeleteConfirmedWrongInfraType drives OnDeleteConfirmed with
// a non-GKE infra provider: the comma-ok assertion returns a descriptive error
// instead of panicking, and no GCP cleanup is attempted.
func TestGkeLifecycleOnDeleteConfirmedWrongInfraType(t *testing.T) {
	api := machinetest.NewAPIStub(t)
	inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)

	err := gkeNewLifecycle(api, inst).OnDeleteConfirmed(gkeFakeInfra{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "KubernetesRuntimeInfraGKE")
}

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

func TestGkeInstanceUpdatedNoop(t *testing.T) {
	inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
	log := logr.Discard()
	delay, err := v0GcpGkeKubernetesRuntimeInstanceUpdated(&controller.Reconciler{}, inst, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

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

func TestGkeInstanceDeletedNotScheduled(t *testing.T) {
	api := machinetest.NewAPIStub(t)
	inst := gkeTestInstance(gkeTestInstanceID, gkeTestInstanceName)
	gkeServeInstances(t, api, inst, nil, http.StatusOK, http.StatusOK)

	log := logr.Discard()
	delay, err := v0GcpGkeKubernetesRuntimeInstanceDeleted(gkeStubReconciler(api), inst, &log)
	assert.Equal(t, int64(0), delay)
	assert.ErrorContains(t, err, "deletion notification received but not scheduled")
}

// TestGkeLifecycleConcurrentInstancesNoRace proves the adapter holds no shared
// mutable state: many adapters for distinct instance IDs run the fetch and
// single-PATCH update concurrently against one stub with no data race and no
// cross-instance bleed in the PATCHed bodies.
func TestGkeLifecycleConcurrentInstancesNoRace(t *testing.T) {
	const instanceCount = 200

	api := machinetest.NewAPIStub(t)
	rec := &gkePatchRecorder{}
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
	errCh := make(chan error, instanceCount*2)
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
			state := datatypes.JSON([]byte(fmt.Sprintf(`{"id":%d}`, id)))
			if err := g.UpdateReconciliation(provider.ReconciliationSnapshot{
				ResourceInventory: &state,
			}); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	// every goroutine PATCHed once, and each carries the ID from its own URL
	// path; a mismatch would indicate cross-instance bleed
	patches := rec.snapshot()
	assert.Len(t, patches, instanceCount)
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
