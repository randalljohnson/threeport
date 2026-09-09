package provider

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// compile-time interface satisfaction check for the order recorder.
var _ RefreshableProvider = (*orderRecordingInfra)(nil)

// configureSemaphoreTest installs a lifecycle config with the given
// semaphore capacity and fast persist retries, registers config
// restoration, and registers a drain that runs before restoration so no
// launch goroutine outlives the test's semaphore channel.
func configureSemaphoreTest(t *testing.T, capacity int) {
	t.Helper()
	restore := setLifecycleConfig(LifecycleConfig{
		StaleAckThreshold: 240 * time.Second,
		RefreshInterval:   time.Hour,
		SemaphoreCapacity: capacity,
		PersistRetries:    3,
		PersistRetryDelay: time.Millisecond,
	})
	t.Cleanup(restore)
	t.Cleanup(func() { waitForSemaphoreDrain(t) })
}

// waitForSemaphoreDrain polls until no infra operations are in flight and
// every semaphore slot has been released. The slot release is the last
// action of a launch goroutine, so an empty semaphore plus a zero
// in-flight count guarantees all launched goroutines have fully exited.
func waitForSemaphoreDrain(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if inFlightCount() == 0 && len(currentSemaphore()) == 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Errorf(
		"lifecycle goroutines did not drain: inFlight=%d, heldSlots=%d",
		inFlightCount(), len(currentSemaphore()),
	)
}

// orderRecordingInfra wraps fakeRefreshableInfra and records the global
// order of restore, refresh, and deploy calls so tests can assert the
// sequencing inside executeInfraCreate.
type orderRecordingInfra struct {
	*fakeRefreshableInfra

	omu   sync.Mutex
	order []string
}

// newOrderRecordingInfra returns an order recorder over a fresh
// refreshable infra fake.
func newOrderRecordingInfra() *orderRecordingInfra {
	return &orderRecordingInfra{fakeRefreshableInfra: newFakeRefreshableInfra()}
}

// record appends a call name to the recorded order.
func (o *orderRecordingInfra) record(name string) {
	o.omu.Lock()
	defer o.omu.Unlock()
	o.order = append(o.order, name)
}

// callOrder returns a copy of the recorded call order.
func (o *orderRecordingInfra) callOrder() []string {
	o.omu.Lock()
	defer o.omu.Unlock()
	out := make([]string, len(o.order))
	copy(out, o.order)
	return out
}

// SetStackState records its position in the call order, then delegates.
func (o *orderRecordingInfra) SetStackState(state *datatypes.JSON) error {
	o.record("SetStackState")
	return o.fakeRefreshableInfra.SetStackState(state)
}

// RefreshStack records its position in the call order, then delegates.
func (o *orderRecordingInfra) RefreshStack() error {
	o.record("RefreshStack")
	return o.fakeRefreshableInfra.RefreshStack()
}

// DeployInfra records its position in the call order, then delegates.
func (o *orderRecordingInfra) DeployInfra() error {
	o.record("DeployInfra")
	return o.fakeRefreshableInfra.DeployInfra()
}

// TestSemaphoreBackpressure_Requeue30 covers a non-blocking full-pool acquire:
// capacity two launches the first two creates at 120 and returns (30, nil)
// without launching the rest.
func TestSemaphoreBackpressure_Requeue30(t *testing.T) {
	configureSemaphoreTest(t, 2)
	log := newTestLogger()

	// drive 5 independent instances with blocking deploys
	var fis [5]*fakeInfra
	var fls [5]*fakeLifecycle
	for i := range fis {
		fis[i] = newFakeInfra()
		fis[i].setDeploy(infraBlock, nil)
		fls[i] = newFakeLifecycle()
		fls[i].setInfra(fis[i])
	}
	t.Cleanup(func() {
		for _, fi := range fis {
			fi.releaseDeploy()
		}
	})

	var requeues [5]int64
	for i := range fls {
		requeue, err := HandleInfraCreate(fls[i], log)
		require.NoError(t, err)
		requeues[i] = requeue
	}

	// the slot acquire is synchronous, so exactly the first two launch
	require.Equal(t, [5]int64{120, 120, 30, 30, 30}, requeues)

	// the two slot holders reach their deploys
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if fis[0].deployCallCount() == 1 && fis[1].deployCallCount() == 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	require.Equal(t, 1, fis[0].deployCallCount())
	require.Equal(t, 1, fis[1].deployCallCount())

	// the three rejected instances launched nothing
	for i := 2; i < 5; i++ {
		require.Equal(t, 0, fis[i].deployCallCount())
	}

	// release the blocked deploys and wait for the slots to free
	fis[0].releaseDeploy()
	fis[1].releaseDeploy()
	waitForSemaphoreDrain(t)

	// a subsequent call acquires a freed slot
	fl := newFakeLifecycle()
	requeue, err := HandleInfraCreate(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue)
}

