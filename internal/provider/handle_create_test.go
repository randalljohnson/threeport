package provider

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestConfig returns lifecycle tunables for create handler tests:
// production stale threshold, a refresh interval long enough that the
// ack refresher never ticks, a single semaphore slot, and fast persist
// retries so failure paths drain quickly.
func createTestConfig() LifecycleConfig {
	return LifecycleConfig{
		StaleAckThreshold: 240 * time.Second,
		RefreshInterval:   time.Hour,
		SemaphoreCapacity: 1,
		PersistRetries:    1,
		PersistRetryDelay: time.Millisecond,
	}
}

// waitForCreateCond polls cond until it returns true or a generous
// deadline passes, then fails the test. Used only to drain background
// goroutines, never to assert timing-dependent logic.
func waitForCreateCond(t *testing.T, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}

// TestHandleInfraCreate_AlreadyConfirmed_EarlyReturn covers an already
// confirmed create: the handler returns (0, nil) with no further provider calls.
func TestHandleInfraCreate_AlreadyConfirmed_EarlyReturn(t *testing.T) {
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		CreationConfirmed: timePtr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
	})

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 1, fl.callCount("GetReconciliation"))
	assert.Equal(t, 0, fl.callCount("IsCreateComplete"))
	assert.Equal(t, 0, fl.callCount("AckCreation"))
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
	assert.Equal(t, 0, fl.callCount("ConfirmCreation"))
}

// TestHandleInfraCreate_AckedComplete_ConfirmsInOrder covers an acked,
// complete create: BuildInfra, OnCreateConfirmed, and ConfirmCreation run
// without re-acking or launching. Count order is checked by the error-
// propagation test below, where a post-creation failure blocks confirmation.
func TestHandleInfraCreate_AckedComplete_ConfirmsInOrder(t *testing.T) {
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		CreationAcknowledged: timePtr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
	})
	fl.setCreateComplete(true)
	fi := newFakeInfra()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 1, fl.callCount("IsCreateComplete"))
	assert.Equal(t, 1, fl.callCount("BuildInfra"))
	assert.Equal(t, 1, fl.callCount("OnCreateConfirmed"))
	assert.Equal(t, 1, fl.callCount("ConfirmCreation"))

	// the confirm path never re-acks or deploys
	assert.Equal(t, 0, fl.callCount("AckCreation"))
	assert.Equal(t, 0, fi.deployCallCount())
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraCreate_OnCreateConfirmedError_Propagates covers a
// post-creation failure: the error returns to the reconciler and ConfirmCreation
// never runs.
func TestHandleInfraCreate_OnCreateConfirmedError_Propagates(t *testing.T) {
	errPostCreate := errors.New("post-creation work failed")
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		CreationAcknowledged: timePtr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
	})
	fl.setCreateComplete(true)
	fl.setErr("OnCreateConfirmed", errPostCreate)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.Error(t, err)
	assert.ErrorIs(t, err, errPostCreate)
	assert.Contains(t, err.Error(), "failed to run post-creation work")
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 1, fl.callCount("OnCreateConfirmed"))
	assert.Equal(t, 0, fl.callCount("ConfirmCreation"))
}

// TestHandleInfraCreate_AckedIncomplete_FreshAck_Requeue120 covers a fresh
// incomplete ack: the handler requeues at 120 without re-acking or relaunching.
func TestHandleInfraCreate_AckedIncomplete_FreshAck_Requeue120(t *testing.T) {
	restoreConfig := setLifecycleConfig(createTestConfig())
	t.Cleanup(restoreConfig)

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	restoreClock := setLifecycleClock(newFakeClock(base))
	t.Cleanup(restoreClock)

	// acked at the clock's current time: zero elapsed, well inside the
	// stale threshold
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		CreationAcknowledged: timePtr(base),
	})

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(120), requeue)
	assert.Equal(t, 1, fl.callCount("IsCreateComplete"))
	assert.Equal(t, 0, fl.callCount("AckCreation"))
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraCreate_StaleAck_Relaunches covers a stale incomplete ack:
// the handler re-acks and launches create, treating the prior op as interrupted.
func TestHandleInfraCreate_StaleAck_Relaunches(t *testing.T) {
	restoreConfig := setLifecycleConfig(createTestConfig())
	t.Cleanup(restoreConfig)

	// clock sits one second past the stale threshold relative to the ack
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	restoreClock := setLifecycleClock(newFakeClock(base.Add(241 * time.Second)))
	t.Cleanup(restoreClock)

	fi := newFakeInfra()
	fi.setDeploy(infraBlock, nil)
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		CreationAcknowledged: timePtr(base),
	})
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(120), requeue)
	assert.Equal(t, 1, fl.callCount("AckCreation"))
	assert.Equal(t, 1, fl.callCount("BuildInfra"))

	// confirm the goroutine launched, then drain it
	waitForCreateCond(t, "deploy launch", func() bool {
		return fi.deployCallCount() == 1
	})
	fi.releaseDeploy()
	waitForCreateCond(t, "in-flight drain", func() bool {
		return inFlightCount() == 0
	})
}

