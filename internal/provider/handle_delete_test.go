package provider

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deleteTestBase is the frozen reference time used by the delete handler
// tests for fresh/stale acknowledgement timestamps.
var deleteTestBase = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// drainDeleteOps waits until all in-flight infrastructure operations have
// finished and released their semaphore slots, so the t.Cleanup restore of
// the lifecycle config cannot swap the semaphore out from under a goroutine
// that still holds a slot.
func drainDeleteOps(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		return inFlightCount() == 0 && len(infraSemaphore) == 0
	}, 5*time.Second, 5*time.Millisecond, "in-flight infrastructure operations did not drain")
}

// TestHandleInfraDelete_NotScheduled_Error covers a delete with no
// DeletionScheduled: returns the not-scheduled error and never acks or builds.
func TestHandleInfraDelete_NotScheduled_Error(t *testing.T) {
	// zero snapshots means an empty snapshot repeats: DeletionScheduled nil
	fl := newFakeLifecycle()

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "received but not scheduled")
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 1, fl.callCount("GetReconciliation"))
	assert.Equal(t, 0, fl.callCount("AckDeletion"))
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
}

// TestHandleInfraDelete_AlreadyConfirmed_EarlyReturn covers an already
// confirmed delete: returns (0, nil) without ack, build, or launch.
func TestHandleInfraDelete_AlreadyConfirmed_EarlyReturn(t *testing.T) {
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: timePtr(deleteTestBase.Add(-time.Hour)),
		DeletionConfirmed: timePtr(deleteTestBase.Add(-time.Minute)),
	})

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 1, fl.callCount("GetReconciliation"))
	assert.Equal(t, 0, fl.callCount("AckDeletion"))
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraDelete_CrossReplicaSafety_Requeue60 covers the cross-replica
// guard: a fresh CreationAcknowledged without CreationConfirmed requeues at 60.
func TestHandleInfraDelete_CrossReplicaSafety_Requeue60(t *testing.T) {
	restoreCfg := setLifecycleConfig(testLifecycleConfig())
	t.Cleanup(restoreCfg)
	clk := newFakeClock(deleteTestBase)
	restoreClk := setLifecycleClock(clk)
	t.Cleanup(restoreClk)

	// ack aged one minute against a 240 second threshold: fresh
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled:    timePtr(deleteTestBase.Add(-time.Hour)),
		CreationAcknowledged: timePtr(deleteTestBase.Add(-time.Minute)),
	})

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(60), requeue)
	assert.Equal(t, 1, fl.callCount("GetReconciliation"))
	assert.Equal(t, 0, fl.callCount("AckDeletion"))
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraDelete_StaleCreateAck_AllowsDelete covers a stale create ack:
// the guard passes and delete proceeds to ack, build, and launch destroy.
func TestHandleInfraDelete_StaleCreateAck_AllowsDelete(t *testing.T) {
	cfg := testLifecycleConfig()
	cfg.SemaphoreCapacity = 1
	restoreCfg := setLifecycleConfig(cfg)
	t.Cleanup(restoreCfg)
	clk := newFakeClock(deleteTestBase)
	restoreClk := setLifecycleClock(clk)
	t.Cleanup(restoreClk)

	fi := newFakeInfra()
	fi.setDestroy(infraBlock, nil)
	// ack aged ten minutes against a 240 second threshold: stale
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled:    timePtr(deleteTestBase.Add(-time.Hour)),
		CreationAcknowledged: timePtr(deleteTestBase.Add(-10 * time.Minute)),
	})
	fl.setInfra(fi)

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(300), requeue)
	require.Eventually(t, func() bool {
		return fi.destroyCallCount() == 1
	}, 5*time.Second, 5*time.Millisecond, "destroy goroutine never launched")
	assert.Equal(t, 1, fl.callCount("AckDeletion"))
	assert.Equal(t, 1, fl.callCount("BuildInfra"))
	assert.Equal(t, 2, fl.callCount("GetReconciliation"))

	fi.releaseDestroy()
	drainDeleteOps(t)
	assert.Equal(t, 1, fl.callCount("ClearInventory"))
	assert.Equal(t, 1, fl.callCount("PublishDeleteNotification"))
}

