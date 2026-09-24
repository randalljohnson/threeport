package oci

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	logr "github.com/go-logr/logr"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	notif "github.com/threeport/threeport/internal/oci/notif"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// fakeJS is a JetStreamContext fake used only for the Publish() call that
// PublishCreateNotification and PublishDeleteNotification make. The embedded
// interface has a nil value so any other method call would panic, but the
// lifecycle handlers never touch anything else.
type fakeJS struct {
	nats.JetStreamContext
	published []publishRecord
	err       error
}

type publishRecord struct {
	subject string
	data    []byte
}

func (f *fakeJS) Publish(subj string, data []byte, _ ...nats.PubOpt) (*nats.PubAck, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.published = append(f.published, publishRecord{subject: subj, data: append([]byte(nil), data...)})
	return &nats.PubAck{Stream: "test", Sequence: uint64(len(f.published))}, nil
}

// newLifecycleForTest wires an okeLifecycle against a stub API and the given
// JetStream fake so the pointer methods under test can be driven directly.
func newLifecycleForTest(id uint, name string, addr string, client *http.Client, js nats.JetStreamContext) *okeLifecycle {
	inst := newMinimalOkeInstance(id, name)
	log := logr.Discard()
	return &okeLifecycle{
		r: &controller.Reconciler{
			APIClient:        client,
			APIServer:        addr,
			JetStreamContext: js,
		},
		instanceID: id,
		instance:   inst,
		log:        &log,
	}
}

// expectPatchInstance registers a mux handler that decodes the incoming PATCH
// body onto an OciOkeKubernetesRuntimeInstance and hands it to inspect for
// per-test assertions. The response echoes the payload back inside the
// standard apiserver_lib.Response envelope so UpdateOciOkeKubernetesRuntimeInstance
// finds Data[0] populated.
func expectPatchInstance(
	t *testing.T,
	mux *http.ServeMux,
	id uint,
	inspect func(*testing.T, *v0.OciOkeKubernetesRuntimeInstance),
	calls *int,
) {
	t.Helper()
	path := fmt.Sprintf("%s/%d", v0.PathOciOkeKubernetesRuntimeInstances, id)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		if calls != nil {
			*calls++
		}
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var payload v0.OciOkeKubernetesRuntimeInstance
		require.NoError(t, json.Unmarshal(body, &payload))
		if inspect != nil {
			inspect(t, &payload)
		}
		// echo the incoming payload back with the ID restored so the client's
		// response decode succeeds
		payload.ID = util.Ptr(id)
		writeOkeInstance(t, w, http.StatusOK, payload)
	})
}

// TestNewOkeLifecycleProvider_CopiesFieldsFromInstance asserts the constructor
// wires the reconciler, instance pointer, and instance ID through unchanged.
func TestNewOkeLifecycleProvider_CopiesFieldsFromInstance(t *testing.T) {
	// build a minimal instance and a bare reconciler
	inst := newMinimalOkeInstance(42, "oke-ctor")
	log := logr.Discard()
	r := &controller.Reconciler{APIServer: "example"}

	// invoke the constructor under test
	got := newOkeLifecycleProvider(r, inst, &log)

	// assert every field the constructor is responsible for populating
	require.NotNil(t, got)
	assert.Same(t, r, got.r, "reconciler should be threaded through")
	assert.Same(t, inst, got.instance, "instance pointer should be threaded through")
	assert.Equal(t, uint(42), got.instanceID, "instanceID should be deref of instance.ID")
	assert.Same(t, &log, got.log, "log pointer should be threaded through")
}

