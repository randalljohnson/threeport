package provider

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	util "github.com/threeport/threeport/pkg/util/v0"
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
		return inFlightCount() == 0 && len(currentSemaphore()) == 0
	}, 5*time.Second, 5*time.Millisecond, "in-flight infrastructure operations did not drain")
}

// TestHandleInfraDelete_NotScheduled_Error rejects a delete that is not scheduled.
func TestHandleInfraDelete_NotScheduled_Error(t *testing.T) {
	// no snapshots: every fetch returns an empty snapshot, DeletionScheduled nil
	fl := newFakeLifecycle()

	// run delete against an unscheduled request
	requeue, err := HandleInfraDelete(fl, newTestLogger())

	// reject the request without acking or building
	require.Error(t, err)
	assert.Contains(t, err.Error(), "received but not scheduled")
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 1, fl.callCount("GetReconciliation"))
	assert.Equal(t, 0, fl.callCount("AckDeletion"))
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
}

// TestHandleInfraDelete_AlreadyConfirmed_EarlyReturn covers a confirmed
// delete returning immediately with no ack, build, or launch.
func TestHandleInfraDelete_AlreadyConfirmed_EarlyReturn(t *testing.T) {
	// scheduled and already confirmed
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: util.Ptr(deleteTestBase.Add(-time.Hour)),
		DeletionConfirmed: util.Ptr(deleteTestBase.Add(-time.Minute)),
	})

	// run delete against a confirmed deletion
	requeue, err := HandleInfraDelete(fl, newTestLogger())

	// return without acking, building, or launching
	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
	assert.Equal(t, 1, fl.callCount("GetReconciliation"))
	assert.Equal(t, 0, fl.callCount("AckDeletion"))
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraDelete_CrossReplicaSafety_Requeue60 covers a fresh
// create ack requeueing delete at 60 seconds without launching.
func TestHandleInfraDelete_CrossReplicaSafety_Requeue60(t *testing.T) {
	// install production stale threshold and freeze the clock
	restoreCfg := setLifecycleConfig(testLifecycleConfig())
	t.Cleanup(restoreCfg)
	clk := newFakeClock(deleteTestBase)
	restoreClk := setLifecycleClock(clk)
	t.Cleanup(restoreClk)

	// ack aged one minute against a 240 second threshold: fresh
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled:    util.Ptr(deleteTestBase.Add(-time.Hour)),
		CreationAcknowledged: util.Ptr(deleteTestBase.Add(-time.Minute)),
	})

	// run delete while a create ack is still fresh
	requeue, err := HandleInfraDelete(fl, newTestLogger())

	// requeue without acking, building, or launching
	require.NoError(t, err)
	assert.Equal(t, int64(60), requeue)
	assert.Equal(t, 1, fl.callCount("GetReconciliation"))
	assert.Equal(t, 0, fl.callCount("AckDeletion"))
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraDelete_StaleCreateAck_AllowsDelete covers a stale
// create ack letting delete acknowledge and launch.
func TestHandleInfraDelete_StaleCreateAck_AllowsDelete(t *testing.T) {
	// one semaphore slot and a frozen clock
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
		DeletionScheduled:    util.Ptr(deleteTestBase.Add(-time.Hour)),
		CreationAcknowledged: util.Ptr(deleteTestBase.Add(-10 * time.Minute)),
	})
	fl.setInfra(fi)

	// run delete against a stale create ack
	requeue, err := HandleInfraDelete(fl, newTestLogger())

	// launch destroy at 300 seconds
	require.NoError(t, err)
	assert.Equal(t, int64(300), requeue)
	require.Eventually(t, func() bool {
		return fi.destroyCallCount() == 1
	}, 5*time.Second, 5*time.Millisecond, "destroy goroutine never launched")
	assert.Equal(t, 1, fl.callCount("AckDeletion"))
	assert.Equal(t, 1, fl.callCount("BuildInfra"))
	assert.Equal(t, 2, fl.callCount("GetReconciliation"))

	// drain the blocked destroy
	fi.releaseDestroy()
	drainDeleteOps(t)
	assert.Equal(t, 1, fl.callCount("ClearInventory"))
	assert.Equal(t, 1, fl.callCount("PublishDeleteNotification"))
}