// TestSemaphoreReleaseOnPanic covers a panicking create goroutine: recover
// persists the failure and the deferred release admits a follow-up launch.
func TestSemaphoreReleaseOnPanic(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	fi := newFakeInfra()
	fi.setDeploy(infraPanic, nil)
	fl := newFakeLifecycle()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue)

	// the slot release is the goroutine's last action, so after the drain
	// the recover path has already persisted the failure
	waitForSemaphoreDrain(t)
	require.Equal(t, 1, fi.deployCallCount())
	require.Equal(t, 1, fl.callCount("SetCreationFailed"))

	// with capacity 1, a successful follow-up launch proves the panicking
	// goroutine released its slot
	fl2 := newFakeLifecycle()
	requeue, err = HandleInfraCreate(fl2, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue)
}

// TestSemaphoreReleaseOnPanic_Delete covers a panicking delete goroutine:
// recover persists the deletion failure and the deferred release admits a follow-up.
func TestSemaphoreReleaseOnPanic_Delete(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	fi := newFakeInfra()
	fi.setDestroy(infraPanic, nil)
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: util.Ptr(time.Now().UTC()),
	})
	fl.setInfra(fi)

	requeue, err := HandleInfraDelete(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(300), requeue)

	// drain after the panic: recover persisted the deletion failure and released the slot
	waitForSemaphoreDrain(t)
	require.Equal(t, 1, fi.destroyCallCount())
	require.Equal(t, 1, fl.callCount("SetDeletionFailed"))
	require.Equal(t, 0, fl.callCount("SetCreationFailed"))
	require.Equal(t, 0, fl.callCount("SaveState"))

	// with capacity 1, a successful follow-up launch proves the panicking
	// goroutine released its slot
	fl2 := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: util.Ptr(time.Now().UTC()),
	})
	requeue, err = HandleInfraDelete(fl2, log)
	require.NoError(t, err)
	require.Equal(t, int64(300), requeue)
}

// TestExecuteInfraCreate_RestoreThenRefreshThenDeploy asserts the create
// goroutine's sequencing when existing state is present on a refreshable
// provider: state is restored first, then refreshed against cloud
// reality, then deployed.
func TestExecuteInfraCreate_RestoreThenRefreshThenDeploy(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	inventory := validStackState()
	oi := newOrderRecordingInfra()
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		ResourceInventory: inventory,
	})
	fl.setInfra(oi)

	requeue, err := HandleInfraCreate(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue)

	waitForSemaphoreDrain(t)

	require.Equal(
		t,
		[]string{"SetStackState", "RefreshStack", "DeployInfra"},
		oi.callOrder(),
	)
	require.Equal(t, 1, oi.refreshCallCount())
	require.NotNil(t, oi.lastRestoredState())
	require.JSONEq(t, string(*inventory), string(*oi.lastRestoredState()))

	// the success path completed after the ordered sequence
	require.Equal(t, 1, fl.callCount("SaveCreateOutputs"))
	require.Equal(t, 1, fl.callCount("PublishCreateNotification"))
}