// TestGetReconciliation_MapsAllFields asserts the snapshot returned by
// GetReconciliation copies every timestamp and inventory pointer straight from
// the API response and dereferences CreationFailed=true when it is set.
func TestGetReconciliation_MapsAllFields(t *testing.T) {
	// build a fully-populated instance the stub API will return
	inv := datatypes.JSON([]byte(`{"some":"state"}`))
	nowAck := mustParseTime(t, "2026-01-01T00:00:00Z")
	nowConfirmed := mustParseTime(t, "2026-01-02T00:00:00Z")
	scheduled := mustParseTime(t, "2026-01-03T00:00:00Z")
	deletedAck := mustParseTime(t, "2026-01-04T00:00:00Z")
	deletedConf := mustParseTime(t, "2026-01-05T00:00:00Z")
	inst := newMinimalOkeInstance(1, "oke-snap")
	inst.CreationAcknowledged = &nowAck
	inst.CreationConfirmed = &nowConfirmed
	inst.CreationFailed = util.Ptr(true)
	inst.DeletionScheduled = &scheduled
	inst.DeletionAcknowledged = &deletedAck
	inst.DeletionConfirmed = &deletedConf
	inst.ResourceInventory = &inv

	// stand up a stub API that returns the instance on GET
	mux := http.NewServeMux()
	path := fmt.Sprintf("%s/1", v0.PathOciOkeKubernetesRuntimeInstances)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		writeOkeInstance(t, w, http.StatusOK, *inst)
	})
	stub := newOkeAPIStub(t, mux)

	// invoke GetReconciliation
	o := newLifecycleForTest(1, "oke-snap", stub.addr, stub.client, nil)
	snap, err := o.GetReconciliation()

	// assert every field of the snapshot maps back to the API instance
	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.WithinDuration(t, nowAck, *snap.CreationAcknowledged, 0)
	assert.WithinDuration(t, nowConfirmed, *snap.CreationConfirmed, 0)
	assert.True(t, snap.CreationFailed, "true pointer should surface as true bool")
	assert.WithinDuration(t, scheduled, *snap.DeletionScheduled, 0)
	assert.WithinDuration(t, deletedAck, *snap.DeletionAcknowledged, 0)
	assert.WithinDuration(t, deletedConf, *snap.DeletionConfirmed, 0)
	require.NotNil(t, snap.ResourceInventory)
	assert.JSONEq(t, string(inv), string(*snap.ResourceInventory))
}

// TestGetReconciliation_CreationFailedNilCoercesToFalse asserts that a nil
// CreationFailed pointer on the API instance is coerced to false in the
// snapshot rather than causing a nil-deref.
func TestGetReconciliation_CreationFailedNilCoercesToFalse(t *testing.T) {
	// stub returns an instance with CreationFailed left nil
	inst := newMinimalOkeInstance(2, "oke-fail-nil")
	mux := http.NewServeMux()
	path := fmt.Sprintf("%s/2", v0.PathOciOkeKubernetesRuntimeInstances)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		writeOkeInstance(t, w, http.StatusOK, *inst)
	})
	stub := newOkeAPIStub(t, mux)

	// invoke GetReconciliation
	o := newLifecycleForTest(2, "oke-fail-nil", stub.addr, stub.client, nil)
	snap, err := o.GetReconciliation()

	// assert the nil pointer safely became false
	require.NoError(t, err)
	assert.False(t, snap.CreationFailed)
}

// TestGetReconciliation_APIError wraps an API 500 into an error with the
// documented "failed to get latest OKE instance" prefix.
func TestGetReconciliation_APIError(t *testing.T) {
	// stub returns a 500
	mux := http.NewServeMux()
	path := fmt.Sprintf("%s/3", v0.PathOciOkeKubernetesRuntimeInstances)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		writeErrorStatus(t, w, http.StatusInternalServerError, "boom")
	})
	stub := newOkeAPIStub(t, mux)

	// invoke GetReconciliation
	o := newLifecycleForTest(3, "oke-err", stub.addr, stub.client, nil)
	snap, err := o.GetReconciliation()

	// assert the wrapped-error contract
	require.Error(t, err)
	assert.Nil(t, snap)
	assert.Contains(t, err.Error(), "failed to get latest OKE instance")
}

// TestIsCreateComplete drives the ClusterOCID branching:
// - nil pointer returns false
// - empty-string pointer returns false
// - non-empty pointer returns true
// - API error surfaces wrapped
func TestIsCreateComplete(t *testing.T) {
	cases := []struct {
		name        string
		ocid        *string
		httpStatus  int
		wantValue   bool
		wantErr     bool
		errContains string
	}{
		{name: "nil ClusterOCID is not complete", ocid: nil, httpStatus: http.StatusOK, wantValue: false},
		{name: "empty ClusterOCID is not complete", ocid: util.Ptr(""), httpStatus: http.StatusOK, wantValue: false},
		{name: "populated ClusterOCID is complete", ocid: util.Ptr("ocid1.cluster.oc1..abcd"), httpStatus: http.StatusOK, wantValue: true},
		{name: "API 500 surfaces wrapped error", httpStatus: http.StatusInternalServerError, wantErr: true, errContains: "failed to check OKE cluster creation status"},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := uint(100 + i)
			mux := http.NewServeMux()
			path := fmt.Sprintf("%s/%d", v0.PathOciOkeKubernetesRuntimeInstances, id)
			mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
				if tc.httpStatus != http.StatusOK {
					writeErrorStatus(t, w, tc.httpStatus, "boom")
					return
				}
				inst := newMinimalOkeInstance(id, tc.name)
				inst.ClusterOCID = tc.ocid
				writeOkeInstance(t, w, http.StatusOK, *inst)
			})
			stub := newOkeAPIStub(t, mux)

			// invoke IsCreateComplete against each ClusterOCID shape
			o := newLifecycleForTest(id, tc.name, stub.addr, stub.client, nil)
			got, err := o.IsCreateComplete()

			// assert either the wrapped error or the boolean the ClusterOCID dictates
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
				assert.False(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantValue, got)
		})
	}
}

