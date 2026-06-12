package provider

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"gorm.io/datatypes"
)

// compile-time interface satisfaction checks for the observe-model fakes.
var (
	_ InfraLifecycleProvider = (*fakeLifecycle)(nil)
	_ InfraProvider          = (*fakeInfra)(nil)
)

// errFakeInfra is the fallback error returned by the infra fake when a
// method is set to fail but no specific error was injected.
var errFakeInfra = errors.New("fakeInfra: injected failure")

// baselineGoroutines holds the goroutine count captured before any test
// runs, so a test that spawns background work can poll against it.
var baselineGoroutines int

// TestMain snapshots the baseline goroutine count, then runs the test
// binary. It deliberately does nothing else: no global skips, no setup.
func TestMain(m *testing.M) {
	baselineGoroutines = runtime.NumGoroutine()
	os.Exit(m.Run())
}

// newTestLogger returns a pointer to a discard logger matching the
// *logr.Logger parameter shape the lifecycle handlers take.
func newTestLogger() *logr.Logger {
	l := logr.Discard()
	return &l
}

// timePtr returns a pointer to the given time, for building reconciliation
// snapshots inline.
func timePtr(t time.Time) *time.Time {
	return &t
}

// jsonPtr returns a pointer to the given string as a JSON value, for
// building resource inventories inline.
func jsonPtr(s string) *datatypes.JSON {
	j := datatypes.JSON(s)
	return &j
}

// validStackState returns a state JSON in deployment format with one
// managed resource, sufficient for countManagedResources to report a
// non-empty stack.
func validStackState() *datatypes.JSON {
	return jsonPtr(`{"deployment":{"resources":[{"urn":"urn:fake:resource","type":"fake:Resource"}]}}`)
}

// applyMode selects the behavior of a fakeInfra Apply or Destroy call.
type applyMode int

const (
	// applySucceed makes the call return nil immediately.
	applySucceed applyMode = iota

	// applyError makes the call return the injected error, or errFakeInfra
	// when none was injected.
	applyError

	// applyBlock makes the call block until released, so a test can hold a
	// semaphore slot open and observe the concurrency cap.
	applyBlock
)

// fakeInfra implements InfraProvider with a programmable observed phase
// sequence and per-method call counters. Observe walks the programmed
// observations in order and repeats the last one once exhausted, so a
// test can hold the observed phase across many requeue passes. Safe for
// concurrent use.
type fakeInfra struct {
	mu sync.Mutex

	// observations is the programmed sequence Observe walks. observeIndex
	// advances until the last entry, which then repeats.
	observations []Observation
	observeIndex int
	observeErr   error

	applyMode applyMode
	applyErr  error

	destroyMode applyMode
	destroyErr  error

	applyRelease    chan struct{}
	applyReleased   bool
	destroyRelease  chan struct{}
	destroyReleased bool

	observeCalls  int
	applyCalls    int
	destroyCalls  int
	setStateCalls int
	getStateCalls int

	restoredStates []*datatypes.JSON
	setStateErr    error

	stackState  *datatypes.JSON
	getStateErr error
}

// newFakeInfra returns an infra fake whose Observe reports PhaseAbsent,
// whose Apply and Destroy succeed immediately, and whose captured stack
// state defaults to a valid one-resource deployment.
func newFakeInfra() *fakeInfra {
	return &fakeInfra{
		observations:   []Observation{{Phase: PhaseAbsent}},
		applyRelease:   make(chan struct{}),
		destroyRelease: make(chan struct{}),
		stackState:     validStackState(),
	}
}

// Observe returns the next programmed observation and any injected error.
func (f *fakeInfra) Observe(ctx context.Context) (Observation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observeCalls++
	if f.observeErr != nil {
		return Observation{}, f.observeErr
	}
	obs := f.observations[f.observeIndex]
	if f.observeIndex < len(f.observations)-1 {
		f.observeIndex++
	}
	return obs, nil
}

