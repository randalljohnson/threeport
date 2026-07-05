package gcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	notif "github.com/threeport/threeport/internal/gcp/notif"
	"github.com/threeport/threeport/internal/machinetest"
	"github.com/threeport/threeport/internal/provider"
	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// makeInstance returns a minimally-populated GcpGkeKubernetesRuntimeInstance
// with the fields the lifecycle helpers touch: ID, Name, Region, and the FK
// pointers to the definition, provider, and kubernetes runtime instance.
func makeInstance(id uint) *v0.GcpGkeKubernetesRuntimeInstance {
	return &v0.GcpGkeKubernetesRuntimeInstance{
		Common:                              v0.Common{ID: util.Ptr(id)},
		Instance:                            v0.Instance{Name: util.Ptr("gke-fixture")},
		Region:                              util.Ptr("us-central1"),
		GcpProviderID:                       util.Ptr(uint(11)),
		GcpGkeKubernetesRuntimeDefinitionID: util.Ptr(uint(22)),
		KubernetesRuntimeInstanceID:         util.Ptr(uint(33)),
	}
}

// newTestLifecycle wires a gkeLifecycle backed by the supplied APIStub. Tests
// register per-path handlers on the stub before invoking lifecycle methods.
func newTestLifecycle(t *testing.T, api *machinetest.APIStub, instance *v0.GcpGkeKubernetesRuntimeInstance) *gkeLifecycle {
	t.Helper()
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient: api.Client,
		APIServer: api.Addr,
	}
	return newGkeLifecycleProvider(r, instance, &log)
}

// TestNewGkeLifecycleProvider covers that the constructor copies the instance
// pointer and unwraps the ID, so subsequent lifecycle calls can target the
// right record.
func TestNewGkeLifecycleProvider(t *testing.T) {
	// build a minimal instance and reconciler
	instance := makeInstance(42)
	log := logr.Discard()
	r := &controller.Reconciler{}

	// invoke the constructor
	got := newGkeLifecycleProvider(r, instance, &log)

	// assert every field on the lifecycle struct is populated
	require.NotNil(t, got)
	assert.Equal(t, uint(42), got.instanceID)
	assert.Same(t, instance, got.instance)
	assert.Same(t, r, got.r)
	assert.Same(t, &log, got.log)
}

// TestGetReconciliation_HappyPath covers the successful path: the client
// fetches the instance, and every reconciliation field is copied into the
// snapshot including the CreationFailed pointer being dereferenced.
func TestGetReconciliation_HappyPath(t *testing.T) {
	// arrange: register a stub that returns an instance with a full set of
	// reconciliation timestamps and CreationFailed=true
	api := machinetest.NewAPIStub(t)
	instance := makeInstance(1)
	inventory := datatypes.JSON([]byte(`{"key":"value"}`))
	stored := *instance
	stored.Reconciliation = v0.Reconciliation{
		CreationAcknowledged: util.Ptr(mustTime(t, "2026-01-01T00:00:00Z")),
		CreationConfirmed:    util.Ptr(mustTime(t, "2026-01-02T00:00:00Z")),
		CreationFailed:       util.Ptr(true),
		DeletionScheduled:    util.Ptr(mustTime(t, "2026-01-03T00:00:00Z")),
		DeletionAcknowledged: util.Ptr(mustTime(t, "2026-01-04T00:00:00Z")),
		DeletionConfirmed:    util.Ptr(mustTime(t, "2026-01-05T00:00:00Z")),
	}
	stored.ResourceInventory = &inventory
	api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeInstances, 1),
		func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodGet, r.Method)
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{stored})
		},
	)

	// act: fetch the snapshot
	lc := newTestLifecycle(t, api, instance)
	snap, err := lc.GetReconciliation()

	// assert: no error and every field surfaced
	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.True(t, snap.CreationFailed)
	assert.NotNil(t, snap.CreationAcknowledged)
	assert.NotNil(t, snap.CreationConfirmed)
	assert.NotNil(t, snap.DeletionScheduled)
	assert.NotNil(t, snap.DeletionAcknowledged)
	assert.NotNil(t, snap.DeletionConfirmed)
	require.NotNil(t, snap.ResourceInventory)
	assert.JSONEq(t, `{"key":"value"}`, string(*snap.ResourceInventory))
}