// TestAckCreation_SendsPatchWithAckAndClearedFailure asserts the PATCH body
// carries CreationAcknowledged set and CreationFailed=false.
func TestAckCreation_SendsPatchWithAckAndClearedFailure(t *testing.T) {
	// stub API captures the PATCH body for the exercised handler
	var calls int
	mux := http.NewServeMux()
	expectPatchInstance(t, mux, 10, func(t *testing.T, payload *v0.OciOkeKubernetesRuntimeInstance) {
		// the ack fills in CreationAcknowledged and clears CreationFailed
		require.NotNil(t, payload.CreationAcknowledged)
		require.NotNil(t, payload.CreationFailed)
		assert.False(t, *payload.CreationFailed)
	}, &calls)
	stub := newOkeAPIStub(t, mux)

	// invoke AckCreation
	o := newLifecycleForTest(10, "oke-ack", stub.addr, stub.client, nil)
	require.NoError(t, o.AckCreation())

	// assert the API was called exactly once
	assert.Equal(t, 1, calls)
}

// TestAckCreation_APIError surfaces PATCH failures directly.
func TestAckCreation_APIError(t *testing.T) {
	// stub API returns a 500 to force the error path
	mux := http.NewServeMux()
	path := fmt.Sprintf("%s/11", v0.PathOciOkeKubernetesRuntimeInstances)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		writeErrorStatus(t, w, http.StatusInternalServerError, "boom")
	})
	stub := newOkeAPIStub(t, mux)

	// invoke AckCreation
	o := newLifecycleForTest(11, "oke-ack-err", stub.addr, stub.client, nil)
	err := o.AckCreation()

	// assert the API's error surfaces from the update path
	require.Error(t, err)
}

// TestRefreshCreationAck_SendsPatchWithAck asserts the PATCH body carries
// only CreationAcknowledged, leaving CreationFailed untouched.
func TestRefreshCreationAck_SendsPatchWithAck(t *testing.T) {
	// capture the payload so we can inspect it
	var calls int
	mux := http.NewServeMux()
	expectPatchInstance(t, mux, 12, func(t *testing.T, payload *v0.OciOkeKubernetesRuntimeInstance) {
		// refresh should update the ack without setting CreationFailed at all
		require.NotNil(t, payload.CreationAcknowledged)
		assert.Nil(t, payload.CreationFailed, "refresh must not touch CreationFailed")
	}, &calls)
	stub := newOkeAPIStub(t, mux)

	// invoke RefreshCreationAck
	o := newLifecycleForTest(12, "oke-refresh-ack", stub.addr, stub.client, nil)
	require.NoError(t, o.RefreshCreationAck())

	// assert a single API PATCH occurred
	assert.Equal(t, 1, calls)
}

// TestSetCreationFailed_SendsPatchWithFailedTrue asserts the PATCH body sets
// CreationFailed=true and does not touch the ack timestamps.
func TestSetCreationFailed_SendsPatchWithFailedTrue(t *testing.T) {
	// capture the payload so we can inspect it
	var calls int
	mux := http.NewServeMux()
	expectPatchInstance(t, mux, 13, func(t *testing.T, payload *v0.OciOkeKubernetesRuntimeInstance) {
		// failed path sets the flag to true and leaves acks alone
		require.NotNil(t, payload.CreationFailed)
		assert.True(t, *payload.CreationFailed)
		assert.Nil(t, payload.CreationAcknowledged)
	}, &calls)
	stub := newOkeAPIStub(t, mux)

	// invoke SetCreationFailed
	o := newLifecycleForTest(13, "oke-set-failed", stub.addr, stub.client, nil)
	require.NoError(t, o.SetCreationFailed())

	// assert one PATCH call landed
	assert.Equal(t, 1, calls)
}

