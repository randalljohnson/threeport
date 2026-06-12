package oci

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	logr "github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	notif "github.com/threeport/threeport/internal/oci/notif"
	"github.com/threeport/threeport/internal/provider"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// Coverage boundaries in this file (observe-and-requeue model):
//
// The concrete OCI methods on the OKE infra type (connection fetch, cluster
// OCID lookup, compartment delete) and the compartment listing inside the
// infra build live behind the real OCI SDK with context.Background() and no
// timeout. Their success paths and everything after them are integration-only;
// unit tests here exercise only the LOCAL failure boundary using
// okeLocalFailConfigProvider() so no test ever opens a socket.
//
// Known-unreachable branch, documented rather than tested: the notification-
// payload error in both publish methods only fires when JSON marshalling of a
// pointer/string struct fails, which cannot be forced.

// TestOkeGetReconciliation_Success asserts the reconciliation snapshot maps the
// durable lifecycle fields from the instance returned by the API. The ack and
// heartbeat timestamps are gone in the observe model, so the snapshot carries
// only confirmation, scheduling, the failure flag, the reconciled flag, and the
// inventory.
func TestOkeGetReconciliation_Success(t *testing.T) {
	baseTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	creationConfirm := baseTime.Add(1 * time.Minute)
	deletionScheduled := baseTime.Add(2 * time.Minute)
	deletionConfirm := baseTime.Add(4 * time.Minute)
	inventory := datatypes.JSON([]byte(`{"vcn":"test-vcn-resource"}`))

	tests := []struct {
		name   string
		mutate func(inst *v0.OciOkeKubernetesRuntimeInstance)
		check  func(t *testing.T, snap *provider.ReconciliationSnapshot)
	}{
		{
			name: "AllFieldsSet",
			mutate: func(inst *v0.OciOkeKubernetesRuntimeInstance) {
				inst.CreationConfirmed = &creationConfirm
				inst.Reconciled = util.Ptr(true)
				inst.DeletionScheduled = &deletionScheduled
				inst.DeletionConfirmed = &deletionConfirm
				inst.ResourceInventory = &inventory
			},
			check: func(t *testing.T, snap *provider.ReconciliationSnapshot) {
				assert.True(t, snap.Reconciled)
				assert.Equal(t, &creationConfirm, snap.CreationConfirmed)
				assert.False(t, snap.CreationFailed)
				assert.Equal(t, &deletionScheduled, snap.DeletionScheduled)
				assert.Equal(t, &deletionConfirm, snap.DeletionConfirmed)
				require.NotNil(t, snap.ResourceInventory)
				assert.JSONEq(t, string(inventory), string(*snap.ResourceInventory))
			},
		},
		{
			name:   "NoFieldsSet",
			mutate: func(inst *v0.OciOkeKubernetesRuntimeInstance) {},
			check: func(t *testing.T, snap *provider.ReconciliationSnapshot) {
				assert.False(t, snap.Reconciled)
				assert.Nil(t, snap.CreationConfirmed)
				assert.False(t, snap.CreationFailed)
				assert.Nil(t, snap.DeletionScheduled)
				assert.Nil(t, snap.DeletionConfirmed)
				assert.Nil(t, snap.ResourceInventory)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := okeInstance(42, "oke-snapshot")
			tt.mutate(inst)
			api := okeNewAPIStub(t)
			okeServeInstance(t, api, inst)
			log := logr.Discard()
			o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

			snap, err := o.GetReconciliation()
			require.NoError(t, err)
			tt.check(t, snap)
		})
	}
}

// TestOkeGetReconciliation_CreationFailedNilVsSet asserts a nil CreationFailed
// defaults to false in the snapshot and a set value propagates.
func TestOkeGetReconciliation_CreationFailedNilVsSet(t *testing.T) {
	tests := []struct {
		name           string
		creationFailed *bool
		want           bool
	}{
		{"NilDefaultsFalse", nil, false},
		{"SetTruePropagates", util.Ptr(true), true},
		{"SetFalsePropagates", util.Ptr(false), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := okeInstance(7, "oke-creation-failed")
			inst.CreationFailed = tt.creationFailed
			api := okeNewAPIStub(t)
			okeServeInstance(t, api, inst)
			log := logr.Discard()
			o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

			snap, err := o.GetReconciliation()
			require.NoError(t, err)
			assert.Equal(t, tt.want, snap.CreationFailed)
		})
	}
}