// TestGetReconciliation_NilCreationFailed covers that when CreationFailed is
// absent (nil), the snapshot defaults it to false rather than dereferencing
// a nil pointer.
func TestGetReconciliation_NilCreationFailed(t *testing.T) {
	// arrange: instance has no CreationFailed set
	api := machinetest.NewAPIStub(t)
	instance := makeInstance(2)
	api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeInstances, 2),
		func(w http.ResponseWriter, r *http.Request) {
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{*instance})
		},
	)

	// act
	lc := newTestLifecycle(t, api, instance)
	snap, err := lc.GetReconciliation()

	// assert: CreationFailed defaulted to false, no panic
	require.NoError(t, err)
	assert.False(t, snap.CreationFailed)
}

// TestGetReconciliation_APIError covers the error path: when the API returns
// a non-200 status, the error is wrapped with the "failed to get latest GKE
// instance" prefix.
func TestGetReconciliation_APIError(t *testing.T) {
	// arrange: server returns 500 on the GET path
	api := machinetest.NewAPIStub(t)
	instance := makeInstance(3)
	api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeInstances, 3),
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"Status":{"code":500,"message":"boom","error":"boom"}}`))
		},
	)

	// act
	lc := newTestLifecycle(t, api, instance)
	snap, err := lc.GetReconciliation()

	// assert: error wrapped, snapshot nil
	require.Error(t, err)
	assert.Nil(t, snap)
	assert.Contains(t, err.Error(), "failed to get latest GKE instance")
}

// TestIsCreateComplete covers the inventory-vs-complete truth table:
// nil, empty, "{}", "null" all read incomplete; a populated JSON reads
// complete.
func TestIsCreateComplete(t *testing.T) {
	tests := []struct {
		name      string
		inventory *datatypes.JSON
		want      bool
	}{
		{name: "nil inventory reads incomplete", inventory: nil, want: false},
		{name: "brace-empty inventory reads incomplete", inventory: ptrJSON("{}"), want: false},
		{name: "literal null inventory reads incomplete", inventory: ptrJSON("null"), want: false},
		{name: "populated inventory reads complete", inventory: ptrJSON(`{"stack":"ok"}`), want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// arrange: stub returns an instance whose ResourceInventory matches
			// the test row
			api := machinetest.NewAPIStub(t)
			instance := makeInstance(4)
			stored := *instance
			stored.ResourceInventory = tc.inventory
			api.Mux.HandleFunc(
				fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeInstances, 4),
				func(w http.ResponseWriter, r *http.Request) {
					machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{stored})
				},
			)

			// act
			lc := newTestLifecycle(t, api, instance)
			got, err := lc.IsCreateComplete()

			// assert
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestIsCreateComplete_APIError covers that a failed GET wraps the error
// with the "failed to check GKE creation status" prefix and returns false.
func TestIsCreateComplete_APIError(t *testing.T) {
	// arrange: server returns 500
	api := machinetest.NewAPIStub(t)
	instance := makeInstance(5)
	api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeInstances, 5),
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"Status":{"code":500,"message":"boom","error":"boom"}}`))
		},
	)

	// act
	lc := newTestLifecycle(t, api, instance)
	done, err := lc.IsCreateComplete()

	// assert
	require.Error(t, err)
	assert.False(t, done)
	assert.Contains(t, err.Error(), "failed to check GKE creation status")
}

// TestOnDeleteConfirmed covers the no-op contract: it returns nil regardless
// of the infra provider argument (including nil).
func TestOnDeleteConfirmed(t *testing.T) {
	// arrange: bare lifecycle, no API interactions expected
	api := machinetest.NewAPIStub(t)
	lc := newTestLifecycle(t, api, makeInstance(6))

	// act + assert: nil infra, nil error
	assert.NoError(t, lc.OnDeleteConfirmed(nil))
}