// TestHandleInfraDelete_FreshAckButCreateFailed_StillRequeues60 documents that
// the cross-replica guard ignores CreationFailed today: a fresh create ack
// with CreationFailed set still requeues at 60. A failed create may retry on
// another replica, so deleting underneath it is unsafe.
func TestHandleInfraDelete_FreshAckButCreateFailed_StillRequeues60(t *testing.T) {
	restoreCfg := setLifecycleConfig(testLifecycleConfig())
	t.Cleanup(restoreCfg)
	clk := newFakeClock(deleteTestBase)
	restoreClk := setLifecycleClock(clk)
	t.Cleanup(restoreClk)

	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled:    timePtr(deleteTestBase.Add(-time.Hour)),
		CreationAcknowledged: timePtr(deleteTestBase.Add(-time.Minute)),
		CreationFailed:       true,
	})

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(60), requeue)
	assert.Equal(t, 0, fl.callCount("AckDeletion"))
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraDelete_AckedInventoryCleared_Confirms covers an acked delete
// with cleared inventory: refresh ack, build, cleanup, confirm, return (0, nil).
func TestHandleInfraDelete_AckedInventoryCleared_Confirms(t *testing.T) {
	// "{}" is one of the cleared inventory sentinels
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled:    timePtr(deleteTestBase.Add(-time.Hour)),
		DeletionAcknowledged: timePtr(deleteTestBase.Add(-time.Minute)),
		ResourceInventory:    jsonPtr("{}"),
	})

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 2, fl.callCount("GetReconciliation"))
	assert.Equal(t, 1, fl.callCount("RefreshDeletionAck"))
	assert.Equal(t, 1, fl.callCount("BuildInfra"))
	assert.Equal(t, 1, fl.callCount("OnDeleteConfirmed"))
	assert.Equal(t, 1, fl.callCount("ConfirmDeletion"))
	assert.Equal(t, 0, fl.callCount("AckDeletion"))
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraDelete_OnDeleteConfirmedError_Requeue60 covers cleanup
// failure: requeue at 60 without ConfirmDeletion so the next pass retries.
func TestHandleInfraDelete_OnDeleteConfirmedError_Requeue60(t *testing.T) {
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled:    timePtr(deleteTestBase.Add(-time.Hour)),
		DeletionAcknowledged: timePtr(deleteTestBase.Add(-time.Minute)),
		ResourceInventory:    jsonPtr("{}"),
	})
	fl.setErr("OnDeleteConfirmed", errors.New("injected: post-deletion cleanup failure"))

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(60), requeue)
	assert.Equal(t, 1, fl.callCount("RefreshDeletionAck"))
	assert.Equal(t, 1, fl.callCount("OnDeleteConfirmed"))
	assert.Equal(t, 0, fl.callCount("ConfirmDeletion"))
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraDelete_AckedInventoryNotCleared_FreshAck_Requeue60 covers a
// fresh acked delete with inventory still present: requeue at 60, no relaunch.
func TestHandleInfraDelete_AckedInventoryNotCleared_FreshAck_Requeue60(t *testing.T) {
	restoreCfg := setLifecycleConfig(testLifecycleConfig())
	t.Cleanup(restoreCfg)
	clk := newFakeClock(deleteTestBase)
	restoreClk := setLifecycleClock(clk)
	t.Cleanup(restoreClk)

	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled:    timePtr(deleteTestBase.Add(-time.Hour)),
		DeletionAcknowledged: timePtr(deleteTestBase.Add(-time.Minute)),
		ResourceInventory:    validStackState(),
	})

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(60), requeue)
	assert.Equal(t, 2, fl.callCount("GetReconciliation"))
	assert.Equal(t, 0, fl.callCount("RefreshDeletionAck"))
	assert.Equal(t, 0, fl.callCount("AckDeletion"))
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraDelete_AckedFreshButDeletionFailed_RelaunchesPromptly asserts
// DeletionFailed closes the stale-window blind spot: a fresh ack with inventory
// still present re-acks, builds, restores inventory, and re-launches destroy
// immediately instead of waiting for the ack to age out.
func TestHandleInfraDelete_AckedFreshButDeletionFailed_RelaunchesPromptly(t *testing.T) {
	cfg := testLifecycleConfig()
	cfg.SemaphoreCapacity = 1
	restoreCfg := setLifecycleConfig(cfg)
	t.Cleanup(restoreCfg)
	clk := newFakeClock(deleteTestBase)
	restoreClk := setLifecycleClock(clk)
	t.Cleanup(restoreClk)

	fi := newFakeInfra()
	fi.setDestroy(infraBlock, nil)
	inventory := validStackState()
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled:    timePtr(deleteTestBase.Add(-time.Hour)),
		DeletionAcknowledged: timePtr(deleteTestBase.Add(-time.Minute)),
		DeletionFailed:       true,
		ResourceInventory:    inventory,
	})
	fl.setInfra(fi)

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(300), requeue)
	require.Eventually(t, func() bool {
		return fi.destroyCallCount() == 1
	}, 5*time.Second, 5*time.Millisecond, "destroy goroutine never relaunched after failed delete")
	assert.Equal(t, 1, fl.callCount("AckDeletion"))
	assert.Equal(t, 1, fl.callCount("BuildInfra"))

	assert.Equal(t, 1, fi.setStackStateCallCount())
	assert.Equal(t, inventory, fi.lastRestoredState())

	fi.releaseDestroy()
	drainDeleteOps(t)
	assert.Equal(t, 1, fl.callCount("ClearInventory"))
	assert.Equal(t, 1, fl.callCount("PublishDeleteNotification"))
}