// TestConfirmCreation_SendsPatchWithReconciledAndConfirmed asserts the PATCH
// carries Reconciled=true and CreationConfirmed set.
func TestConfirmCreation_SendsPatchWithReconciledAndConfirmed(t *testing.T) {
	// capture the payload so we can inspect it
	var calls int
	mux := http.NewServeMux()
	expectPatchInstance(t, mux, 14, func(t *testing.T, payload *v0.OciOkeKubernetesRuntimeInstance) {
		// confirm sets Reconciled and CreationConfirmed at once
		require.NotNil(t, payload.Reconciled)
		assert.True(t, *payload.Reconciled)
		require.NotNil(t, payload.CreationConfirmed)
	}, &calls)
	stub := newOkeAPIStub(t, mux)

	// invoke ConfirmCreation
	o := newLifecycleForTest(14, "oke-confirm", stub.addr, stub.client, nil)
	require.NoError(t, o.ConfirmCreation())

	// assert one PATCH call landed
	assert.Equal(t, 1, calls)
}

// TestAckDeletion_SendsPatchWithAck asserts the PATCH body carries only
// DeletionAcknowledged.
func TestAckDeletion_SendsPatchWithAck(t *testing.T) {
	// capture the payload so we can inspect it
	var calls int
	mux := http.NewServeMux()
	expectPatchInstance(t, mux, 15, func(t *testing.T, payload *v0.OciOkeKubernetesRuntimeInstance) {
		// deletion ack fills the timestamp
		require.NotNil(t, payload.DeletionAcknowledged)
	}, &calls)
	stub := newOkeAPIStub(t, mux)

	// invoke AckDeletion
	o := newLifecycleForTest(15, "oke-ack-del", stub.addr, stub.client, nil)
	require.NoError(t, o.AckDeletion())

	// assert one PATCH call landed
	assert.Equal(t, 1, calls)
}

// TestRefreshDeletionAck_SendsPatchWithAck asserts the PATCH body carries
// only DeletionAcknowledged (mirror of RefreshCreationAck).
func TestRefreshDeletionAck_SendsPatchWithAck(t *testing.T) {
	// capture the payload so we can inspect it
	var calls int
	mux := http.NewServeMux()
	expectPatchInstance(t, mux, 16, func(t *testing.T, payload *v0.OciOkeKubernetesRuntimeInstance) {
		// refresh path updates only DeletionAcknowledged
		require.NotNil(t, payload.DeletionAcknowledged)
	}, &calls)
	stub := newOkeAPIStub(t, mux)

	// invoke RefreshDeletionAck
	o := newLifecycleForTest(16, "oke-refresh-del", stub.addr, stub.client, nil)
	require.NoError(t, o.RefreshDeletionAck())

	// assert one PATCH call landed
	assert.Equal(t, 1, calls)
}

// TestConfirmDeletion_SendsPatchWithConfirmed asserts the PATCH body sets
// DeletionConfirmed.
func TestConfirmDeletion_SendsPatchWithConfirmed(t *testing.T) {
	// capture the payload so we can inspect it
	var calls int
	mux := http.NewServeMux()
	expectPatchInstance(t, mux, 17, func(t *testing.T, payload *v0.OciOkeKubernetesRuntimeInstance) {
		// confirm-deletion updates only DeletionConfirmed
		require.NotNil(t, payload.DeletionConfirmed)
	}, &calls)
	stub := newOkeAPIStub(t, mux)

	// invoke ConfirmDeletion
	o := newLifecycleForTest(17, "oke-confirm-del", stub.addr, stub.client, nil)
	require.NoError(t, o.ConfirmDeletion())

	// assert one PATCH call landed
	assert.Equal(t, 1, calls)
}

// TestSaveState_SendsPatchWithResourceInventory asserts SaveState passes the
// caller's inventory bytes through unchanged in the PATCH body.
func TestSaveState_SendsPatchWithResourceInventory(t *testing.T) {
	// prepare a nonempty state blob and capture the PATCH body
	state := datatypes.JSON([]byte(`{"a":1}`))
	var calls int
	mux := http.NewServeMux()
	expectPatchInstance(t, mux, 18, func(t *testing.T, payload *v0.OciOkeKubernetesRuntimeInstance) {
		// save-state carries the caller-supplied inventory bytes
		require.NotNil(t, payload.ResourceInventory)
		assert.JSONEq(t, string(state), string(*payload.ResourceInventory))
	}, &calls)
	stub := newOkeAPIStub(t, mux)

	// invoke SaveState
	o := newLifecycleForTest(18, "oke-save-state", stub.addr, stub.client, nil)
	require.NoError(t, o.SaveState(&state))

	// assert one PATCH call landed
	assert.Equal(t, 1, calls)
}