// TestBuildInfra_HappyPath covers the successful build: instance, definition,
// and provider all fetched, and buildGkeInfra composes them into a
// KubernetesRuntimeInfraGKE with the expected fields.
func TestBuildInfra_HappyPath(t *testing.T) {
	// arrange: stubs for all three API objects
	api := machinetest.NewAPIStub(t)
	instance := makeInstance(7)
	def := &v0.GcpGkeKubernetesRuntimeDefinition{
		Common:                      v0.Common{ID: util.Ptr(uint(22))},
		DefaultNodeGroupInitialSize: util.Ptr(3),
	}
	provider := &v0.GcpProvider{
		Common:                    v0.Common{ID: util.Ptr(uint(11))},
		ProjectID:                 util.Ptr("proj-x"),
		ServiceAccountCredentials: util.Ptr("json-creds"),
	}
	registerStubs(t, api, instance, def, provider)

	// act
	lc := newTestLifecycle(t, api, instance)
	got, err := lc.BuildInfra()

	// assert: correct concrete type and field mapping
	require.NoError(t, err)
	infraGKE, ok := got.(*infraGKEType)
	require.True(t, ok)
	assert.Equal(t, "proj-x", infraGKE.ProjectID)
	assert.Equal(t, "us-central1", infraGKE.Region)
	assert.Equal(t, int32(3), infraGKE.WorkerNodeInitialCount)
	assert.Equal(t, "json-creds", infraGKE.ServiceAccountCredentials)
	assert.Equal(t, "gke-fixture", infraGKE.RuntimeInstanceName)
}

// TestBuildInfra_InstanceLookupError covers the first-hop failure: the GET
// for the instance fails, so BuildInfra returns the wrapped
// "failed to get GKE instance for infra build" error.
func TestBuildInfra_InstanceLookupError(t *testing.T) {
	// arrange: GET returns 500 for the instance path
	api := machinetest.NewAPIStub(t)
	instance := makeInstance(8)
	api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeInstances, 8),
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"Status":{"code":500,"error":"boom"}}`))
		},
	)

	// act
	lc := newTestLifecycle(t, api, instance)
	got, err := lc.BuildInfra()

	// assert
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "failed to get GKE instance for infra build")
}

// TestBuildInfra_DefinitionLookupError covers the second-hop failure: the
// instance fetches fine but the definition lookup fails, and the error is
// wrapped with the "failed to get GKE definition" prefix.
func TestBuildInfra_DefinitionLookupError(t *testing.T) {
	// arrange: instance stub returns OK, definition returns 500
	api := machinetest.NewAPIStub(t)
	instance := makeInstance(9)
	api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeInstances, 9),
		func(w http.ResponseWriter, r *http.Request) {
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{*instance})
		},
	)
	api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeDefinitions, 22),
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"Status":{"code":500,"error":"boom"}}`))
		},
	)

	// act
	lc := newTestLifecycle(t, api, instance)
	got, err := lc.BuildInfra()

	// assert
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "failed to get GKE definition")
}

