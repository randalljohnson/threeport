package v0

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunParallel_AllTasksSucceed covers the happy path: every task returns nil,
// so RunParallel returns nil and each task runs exactly once.
func TestRunParallel_AllTasksSucceed(t *testing.T) {
	// setup: five tasks that increment a shared counter
	var counter int64
	tasks := make([]func() error, 0, 5)
	for i := 0; i < 5; i++ {
		tasks = append(tasks, func() error {
			atomic.AddInt64(&counter, 1)
			return nil
		})
	}

	// action: run with two workers
	err := RunParallel(2, tasks)

	// assert: no error and every task ran exactly once
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got := atomic.LoadInt64(&counter); got != 5 {
		t.Fatalf("expected counter=5, got %d", got)
	}
}

// TestRunParallel_AggregatesErrors covers the error path: failing tasks feed
// into MultiError and RunParallel returns a single aggregated error containing
// every message, while non-failing tasks still run.
func TestRunParallel_AggregatesErrors(t *testing.T) {
	// setup: mix of failing and succeeding tasks
	var counter int64
	tasks := []func() error{
		func() error { atomic.AddInt64(&counter, 1); return errors.New("boom-1") },
		func() error { atomic.AddInt64(&counter, 1); return nil },
		func() error { atomic.AddInt64(&counter, 1); return errors.New("boom-2") },
		func() error { atomic.AddInt64(&counter, 1); return nil },
	}

	// action: run with three workers
	err := RunParallel(3, tasks)

	// assert: error contains both failure messages
	if err == nil {
		t.Fatal("expected aggregated error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "boom-1") || !strings.Contains(msg, "boom-2") {
		t.Fatalf("expected both messages in %q", msg)
	}
	// assert: succeeding tasks still executed
	if got := atomic.LoadInt64(&counter); got != 4 {
		t.Fatalf("expected all 4 tasks to run, got counter=%d", got)
	}
}

// TestRunParallel_EmptyTasks covers the boundary: nil-return on empty input
// without spawning stray work.
func TestRunParallel_EmptyTasks(t *testing.T) {
	// setup + action: no tasks
	err := RunParallel(4, nil)

	// assert: nil result
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	// action: explicit empty slice
	err = RunParallel(4, []func() error{})

	// assert: nil result again
	if err != nil {
		t.Fatalf("expected nil for empty slice, got %v", err)
	}
}

// TestRunParallel_ParallelismCollapse covers the subtle branch where a
// parallel value less than 1 collapses to a single sequential worker rather
// than deadlocking on a zero-worker pool.
func TestRunParallel_ParallelismCollapse(t *testing.T) {
	cases := []struct {
		name     string
		parallel int
	}{
		{"zero", 0},
		{"negative", -3},
		{"one", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// setup: three tasks incrementing a counter
			var counter int64
			tasks := []func() error{
				func() error { atomic.AddInt64(&counter, 1); return nil },
				func() error { atomic.AddInt64(&counter, 1); return nil },
				func() error { atomic.AddInt64(&counter, 1); return nil },
			}

			// action: run under sub-1 or 1 parallelism
			done := make(chan error, 1)
			go func() { done <- RunParallel(tc.parallel, tasks) }()

			// assert: completes without deadlock and runs every task
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("RunParallel deadlocked under collapsed worker count")
			}
			if got := atomic.LoadInt64(&counter); got != 3 {
				t.Fatalf("expected 3 tasks run, got %d", got)
			}
		})
	}
}

// TestRunParallel_ConcurrentExecution covers that worker count enables real
// concurrency: with parallel=N and N tasks that block on a barrier, all tasks
// must be in flight simultaneously for the barrier to release.
func TestRunParallel_ConcurrentExecution(t *testing.T) {
	// setup: barrier that releases only when three tasks are in flight
	const workers = 3
	var wg sync.WaitGroup
	wg.Add(workers)
	release := make(chan struct{})
	tasks := make([]func() error, workers)
	for i := 0; i < workers; i++ {
		tasks[i] = func() error {
			wg.Done()
			<-release
			return nil
		}
	}

	// action: run with three workers, then release once all are waiting
	done := make(chan error, 1)
	go func() { done <- RunParallel(workers, tasks) }()

	waitDone := make(chan struct{})
	go func() { wg.Wait(); close(waitDone) }()

	// assert: all three tasks reach the barrier concurrently
	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("tasks did not run concurrently under parallel=3")
	}
	close(release)

	// assert: RunParallel returns nil once tasks complete
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunParallel did not return after tasks released")
	}
}
