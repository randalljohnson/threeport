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

// TestHandleInfraCreate_AlreadyConfirmed_EarlyReturn pins the branch
// where CreationConfirmed is already set: the handler returns (0, nil)
// after the initial fetch with no further calls on the provider.
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

// TestHandleInfraCreate_AckedComplete_ConfirmsInOrder pins the branch
// where creation is acknowledged, not failed, and complete: the handler
// builds infra, runs post-creation work, confirms creation, and returns
// (0, nil) without re-acking or launching. The fakes record counts, not
// sequence, so the BuildInfra -> OnCreateConfirmed -> ConfirmCreation
// order is pinned indirectly by the error-propagation test below, which
// shows a post-creation failure prevents confirmation.
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

// TestHandleInfraCreate_OnCreateConfirmedError_Propagates pins the
// branch where post-creation work fails: the wrapped error is returned
// to the reconciler for retry and ConfirmCreation is never called, so
// the create is not marked confirmed past a failed post-creation step.
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

// TestHandleInfraCreate_AckedIncomplete_FreshAck_Requeue120 pins the
// branch where creation is acknowledged but incomplete and the ack is
// still fresh: the handler requeues at 120 seconds without re-acking
// or relaunching.
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

// TestHandleInfraCreate_StaleAck_Relaunches pins the branch where
// creation is acknowledged but incomplete and the ack has gone stale:
// the handler re-acks and launches the create goroutine, indicating the
// prior operation was interrupted.
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

// TestHandleInfraCreate_NewRequest_AcksBuildsLaunches pins the brand
// new create path: no ack on the snapshot, so the handler acks, builds
// infra, re-fetches reconciliation for the deletion check, launches the
// create goroutine, and returns (120, nil).
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

// TestHandleInfraCreate_AckCreationError pins the branch where the
// creation acknowledgement write fails: the wrapped error is returned
// and nothing is built or launched.
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

// TestHandleInfraCreate_DeletionScheduledBeforeLaunch_Aborts pins the
// pre-launch deletion check: when the second reconciliation fetch shows
// DeletionScheduled set, the handler aborts with (0, nil) and never
// deploys, leaving the field clear for the delete handler.
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
	assert.Equal(t, 1, fl.callCount("AckCreation"))
	assert.Equal(t, 1, fl.callCount("BuildInfra"))
	assert.Equal(t, 2, fl.callCount("GetReconciliation"))
	assert.Equal(t, 0, fi.deployCallCount())
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraCreate_DeletionScheduledDuringInfra_SuppressesNotification
// pins the success-callback deletion check: when deletion is scheduled
// while infrastructure is being created, the callback still saves the
// create outputs but suppresses the create notification so the delete
// handler proceeds. Driven through a real launch with a blocked deploy
// so the callback runs on the production goroutine wiring.
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

// TestHandleInfraCreate_GetReconciliationError_FirstFetch pins the
// branch where the initial reconciliation fetch fails: the wrapped
// error is returned and no state transitions occur.
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