// Apply runs the programmed apply behavior.
func (f *fakeInfra) Apply(ctx context.Context) error {
	f.mu.Lock()
	f.applyCalls++
	mode := f.applyMode
	err := f.applyErr
	release := f.applyRelease
	f.mu.Unlock()

	return runApplyMode(mode, err, release)
}

// Destroy runs the programmed destroy behavior.
func (f *fakeInfra) Destroy(ctx context.Context) error {
	f.mu.Lock()
	f.destroyCalls++
	mode := f.destroyMode
	err := f.destroyErr
	release := f.destroyRelease
	f.mu.Unlock()

	return runApplyMode(mode, err, release)
}

// runApplyMode executes the shared mode dispatch for apply and destroy.
func runApplyMode(mode applyMode, err error, release chan struct{}) error {
	switch mode {
	case applyError:
		if err != nil {
			return err
		}
		return errFakeInfra
	case applyBlock:
		<-release
		return nil
	}
	return nil
}

// SetStackState records the restored state and returns the injected error.
func (f *fakeInfra) SetStackState(state *datatypes.JSON) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setStateCalls++
	f.restoredStates = append(f.restoredStates, state)
	return f.setStateErr
}

// GetStackState returns the programmed stack state and error.
func (f *fakeInfra) GetStackState() (*datatypes.JSON, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getStateCalls++
	return f.stackState, f.getStateErr
}

// setObservations programs the phase sequence Observe walks.
func (f *fakeInfra) setObservations(obs ...Observation) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observations = obs
	f.observeIndex = 0
}

// setObserveErr programs an error returned by every Observe call.
func (f *fakeInfra) setObserveErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observeErr = err
}

// setApply programs the apply mode and, for applyError, the error returned.
func (f *fakeInfra) setApply(mode applyMode, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applyMode = mode
	f.applyErr = err
}

// setDestroy programs the destroy mode and, for applyError, the error
// returned.
func (f *fakeInfra) setDestroy(mode applyMode, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.destroyMode = mode
	f.destroyErr = err
}

// setGetStackState programs the state and error returned when the handler
// captures stack state.
func (f *fakeInfra) setGetStackState(state *datatypes.JSON, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stackState = state
	f.getStateErr = err
}

// setSetStackStateErr programs the error returned when the handler restores
// stack state.
func (f *fakeInfra) setSetStackStateErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setStateErr = err
}

// releaseApply unblocks an apply call in applyBlock mode. Idempotent.
func (f *fakeInfra) releaseApply() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.applyReleased {
		close(f.applyRelease)
		f.applyReleased = true
	}
}

// releaseDestroy unblocks a destroy call in applyBlock mode. Idempotent.
func (f *fakeInfra) releaseDestroy() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.destroyReleased {
		close(f.destroyRelease)
		f.destroyReleased = true
	}
}

// observeCallCount returns the number of Observe invocations.
func (f *fakeInfra) observeCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.observeCalls
}

// applyCallCount returns the number of Apply invocations.
func (f *fakeInfra) applyCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.applyCalls
}

// destroyCallCount returns the number of Destroy invocations.
func (f *fakeInfra) destroyCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.destroyCalls
}

// setStackStateCallCount returns the number of state-restore invocations.
func (f *fakeInfra) setStackStateCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.setStateCalls
}

// lastRestoredState returns the state passed to the most recent
// state-restore call, or nil if none occurred.
func (f *fakeInfra) lastRestoredState() *datatypes.JSON {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.restoredStates) == 0 {
		return nil
	}
	return f.restoredStates[len(f.restoredStates)-1]
}

// fakeLifecycle implements InfraLifecycleProvider with per-method call
// counters, per-method error injection, a programmable sequence of
// reconciliation snapshots, and a capture of every snapshot passed to
// UpdateReconciliation. Safe for concurrent use.
type fakeLifecycle struct {
	mu sync.Mutex

	calls map[string]int
	errs  map[string]error

	snaps     []*ReconciliationSnapshot
	snapIndex int

	infra InfraProvider

	updated []ReconciliationSnapshot
}

