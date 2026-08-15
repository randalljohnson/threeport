package provider

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
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
		if inFlightCount() == 0 && len(infraSemaphore) == 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Errorf(
		"lifecycle goroutines did not drain: inFlight=%d, heldSlots=%d",
		inFlightCount(), len(infraSemaphore),
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

// TestSemaphoreBackpressure_Requeue30 covers the non-blocking semaphore
// acquire: with capacity 2 and blocking deploys, the first two creates
// get slots and requeue at 120, the next three get (30, nil) without
// launching a goroutine, and slots freed by completed deploys can be
// acquired by a subsequent call.
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

// TestSemaphoreReleaseOnPanic covers the create launch goroutine's recover
// path: a panicking deploy is recovered, the failure is persisted via
// SetCreationFailed, and the semaphore slot is released so a subsequent
// create can acquire it.
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

// TestSemaphoreReleaseOnPanic_Delete asserts the delete launch goroutine's
// recover path: a panicking destroy is recovered, the deletion failure is
// persisted via SetDeletionFailed so the next reconciliation retries, and the
// semaphore slot is released for a subsequent delete.
func TestSemaphoreReleaseOnPanic_Delete(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	fi := newFakeInfra()
	fi.setDestroy(infraPanic, nil)
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: timePtr(time.Now().UTC()),
	})
	fl.setInfra(fi)

	requeue, err := HandleInfraDelete(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(300), requeue)

	// the test process surviving the panic plus a clean drain confirms the
	// recover; the delete path persists the deletion failure on panic
	waitForSemaphoreDrain(t)
	require.Equal(t, 1, fi.destroyCallCount())
	require.Equal(t, 1, fl.callCount("SetDeletionFailed"))
	require.Equal(t, 0, fl.callCount("SetCreationFailed"))
	require.Equal(t, 0, fl.callCount("SaveState"))

	// with capacity 1, a successful follow-up launch proves the panicking
	// goroutine released its slot
	fl2 := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: timePtr(time.Now().UTC()),
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

// TestExecuteInfraCreate_DeployError_CapturesStateAndPersistsFailure covers
// the deploy failure branch: partial state is captured via GetStackState
// and saved for retry restoration, then the failure is persisted via
// SetCreationFailed, and the success callbacks never run.
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

// TestExecuteInfraCreate_VerifyStateFails_PersistsFailure asserts the state
// verification gate: a successful deploy whose captured state matches no known
// Pulumi schema fails verification, persists the failure, and never invokes
// the success callback.
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

	// deploy succeeded but the unrecognized state failed verification
	require.Equal(t, 1, fi.deployCallCount())
	require.Equal(t, 1, fi.getStackStateCallCount())
	require.Equal(t, 1, fl.callCount("SetCreationFailed"))

	// the success callback never ran
	require.Equal(t, 0, fl.callCount("SaveCreateOutputs"))
	require.Equal(t, 0, fl.callCount("PublishCreateNotification"))
}