// TestBuildGkeInfra_ProviderLookupError covers the third-hop failure inside
// buildGkeInfra: the provider lookup fails, wrapped as
// "failed to retrieve GCP provider by ID".
func TestBuildGkeInfra_ProviderLookupError(t *testing.T) {
	// arrange: provider path returns 500; instance/definition are unused
	// because we call buildGkeInfra directly with the objects we already have
	api := machinetest.NewAPIStub(t)
	api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGcpProviders, 11),
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"Status":{"code":500,"error":"boom"}}`))
		},
	)
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient: api.Client,
		APIServer: api.Addr,
	}
	instance := makeInstance(10)
	def := &v0.GcpGkeKubernetesRuntimeDefinition{
		Common:                      v0.Common{ID: util.Ptr(uint(22))},
		DefaultNodeGroupInitialSize: util.Ptr(2),
	}

	// act: call the unexported helper directly
	got, err := buildGkeInfra(r, instance, def, &log)

	// assert
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "failed to retrieve GCP provider by ID")
}

// TestBuildGkeInfra_EmptyServiceAccountCredentials covers the credentials
// branch: when the provider stores empty credentials, buildGkeInfra leaves
// the field on the infra object empty rather than setting the string.
func TestBuildGkeInfra_EmptyServiceAccountCredentials(t *testing.T) {
	// arrange: provider returns an empty ServiceAccountCredentials pointer
	api := machinetest.NewAPIStub(t)
	provider := &v0.GcpProvider{
		Common:                    v0.Common{ID: util.Ptr(uint(11))},
		ProjectID:                 util.Ptr("proj-y"),
		ServiceAccountCredentials: util.Ptr(""),
	}
	api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGcpProviders, 11),
		func(w http.ResponseWriter, r *http.Request) {
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{*provider})
		},
	)
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient: api.Client,
		APIServer: api.Addr,
	}
	instance := makeInstance(11)
	def := &v0.GcpGkeKubernetesRuntimeDefinition{
		Common:                      v0.Common{ID: util.Ptr(uint(22))},
		DefaultNodeGroupInitialSize: util.Ptr(1),
	}

	// act
	got, err := buildGkeInfra(r, instance, def, &log)

	// assert: credentials remain unset while ProjectID still flows through
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "", got.ServiceAccountCredentials)
	assert.Equal(t, "proj-y", got.ProjectID)
	assert.Equal(t, int32(1), got.WorkerNodeInitialCount)
}

// TestSaveCreateOutputs covers persisting the final Pulumi state: the client
// issues a PATCH carrying the ResourceInventory blob, and the recorded body
// contains the expected JSON.
func TestSaveCreateOutputs(t *testing.T) {
	// arrange: PATCH handler captures the body
	api := machinetest.NewAPIStub(t)
	instance := makeInstance(20)
	body := captureLastPatchBody(t, api, 20, instance)

	// act
	lc := newTestLifecycle(t, api, instance)
	state := datatypes.JSON([]byte(`{"resources":[1,2]}`))
	err := lc.SaveCreateOutputs(nil, &state)

	// assert: no error and the persisted body echoes the inventory bytes
	require.NoError(t, err)
	assert.Contains(t, string(body.get()), `"resources":[1,2]`)
}

// TestSaveCreateOutputs_APIError covers the error path: a 500 on PATCH is
// wrapped with the "failed to update GKE instance with resource inventory"
// prefix.
func TestSaveCreateOutputs_APIError(t *testing.T) {
	// arrange: PATCH returns 500
	api := machinetest.NewAPIStub(t)
	instance := makeInstance(21)
	api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeInstances, 21),
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"Status":{"code":500,"error":"boom"}}`))
		},
	)

	// act
	lc := newTestLifecycle(t, api, instance)
	state := datatypes.JSON([]byte(`{}`))
	err := lc.SaveCreateOutputs(nil, &state)

	// assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update GKE instance with resource inventory")
}