// TestOkeGetReconciliation_APIError asserts the client error is wrapped.
func TestOkeGetReconciliation_APIError(t *testing.T) {
	inst := okeInstance(8, "oke-snapshot-error")
	api := okeNewAPIStub(t)
	okeServeError(t, api, okeInstancePath(8), http.StatusInternalServerError)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	snap, err := o.GetReconciliation()
	require.Error(t, err)
	assert.Nil(t, snap)
	assert.Contains(t, err.Error(), "failed to get latest OKE instance")
}

// TestOkeUpdateReconciliation covers the single-PATCH write that replaced the
// per-field setters: the adapter sends one PATCH to the instance endpoint
// carrying the snapshot's lifecycle fields, the boolean reconciliation flags
// are always written, and a PATCH error propagates.
func TestOkeUpdateReconciliation(t *testing.T) {
	t.Run("create confirmation snapshot", func(t *testing.T) {
		inst := okeInstance(14, "oke-confirm-creation")
		api := okeNewAPIStub(t)
		rec := okeServeInstance(t, api, inst)
		log := logr.Discard()
		o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

		now := time.Now().UTC()
		inventory := datatypes.JSON([]byte(`{"vcn":"test-vcn-ocid"}`))
		require.NoError(t, o.UpdateReconciliation(provider.ReconciliationSnapshot{
			ResourceInventory: &inventory,
			CreationConfirmed: &now,
			Reconciled:        true,
		}))

		bodies := rec.patchBodies()
		require.Len(t, bodies, 1)
		assert.Contains(t, bodies[0], `"Reconciled":true`)
		assert.Contains(t, bodies[0], `"CreationConfirmed"`)
		assert.Contains(t, bodies[0], `"vcn":"test-vcn-ocid"`)
	})

	t.Run("provisioning state snapshot", func(t *testing.T) {
		inst := okeInstance(18, "oke-save-state")
		api := okeNewAPIStub(t)
		rec := okeServeInstance(t, api, inst)
		log := logr.Discard()
		o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

		state := datatypes.JSON([]byte(`{"vcn":"in-progress"}`))
		require.NoError(t, o.UpdateReconciliation(provider.ReconciliationSnapshot{
			ResourceInventory: &state,
		}))

		bodies := rec.patchBodies()
		require.Len(t, bodies, 1)
		assert.Contains(t, bodies[0], `"ResourceInventory"`)
		assert.Contains(t, bodies[0], `"vcn":"in-progress"`)
		// an in-progress write carries the zero reconciliation flags
		assert.Contains(t, bodies[0], `"Reconciled":false`)
		assert.Contains(t, bodies[0], `"CreationFailed":false`)
	})

	t.Run("failure snapshot", func(t *testing.T) {
		inst := okeInstance(13, "oke-set-creation-failed")
		api := okeNewAPIStub(t)
		rec := okeServeInstance(t, api, inst)
		log := logr.Discard()
		o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

		require.NoError(t, o.UpdateReconciliation(provider.ReconciliationSnapshot{
			CreationFailed: true,
		}))

		bodies := rec.patchBodies()
		require.Len(t, bodies, 1)
		assert.Contains(t, bodies[0], `"CreationFailed":true`)
	})

	t.Run("delete confirmation snapshot clears inventory", func(t *testing.T) {
		inst := okeInstance(19, "oke-clear-inventory")
		api := okeNewAPIStub(t)
		rec := okeServeInstance(t, api, inst)
		log := logr.Discard()
		o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

		now := time.Now().UTC()
		cleared := datatypes.JSON([]byte("{}"))
		require.NoError(t, o.UpdateReconciliation(provider.ReconciliationSnapshot{
			ResourceInventory: &cleared,
			DeletionConfirmed: &now,
		}))

		bodies := rec.patchBodies()
		require.Len(t, bodies, 1)
		assert.Contains(t, bodies[0], `"ResourceInventory":{}`)
		assert.Contains(t, bodies[0], `"DeletionConfirmed"`)
	})

	t.Run("update error", func(t *testing.T) {
		inst := okeInstance(11, "oke-update-error")
		api := okeNewAPIStub(t)
		okeServeError(t, api, okeInstancePath(11), http.StatusInternalServerError)
		log := logr.Discard()
		o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

		err := o.UpdateReconciliation(provider.ReconciliationSnapshot{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to update OKE reconciliation state")
	})
}

// TestOkeOnCreateConfirmed_GetClusterOCIDError covers the first reachable OCI
// call of OnCreateConfirmed(): the cluster OCID lookup, which the adapter now
// persists before fetching connection info so the OCID is not lost when the
// create is confirmed. The local-fail config provider makes the OCI client
// constructor reject the credentials before any socket opens; the connection
// fetch, instance get, KRI get, and KRI update all sit after it and are
// integration-only. The forbid-all stub proves no API call precedes the
// failure.
func TestOkeOnCreateConfirmed_GetClusterOCIDError(t *testing.T) {
	inst := okeInstance(20, "oke-create-confirmed")
	api := okeNewAPIStub(t)
	okeForbidAPIRequests(t, api)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	err := o.OnCreateConfirmed(okeLocalFailInfra("oke-create-confirmed"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get OKE cluster OCID")
}

// TestOkeOnDeleteConfirmed_CompartmentErrorPropagates is the critical
// delete-path guard: a compartment delete failure means cloud resources are
// orphaned, so OnDeleteConfirmed() must RETURN the error, never swallow it. The
// local-fail config provider makes the OCI identity client constructor fail
// locally, so no socket opens and the method returns before the post-
// compartment cleanup. The forbid-all stub proves no API call is issued.
func TestOkeOnDeleteConfirmed_CompartmentErrorPropagates(t *testing.T) {
	inst := okeInstance(22, "oke-delete-confirmed")
	api := okeNewAPIStub(t)
	okeForbidAPIRequests(t, api)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	err := o.OnDeleteConfirmed(okeLocalFailInfra("oke-delete-confirmed"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete OCI compartment")
	assert.Contains(t, err.Error(), "failed to create identity client")
}

// TestOkePublishCreateNotification_Success asserts the create subject and a
// payload built from the in-memory instance; no API call is involved.
func TestOkePublishCreateNotification_Success(t *testing.T) {
	inst := okeInstance(30, "oke-notify-create")
	js := &okeFakeJetStream{}
	api := okeNewAPIStub(t)
	okeForbidAPIRequests(t, api)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", js), inst, &log)

	require.NoError(t, o.PublishCreateNotification())

	subjects, payloads := js.published()
	require.Equal(t, []string{notif.OciOkeKubernetesRuntimeInstanceCreateSubject}, subjects)
	require.Len(t, payloads, 1)
	assert.Contains(t, string(payloads[0]), `"Operation":"Created"`)
	assert.Contains(t, string(payloads[0]), `"oke-notify-create"`)
}

// TestOkePublishCreateNotification_PublishError asserts the publish error wrap.
func TestOkePublishCreateNotification_PublishError(t *testing.T) {
	inst := okeInstance(31, "oke-notify-create-error")
	js := &okeFakeJetStream{publishErr: errors.New("nats unavailable")}
	api := okeNewAPIStub(t)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", js), inst, &log)

	err := o.PublishCreateNotification()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to publish create notification")
}

// TestOkePublishDeleteNotification_Success asserts the delete subject and
// payload operation.
func TestOkePublishDeleteNotification_Success(t *testing.T) {
	inst := okeInstance(32, "oke-notify-delete")
	js := &okeFakeJetStream{}
	api := okeNewAPIStub(t)
	okeForbidAPIRequests(t, api)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", js), inst, &log)

	require.NoError(t, o.PublishDeleteNotification())

	subjects, payloads := js.published()
	require.Equal(t, []string{notif.OciOkeKubernetesRuntimeInstanceDeleteSubject}, subjects)
	require.Len(t, payloads, 1)
	assert.Contains(t, string(payloads[0]), `"Operation":"Deleted"`)
	assert.Contains(t, string(payloads[0]), `"oke-notify-delete"`)
}

// TestOkePublishDeleteNotification_PublishError asserts the publish error wrap.
func TestOkePublishDeleteNotification_PublishError(t *testing.T) {
	inst := okeInstance(33, "oke-notify-delete-error")
	js := &okeFakeJetStream{publishErr: errors.New("nats unavailable")}
	api := okeNewAPIStub(t)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", js), inst, &log)

	err := o.PublishDeleteNotification()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to publish delete notification")
}

// TestBuildOkeInfra_ProviderGetError asserts the provider fetch error wrap.
func TestBuildOkeInfra_ProviderGetError(t *testing.T) {
	inst := okeInstance(40, "oke-build-provider-error")
	inst.OciProviderID = util.Ptr(uint(400))
	api := okeNewAPIStub(t)
	okeServeError(t, api, fmt.Sprintf("%s/%d", v0.PathOciProviders, 400), http.StatusInternalServerError)
	log := logr.Discard()

	infra, err := buildOkeInfra(okeReconciler(api, "", nil), inst, okeDefinition(41), &log)
	require.Error(t, err)
	assert.Nil(t, infra)
	assert.Contains(t, err.Error(), "failed to retrieve OCI provider by ID")
}

// TestBuildOkeInfra_DecryptError asserts the private key decrypt error wrap
// when the stored key is not valid ciphertext for the reconciler's key.
func TestBuildOkeInfra_DecryptError(t *testing.T) {
	key := okeNewEncryptionKey(t)
	inst := okeInstance(42, "oke-build-decrypt-error")
	inst.OciProviderID = util.Ptr(uint(402))
	prov := okeProvider(t, 402, key)
	prov.PrivateKey = util.Ptr("not-valid-ciphertext")
	api := okeNewAPIStub(t)
	okeServeGet(t, api, fmt.Sprintf("%s/%d", v0.PathOciProviders, 402), *prov)
	log := logr.Discard()

	infra, err := buildOkeInfra(okeReconciler(api, key, nil), inst, okeDefinition(43), &log)
	require.Error(t, err)
	assert.Nil(t, infra)
	assert.Contains(t, err.Error(), "failed to decrypt OCI provider private key")
}

// TestBuildOkeInfra_RegionFallback drives all three region-selection variants
// through the provider-fetch, decrypt, and region-resolution phases. The
// selected region is not observable from a unit test: the provider's private
// key decrypts to a non-PEM plaintext, so the OCI identity client constructor
// rejects the config locally before any socket opens. Asserting the error is
// the identity-client wrap proves each variant got through region resolution
// without a nil-deref panic.
func TestBuildOkeInfra_RegionFallback(t *testing.T) {
	tests := []struct {
		name   string
		region *string
	}{
		{"NilRegionUsesProviderDefault", nil},
		{"EmptyRegionUsesProviderDefault", util.Ptr("")},
		{"InstanceRegionUsed", util.Ptr("us-ashburn-1")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := okeNewEncryptionKey(t)
			inst := okeInstance(44, "oke-build-region")
			inst.OciProviderID = util.Ptr(uint(404))
			inst.Region = tt.region
			api := okeNewAPIStub(t)
			okeServeGet(t, api, fmt.Sprintf("%s/%d", v0.PathOciProviders, 404), *okeProvider(t, 404, key))
			log := logr.Discard()

			infra, err := buildOkeInfra(okeReconciler(api, key, nil), inst, okeDefinition(45), &log)
			require.Error(t, err)
			assert.Nil(t, infra)
			assert.Contains(t, err.Error(), "failed to create identity client")
		})
	}
}

// TestBuildOkeInfra_DefinitionGetError drives the definition fetch error
// through BuildInfra(), which resolves the definition before delegating to the
// infra build.
func TestBuildOkeInfra_DefinitionGetError(t *testing.T) {
	inst := okeInstance(46, "oke-build-definition-error")
	inst.OciOkeKubernetesRuntimeDefinitionID = util.Ptr(uint(460))
	api := okeNewAPIStub(t)
	okeServeInstance(t, api, inst)
	okeServeError(t, api, fmt.Sprintf("%s/%d", v0.PathOciOkeKubernetesRuntimeDefinitions, 460), http.StatusInternalServerError)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	infra, err := o.BuildInfra()
	require.Error(t, err)
	assert.Nil(t, infra)
	assert.Contains(t, err.Error(), "failed to get OKE definition")
}

// TestOkeBuildInfra_InstanceGetError asserts the instance fetch error wrap on
// BuildInfra's first call.
func TestOkeBuildInfra_InstanceGetError(t *testing.T) {
	inst := okeInstance(47, "oke-build-instance-error")
	api := okeNewAPIStub(t)
	okeServeError(t, api, okeInstancePath(47), http.StatusInternalServerError)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	infra, err := o.BuildInfra()
	require.Error(t, err)
	assert.Nil(t, infra)
	assert.Contains(t, err.Error(), "failed to get OKE instance for infra build")
}

// TestOkeEntryUpdated_NoOp asserts the update entry point is a no-op.
func TestOkeEntryUpdated_NoOp(t *testing.T) {
	inst := okeInstance(50, "oke-entry-updated")
	api := okeNewAPIStub(t)
	okeForbidAPIRequests(t, api)
	log := logr.Discard()

	delay, err := v0OciOkeKubernetesRuntimeInstanceUpdated(okeReconciler(api, "", nil), inst, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// TestOkeEntryCreated_DelegatesToHandleInfraCreate proves the create entry
// point wires the lifecycle provider into the shared state machine. The served
// instance is already creation-confirmed, so the state machine short-circuits
// right after the reconciliation fetch: no kick fires and no OCI code runs.
func TestOkeEntryCreated_DelegatesToHandleInfraCreate(t *testing.T) {
	inst := okeInstance(51, "oke-entry-created")
	confirmed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	inst.CreationConfirmed = &confirmed
	api := okeNewAPIStub(t)
	okeServeInstance(t, api, inst)
	log := logr.Discard()

	delay, err := v0OciOkeKubernetesRuntimeInstanceCreated(okeReconciler(api, "", nil), inst, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// TestOkeEntryDeleted_NotScheduledError proves the delete entry point wiring
// via the OCI-free guard: a delete notification for an instance with no
// deletion scheduled is an error.
func TestOkeEntryDeleted_NotScheduledError(t *testing.T) {
	inst := okeInstance(52, "oke-entry-deleted")
	api := okeNewAPIStub(t)
	okeServeInstance(t, api, inst)
	log := logr.Discard()

	_, err := v0OciOkeKubernetesRuntimeInstanceDeleted(okeReconciler(api, "", nil), inst, &log)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deletion notification received but not scheduled")
}

// TestOkeEntryDeleted_AlreadyConfirmed asserts a deletion-confirmed instance
// short-circuits the delete state machine with no OCI work.
func TestOkeEntryDeleted_AlreadyConfirmed(t *testing.T) {
	inst := okeInstance(53, "oke-entry-deleted-confirmed")
	scheduled := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	confirmed := scheduled.Add(time.Minute)
	inst.DeletionScheduled = &scheduled
	inst.DeletionConfirmed = &confirmed
	api := okeNewAPIStub(t)
	okeServeInstance(t, api, inst)
	log := logr.Discard()

	delay, err := v0OciOkeKubernetesRuntimeInstanceDeleted(okeReconciler(api, "", nil), inst, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}
