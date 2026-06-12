package provider

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deleteBase is the frozen reference time the delete handler tests use for
// scheduling and confirmation timestamps.
var deleteBase = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// scheduledSnap returns a snapshot with deletion scheduled an hour before
// the reference time, the precondition for the delete handler to proceed.
func scheduledSnap() *ReconciliationSnapshot {
	return &ReconciliationSnapshot{DeletionScheduled: timePtr(deleteBase.Add(-time.Hour))}
}

// TestHandleInfraDelete_NotScheduled_Error covers the validation branch: a
// delete notification with no DeletionScheduled timestamp returns the
// "received but not scheduled" error and never builds infra.
func TestHandleInfraDelete_NotScheduled_Error(t *testing.T) {
	withInfraConcurrency(t, 1)
	fl := newFakeLifecycle()

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "received but not scheduled")
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
}

// TestHandleInfraDelete_AlreadyConfirmed_EarlyReturn covers the idempotency
// branch: with DeletionConfirmed already set the handler returns (0, nil)
// without building infra or observing.
func TestHandleInfraDelete_AlreadyConfirmed_EarlyReturn(t *testing.T) {
	withInfraConcurrency(t, 1)
	snap := scheduledSnap()
	snap.DeletionConfirmed = timePtr(deleteBase.Add(-time.Minute))
	fl := newFakeLifecycle(snap)
	fi := newFakeInfra()
	fl.setInfra(fi)

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
	assert.Equal(t, 0, fi.observeCallCount())
}

// TestHandleInfraDelete_Present_KicksDestroyOnce covers the kick branch:
// resources still present (observed ready) kick exactly one destroy step,
// persist the refreshed state, and requeue for deletion.
func TestHandleInfraDelete_Present_KicksDestroyOnce(t *testing.T) {
	withInfraConcurrency(t, 1)
	fi := newFakeInfra()
	fi.setObservations(Observation{Phase: PhaseReady, State: validStackState()})
	fi.setDestroy(applySucceed, nil)
	fi.setGetStackState(validStackState(), nil)
	fl := newFakeLifecycle(scheduledSnap())
	fl.setInfra(fi)

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(requeueDeleting), requeue)
	assert.Equal(t, 1, fi.destroyCallCount())

	// not confirmed: resources may remain after one bounded destroy step
	assert.Equal(t, 0, fl.callCount("OnDeleteConfirmed"))
	assert.Equal(t, 0, fl.callCount("PublishDeleteNotification"))
}

// TestHandleInfraDelete_Deleting_PersistsAndRequeues covers the in-flight
// branch: a deleting observation persists the refreshed state and requeues
// without kicking another destroy, so a teardown already underway is never
// kicked twice on the same pass.
func TestHandleInfraDelete_Deleting_PersistsAndRequeues(t *testing.T) {
	withInfraConcurrency(t, 1)
	state := validStackState()
	fi := newFakeInfra()
	fi.setObservations(Observation{Phase: PhaseDeleting, State: state})
	fl := newFakeLifecycle(scheduledSnap())
	fl.setInfra(fi)

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(requeueDeleting), requeue)
	assert.Equal(t, 0, fi.destroyCallCount(), "a deleting teardown must not kick another destroy")

	last, ok := fl.lastUpdate()
	require.True(t, ok)
	require.NotNil(t, last.ResourceInventory)
	assert.JSONEq(t, string(*state), string(*last.ResourceInventory))
}

// TestHandleInfraDelete_Absent_ConfirmsAndPublishes covers the confirm
// branch: an absent observation runs post-deletion cleanup, clears the
// inventory, sets the deletion-confirmed timestamp, publishes the
// notification, and returns (0, nil).
func TestHandleInfraDelete_Absent_ConfirmsAndPublishes(t *testing.T) {
	withInfraConcurrency(t, 1)
	fi := newFakeInfra()
	fi.setObservations(Observation{Phase: PhaseAbsent})
	fl := newFakeLifecycle(scheduledSnap())
	fl.setInfra(fi)

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 0, fi.destroyCallCount(), "an absent stack must not kick a destroy")
	assert.Equal(t, 1, fl.callCount("OnDeleteConfirmed"))
	assert.Equal(t, 1, fl.callCount("PublishDeleteNotification"))

	last, ok := fl.lastUpdate()
	require.True(t, ok)
	require.NotNil(t, last.DeletionConfirmed)
	require.NotNil(t, last.ResourceInventory)
	assert.JSONEq(t, "{}", string(*last.ResourceInventory))
}