// TestPatchHelpers covers every method that issues a single PATCH to update a
// specific reconciliation field. The table asserts the invoker returns no
// error and that the persisted body contains the expected marker JSON
// fragment.
func TestPatchHelpers(t *testing.T) {
	tests := []struct {
		name    string
		call    func(lc *gkeLifecycle) error
		wantSub string
	}{
		{
			name:    "AckCreation sets CreationAcknowledged and clears CreationFailed",
			call:    func(lc *gkeLifecycle) error { return lc.AckCreation() },
			wantSub: `"CreationFailed":false`,
		},
		{
			name:    "RefreshCreationAck touches CreationAcknowledged only",
			call:    func(lc *gkeLifecycle) error { return lc.RefreshCreationAck() },
			wantSub: `"CreationAcknowledged"`,
		},
		{
			name:    "SetCreationFailed marks CreationFailed=true",
			call:    func(lc *gkeLifecycle) error { return lc.SetCreationFailed() },
			wantSub: `"CreationFailed":true`,
		},
		{
			name:    "ConfirmCreation flips Reconciled=true",
			call:    func(lc *gkeLifecycle) error { return lc.ConfirmCreation() },
			wantSub: `"Reconciled":true`,
		},
		{
			name:    "AckDeletion sets DeletionAcknowledged",
			call:    func(lc *gkeLifecycle) error { return lc.AckDeletion() },
			wantSub: `"DeletionAcknowledged"`,
		},
		{
			name:    "RefreshDeletionAck touches DeletionAcknowledged only",
			call:    func(lc *gkeLifecycle) error { return lc.RefreshDeletionAck() },
			wantSub: `"DeletionAcknowledged"`,
		},
		{
			name:    "ConfirmDeletion sets DeletionConfirmed",
			call:    func(lc *gkeLifecycle) error { return lc.ConfirmDeletion() },
			wantSub: `"DeletionConfirmed"`,
		},
		{
			name:    "SaveState persists ResourceInventory bytes",
			call:    func(lc *gkeLifecycle) error { state := datatypes.JSON([]byte(`{"marker":"save"}`)); return lc.SaveState(&state) },
			wantSub: `"marker":"save"`,
		},
		{
			name:    "ClearInventory writes an empty JSON object",
			call:    func(lc *gkeLifecycle) error { return lc.ClearInventory() },
			wantSub: `"ResourceInventory":{}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// arrange: a fresh stub for each row, PATCH handler captures the body
			api := machinetest.NewAPIStub(t)
			instance := makeInstance(30)
			body := captureLastPatchBody(t, api, 30, instance)

			// act
			lc := newTestLifecycle(t, api, instance)
			err := tc.call(lc)

			// assert: no error and the request body contains the marker
			require.NoError(t, err)
			assert.Contains(t, string(body.get()), tc.wantSub)
		})
	}
}

// TestPatchHelpers_APIError covers that every PATCH-driven method surfaces
// the client error verbatim when the API returns 500.
func TestPatchHelpers_APIError(t *testing.T) {
	calls := map[string]func(lc *gkeLifecycle) error{
		"AckCreation":        func(lc *gkeLifecycle) error { return lc.AckCreation() },
		"RefreshCreationAck": func(lc *gkeLifecycle) error { return lc.RefreshCreationAck() },
		"SetCreationFailed":  func(lc *gkeLifecycle) error { return lc.SetCreationFailed() },
		"ConfirmCreation":    func(lc *gkeLifecycle) error { return lc.ConfirmCreation() },
		"AckDeletion":        func(lc *gkeLifecycle) error { return lc.AckDeletion() },
		"RefreshDeletionAck": func(lc *gkeLifecycle) error { return lc.RefreshDeletionAck() },
		"ConfirmDeletion":    func(lc *gkeLifecycle) error { return lc.ConfirmDeletion() },
		"SaveState": func(lc *gkeLifecycle) error {
			state := datatypes.JSON([]byte(`{}`))
			return lc.SaveState(&state)
		},
		"ClearInventory": func(lc *gkeLifecycle) error { return lc.ClearInventory() },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			// arrange: PATCH handler returns 500
			api := machinetest.NewAPIStub(t)
			instance := makeInstance(31)
			api.Mux.HandleFunc(
				fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeInstances, 31),
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"Status":{"code":500,"error":"boom"}}`))
				},
			)

			// act
			lc := newTestLifecycle(t, api, instance)
			err := call(lc)

			// assert: caller returns the wrapped API error
			require.Error(t, err)
		})
	}
}

// TestPublishCreateNotification covers the happy path: the payload is
// generated from the instance and published on the create subject.
func TestPublishCreateNotification(t *testing.T) {
	// arrange: fake JetStream captures Publish invocations
	fake := &fakeJetStream{}
	instance := makeInstance(40)
	log := logr.Discard()
	lc := &gkeLifecycle{
		r:          &controller.Reconciler{JetStreamContext: fake},
		instanceID: *instance.ID,
		instance:   instance,
		log:        &log,
	}

	// act
	err := lc.PublishCreateNotification()

	// assert: single publish on the create subject with a non-empty payload
	require.NoError(t, err)
	require.Len(t, fake.calls, 1)
	assert.Equal(t, notif.GcpGkeKubernetesRuntimeInstanceCreateSubject, fake.calls[0].subject)
	assert.NotEmpty(t, fake.calls[0].data)
}

// TestPublishCreateNotification_PublishError covers that a JetStream Publish
// failure surfaces the wrapped "failed to publish create notification"
// error.
func TestPublishCreateNotification_PublishError(t *testing.T) {
	// arrange: fake returns an error on Publish
	fake := &fakeJetStream{publishErr: fmt.Errorf("nats down")}
	instance := makeInstance(41)
	log := logr.Discard()
	lc := &gkeLifecycle{
		r:          &controller.Reconciler{JetStreamContext: fake},
		instanceID: *instance.ID,
		instance:   instance,
		log:        &log,
	}

	// act
	err := lc.PublishCreateNotification()

	// assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to publish create notification")
}