// TestExecuteInfraCreate_NonStreamable_NoWatcher asserts that a provider
// without streaming support runs the create to success with no state
// watcher: SaveState is the watcher's only writer on the success path,
// so its count staying at zero proves no watcher streamed state.
func TestExecuteInfraCreate_NonStreamable_NoWatcher(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	fi := newFakeInfra()
	fl := newFakeLifecycle()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue)

	waitForSemaphoreDrain(t)

	// success path completed end to end
	require.Equal(t, 1, fi.deployCallCount())
	require.Equal(t, 1, fl.callCount("SaveCreateOutputs"))
	require.NotNil(t, fl.createOutputs())
	require.JSONEq(t, string(*validStackState()), string(*fl.createOutputs()))
	require.Equal(t, 1, fl.callCount("PublishCreateNotification"))

	// no existing state, so no restore; no watcher, so no streamed saves
	require.Equal(t, 0, fi.setStackStateCallCount())
	require.Equal(t, 0, fl.callCount("SaveState"))
}

// TestExecuteInfraCreate_DeployError_CapturesStateAndPersistsFailure
// covers a failed deploy: partial stack state is saved and creation is marked failed.
func TestExecuteInfraCreate_DeployError_CapturesStateAndPersistsFailure(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	errDeploy := errors.New("deploy exploded")
	fi := newFakeInfra()
	fi.setDeploy(infraError, errDeploy)
	fl := newFakeLifecycle()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue)

	waitForSemaphoreDrain(t)

	// partial state captured and saved for the retry
	require.Equal(t, 1, fi.getStackStateCallCount())
	saved := fl.savedStateHistory()
	require.Len(t, saved, 1)
	require.JSONEq(t, string(*validStackState()), string(*saved[0]))

	// failure persisted, success callbacks skipped
	require.Equal(t, 1, fl.callCount("SetCreationFailed"))
	require.Equal(t, 0, fl.callCount("SaveCreateOutputs"))
	require.Equal(t, 0, fl.callCount("PublishCreateNotification"))
}

// TestExecuteInfraCreate_VerifyStateFails_PersistsFailure covers a successful
// deploy whose captured state matches no known Pulumi schema: creation is marked failed.
func TestExecuteInfraCreate_VerifyStateFails_PersistsFailure(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	fi := newFakeInfra()
	fi.setGetStackState(jsonPtr(`{"unrecognized":"state"}`), nil)
	fl := newFakeLifecycle()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue)

	waitForSemaphoreDrain(t)

	// deploy succeeded; unrecognized state then failed verification
	require.Equal(t, 1, fi.deployCallCount())
	require.Equal(t, 1, fi.getStackStateCallCount())
	require.Equal(t, 1, fl.callCount("SetCreationFailed"))

	// the success callback never ran
	require.Equal(t, 0, fl.callCount("SaveCreateOutputs"))
	require.Equal(t, 0, fl.callCount("PublishCreateNotification"))
}

// TestExecuteInfraDelete_InvalidExistingStateJSON_SkipsRestore covers
// invalid inventory JSON: restore is skipped and destroy still runs.
func TestExecuteInfraDelete_InvalidExistingStateJSON_SkipsRestore(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	fi := newFakeInfra()
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: util.Ptr(time.Now().UTC()),
		ResourceInventory: jsonPtr(`{"deployment":{"resources":[`),
	})
	fl.setInfra(fi)

	requeue, err := HandleInfraDelete(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(300), requeue)

	waitForSemaphoreDrain(t)

	// restore skipped, destroy still ran
	require.Equal(t, 0, fi.setStackStateCallCount())
	require.Equal(t, 1, fi.destroyCallCount())

	// success callbacks ran
	require.Equal(t, 1, fl.callCount("ClearInventory"))
	require.Equal(t, 1, fl.callCount("PublishDeleteNotification"))
}

