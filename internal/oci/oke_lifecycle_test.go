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
	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// GetConnection, GetClusterOCID, DeleteCompartment, and ListCompartments call
// the OCI SDK with context.Background and no timeout. Unit tests here stop at
// the local client constructor: okeLocalFailConfigProvider or a decrypted
// non-PEM private key, so no socket opens. Paths after those constructors are
// integration-only. NotificationPayload errors in both publish methods are
// unforced: encoding a pointer/string struct cannot fail.

// TestOkeGetReconciliation_Success covers GetReconciliation field copies when
// timestamps and inventory are set and when they are unset.
func TestOkeGetReconciliation_Success(t *testing.T) {
	baseTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	creationAck := baseTime
	creationConfirm := baseTime.Add(1 * time.Minute)
	deletionScheduled := baseTime.Add(2 * time.Minute)
	deletionAck := baseTime.Add(3 * time.Minute)
	deletionConfirm := baseTime.Add(4 * time.Minute)
	inventory := datatypes.JSON([]byte(`{"vcn":"test-vcn-resource"}`))

	tests := []struct {
		name   string
		mutate func(inst *v0.OciOkeKubernetesRuntimeInstance)
		check  func(t *testing.T, snap okeReconciliationSnapshotCheck)
	}{
		{
			name: "AllTimestampsSet",
			mutate: func(inst *v0.OciOkeKubernetesRuntimeInstance) {
				inst.CreationAcknowledged = &creationAck
				inst.CreationConfirmed = &creationConfirm
				inst.DeletionScheduled = &deletionScheduled
				inst.DeletionAcknowledged = &deletionAck
				inst.DeletionConfirmed = &deletionConfirm
				inst.ResourceInventory = &inventory
			},
			check: func(t *testing.T, snap okeReconciliationSnapshotCheck) {
				assert.Equal(t, &creationAck, snap.creationAcknowledged)
				assert.Equal(t, &creationConfirm, snap.creationConfirmed)
				assert.False(t, snap.creationFailed)
				assert.Equal(t, &deletionScheduled, snap.deletionScheduled)
				assert.Equal(t, &deletionAck, snap.deletionAcknowledged)
				assert.Equal(t, &deletionConfirm, snap.deletionConfirmed)
				require.NotNil(t, snap.resourceInventory)
				assert.JSONEq(t, string(inventory), string(*snap.resourceInventory))
			},
		},
		{
			name:   "NoTimestampsSet",
			mutate: func(inst *v0.OciOkeKubernetesRuntimeInstance) {},
			check: func(t *testing.T, snap okeReconciliationSnapshotCheck) {
				assert.Nil(t, snap.creationAcknowledged)
				assert.Nil(t, snap.creationConfirmed)
				assert.False(t, snap.creationFailed)
				assert.Nil(t, snap.deletionScheduled)
				assert.Nil(t, snap.deletionAcknowledged)
				assert.Nil(t, snap.deletionConfirmed)
				assert.Nil(t, snap.resourceInventory)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// serve mutated instance and snapshot reconciliation
			inst := okeInstance(42, "oke-snapshot")
			tt.mutate(inst)
			api := okeNewAPIStub(t)
			okeServeInstance(t, api, inst)
			log := logr.Discard()
			o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

			snap, err := o.GetReconciliation()
			require.NoError(t, err)
			tt.check(t, okeReconciliationSnapshotCheck{
				creationAcknowledged: snap.CreationAcknowledged,
				creationConfirmed:    snap.CreationConfirmed,
				creationFailed:       snap.CreationFailed,
				deletionScheduled:    snap.DeletionScheduled,
				deletionAcknowledged: snap.DeletionAcknowledged,
				deletionConfirmed:    snap.DeletionConfirmed,
				resourceInventory:    snap.ResourceInventory,
			})
		})
	}
}

// okeReconciliationSnapshotCheck is a copy of ReconciliationSnapshot fields
// so table cases can assert without importing the provider package.
type okeReconciliationSnapshotCheck struct {
	creationAcknowledged *time.Time
	creationConfirmed    *time.Time
	creationFailed       bool
	deletionScheduled    *time.Time
	deletionAcknowledged *time.Time
	deletionConfirmed    *time.Time
	resourceInventory    *datatypes.JSON
}

// TestOkeGetReconciliation_CreationFailedNilVsSet covers CreationFailed mapping
// from nil, true, and false instance values.
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
			// serve instance with CreationFailed and snapshot it
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