// TestHandleInfraDelete_FreshAckButCreateFailed_StillRequeues60 documents
// current behavior: the cross-replica guard ignores
// CreationFailed by design today, so a fresh CreationAcknowledged with
// CreationFailed set and no CreationConfirmed still requeues at 60 seconds
// without launching. A failed create may be retried by another replica at
// any moment, so deleting underneath it is unsafe; making the guard treat
// failed creates as deletable requires coordination with the provider
// adapter owners. If this test starts failing, that contract changed.
func TestHandleInfraDelete_FreshAckButCreateFailed_StillRequeues60(t *testing.T) {
	restoreCfg := setLifecycleConfig(testLifecycleConfig())
	t.Cleanup(restoreCfg)
	clk := newFakeClock(deleteTestBase)
	restoreClk := setLifecycleClock(clk)
	t.Cleanup(restoreClk)

	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled:    util.Ptr(deleteTestBase.Add(-time.Hour)),
		CreationAcknowledged: util.Ptr(deleteTestBase.Add(-time.Minute)),
		CreationFailed:       true,
	})

	requeue, err := HandleInfraDelete(fl, newTestLogger())

	require.NoError(t, err)
	assert.Equal(t, int64(60), requeue)
	assert.Equal(t, 0, fl.callCount("AckDeletion"))
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraDelete_AckedInventoryCleared_Confirms covers a cleared
// inventory running post-deletion cleanup then confirmation.
func TestHandleInfraDelete_AckedInventoryCleared_Confirms(t *testing.T) {
	// "{}" is one of the values that count as cleared inventory
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled:    util.Ptr(deleteTestBase.Add(-time.Hour)),
		DeletionAcknowledged: util.Ptr(deleteTestBase.Add(-time.Minute)),
		ResourceInventory:    jsonPtr("{}"),
	})

	// run delete against a cleared inventory
	requeue, err := HandleInfraDelete(fl, newTestLogger())

	// confirm deletion after post-deletion cleanup
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

// TestHandleInfraDelete_OnDeleteConfirmedError_Requeue60 covers a
// post-deletion cleanup failure requeueing at 60 seconds without confirming.
func TestHandleInfraDelete_OnDeleteConfirmedError_Requeue60(t *testing.T) {
	// cleared inventory with post-deletion cleanup failing
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled:    util.Ptr(deleteTestBase.Add(-time.Hour)),
		DeletionAcknowledged: util.Ptr(deleteTestBase.Add(-time.Minute)),
		ResourceInventory:    jsonPtr("{}"),
	})
	fl.setErr("OnDeleteConfirmed", errors.New("injected: post-deletion cleanup failure"))

	// run delete against the cleanup failure
	requeue, err := HandleInfraDelete(fl, newTestLogger())

	// requeue without confirming
	require.NoError(t, err)
	assert.Equal(t, int64(60), requeue)
	assert.Equal(t, 1, fl.callCount("RefreshDeletionAck"))
	assert.Equal(t, 1, fl.callCount("OnDeleteConfirmed"))
	assert.Equal(t, 0, fl.callCount("ConfirmDeletion"))
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraDelete_AckedInventoryNotCleared_FreshAck_Requeue60 covers
// a fresh deletion ack with remaining inventory requeueing at 60 seconds.
func TestHandleInfraDelete_AckedInventoryNotCleared_FreshAck_Requeue60(t *testing.T) {
	// install production stale threshold and freeze the clock
	restoreCfg := setLifecycleConfig(testLifecycleConfig())
	t.Cleanup(restoreCfg)
	clk := newFakeClock(deleteTestBase)
	restoreClk := setLifecycleClock(clk)
	t.Cleanup(restoreClk)

	// acked delete with remaining inventory and a fresh ack
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled:    util.Ptr(deleteTestBase.Add(-time.Hour)),
		DeletionAcknowledged: util.Ptr(deleteTestBase.Add(-time.Minute)),
		ResourceInventory:    validStackState(),
	})

	// run delete while the destroy is still in progress
	requeue, err := HandleInfraDelete(fl, newTestLogger())

	// requeue without refreshing, acking, or launching
	require.NoError(t, err)
	assert.Equal(t, int64(60), requeue)
	assert.Equal(t, 2, fl.callCount("GetReconciliation"))
	assert.Equal(t, 0, fl.callCount("RefreshDeletionAck"))
	assert.Equal(t, 0, fl.callCount("AckDeletion"))
	assert.Equal(t, 0, fl.callCount("BuildInfra"))
	assert.Equal(t, int64(0), inFlightCount())
}