// TestPublishDeleteNotification covers the happy path: the payload is
// published on the delete subject.
func TestPublishDeleteNotification(t *testing.T) {
	// arrange
	fake := &fakeJetStream{}
	instance := makeInstance(42)
	log := logr.Discard()
	lc := &gkeLifecycle{
		r:          &controller.Reconciler{JetStreamContext: fake},
		instanceID: *instance.ID,
		instance:   instance,
		log:        &log,
	}

	// act
	err := lc.PublishDeleteNotification()

	// assert
	require.NoError(t, err)
	require.Len(t, fake.calls, 1)
	assert.Equal(t, notif.GcpGkeKubernetesRuntimeInstanceDeleteSubject, fake.calls[0].subject)
}

// TestPublishDeleteNotification_PublishError covers that a JetStream Publish
// failure surfaces the wrapped "failed to publish delete notification"
// error.
func TestPublishDeleteNotification_PublishError(t *testing.T) {
	// arrange
	fake := &fakeJetStream{publishErr: fmt.Errorf("nats down")}
	instance := makeInstance(43)
	log := logr.Discard()
	lc := &gkeLifecycle{
		r:          &controller.Reconciler{JetStreamContext: fake},
		instanceID: *instance.ID,
		instance:   instance,
		log:        &log,
	}

	// act
	err := lc.PublishDeleteNotification()

	// assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to publish delete notification")
}

// -- helpers ---------------------------------------------------------------

// infraGKEType aliases the concrete infra type so the assertion signature
// stays short in TestBuildInfra_HappyPath.
type infraGKEType = provider.KubernetesRuntimeInfraGKE

// ptrJSON returns a pointer to a datatypes.JSON backed by the input string.
func ptrJSON(s string) *datatypes.JSON {
	j := datatypes.JSON([]byte(s))
	return &j
}

// mustTime parses an RFC3339 timestamp for fixtures, failing the test if the
// input string is malformed.
func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return tm
}

// registerStubs registers GET handlers for the instance, definition, and
// provider paths used by BuildInfra so the whole three-hop chain resolves.
func registerStubs(
	t *testing.T,
	api *machinetest.APIStub,
	instance *v0.GcpGkeKubernetesRuntimeInstance,
	def *v0.GcpGkeKubernetesRuntimeDefinition,
	prov *v0.GcpProvider,
) {
	t.Helper()
	api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeInstances, *instance.ID),
		func(w http.ResponseWriter, r *http.Request) {
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{*instance})
		},
	)
	api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeDefinitions, *def.ID),
		func(w http.ResponseWriter, r *http.Request) {
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{*def})
		},
	)
	api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGcpProviders, *prov.ID),
		func(w http.ResponseWriter, r *http.Request) {
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{*prov})
		},
	)
}

// captureBody is a mutex-protected byte slice for recording the last PATCH
// body observed by the test stub, so assertions can inspect what the
// lifecycle actually sent to the API.
type captureBody struct {
	mu   sync.Mutex
	last []byte
}

func (c *captureBody) set(b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = append(c.last[:0], b...)
}

func (c *captureBody) get() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.last...)
}

// captureLastPatchBody registers a PATCH handler for the instance path and
// returns a captureBody the test can inspect after the call to see what the
// client actually sent.
func captureLastPatchBody(
	t *testing.T,
	api *machinetest.APIStub,
	id uint,
	instance *v0.GcpGkeKubernetesRuntimeInstance,
) *captureBody {
	t.Helper()
	body := &captureBody{}
	api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeInstances, id),
		func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodPatch, r.Method)
			buf, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			body.set(buf)
			var updated v0.GcpGkeKubernetesRuntimeInstance
			require.NoError(t, json.Unmarshal(buf, &updated))
			updated.ID = util.Ptr(id)
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{updated})
		},
	)
	return body
}

// publishCall records one Publish invocation on the fake JetStream.
type publishCall struct {
	subject string
	data    []byte
}

// fakeJetStream implements just enough of nats.JetStreamContext for the
// notification tests: Publish records calls; every other method the
// interface embeds is inherited as a nil field that panics if invoked,
// which is fine because the code under test only calls Publish.
type fakeJetStream struct {
	nats.JetStreamContext
	publishErr error
	calls      []publishCall
}

func (f *fakeJetStream) Publish(subj string, data []byte, _ ...nats.PubOpt) (*nats.PubAck, error) {
	f.calls = append(f.calls, publishCall{subject: subj, data: append([]byte(nil), data...)})
	if f.publishErr != nil {
		return nil, f.publishErr
	}
	return &nats.PubAck{}, nil
}