// TestOkeGetReconciliation_APIError covers a GET 500 wrapping as failed to get
// latest OKE instance.
func TestOkeGetReconciliation_APIError(t *testing.T) {
	// serve GET error and call GetReconciliation
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

// TestOkeIsCreateComplete covers ClusterOCID nil, empty, set, and a GET error.
func TestOkeIsCreateComplete(t *testing.T) {
	tests := []struct {
		name        string
		clusterOCID *string
		apiError    bool
		want        bool
		wantErr     string
	}{
		{name: "NoCluster", clusterOCID: nil, want: false},
		{name: "EmptyCluster", clusterOCID: util.Ptr(""), want: false},
		{name: "HasCluster", clusterOCID: util.Ptr("test-cluster-ocid"), want: true},
		{name: "APIError", apiError: true, wantErr: "failed to check OKE cluster creation status"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// serve instance or GET error and check IsCreateComplete
			inst := okeInstance(9, "oke-create-complete")
			inst.ClusterOCID = tt.clusterOCID
			api := okeNewAPIStub(t)
			if tt.apiError {
				okeServeError(t, api, okeInstancePath(9), http.StatusInternalServerError)
			} else {
				okeServeInstance(t, api, inst)
			}
			log := logr.Discard()
			o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

			complete, err := o.IsCreateComplete()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, complete)
		})
	}
}

// TestOkeAckCreation_SetsAckClearsFailed covers AckCreation patching
// CreationAcknowledged and CreationFailed false.
func TestOkeAckCreation_SetsAckClearsFailed(t *testing.T) {
	// ack creation and inspect the PATCH body
	inst := okeInstance(10, "oke-ack-creation")
	api := okeNewAPIStub(t)
	rec := okeServeInstance(t, api, inst)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	require.NoError(t, o.AckCreation())

	bodies := rec.patchBodies()
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], `"CreationAcknowledged"`)
	assert.Contains(t, bodies[0], `"CreationFailed":false`)
}

// TestOkeAckCreation_UpdateError covers AckCreation wrapping a PATCH 500.
func TestOkeAckCreation_UpdateError(t *testing.T) {
	// serve PATCH error and call AckCreation
	inst := okeInstance(11, "oke-ack-creation-error")
	api := okeNewAPIStub(t)
	okeServeError(t, api, okeInstancePath(11), http.StatusInternalServerError)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	err := o.AckCreation()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "call to threeport API returned unexpected response")
}

// TestOkeRefreshCreationAck_SetsAckOnly covers RefreshCreationAck patching
// CreationAcknowledged without CreationFailed or CreationConfirmed.
func TestOkeRefreshCreationAck_SetsAckOnly(t *testing.T) {
	// refresh creation ack and inspect the PATCH body
	inst := okeInstance(12, "oke-refresh-creation-ack")
	api := okeNewAPIStub(t)
	rec := okeServeInstance(t, api, inst)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	require.NoError(t, o.RefreshCreationAck())

	bodies := rec.patchBodies()
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], `"CreationAcknowledged"`)
	assert.NotContains(t, bodies[0], `"CreationFailed"`)
	assert.NotContains(t, bodies[0], `"CreationConfirmed"`)
}

// TestOkeSetCreationFailed_SetsTrue covers SetCreationFailed patching
// CreationFailed true without CreationAcknowledged.
func TestOkeSetCreationFailed_SetsTrue(t *testing.T) {
	// set creation failed and inspect the PATCH body
	inst := okeInstance(13, "oke-set-creation-failed")
	api := okeNewAPIStub(t)
	rec := okeServeInstance(t, api, inst)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	require.NoError(t, o.SetCreationFailed())

	bodies := rec.patchBodies()
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], `"CreationFailed":true`)
	assert.NotContains(t, bodies[0], `"CreationAcknowledged"`)
}

// TestOkeConfirmCreation_SetsReconciledAndConfirmed covers ConfirmCreation
// patching Reconciled true and CreationConfirmed.
func TestOkeConfirmCreation_SetsReconciledAndConfirmed(t *testing.T) {
	// confirm creation and inspect the PATCH body
	inst := okeInstance(14, "oke-confirm-creation")
	api := okeNewAPIStub(t)
	rec := okeServeInstance(t, api, inst)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	require.NoError(t, o.ConfirmCreation())

	bodies := rec.patchBodies()
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], `"Reconciled":true`)
	assert.Contains(t, bodies[0], `"CreationConfirmed"`)
}

