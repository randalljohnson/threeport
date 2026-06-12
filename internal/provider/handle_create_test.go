package provider

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createBase is the frozen reference time the create handler tests use for
// confirmation and deletion timestamps.
var createBase = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// TestHandleInfraCreate_AlreadyConfirmed_EarlyReturn covers the terminal
// branch: with CreationConfirmed already set the handler returns (0, nil)
// after the initial fetch and never builds infra or observes.
func TestHandleInfraCreate_AlreadyConfirmed_EarlyReturn(t *testing.T) {
	withInfraConcurrency(t, 1)
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		CreationConfirmed: timePtr(createBase),
	})
	fi := newFakeInfra()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 1, fl.callCount("GetReconciliation"))
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
	assert.Equal(t, 0, fi.observeCallCount())
	assert.Equal(t, 0, fi.applyCallCount())
}

// TestHandleInfraCreate_DeletionScheduled_Yields covers the race branch:
// when a delete has been scheduled before the create confirmed, the handler
// yields to the delete handler with (0, nil) and never observes or kicks.
func TestHandleInfraCreate_DeletionScheduled_Yields(t *testing.T) {
	withInfraConcurrency(t, 1)
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: timePtr(createBase),
	})
	fi := newFakeInfra()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
	assert.Equal(t, 0, fi.applyCallCount())
}

// TestHandleInfraCreate_Absent_KicksApplyOnce covers the kick branch: an
// absent observation kicks exactly one apply step, persists the refreshed
// state with the failure flag cleared, and requeues for provisioning.
func TestHandleInfraCreate_Absent_KicksApplyOnce(t *testing.T) {
	withInfraConcurrency(t, 1)
	fi := newFakeInfra()
	fi.setObservations(Observation{Phase: PhaseAbsent})
	fi.setApply(applySucceed, nil)
	fi.setGetStackState(validStackState(), nil)
	fl := newFakeLifecycle()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(requeueProvisioning), requeue)
	assert.Equal(t, 1, fi.applyCallCount())

	// the post-kick state was persisted with the failure flag cleared and
	// the create not yet confirmed
	last, ok := fl.lastUpdate()
	require.True(t, ok)
	assert.False(t, last.CreationFailed)
	assert.Nil(t, last.CreationConfirmed)
	require.NotNil(t, last.ResourceInventory)
	assert.JSONEq(t, string(*validStackState()), string(*last.ResourceInventory))

	// the success callbacks did not run on a still-provisioning create
	assert.Equal(t, 0, fl.callCount("OnCreateConfirmed"))
	assert.Equal(t, 0, fl.callCount("PublishCreateNotification"))
}

// TestHandleInfraCreate_Provisioning_PersistsAndRequeues covers the
// in-flight branch: a provisioning observation persists the refreshed state
// and requeues without kicking another apply, so a create already underway
// is never kicked a second time on the same pass.
func TestHandleInfraCreate_Provisioning_PersistsAndRequeues(t *testing.T) {
	withInfraConcurrency(t, 1)
	state := validStackState()
	fi := newFakeInfra()
	fi.setObservations(Observation{Phase: PhaseProvisioning, State: state})
	fl := newFakeLifecycle()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(requeueProvisioning), requeue)
	assert.Equal(t, 0, fi.applyCallCount(), "a provisioning create must not kick another apply")

	last, ok := fl.lastUpdate()
	require.True(t, ok)
	require.NotNil(t, last.ResourceInventory)
	assert.JSONEq(t, string(*state), string(*last.ResourceInventory))
}

// TestHandleInfraCreate_Ready_ConfirmsAndPublishes covers the confirm
// branch: a ready observation runs post-creation work, persists the
// confirmation snapshot (state, confirmed timestamp, reconciled, failure
// cleared), publishes the notification, and returns (0, nil).
func TestHandleInfraCreate_Ready_ConfirmsAndPublishes(t *testing.T) {
	withInfraConcurrency(t, 1)
	state := validStackState()
	fi := newFakeInfra()
	fi.setObservations(Observation{Phase: PhaseReady, State: state})
	fl := newFakeLifecycle()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 0, fi.applyCallCount(), "a ready create must not kick an apply")
	assert.Equal(t, 1, fl.callCount("OnCreateConfirmed"))
	assert.Equal(t, 1, fl.callCount("PublishCreateNotification"))

	last, ok := fl.lastUpdate()
	require.True(t, ok)
	assert.True(t, last.Reconciled)
	assert.False(t, last.CreationFailed)
	require.NotNil(t, last.CreationConfirmed)
	require.NotNil(t, last.ResourceInventory)
	assert.JSONEq(t, string(*state), string(*last.ResourceInventory))
}

// TestHandleInfraCreate_OnCreateConfirmedError_Propagates covers the failed
// post-creation branch: the wrapped error is returned for retry and the
// create is never marked confirmed.
func TestHandleInfraCreate_OnCreateConfirmedError_Propagates(t *testing.T) {
	withInfraConcurrency(t, 1)
	errPost := errors.New("post-creation work failed")
	fi := newFakeInfra()
	fi.setObservations(Observation{Phase: PhaseReady, State: validStackState()})
	fl := newFakeLifecycle()
	fl.setInfra(fi)
	fl.setErr("OnCreateConfirmed", errPost)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.Error(t, err)
	assert.ErrorIs(t, err, errPost)
	assert.Contains(t, err.Error(), "failed to run post-creation work")
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 0, fl.callCount("UpdateReconciliation"))
	assert.Equal(t, 0, fl.callCount("PublishCreateNotification"))
}