// TestHandleInfraCreate_NewRequest_AcksBuildsLaunches covers a brand-new
// create: ack, build, deletion re-check, launch, and return (120, nil).
func TestHandleInfraCreate_NewRequest_AcksBuildsLaunches(t *testing.T) {
	restoreConfig := setLifecycleConfig(createTestConfig())
	t.Cleanup(restoreConfig)

	fi := newFakeInfra()
	fi.setDeploy(infraBlock, nil)
	fl := newFakeLifecycle()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(120), requeue)
	assert.Equal(t, 1, fl.callCount("AckCreation"))
	assert.Equal(t, 1, fl.callCount("BuildInfra"))

	// initial fetch plus the pre-launch deletion-scheduled check; deploy
	// is still blocked, so the success callback has not fetched yet
	assert.Equal(t, 2, fl.callCount("GetReconciliation"))

	// confirm the goroutine launched, then drain it
	waitForCreateCond(t, "deploy launch", func() bool {
		return fi.deployCallCount() == 1
	})
	fi.releaseDeploy()
	waitForCreateCond(t, "in-flight drain", func() bool {
		return inFlightCount() == 0
	})
}

// TestHandleInfraCreate_AckCreationError covers AckCreation failure: the
// wrapped error returns and nothing is built or launched.
func TestHandleInfraCreate_AckCreationError(t *testing.T) {
	errAck := errors.New("ack write failed")
	fl := newFakeLifecycle()
	fl.setErr("AckCreation", errAck)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.Error(t, err)
	assert.ErrorIs(t, err, errAck)
	assert.Contains(t, err.Error(), "failed to acknowledge creation")
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraCreate_DeletionScheduledBeforeLaunch_Aborts covers the
// pre-acknowledge deletion check: DeletionScheduled on the second fetch aborts
// with (0, nil) before ack or build, leaving no fresh ack for the delete
// handler's cross-replica guard.
func TestHandleInfraCreate_DeletionScheduledBeforeLaunch_Aborts(t *testing.T) {
	fi := newFakeInfra()
	fl := newFakeLifecycle(
		&ReconciliationSnapshot{},
		&ReconciliationSnapshot{
			DeletionScheduled: timePtr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
	)
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 0, fl.callCount("AckCreation"))
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
	assert.Equal(t, 2, fl.callCount("GetReconciliation"))
	assert.Equal(t, 0, fi.deployCallCount())
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraCreate_DeletionScheduledDuringInfra_SuppressesNotification
// covers deletion scheduled mid-create: outputs still save, but the create
// notification is suppressed so the delete handler can proceed.
func TestHandleInfraCreate_DeletionScheduledDuringInfra_SuppressesNotification(t *testing.T) {
	restoreConfig := setLifecycleConfig(createTestConfig())
	t.Cleanup(restoreConfig)

	fi := newFakeInfra()
	fi.setDeploy(infraBlock, nil)

	// snapshots: initial fetch sees a new create, the pre-launch check is
	// clear, and the success-callback fetch sees deletion scheduled
	fl := newFakeLifecycle(
		&ReconciliationSnapshot{},
		&ReconciliationSnapshot{},
		&ReconciliationSnapshot{
			DeletionScheduled: timePtr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
	)
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(120), requeue)

	// let the deploy finish so the success callback runs, then drain
	waitForCreateCond(t, "deploy launch", func() bool {
		return fi.deployCallCount() == 1
	})
	fi.releaseDeploy()
	waitForCreateCond(t, "in-flight drain", func() bool {
		return inFlightCount() == 0
	})

	// outputs saved, notification suppressed
	assert.Equal(t, 1, fl.callCount("SaveCreateOutputs"))
	assert.Equal(t, validStackState(), fl.createOutputs())
	assert.Equal(t, 0, fl.callCount("PublishCreateNotification"))
}

// TestHandleInfraCreate_GetReconciliationError_FirstFetch covers an initial
// GetReconciliation failure: the wrapped error returns with no state changes.
func TestHandleInfraCreate_GetReconciliationError_FirstFetch(t *testing.T) {
	errFetch := errors.New("api unavailable")
	fl := newFakeLifecycle()
	fl.setErr("GetReconciliation", errFetch)

	requeue, err := HandleInfraCreate(fl, newTestLogger())

	require.Error(t, err)
	assert.ErrorIs(t, err, errFetch)
	assert.Contains(t, err.Error(), "failed to get reconciliation state")
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 0, fl.callCount("AckCreation"))
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
}
