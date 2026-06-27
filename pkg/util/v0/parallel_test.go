package v0

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

// TestRunParallelRunsEveryTaskDespiteAnError covers RunParallel executing all
// tasks even when one returns an error, surfacing the error in the aggregate.
func TestRunParallelRunsEveryTaskDespiteAnError(t *testing.T) {
	// a counter the tasks bump so we can prove every one ran
	var ran int32
	failure := errors.New("boom")
	tasks := []func() error{
		func() error { atomic.AddInt32(&ran, 1); return nil },
		func() error { atomic.AddInt32(&ran, 1); return failure },
		func() error { atomic.AddInt32(&ran, 1); return nil },
	}
	// the action under test: run the tasks across two workers
	err := RunParallel(2, tasks)
	// the failing task does not short-circuit the others
	if got := atomic.LoadInt32(&ran); got != 3 {
		t.Errorf("ran %d tasks, want all 3", got)
	}
	// the single failure surfaces in the aggregate error
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("RunParallel error = %v, want it to surface boom", err)
	}
}

// TestRunParallelRunsAllSequentiallyBelowOne covers RunParallel collapsing a
// worker count below one to a single sequential worker that still runs every
// task.
func TestRunParallelRunsAllSequentiallyBelowOne(t *testing.T) {
	// a counter the tasks bump under a zero parallel value
	var ran int32
	tasks := []func() error{
		func() error { atomic.AddInt32(&ran, 1); return nil },
		func() error { atomic.AddInt32(&ran, 1); return nil },
	}
	// the action under test: a zero worker count collapses to sequential
	if err := RunParallel(0, tasks); err != nil {
		t.Fatalf("RunParallel returned error: %v", err)
	}
	// every task still runs despite the sub-one worker count
	if got := atomic.LoadInt32(&ran); got != 2 {
		t.Errorf("ran %d tasks, want all 2", got)
	}
}

// TestRunParallelAggregatesEveryError covers RunParallel collecting every
// failing task's error into the returned aggregate.
func TestRunParallelAggregatesEveryError(t *testing.T) {
	// two distinct failures from separate tasks
	tasks := []func() error{
		func() error { return errors.New("first failure") },
		func() error { return nil },
		func() error { return errors.New("second failure") },
	}
	// the action under test: run the mixed success and failure set
	err := RunParallel(3, tasks)
	if err == nil {
		t.Fatalf("RunParallel returned nil, want an aggregate error")
	}
	// both failures appear in the aggregate, not just the first
	if !strings.Contains(err.Error(), "first failure") || !strings.Contains(err.Error(), "second failure") {
		t.Errorf("aggregate error = %q, want both failures", err.Error())
	}
}

// TestRunParallelEmptyTasksReturnsNil covers RunParallel returning nil when
// given no tasks.
func TestRunParallelEmptyTasksReturnsNil(t *testing.T) {
	// an empty task slice has nothing to run or fail
	if err := RunParallel(4, nil); err != nil {
		t.Errorf("RunParallel(nil) = %v, want nil", err)
	}
}

// TestRunParallelAllSuccessReturnsNil covers RunParallel returning nil when
// every task succeeds.
func TestRunParallelAllSuccessReturnsNil(t *testing.T) {
	// every task succeeds
	tasks := []func() error{
		func() error { return nil },
		func() error { return nil },
	}
	// the action under test: a fully successful run yields no error
	if err := RunParallel(2, tasks); err != nil {
		t.Errorf("RunParallel = %v, want nil", err)
	}
}