// TestHandleInfraCreate_ApplyError_PersistsFailureAndRequeues covers the
// failed kick branch: a failing apply captures the partial state, persists
// it with the failure flag set, and requeues after the failure interval so
// the next pass resumes against what already exists.
func TestHandleInfraCreate_ApplyError_PersistsFailureAndRequeues(t *testing.T) {
	withInfraConcurrency(t, 1)
	errApply := errors.New("apply exploded")
	fi := newFakeInfra()
	fi.setObservations(Observation{Phase: PhaseAbsent})
	fi.setApply(applyError, errApply)
	fi.setGetStackState(validStackState(), nil)
	fl := newFakeLifecycle()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(requeueAfterFailure), requeue)
	assert.Equal(t, 1, fi.applyCallCount())

	last, ok := fl.lastUpdate()
	require.True(t, ok)
	assert.True(t, last.CreationFailed)
	require.NotNil(t, last.ResourceInventory)
	assert.JSONEq(t, string(*validStackState()), string(*last.ResourceInventory))

	// success callbacks never ran
	assert.Equal(t, 0, fl.callCount("OnCreateConfirmed"))
	assert.Equal(t, 0, fl.callCount("PublishCreateNotification"))
}

// TestHandleInfraCreate_Failed_KicksApplyToResume covers the resume branch:
// a previously failed create observes PhaseFailed and kicks one apply to
// resume, persisting state with the failure flag cleared on success.
func TestHandleInfraCreate_Failed_KicksApplyToResume(t *testing.T) {
	withInfraConcurrency(t, 1)
	fi := newFakeInfra()
	fi.setObservations(Observation{Phase: PhaseFailed})
	fi.setApply(applySucceed, nil)
	fi.setGetStackState(validStackState(), nil)
	fl := newFakeLifecycle(&ReconciliationSnapshot{CreationFailed: true})
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(requeueProvisioning), requeue)
	assert.Equal(t, 1, fi.applyCallCount())

	last, ok := fl.lastUpdate()
	require.True(t, ok)
	assert.False(t, last.CreationFailed)
}

// TestHandleInfraCreate_RestoresExistingState covers the resume-state
// branch: when the snapshot carries a non-empty inventory the handler
// restores it before observing, so the observe refreshes against resources
// an earlier process created.
func TestHandleInfraCreate_RestoresExistingState(t *testing.T) {
	withInfraConcurrency(t, 1)
	inventory := validStackState()
	fi := newFakeInfra()
	fi.setObservations(Observation{Phase: PhaseProvisioning, State: inventory})
	fl := newFakeLifecycle(&ReconciliationSnapshot{ResourceInventory: inventory})
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(requeueProvisioning), requeue)
	assert.Equal(t, 1, fi.setStackStateCallCount())
	require.NotNil(t, fi.lastRestoredState())
	assert.JSONEq(t, string(*inventory), string(*fi.lastRestoredState()))
}

// TestHandleInfraCreate_EmptyInventory_SkipsRestore covers the no-restore
// branch: an empty-object inventory is not restorable existing state, so
// the handler observes without restoring first.
func TestHandleInfraCreate_EmptyInventory_SkipsRestore(t *testing.T) {
	withInfraConcurrency(t, 1)
	fi := newFakeInfra()
	fi.setObservations(Observation{Phase: PhaseAbsent})
	fi.setApply(applySucceed, nil)
	fl := newFakeLifecycle(&ReconciliationSnapshot{ResourceInventory: jsonPtr("{}")})
	fl.setInfra(fi)

	_, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, 0, fi.setStackStateCallCount(), "an empty inventory must not be restored")
}

// TestHandleInfraCreate_ObserveError_RequeuesWithoutKicking covers the
// observe-failure branch, the heart of the do-not-double-provision
// guarantee: a refresh error surfaces as a handler error so the reconciler
// requeues, and apply is never kicked against infrastructure whose state
// could not be read.
func TestHandleInfraCreate_ObserveError_RequeuesWithoutKicking(t *testing.T) {
	withInfraConcurrency(t, 1)
	errObserve := errors.New("refresh against cloud failed")
	fi := newFakeInfra()
	fi.setObserveErr(errObserve)
	fl := newFakeLifecycle()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.Error(t, err)
	assert.ErrorIs(t, err, errObserve)
	assert.Contains(t, err.Error(), "failed to observe infrastructure")
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 0, fi.applyCallCount(), "a refresh error must never kick an apply")
	assert.Equal(t, 0, fl.callCount("UpdateReconciliation"))
}

// TestHandleInfraCreate_GetReconciliationError covers the initial-fetch
// failure branch: the wrapped error is returned and nothing is built or
// observed.
func TestHandleInfraCreate_GetReconciliationError(t *testing.T) {
	withInfraConcurrency(t, 1)
	errFetch := errors.New("api unavailable")
	fl := newFakeLifecycle()
	fl.setErr("GetReconciliation", errFetch)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.Error(t, err)
	assert.ErrorIs(t, err, errFetch)
	assert.Contains(t, err.Error(), "failed to get reconciliation state")
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
}

// TestHandleInfraCreate_BuildInfraError covers the build-failure branch:
// the wrapped error is returned and nothing is observed or kicked.
func TestHandleInfraCreate_BuildInfraError(t *testing.T) {
	withInfraConcurrency(t, 1)
	errBuild := errors.New("build infra failed")
	fl := newFakeLifecycle()
	fl.setErr("BuildInfra", errBuild)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.Error(t, err)
	assert.ErrorIs(t, err, errBuild)
	assert.Equal(t, int64(0), requeue)
}