// TestSemaphoreSerializesPerStack covers the per-stack lock: a second
// create for an in-flight stack requeues non-blockingly at 30 without launching.
func TestSemaphoreSerializesPerStack(t *testing.T) {
	configureSemaphoreTest(t, 5)
	log := newTestLogger()

	// both instances share one stack key so the per-stack lock serializes them
	const sharedKey = "shared-stack"

	fi1 := newFakeInfra()
	fi1.setDeploy(infraBlock, nil)
	fl1 := newFakeLifecycle()
	fl1.setStackKey(sharedKey)
	fl1.setInfra(fi1)

	fi2 := newFakeInfra()
	fl2 := newFakeLifecycle()
	fl2.setStackKey(sharedKey)
	fl2.setInfra(fi2)

	t.Cleanup(func() {
		fi1.releaseDeploy()
	})

	// launch the first create; it holds the lock through a blocked deploy
	requeue1, err := HandleInfraCreate(fl1, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue1)

	// wait until the first deploy is inside before the second call
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fi1.deployCallCount() == 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	require.Equal(t, 1, fi1.deployCallCount())

	// a second create for the same stack must requeue immediately without launching
	requeue2, err := HandleInfraCreate(fl2, log)
	require.NoError(t, err)
	require.Equal(t, int64(30), requeue2, "second call for in-flight stack must non-blockingly requeue at 30")
	require.Equal(t, 0, fi2.deployCallCount(), "second stack-mate must not deploy while first still holds per-stack lock")

	// release the first deploy so the per-stack lock frees for a later create
	fi1.releaseDeploy()
	waitForSemaphoreDrain(t)

	requeue3, err := HandleInfraCreate(fl2, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue3, "third call after lock release must launch and requeue at 120")

	waitForSemaphoreDrain(t)
	require.Equal(t, 1, fi2.deployCallCount(), "second caller failed to run its deploy after the first released the lock")
}

// TestSemaphoreAllowsDifferentStacks covers concurrent creates on distinct
// stacks: both acquire a slot and both deploys run.
func TestSemaphoreAllowsDifferentStacks(t *testing.T) {
	configureSemaphoreTest(t, 5)
	log := newTestLogger()

	// two instances on distinct stack keys with blocking deploys
	fi1 := newFakeInfra()
	fi1.setDeploy(infraBlock, nil)
	fl1 := newFakeLifecycle()
	fl1.setStackKey("stack-a")
	fl1.setInfra(fi1)

	fi2 := newFakeInfra()
	fi2.setDeploy(infraBlock, nil)
	fl2 := newFakeLifecycle()
	fl2.setStackKey("stack-b")
	fl2.setInfra(fi2)

	t.Cleanup(func() {
		fi1.releaseDeploy()
		fi2.releaseDeploy()
	})

	// both creates launch promptly because the per-stack lock is per key
	requeue1, err := HandleInfraCreate(fl1, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue1)

	requeue2, err := HandleInfraCreate(fl2, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue2)

	// wait until both blocked deploys are inside concurrently
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fi1.deployCallCount() == 1 && fi2.deployCallCount() == 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	require.Equal(t, 1, fi1.deployCallCount())
	require.Equal(t, 1, fi2.deployCallCount())

	// release both deploys so the slots drain
	fi1.releaseDeploy()
	fi2.releaseDeploy()
	waitForSemaphoreDrain(t)
}

// TestExecuteInfraDelete_DestroyError_CapturesStateAndPersistsFailure
// covers a failed destroy: remaining stack state is saved and deletion is marked failed.
func TestExecuteInfraDelete_DestroyError_CapturesStateAndPersistsFailure(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	errDestroy := errors.New("destroy exploded")
	fi := newFakeInfra()
	fi.setDestroy(infraError, errDestroy)
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: util.Ptr(time.Now().UTC()),
	})
	fl.setInfra(fi)

	requeue, err := HandleInfraDelete(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(300), requeue)

	waitForSemaphoreDrain(t)

	// remaining state captured and saved for the retry
	require.Equal(t, 1, fi.destroyCallCount())
	require.Equal(t, 1, fi.getStackStateCallCount())
	saved := fl.savedStateHistory()
	require.Len(t, saved, 1)
	require.JSONEq(t, string(*validStackState()), string(*saved[0]))

	// deletion failure persisted so the retry does not wait for a stale ack
	require.Equal(t, 1, fl.callCount("SetDeletionFailed"))

	// success callbacks skipped
	require.Equal(t, 0, fl.callCount("ClearInventory"))
	require.Equal(t, 0, fl.callCount("PublishDeleteNotification"))
}