// TestClearInventory_SendsPatchWithEmptyObject asserts ClearInventory writes
// the literal "{}" JSON that HandleInfraDelete keys off of.
func TestClearInventory_SendsPatchWithEmptyObject(t *testing.T) {
	// capture the payload so we can inspect it
	var calls int
	mux := http.NewServeMux()
	expectPatchInstance(t, mux, 19, func(t *testing.T, payload *v0.OciOkeKubernetesRuntimeInstance) {
		// clear-inventory writes exactly "{}"
		require.NotNil(t, payload.ResourceInventory)
		assert.Equal(t, "{}", string(*payload.ResourceInventory))
	}, &calls)
	stub := newOkeAPIStub(t, mux)

	// invoke ClearInventory
	o := newLifecycleForTest(19, "oke-clear-inv", stub.addr, stub.client, nil)
	require.NoError(t, o.ClearInventory())

	// assert one PATCH call landed
	assert.Equal(t, 1, calls)
}

// TestPublishCreateNotification_PublishesOnCreateSubject asserts a create
// publish targets the create subject and produces a valid notification
// payload with Operation=Created.
func TestPublishCreateNotification_PublishesOnCreateSubject(t *testing.T) {
	// wire a fake JetStream that captures publishes
	js := &fakeJS{}
	o := newLifecycleForTest(20, "oke-notif-create", "", nil, js)

	// invoke PublishCreateNotification
	require.NoError(t, o.PublishCreateNotification())

	// assert exactly one publish on the create subject with a create payload
	require.Len(t, js.published, 1)
	rec := js.published[0]
	assert.Equal(t, notif.OciOkeKubernetesRuntimeInstanceCreateSubject, rec.subject)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(rec.data, &parsed))
	assert.Equal(t, "Created", parsed["Operation"], "notification carries the Created operation string")
}

// TestPublishCreateNotification_JSErrorSurfacesWrapped asserts a JetStream
// publish failure surfaces as a wrapped "failed to publish create
// notification" error.
func TestPublishCreateNotification_JSErrorSurfacesWrapped(t *testing.T) {
	// wire a fake JetStream that returns a canned publish error
	sentinel := errors.New("publish rejected")
	js := &fakeJS{err: sentinel}
	o := newLifecycleForTest(21, "oke-notif-err", "", nil, js)

	// invoke PublishCreateNotification
	err := o.PublishCreateNotification()

	// assert the error is wrapped with the documented prefix
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), "failed to publish create notification")
}

// TestPublishDeleteNotification_PublishesOnDeleteSubject mirrors the create
// case: publishes on the delete subject with Operation=Deleted.
func TestPublishDeleteNotification_PublishesOnDeleteSubject(t *testing.T) {
	// wire a fake JetStream that captures publishes
	js := &fakeJS{}
	o := newLifecycleForTest(22, "oke-notif-del", "", nil, js)

	// invoke PublishDeleteNotification
	require.NoError(t, o.PublishDeleteNotification())

	// assert exactly one publish on the delete subject with a delete payload
	require.Len(t, js.published, 1)
	rec := js.published[0]
	assert.Equal(t, notif.OciOkeKubernetesRuntimeInstanceDeleteSubject, rec.subject)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(rec.data, &parsed))
	assert.Equal(t, "Deleted", parsed["Operation"], "notification carries the Deleted operation string")
}

// TestPublishDeleteNotification_JSErrorSurfacesWrapped mirrors the create
// error case: a publish failure returns a wrapped "failed to publish delete
// notification" error.
func TestPublishDeleteNotification_JSErrorSurfacesWrapped(t *testing.T) {
	// wire a fake JetStream that returns a canned publish error
	sentinel := errors.New("publish rejected")
	js := &fakeJS{err: sentinel}
	o := newLifecycleForTest(23, "oke-notif-del-err", "", nil, js)

	// invoke PublishDeleteNotification
	err := o.PublishDeleteNotification()

	// assert the error is wrapped with the documented prefix
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), "failed to publish delete notification")
}

// mustParseTime is a small helper to build stable timestamps for snapshot
// assertions without dragging test time.Now() into the mix.
func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return parsed
}