// newFakeLifecycle returns a lifecycle fake that walks the given
// reconciliation snapshots in order: each GetReconciliation call consumes
// the next snapshot, and once exhausted the last snapshot repeats. With no
// snapshots, an empty snapshot (a brand new create request) repeats. The
// built infra defaults to a fresh fakeInfra.
func newFakeLifecycle(snaps ...*ReconciliationSnapshot) *fakeLifecycle {
	return &fakeLifecycle{
		calls: make(map[string]int),
		errs:  make(map[string]error),
		snaps: snaps,
		infra: newFakeInfra(),
	}
}

// recordSimple counts a call and returns its injected error, if any.
func (f *fakeLifecycle) recordSimple(method string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[method]++
	return f.errs[method]
}

// GetReconciliation counts the call and returns the next snapshot in the
// programmed sequence. An injected error is returned without advancing the
// sequence. Callers must not mutate returned snapshots.
func (f *fakeLifecycle) GetReconciliation() (*ReconciliationSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["GetReconciliation"]++
	if err := f.errs["GetReconciliation"]; err != nil {
		return nil, err
	}
	if len(f.snaps) == 0 {
		return &ReconciliationSnapshot{}, nil
	}
	snap := f.snaps[f.snapIndex]
	if f.snapIndex < len(f.snaps)-1 {
		f.snapIndex++
	}
	return snap, nil
}

// UpdateReconciliation counts the call, records the snapshot for later
// assertion, and returns its injected error.
func (f *fakeLifecycle) UpdateReconciliation(snapshot ReconciliationSnapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["UpdateReconciliation"]++
	f.updated = append(f.updated, snapshot)
	return f.errs["UpdateReconciliation"]
}

// BuildInfra counts the call and returns the configured infra, or the
// injected error.
func (f *fakeLifecycle) BuildInfra() (InfraProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["BuildInfra"]++
	if err := f.errs["BuildInfra"]; err != nil {
		return nil, err
	}
	return f.infra, nil
}

// OnCreateConfirmed counts the call and returns its injected error.
func (f *fakeLifecycle) OnCreateConfirmed(infra InfraProvider) error {
	return f.recordSimple("OnCreateConfirmed")
}

// OnDeleteConfirmed counts the call and returns its injected error.
func (f *fakeLifecycle) OnDeleteConfirmed(infra InfraProvider) error {
	return f.recordSimple("OnDeleteConfirmed")
}

// PublishCreateNotification counts the call and returns its injected error.
func (f *fakeLifecycle) PublishCreateNotification() error {
	return f.recordSimple("PublishCreateNotification")
}

// PublishDeleteNotification counts the call and returns its injected error.
func (f *fakeLifecycle) PublishDeleteNotification() error {
	return f.recordSimple("PublishDeleteNotification")
}

// setErr injects an error for the named interface method; passing nil
// clears the injection.
func (f *fakeLifecycle) setErr(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.errs, method)
		return
	}
	f.errs[method] = err
}

// setInfra replaces the infra returned when the lifecycle builds one.
func (f *fakeLifecycle) setInfra(infra InfraProvider) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.infra = infra
}

// callCount returns how many times the named interface method was called.
func (f *fakeLifecycle) callCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[method]
}

// updates returns a copy of every snapshot passed to UpdateReconciliation
// in call order.
func (f *fakeLifecycle) updates() []ReconciliationSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ReconciliationSnapshot, len(f.updated))
	copy(out, f.updated)
	return out
}

// lastUpdate returns the most recent snapshot passed to
// UpdateReconciliation, and whether any update occurred.
func (f *fakeLifecycle) lastUpdate() (ReconciliationSnapshot, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.updated) == 0 {
		return ReconciliationSnapshot{}, false
	}
	return f.updated[len(f.updated)-1], true
}