// TestDeployInfra_TransientLockError_DoesNotSetCreationFailed rejects
// marking creation failed when deploy returns a Pulumi stack-lock error.
func TestDeployInfra_TransientLockError_DoesNotSetCreationFailed(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	// lock error text contains the "stack is currently locked" marker
	errLocked := errors.New("stack is currently locked by 1 lock(s)")
	fi := newFakeInfra()
	fi.setDeploy(infraError, errLocked)
	fl := newFakeLifecycle()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue)

	waitForSemaphoreDrain(t)

	// deploy ran once; the transient lock error must not persist a failure
	require.Equal(t, 1, fi.deployCallCount())
	require.Equal(t, 0, fl.callCount("SetCreationFailed"), "transient error incorrectly flipped CreationFailed=true")
	require.Equal(t, 0, fl.callCount("SaveCreateOutputs"))
	require.Equal(t, 0, fl.callCount("PublishCreateNotification"))
}

// TestDeployInfra_PermanentError_SetsCreationFailed covers a non-transient
// deploy error: creation is marked failed.
func TestDeployInfra_PermanentError_SetsCreationFailed(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	// a provider rejection contains no transient marker
	errPermanent := errors.New("gcp compute api rejected instance: invalid machine type")
	fi := newFakeInfra()
	fi.setDeploy(infraError, errPermanent)
	fl := newFakeLifecycle()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue)

	waitForSemaphoreDrain(t)

	// deploy ran once; failure persisted so the retry does not wait for a stale ack
	require.Equal(t, 1, fi.deployCallCount())
	require.Equal(t, 1, fl.callCount("SetCreationFailed"))
	require.Equal(t, 0, fl.callCount("SaveCreateOutputs"))
	require.Equal(t, 0, fl.callCount("PublishCreateNotification"))
}

// TestDeployInfra_TransientErrorAfterCreationConfirmed_LeavesCreationFailedFalse
// covers a transient lock error on a create that restores existing inventory: creation stays not failed.
func TestDeployInfra_TransientErrorAfterCreationConfirmed_LeavesCreationFailedFalse(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	// inventory on the second snapshot is restored before deploy
	confirmedAt := time.Now().UTC().Add(-time.Hour)
	acknowledgedAt := confirmedAt.Add(-time.Minute)
	snap := &ReconciliationSnapshot{
		CreationAcknowledged: util.Ptr(acknowledgedAt),
		CreationConfirmed:    util.Ptr(confirmedAt),
		ResourceInventory:    validStackState(),
	}

	// wrapped lock error still matches the "stack is currently locked" marker
	errTransient := errors.New("failed to update stack: refreshing stack: stack is currently locked by 1 lock(s)")
	fi := newFakeInfra()
	fi.setDeploy(infraError, errTransient)

	// first snapshot is unconfirmed so the handler launches
	updateSnap := &ReconciliationSnapshot{
		CreationAcknowledged: nil,
		CreationConfirmed:    nil,
		ResourceInventory:    validStackState(),
	}
	fl := newFakeLifecycle(updateSnap, snap)
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue)

	waitForSemaphoreDrain(t)

	// deploy ran once; the transient error must not persist a failure
	require.Equal(t, 1, fi.deployCallCount())
	require.Equal(t, 0, fl.callCount("SetCreationFailed"), "transient error on post-confirmation update incorrectly flipped CreationFailed=true")
	require.Equal(t, 0, fl.callCount("SaveCreateOutputs"))
	require.Equal(t, 0, fl.callCount("PublishCreateNotification"))
}

