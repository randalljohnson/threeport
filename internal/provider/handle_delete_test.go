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
		return inFlightCount() == 0 && len(currentSemaphore()) == 0
	}, 5*time.Second, 5*time.Millisecond, "in-flight infrastructure operations did not drain")
}

// TestHandleInfraDelete_NotScheduled_Error pins the validation branch: a
// delete notification for an instance with no DeletionScheduled timestamp
// returns the "received but not scheduled" error and never acknowledges or
// builds infra.
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

// TestHandleInfraDelete_AlreadyConfirmed_EarlyReturn pins the idempotency
// branch: when DeletionConfirmed is already set the handler returns (0, nil)
// without acknowledging, building infra, or launching anything.
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

// TestHandleInfraDelete_CrossReplicaSafety_Requeue60 pins the cross-replica
// guard: a fresh CreationAcknowledged with no CreationConfirmed means a
// create is in progress on some replica, so the delete requeues at 60
// seconds without launching.
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

// TestHandleInfraDelete_StaleCreateAck_AllowsDelete pins the guard's stale
// escape hatch: when the CreationAcknowledged timestamp has gone stale the
// create is presumed interrupted, the guard passes, and the delete proceeds
// to acknowledge, build infra, and launch the destroy goroutine.
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

// TestHandleInfraDelete_FreshAckButCreateFailed_StillRequeues60 is a
// behavior pin for the cross-replica guard: it ignores
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

// TestHandleInfraDelete_AckedInventoryCleared_Confirms pins the confirmation
// branch: with deletion already acknowledged, the handler re-fetches state
// and reads inventory cleared from the latest snapshot's ResourceInventory,
// then refreshes the ack, builds infra, runs post-deletion cleanup, confirms
// deletion, and returns (0, nil) without launching a goroutine.
func TestHandleInfraDelete_AckedInventoryCleared_Confirms(t *testing.T) {
	// "{}" is one of the values that count as cleared inventory
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

// TestHandleInfraDelete_OnDeleteConfirmedError_Requeue60 pins the cleanup
// retry branch: when post-deletion cleanup fails the handler requeues at 60
// seconds and does not confirm deletion, so the next reconciliation retries
// the cleanup.
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

// TestHandleInfraDelete_AckedInventoryNotCleared_FreshAck_Requeue60 pins the
// wait branch: deletion acknowledged, inventory still holds resources, and
// the ack is fresh, meaning a destroy is presumed in progress elsewhere, so
// the handler requeues at 60 seconds without re-acking or launching.
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

// TestHandleInfraDelete_AckedInventoryNotCleared_StaleAck_Relaunches pins
// the recovery branch: deletion acknowledged, inventory still holds
// resources, but the ack has gone stale, so the prior destroy is presumed
// interrupted. The handler re-acks, builds infra, restores the surviving
// inventory into the stack, and re-launches the destroy goroutine.
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

// TestHandleInfraDelete_NewRequest_AcksBuildsLaunches pins the happy-path
// launch branch: deletion scheduled with no prior acknowledgement means a
// brand new delete request, so the handler acks deletion, builds infra,
// launches the destroy goroutine, and returns the 300 second requeue.
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