// TestHandleInfraDelete_AckedInventoryNotCleared_StaleAck_Relaunches covers a
// stale acked delete with inventory still present: re-ack, build, restore, relaunch.
func TestHandleInfraDelete_AckedInventoryNotCleared_StaleAck_Relaunches(t *testing.T) {
	cfg := testLifecycleConfig()
	cfg.SemaphoreCapacity = 1
	restoreCfg := setLifecycleConfig(cfg)
	t.Cleanup(restoreCfg)
	clk := newFakeClock(deleteTestBase)
	restoreClk := setLifecycleClock(clk)
	t.Cleanup(restoreClk)

	fi := newFakeInfra()
	fi.setDestroy(infraBlock, nil)
	inventory := validStackState()
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled:    timePtr(deleteTestBase.Add(-time.Hour)),
		DeletionAcknowledged: timePtr(deleteTestBase.Add(-10 * time.Minute)),
		ResourceInventory:    inventory,
	})
	fl.setInfra(fi)

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(300), requeue)
	require.Eventually(t, func() bool {
		return fi.destroyCallCount() == 1
	}, 5*time.Second, 5*time.Millisecond, "destroy goroutine never launched")
	assert.Equal(t, 1, fl.callCount("AckDeletion"))
	assert.Equal(t, 1, fl.callCount("BuildInfra"))
	assert.Equal(t, 3, fl.callCount("GetReconciliation"))

	// state restoration happens before destroy, so it is settled by now
	assert.Equal(t, 1, fi.setStackStateCallCount())
	assert.Equal(t, inventory, fi.lastRestoredState())

	fi.releaseDestroy()
	drainDeleteOps(t)
	assert.Equal(t, 1, fl.callCount("ClearInventory"))
	assert.Equal(t, 1, fl.callCount("PublishDeleteNotification"))
}

// TestHandleInfraDelete_NewRequest_AcksBuildsLaunches covers a brand-new
// delete: ack, build, launch destroy, and return the 300 second requeue.
func TestHandleInfraDelete_NewRequest_AcksBuildsLaunches(t *testing.T) {
	cfg := testLifecycleConfig()
	cfg.SemaphoreCapacity = 1
	restoreCfg := setLifecycleConfig(cfg)
	t.Cleanup(restoreCfg)

	fi := newFakeInfra()
	fi.setDestroy(infraBlock, nil)
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: timePtr(deleteTestBase.Add(-time.Minute)),
	})
	fl.setInfra(fi)

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(300), requeue)
	require.Eventually(t, func() bool {
		return fi.destroyCallCount() == 1
	}, 5*time.Second, 5*time.Millisecond, "destroy goroutine never launched")
	assert.Equal(t, 1, fl.callCount("AckDeletion"))
	assert.Equal(t, 1, fl.callCount("BuildInfra"))
	assert.Equal(t, 2, fl.callCount("GetReconciliation"))

	// nil inventory means nothing to restore before the destroy
	assert.Equal(t, 0, fi.setStackStateCallCount())

	fi.releaseDestroy()
	drainDeleteOps(t)
	assert.Equal(t, 1, fl.callCount("ClearInventory"))
	assert.Equal(t, 1, fl.callCount("PublishDeleteNotification"))
}