// TestHandleInfraDelete_AckedFreshButDeletionFailed_RelaunchesPromptly
// covers a failed delete relaunching instead of waiting for a stale ack.
func TestHandleInfraDelete_AckedFreshButDeletionFailed_RelaunchesPromptly(t *testing.T) {
	// one semaphore slot and a frozen clock
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
	// ack aged one minute against a 240 second threshold: fresh, so without
	// the failed flag this would requeue at 60 instead of relaunching
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled:    util.Ptr(deleteTestBase.Add(-time.Hour)),
		DeletionAcknowledged: util.Ptr(deleteTestBase.Add(-time.Minute)),
		DeletionFailed:       true,
		ResourceInventory:    inventory,
	})
	fl.setInfra(fi)

	// run delete against a failed destroy with a still-fresh ack
	requeue, err := HandleInfraDelete(fl, newTestLogger())

	// relaunch immediately at 300 seconds
	require.NoError(t, err)
	assert.Equal(t, int64(300), requeue)
	require.Eventually(t, func() bool {
		return fi.destroyCallCount() == 1
	}, 5*time.Second, 5*time.Millisecond, "destroy goroutine never relaunched after failed delete")
	assert.Equal(t, 1, fl.callCount("AckDeletion"))
	assert.Equal(t, 1, fl.callCount("BuildInfra"))

	// surviving inventory is restored before the relaunched destroy
	assert.Equal(t, 1, fi.setStackStateCallCount())
	assert.Equal(t, inventory, fi.lastRestoredState())

	// drain the blocked destroy
	fi.releaseDestroy()
	drainDeleteOps(t)
	assert.Equal(t, 1, fl.callCount("ClearInventory"))
	assert.Equal(t, 1, fl.callCount("PublishDeleteNotification"))
}

// TestHandleInfraDelete_AckedInventoryNotCleared_StaleAck_Relaunches
// covers a stale deletion ack relaunching destroy.
func TestHandleInfraDelete_AckedInventoryNotCleared_StaleAck_Relaunches(t *testing.T) {
	// one semaphore slot and a frozen clock
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
		DeletionScheduled:    util.Ptr(deleteTestBase.Add(-time.Hour)),
		DeletionAcknowledged: util.Ptr(deleteTestBase.Add(-10 * time.Minute)),
		ResourceInventory:    inventory,
	})
	fl.setInfra(fi)

	// run delete against a stale deletion ack
	requeue, err := HandleInfraDelete(fl, newTestLogger())

	// relaunch destroy at 300 seconds
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

	// drain the blocked destroy
	fi.releaseDestroy()
	drainDeleteOps(t)
	assert.Equal(t, 1, fl.callCount("ClearInventory"))
	assert.Equal(t, 1, fl.callCount("PublishDeleteNotification"))
}

// TestHandleInfraDelete_NewRequest_AcksBuildsLaunches covers an unacked
// delete acknowledging, building, and launching.
func TestHandleInfraDelete_NewRequest_AcksBuildsLaunches(t *testing.T) {
	// one semaphore slot so the launch is deterministic
	cfg := testLifecycleConfig()
	cfg.SemaphoreCapacity = 1
	restoreCfg := setLifecycleConfig(cfg)
	t.Cleanup(restoreCfg)

	fi := newFakeInfra()
	fi.setDestroy(infraBlock, nil)
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: util.Ptr(deleteTestBase.Add(-time.Minute)),
	})
	fl.setInfra(fi)

	// run delete for a new request
	requeue, err := HandleInfraDelete(fl, newTestLogger())

	// ack, build, and launch at 300 seconds
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

	// drain the blocked destroy
	fi.releaseDestroy()
	drainDeleteOps(t)
	assert.Equal(t, 1, fl.callCount("ClearInventory"))
	assert.Equal(t, 1, fl.callCount("PublishDeleteNotification"))
}
