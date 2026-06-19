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

// Coverage boundaries in this file:
//
// The concrete OCI methods on the OKE infra type (connection fetch, cluster
// OCID lookup, compartment delete) and the compartment listing inside the
// infra build live behind the real OCI SDK with context.Background() and no
// timeout. Their success paths and everything after them are
// integration-only; unit tests here exercise only the LOCAL failure boundary
// using okeLocalFailConfigProvider() so no test ever opens a socket.
//
// Known-unreachable branch, documented rather than tested: the
// notification-payload error in both publish methods only fires when JSON
// marshalling of a pointer/string struct fails, which cannot be forced.

// TestOkeGetReconciliation_Success asserts the reconciliation snapshot maps
// 1:1 from the instance returned by the API.
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

// okeReconciliationSnapshotCheck mirrors the snapshot fields so table cases can
// assert against them without importing the provider package in each case.
type okeReconciliationSnapshotCheck struct {
	creationAcknowledged *time.Time
	creationConfirmed    *time.Time
	creationFailed       bool
	deletionScheduled    *time.Time
	deletionAcknowledged *time.Time
	deletionConfirmed    *time.Time
	resourceInventory    *datatypes.JSON
}

// TestOkeGetReconciliation_CreationFailedNilVsSet asserts a nil
// CreationFailed defaults to false in the snapshot and a set value
// propagates.
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

// TestOkeIsCreateComplete covers the nil, empty, and set cluster OCID
// branches plus the API error wrap.
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

// TestOkeAckCreation_SetsAckClearsFailed asserts the PATCH body carries a
// creation acknowledgement and explicitly clears the failed flag.
func TestOkeAckCreation_SetsAckClearsFailed(t *testing.T) {
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

// TestOkeAckCreation_UpdateError asserts the update error propagates.
func TestOkeAckCreation_UpdateError(t *testing.T) {
	inst := okeInstance(11, "oke-ack-creation-error")
	api := okeNewAPIStub(t)
	okeServeError(t, api, okeInstancePath(11), http.StatusInternalServerError)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	err := o.AckCreation()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "call to threeport API returned unexpected response")
}

// TestOkeRefreshCreationAck_SetsAckOnly asserts the refresh PATCH carries
// only the acknowledgement, leaving the failed flag untouched.
func TestOkeRefreshCreationAck_SetsAckOnly(t *testing.T) {
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

// TestOkeSetCreationFailed_SetsTrue asserts the PATCH body marks creation
// failed without touching the acknowledgement.
func TestOkeSetCreationFailed_SetsTrue(t *testing.T) {
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

// TestOkeConfirmCreation_SetsReconciledAndConfirmed asserts the PATCH body
// carries both the confirmation timestamp and Reconciled=true so the
// resulting update notification does not retrigger reconciliation.
func TestOkeConfirmCreation_SetsReconciledAndConfirmed(t *testing.T) {
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

// TestOkeAckDeletion_SetsTimestamp asserts the PATCH body carries a deletion
// acknowledgement.
func TestOkeAckDeletion_SetsTimestamp(t *testing.T) {
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

// TestOkeRefreshDeletionAck_SetsTimestamp asserts the refresh PATCH carries
// only the deletion acknowledgement.
func TestOkeRefreshDeletionAck_SetsTimestamp(t *testing.T) {
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

// TestOkeConfirmDeletion_SetsTimestamp asserts the PATCH body carries the
// deletion confirmation.
func TestOkeConfirmDeletion_SetsTimestamp(t *testing.T) {
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

// TestOkeSaveState_PersistsInventory asserts the passed state lands in the
// PATCH body as the resource inventory.
func TestOkeSaveState_PersistsInventory(t *testing.T) {
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

// TestOkeClearInventory_SetsEmptyObject asserts the PATCH body resets the
// resource inventory to an empty JSON object, the destroy-complete signal.
func TestOkeClearInventory_SetsEmptyObject(t *testing.T) {
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

// TestOkeOnCreateConfirmed_GetConnectionError covers the only reachable
// branch of OnCreateConfirmed(): its first call is GetConnection() on the
// concrete OKE infra, which needs a real OCI endpoint. The local-fail config
// provider makes the OCI client constructor reject the credentials before
// any socket opens. The instance-get, KRI-get, KRI-update, and success
// branches all sit after GetConnection() and are integration-only. The
// forbid-all stub proves no API call is issued before the failure.
func TestOkeOnCreateConfirmed_GetConnectionError(t *testing.T) {
	inst := okeInstance(20, "oke-create-confirmed")
	api := okeNewAPIStub(t)
	okeForbidAPIRequests(t, api)
	log := logr.Discard()
	o := newOkeLifecycleProvider(okeReconciler(api, "", nil), inst, &log)

	err := o.OnCreateConfirmed(okeLocalFailInfra("oke-create-confirmed"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get Kubernetes API connection info")
}

// TestOkeSaveCreateOutputs_GetClusterOCIDError covers the only reachable
// branch of SaveCreateOutputs(): its first call is GetClusterOCID() on the
// concrete OKE infra. The local-fail config provider forces a local client
// construction failure with zero sockets; the update/success path after it
// is integration-only. The forbid-all stub proves no PATCH is issued.
func TestOkeSaveCreateOutputs_GetClusterOCIDError(t *testing.T) {
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

// TestOkeOnDeleteConfirmed_CompartmentErrorPropagates is the critical
// delete-path guard: a compartment delete failure means cloud resources are
// orphaned, so OnDeleteConfirmed() must RETURN the error, never swallow it.
// The local-fail config provider makes the OCI identity client constructor
// fail locally (asserted via the inner wrap), so no socket opens and the
// method returns before the post-compartment cleanup. The cleanup branches
// (stack-state delete, instance get, KRI update/delete) run only after a
// successful compartment delete and are integration-only. The forbid-all
// stub proves no API call is issued.
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
// payload built from the in-memory instance; no API call is involved. The
// payload-error branch is known-unreachable: marshalling a pointer/string
// struct cannot fail.
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

// TestOkePublishCreateNotification_PublishError asserts the publish error
// wrap.
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

// TestOkePublishDeleteNotification_PublishError asserts the publish error
// wrap.
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

// TestBuildOkeInfra_RegionFallback drives all three region-selection
// variants through the provider-fetch, decrypt, and region-resolution
// phases. The selected region is NOT observable from a unit test: the
// provider's private key decrypts to a non-PEM plaintext, so the OCI
// identity client constructor rejects the config LOCALLY before any socket
// opens, and the constructed infra carrying the resolved region is
// discarded. Asserting the error is the identity-client wrap, not an
// earlier provider/decrypt wrap, proves each variant got through region
// resolution without a nil-deref panic. The compartment listing and its
// fallback branch after client construction need a real OCI call and are
// integration-only.
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
// through BuildInfra(), which resolves the definition before delegating to
// the infra build.
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

// TestOkeBuildInfra_InstanceGetError asserts the instance fetch error wrap
// on BuildInfra's first call.
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
// point wires the lifecycle provider into the shared state machine. The
// served instance is already creation-confirmed, so the state machine
// short-circuits right after the reconciliation fetch: no goroutine
// launches and no OCI code runs.
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

// TestOkeEntryDeleted_NotScheduledError proves the delete entry point
// wiring via the OCI-free guard: a delete notification for an instance with
// no deletion scheduled is an error.
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