// TestHandleInfraDelete_OnDeleteConfirmedError_Requeues covers the cleanup
// retry branch: a failing post-deletion cleanup requeues for deletion
// without confirming, so the next pass retries the cleanup.
func TestHandleInfraDelete_OnDeleteConfirmedError_Requeues(t *testing.T) {
	withInfraConcurrency(t, 1)
	fi := newFakeInfra()
	fi.setObservations(Observation{Phase: PhaseAbsent})
	fl := newFakeLifecycle(scheduledSnap())
	fl.setInfra(fi)
	fl.setErr("OnDeleteConfirmed", errors.New("post-deletion cleanup failed"))

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(requeueDeleting), requeue)
	assert.Equal(t, 1, fl.callCount("OnDeleteConfirmed"))
	assert.Equal(t, 0, fl.callCount("PublishDeleteNotification"))

	// deletion was not confirmed, so the inventory was not cleared
	_, ok := fl.lastUpdate()
	assert.False(t, ok, "a failed cleanup must not persist a confirmation")
}

// TestHandleInfraDelete_DestroyError_PersistsRemainingAndRequeues covers the
// failed kick branch: a failing destroy captures the remaining state,
// persists it, and requeues after the failure interval so the retry knows
// what is left.
func TestHandleInfraDelete_DestroyError_PersistsRemainingAndRequeues(t *testing.T) {
	withInfraConcurrency(t, 1)
	errDestroy := errors.New("destroy exploded")
	fi := newFakeInfra()
	fi.setObservations(Observation{Phase: PhaseReady, State: validStackState()})
	fi.setDestroy(applyError, errDestroy)
	fi.setGetStackState(validStackState(), nil)
	fl := newFakeLifecycle(scheduledSnap())
	fl.setInfra(fi)

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(requeueAfterFailure), requeue)
	assert.Equal(t, 1, fi.destroyCallCount())

	last, ok := fl.lastUpdate()
	require.True(t, ok)
	require.NotNil(t, last.ResourceInventory)
	assert.JSONEq(t, string(*validStackState()), string(*last.ResourceInventory))

	assert.Equal(t, 0, fl.callCount("PublishDeleteNotification"))
}

// TestHandleInfraDelete_RestoresExistingState covers the resume-state
// branch: a non-empty inventory is restored before observing so the destroy
// knows which cloud resources to tear down.
func TestHandleInfraDelete_RestoresExistingState(t *testing.T) {
	withInfraConcurrency(t, 1)
	inventory := validStackState()
	fi := newFakeInfra()
	fi.setObservations(Observation{Phase: PhaseDeleting, State: inventory})
	snap := scheduledSnap()
	snap.ResourceInventory = inventory
	fl := newFakeLifecycle(snap)
	fl.setInfra(fi)

	_, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, 1, fi.setStackStateCallCount())
	require.NotNil(t, fi.lastRestoredState())
	assert.JSONEq(t, string(*inventory), string(*fi.lastRestoredState()))
}

// TestHandleInfraDelete_ObserveError_RequeuesWithoutConfirming covers the
// observe-failure branch, the heart of the do-not-orphan guarantee: a
// refresh error surfaces as a handler error so the reconciler requeues, and
// deletion is never confirmed and the inventory never cleared while the
// state could not be read.
func TestHandleInfraDelete_ObserveError_RequeuesWithoutConfirming(t *testing.T) {
	withInfraConcurrency(t, 1)
	errObserve := errors.New("refresh against cloud failed")
	fi := newFakeInfra()
	fi.setObserveErr(errObserve)
	fl := newFakeLifecycle(scheduledSnap())
	fl.setInfra(fi)

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.Error(t, err)
	assert.ErrorIs(t, err, errObserve)
	assert.Contains(t, err.Error(), "failed to observe infrastructure")
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 0, fi.destroyCallCount(), "a refresh error must never kick a destroy")
	assert.Equal(t, 0, fl.callCount("UpdateReconciliation"))
	assert.Equal(t, 0, fl.callCount("PublishDeleteNotification"))
}