// TestExecuteInfraDelete_InvalidExistingStateJSON_SkipsRestore covers the
// delete goroutine's corrupt-state guard: invalid existing state JSON
// skips the restore entirely but the destroy still proceeds and the
// success callbacks run.
func TestExecuteInfraDelete_InvalidExistingStateJSON_SkipsRestore(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	fi := newFakeInfra()
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: timePtr(time.Now().UTC()),
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

// TestSemaphoreSerializesPerStack asserts that a second HandleInfraCreate
// call pointed at a stack that already has an operation in flight is
// rejected non-blockingly with the 30-second requeue delay, without
// launching a competing deploy. The reconciler is a poll loop that must
// never block, so per-stack serialization is enforced by short-circuit
// requeue, not by holding the caller until the running deploy finishes.
// The second call becomes eligible to launch once the running deploy
// releases the per-stack lock on the next reconcile pass.
func TestSemaphoreSerializesPerStack(t *testing.T) {
	configureSemaphoreTest(t, 5)
	log := newTestLogger()

	// two fakes share the same stack key so both callers contend on the
	// same per-stack lock
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

	// first call: acquire the per-stack lock and launch a blocking deploy
	requeue1, err := HandleInfraCreate(fl1, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue1)

	// wait until fi1 is actually inside the deploy so the lock is held by
	// the goroutine before the second call arrives
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fi1.deployCallCount() == 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	require.Equal(t, 1, fi1.deployCallCount())

	// second call for the same stack while the first is still in flight
	// must return quickly with the pool-full requeue delay and must NOT
	// launch a competing deploy
	requeue2, err := HandleInfraCreate(fl2, log)
	require.NoError(t, err)
	require.Equal(t, int64(30), requeue2, "second call for in-flight stack must non-blockingly requeue at 30")
	require.Equal(t, 0, fi2.deployCallCount(), "second stack-mate must not deploy while first still holds per-stack lock")

	// release fi1's deploy so its goroutine returns and releases the
	// per-stack lock; a subsequent reconcile pass for the same stack
	// should now succeed and launch its deploy
	fi1.releaseDeploy()
	waitForSemaphoreDrain(t)

	requeue3, err := HandleInfraCreate(fl2, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue3, "third call after lock release must launch and requeue at 120")

	waitForSemaphoreDrain(t)
	require.Equal(t, 1, fi2.deployCallCount(), "second caller failed to run its deploy after the first released the lock")
}

// TestSemaphoreAllowsDifferentStacks asserts that two HandleInfraCreate calls
// pointed at different stack keys can launch concurrently under the global
// cap: distinct stacks do not share a per-stack lock, so unrelated reconciles
// run in parallel up to the cap.
func TestSemaphoreAllowsDifferentStacks(t *testing.T) {
	configureSemaphoreTest(t, 5)
	log := newTestLogger()

	// two fakes with distinct stack keys and blocking deploys
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

	// both HandleInfraCreate calls acquire their own per-stack lock and
	// return promptly since distinct keys don't contend
	requeue1, err := HandleInfraCreate(fl1, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue1)

	requeue2, err := HandleInfraCreate(fl2, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue2)

	// both deploys reach their blocking calls concurrently, proving they
	// were launched independently instead of serializing
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fi1.deployCallCount() == 1 && fi2.deployCallCount() == 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	require.Equal(t, 1, fi1.deployCallCount())
	require.Equal(t, 1, fi2.deployCallCount())

	// release both and drain
	fi1.releaseDeploy()
	fi2.releaseDeploy()
	waitForSemaphoreDrain(t)
}

// TestExecuteInfraDelete_DestroyError_CapturesStateAndPersistsFailure asserts
// the destroy failure branch: remaining state is captured via GetStackState
// and saved so retries know which resources remain, the deletion failure is
// persisted via SetDeletionFailed so the next reconciliation retries promptly,
// and the success callbacks never run.
func TestExecuteInfraDelete_DestroyError_CapturesStateAndPersistsFailure(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	errDestroy := errors.New("destroy exploded")
	fi := newFakeInfra()
	fi.setDestroy(infraError, errDestroy)
	fl := newFakeLifecycle(&ReconciliationSnapshot{
		DeletionScheduled: timePtr(time.Now().UTC()),
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

	// failure persisted so the retry does not wait for the ack to go stale
	require.Equal(t, 1, fl.callCount("SetDeletionFailed"))

	// success callbacks skipped
	require.Equal(t, 0, fl.callCount("ClearInventory"))
	require.Equal(t, 0, fl.callCount("PublishDeleteNotification"))
}

// TestDeployInfra_TransientLockError_DoesNotSetCreationFailed asserts that a
// transient pulumi stack-lock error from DeployInfra does not flip
// CreationFailed=true. A transient error is expected to clear on the next
// reconcile pass, so persisting a permanent failure would widen the retry
// path and short-circuit subsequent reconciles.
func TestDeployInfra_TransientLockError_DoesNotSetCreationFailed(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	// deploy returns the canonical transient marker so the classifier
	// recognizes it and skips persistFailure
	errLocked := errors.New("stack is currently locked by 1 lock(s)")
	fi := newFakeInfra()
	fi.setDeploy(infraError, errLocked)
	fl := newFakeLifecycle()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue)

	waitForSemaphoreDrain(t)

	// deploy attempted, but the transient error must not persist
	// CreationFailed=true and must not run the success callbacks
	require.Equal(t, 1, fi.deployCallCount())
	require.Equal(t, 0, fl.callCount("SetCreationFailed"), "transient error incorrectly flipped CreationFailed=true")
	require.Equal(t, 0, fl.callCount("SaveCreateOutputs"))
	require.Equal(t, 0, fl.callCount("PublishCreateNotification"))
}

// TestDeployInfra_PermanentError_SetsCreationFailed asserts that a permanent
// deploy error still flips CreationFailed=true so the reconciler retries
// promptly instead of waiting for the acknowledgement to go stale. This
// covers the classifier's negative case and the pre-existing behavior.
func TestDeployInfra_PermanentError_SetsCreationFailed(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	// a plain error that matches no transient marker
	errPermanent := errors.New("gcp compute api rejected instance: invalid machine type")
	fi := newFakeInfra()
	fi.setDeploy(infraError, errPermanent)
	fl := newFakeLifecycle()
	fl.setInfra(fi)

	requeue, err := HandleInfraCreate(fl, log)
	require.NoError(t, err)
	require.Equal(t, int64(120), requeue)

	waitForSemaphoreDrain(t)

	// deploy attempted and the failure was persisted so the retry does
	// not wait for the ack to go stale
	require.Equal(t, 1, fi.deployCallCount())
	require.Equal(t, 1, fl.callCount("SetCreationFailed"))
	require.Equal(t, 0, fl.callCount("SaveCreateOutputs"))
	require.Equal(t, 0, fl.callCount("PublishCreateNotification"))
}

// TestDeployInfra_TransientErrorAfterCreationConfirmed_LeavesCreationFailedFalse
// covers the observed live failure: an in-place update on an already-confirmed
// runtime instance re-runs the create state machine (the update handler adapts
// the create lifecycle for an update pass). If the up hits a transient error,
// CreationFailed must stay false so the next reconcile pass re-fires without
// short-circuiting on the permanent-failure path.
func TestDeployInfra_TransientErrorAfterCreationConfirmed_LeavesCreationFailedFalse(t *testing.T) {
	configureSemaphoreTest(t, 1)
	log := newTestLogger()

	// snapshot mimics an already-confirmed create with an intact inventory
	// so the update path restores state before running deploy
	confirmedAt := time.Now().UTC().Add(-time.Hour)
	acknowledgedAt := confirmedAt.Add(-time.Minute)
	snap := &ReconciliationSnapshot{
		CreationAcknowledged: timePtr(acknowledgedAt),
		CreationConfirmed:    timePtr(confirmedAt),
		ResourceInventory:    validStackState(),
	}

	// a transient error surfacing on the second-pass deploy
	errTransient := errors.New("failed to update stack: refreshing stack: stack is currently locked by 1 lock(s)")
	fi := newFakeInfra()
	fi.setDeploy(infraError, errTransient)

	// the update handler in production feeds the create state machine a
	// snapshot with confirmation cleared so it falls through to launch a
	// fresh up; simulate that by presenting a snapshot that lacks the
	// confirmation on the second GetReconciliation call
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

	// deploy attempted, transient error surfaced, but CreationFailed
	// must NOT be flipped or the next reconcile pass short-circuits into
	// a permanent-failure path and the update never converges
	require.Equal(t, 1, fi.deployCallCount())
	require.Equal(t, 0, fl.callCount("SetCreationFailed"), "transient error on post-confirmation update incorrectly flipped CreationFailed=true")
	require.Equal(t, 0, fl.callCount("SaveCreateOutputs"))
	require.Equal(t, 0, fl.callCount("PublishCreateNotification"))
}

// TestIsTransientPulumiError_MarkerMatch asserts that the transient error
// classifier accepts the canonical pulumi stack-lock message that surfaces
// when a prior operation left a stale lock; the classifier's whole purpose
// is to keep this class of error out of the permanent-failure path.
func TestIsTransientPulumiError_MarkerMatch(t *testing.T) {
	// each marker string must be recognized so a real error carrying
	// that phrase routes to the transient path
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

// TestIsTransientPulumiError_PermanentRejected asserts that a permanent
// failure (invalid input, unauthenticated, resource not found) does not
// match the transient markers so the caller still persists CreationFailed.
func TestIsTransientPulumiError_PermanentRejected(t *testing.T) {
	permanent := []string{
		"gcp compute api rejected instance: invalid machine type",
		"authentication required: no credentials found",
		"resource not found: project sxalable-module",
	}
	for _, msg := range permanent {
		require.False(t, isTransientPulumiError(errors.New(msg)), "permanent error %q should not classify as transient", msg)
	}

	// nil in must return false
	require.False(t, isTransientPulumiError(nil))
}