// TestOkeAckDeletion_SetsTimestamp covers AckDeletion patching
// DeletionAcknowledged without DeletionConfirmed.
func TestOkeAckDeletion_SetsTimestamp(t *testing.T) {
	// ack deletion and inspect the PATCH body
	inst := okeInstance(15, "oke-ack-deletion")
	api := okeNewAPIStub(t)
	rec := okeServeInstance(t, api, inst)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	require.NoError(t, o.AckDeletion())

	bodies := rec.patchBodies()
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], `"DeletionAcknowledged"`)
	assert.NotContains(t, bodies[0], `"DeletionConfirmed"`)
}

// TestOkeRefreshDeletionAck_SetsTimestamp covers RefreshDeletionAck patching
// DeletionAcknowledged without DeletionConfirmed.
func TestOkeRefreshDeletionAck_SetsTimestamp(t *testing.T) {
	// refresh deletion ack and inspect the PATCH body
	inst := okeInstance(16, "oke-refresh-deletion-ack")
	api := okeNewAPIStub(t)
	rec := okeServeInstance(t, api, inst)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	require.NoError(t, o.RefreshDeletionAck())

	bodies := rec.patchBodies()
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], `"DeletionAcknowledged"`)
	assert.NotContains(t, bodies[0], `"DeletionConfirmed"`)
}

// TestOkeConfirmDeletion_SetsTimestamp covers ConfirmDeletion patching
// DeletionConfirmed.
func TestOkeConfirmDeletion_SetsTimestamp(t *testing.T) {
	// confirm deletion and inspect the PATCH body
	inst := okeInstance(17, "oke-confirm-deletion")
	api := okeNewAPIStub(t)
	rec := okeServeInstance(t, api, inst)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	require.NoError(t, o.ConfirmDeletion())

	bodies := rec.patchBodies()
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], `"DeletionConfirmed"`)
}

// TestOkeSaveState_PersistsInventory covers SaveState patching ResourceInventory
// with the supplied JSON.
func TestOkeSaveState_PersistsInventory(t *testing.T) {
	// save state and inspect the PATCH body
	inst := okeInstance(18, "oke-save-state")
	api := okeNewAPIStub(t)
	rec := okeServeInstance(t, api, inst)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	state := datatypes.JSON([]byte(`{"vcn":"test-vcn-ocid"}`))
	require.NoError(t, o.SaveState(&state))

	bodies := rec.patchBodies()
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], `"ResourceInventory"`)
	assert.Contains(t, bodies[0], `"vcn":"test-vcn-ocid"`)
}

// TestOkeClearInventory_SetsEmptyObject covers ClearInventory patching
// ResourceInventory to {}, the destroy-complete signal.
func TestOkeClearInventory_SetsEmptyObject(t *testing.T) {
	// clear inventory and inspect the PATCH body
	inst := okeInstance(19, "oke-clear-inventory")
	api := okeNewAPIStub(t)
	rec := okeServeInstance(t, api, inst)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	require.NoError(t, o.ClearInventory())

	bodies := rec.patchBodies()
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], `"ResourceInventory":{}`)
}