// TestIsTransientPulumiError_MarkerMatch accepts error strings that contain
// a configured transient marker.
func TestIsTransientPulumiError_MarkerMatch(t *testing.T) {
	// one message per marker family: lock, deadline, quota, connection reset
	transient := []string{
		"stack is currently locked by 1 lock(s)",
		"rpc error: code = DeadlineExceeded desc = context deadline exceeded",
		"googleapi: Error 429: quotaExceeded",
		"read: connection reset by peer",
	}
	for _, msg := range transient {
		require.True(t, isTransientPulumiError(errors.New(msg)), "marker %q should classify as transient", msg)
	}
}

// TestIsTransientPulumiError_PermanentRejected rejects provider, auth, and
// not-found errors, and a nil error, as transient.
func TestIsTransientPulumiError_PermanentRejected(t *testing.T) {
	permanent := []string{
		"gcp compute api rejected instance: invalid machine type",
		"authentication required: no credentials found",
		"resource not found: project sxalable-module",
	}
	for _, msg := range permanent {
		require.False(t, isTransientPulumiError(errors.New(msg)), "permanent error %q should not classify as transient", msg)
	}

	// a nil error is not transient
	require.False(t, isTransientPulumiError(nil))
}

// TestExecuteInfraCreate_RestoreError_PersistsFailureWithoutDeploying
// covers the restore failure branch of the create goroutine. A retry whose
// stored inventory cannot be loaded back into the provider must not deploy,
// because deploying without the previous state would build a second copy of
// resources the first attempt already created.
func TestExecuteInfraCreate_RestoreError_PersistsFailureWithoutDeploying(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	fi := newFakeInfra()
	fi.setSetStackStateErr(errors.New("state blob is corrupt"))
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		ResourceInventory: validStackState(),
	})
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue)

	waitForSemaphoreDrain(t)

	require.Equal(t, 1, fi.setStackStateCallCount(), "the restore is attempted once")
	require.Equal(t, 0, fi.deployCallCount(), "a failed restore must not deploy")
	require.Equal(t, 1, fl.callCount("SetCreationFailed"))
	require.Equal(t, 0, fl.callCount("SaveCreateOutputs"))
	require.Equal(t, 0, fl.callCount("PublishCreateNotification"))
}

// TestExecuteInfraCreate_RefreshError_PersistsFailureWithoutDeploying
// covers the create side of the refresh policy. Create treats a failed
// refresh as fatal, because deploying against state that does not match
// cloud reality can duplicate or orphan resources.
func TestExecuteInfraCreate_RefreshError_PersistsFailureWithoutDeploying(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	ri := newFakeRefreshableInfra()
	ri.setRefreshErr(errors.New("refresh could not reach the cloud provider"))
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		ResourceInventory: validStackState(),
	})
	fl.setInfra(ri)

	requeue, err := HandleInfraCreate(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue)

	waitForSemaphoreDrain(t)

	require.Equal(t, 1, ri.refreshCallCount())
	require.Equal(t, 0, ri.deployCallCount(), "a failed refresh must not deploy on create")
	require.Equal(t, 1, fl.callCount("SetCreationFailed"))
	require.Equal(t, 0, fl.callCount("SaveCreateOutputs"))
}

// TestExecuteInfraDelete_RefreshError_StillDestroys covers the delete side
// of the same refresh policy, which is the opposite of create. Delete logs
// a failed refresh and destroys anyway, because refusing to destroy would
// strand the cloud resources the caller asked to remove.
func TestExecuteInfraDelete_RefreshError_StillDestroys(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	ri := newFakeRefreshableInfra()
	ri.setRefreshErr(errors.New("refresh could not reach the cloud provider"))
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: util.Ptr(deleteTestBase.Add(-time.Hour)),
		ResourceInventory: validStackState(),
	})
	fl.setInfra(ri)

	_, err := HandleInfraDelete(fl, log)
	require.NoError(t, err)

	waitForSemaphoreDrain(t)

	require.Equal(t, 1, ri.refreshCallCount())
	require.Equal(t, 1, ri.destroyCallCount(), "a failed refresh must not block the destroy")
}