// TestOkeOnCreateConfirmed_GetConnectionError covers OnCreateConfirmed wrapping
// GetConnection's local client-constructor failure; later branches are untested.
func TestOkeOnCreateConfirmed_GetConnectionError(t *testing.T) {
	// forbid API and call OnCreateConfirmed with failing infra
	inst := okeInstance(20, "oke-create-confirmed")
	api := okeNewAPIStub(t)
	okeForbidAPIRequests(t, api)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	err := o.OnCreateConfirmed(okeLocalFailInfra("oke-create-confirmed"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get Kubernetes API connection info")
}

// TestOkeSaveCreateOutputs_GetClusterOCIDError covers SaveCreateOutputs wrapping
// GetClusterOCID's local client-constructor failure; the PATCH path is untested.
func TestOkeSaveCreateOutputs_GetClusterOCIDError(t *testing.T) {
	// forbid API and call SaveCreateOutputs with failing infra
	inst := okeInstance(21, "oke-save-outputs")
	api := okeNewAPIStub(t)
	okeForbidAPIRequests(t, api)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	state := datatypes.JSON([]byte(`{}`))
	err := o.SaveCreateOutputs(okeLocalFailInfra("oke-save-outputs"), &state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get OKE cluster OCID")
}

// TestOkeOnDeleteConfirmed_CompartmentErrorPropagates covers OnDeleteConfirmed
// returning DeleteCompartment's local identity-client failure, not swallowing it.
func TestOkeOnDeleteConfirmed_CompartmentErrorPropagates(t *testing.T) {
	// forbid API and call OnDeleteConfirmed with failing infra
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

// TestOkePublishCreateNotification_Success covers PublishCreateNotification
// publishing Operation Created from the in-memory instance with no API call.
func TestOkePublishCreateNotification_Success(t *testing.T) {
	// publish create notification and inspect JetStream
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

// TestOkePublishCreateNotification_PublishError covers PublishCreateNotification
// wrapping a JetStream publish error.
func TestOkePublishCreateNotification_PublishError(t *testing.T) {
	// publish create with JetStream error
	inst := okeInstance(31, "oke-notify-create-error")
	js := &okeFakeJetStream{publishErr: errors.New("nats unavailable")}
	api := okeNewAPIStub(t)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", js), inst, &log)

	err := o.PublishCreateNotification()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to publish create notification")
}

// TestOkePublishDeleteNotification_Success covers PublishDeleteNotification
// publishing Operation Deleted on the delete subject.
func TestOkePublishDeleteNotification_Success(t *testing.T) {
	// publish delete notification and inspect JetStream
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

// TestOkePublishDeleteNotification_PublishError covers PublishDeleteNotification
// wrapping a JetStream publish error.
func TestOkePublishDeleteNotification_PublishError(t *testing.T) {
	// publish delete with JetStream error
	inst := okeInstance(33, "oke-notify-delete-error")
	js := &okeFakeJetStream{publishErr: errors.New("nats unavailable")}
	api := okeNewAPIStub(t)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", js), inst, &log)

	err := o.PublishDeleteNotification()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to publish delete notification")
}

// TestBuildOkeInfra_ProviderGetError covers buildOkeInfra wrapping a GET 500
// for the OCI provider.
func TestBuildOkeInfra_ProviderGetError(t *testing.T) {
	// serve provider GET error and call buildOkeInfra
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

// TestBuildOkeInfra_DecryptError covers buildOkeInfra wrapping decrypt of a
// non-ciphertext PrivateKey.
func TestBuildOkeInfra_DecryptError(t *testing.T) {
	// serve provider with plaintext PrivateKey and call buildOkeInfra
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

// TestBuildOkeInfra_RegionFallback covers nil, empty, and set Region reaching
// identity-client construction after decrypt; the resolved region is discarded.
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
			// serve provider and call buildOkeInfra with the case region
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

// TestBuildOkeInfra_DefinitionGetError covers BuildInfra wrapping a GET 500 for
// the OKE definition.
func TestBuildOkeInfra_DefinitionGetError(t *testing.T) {
	// serve instance and definition GET error and call BuildInfra
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

// TestOkeBuildInfra_InstanceGetError covers BuildInfra wrapping a GET 500 for
// the OKE instance.
func TestOkeBuildInfra_InstanceGetError(t *testing.T) {
	// serve instance GET error and call BuildInfra
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

// TestOkeEntryUpdated_NoOp covers v0OciOkeKubernetesRuntimeInstanceUpdated
// returning delay 0 with no API calls.
func TestOkeEntryUpdated_NoOp(t *testing.T) {
	// forbid API and call the updated entry
	inst := okeInstance(50, "oke-entry-updated")
	api := okeNewAPIStub(t)
	okeForbidAPIRequests(t, api)
	log := logr.Discard()

	delay, err := v0OciOkeKubernetesRuntimeInstanceUpdated(okeReconciler(api, "", nil), inst, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// TestOkeEntryCreated_DelegatesToHandleInfraCreate covers the created entry
// calling HandleInfraCreate, which returns delay 0 once CreationConfirmed is set.
func TestOkeEntryCreated_DelegatesToHandleInfraCreate(t *testing.T) {
	// serve confirmed instance and call the created entry
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

// TestOkeEntryDeleted_NotScheduledError covers the deleted entry wrapping
// deletion notification received but not scheduled.
func TestOkeEntryDeleted_NotScheduledError(t *testing.T) {
	// serve instance without DeletionScheduled and call the deleted entry
	inst := okeInstance(52, "oke-entry-deleted")
	api := okeNewAPIStub(t)
	okeServeInstance(t, api, inst)
	log := logr.Discard()

	_, err := v0OciOkeKubernetesRuntimeInstanceDeleted(okeReconciler(api, "", nil), inst, &log)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deletion notification received but not scheduled")
}

// TestOkeEntryDeleted_AlreadyConfirmed covers the deleted entry returning delay
// 0 once DeletionScheduled and DeletionConfirmed are set, with no OCI work.
func TestOkeEntryDeleted_AlreadyConfirmed(t *testing.T) {
	// serve scheduled and confirmed instance and call the deleted entry
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
