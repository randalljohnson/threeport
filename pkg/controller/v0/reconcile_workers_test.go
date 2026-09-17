package controller

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestStartReconcileWorkersHonorsConcurrentReconciles covers N workers
// each getting a distinct shutdown channel.
func TestStartReconcileWorkersHonorsConcurrentReconciles(t *testing.T) {
	started := &atomic.Int32{}
	ready := make(chan struct{}, 8)

	// start three workers that wait on shutdown
	config := ReconcilerConfig{
		ConcurrentReconciles: 3,
		ReconcileFunc: func(r *Reconciler) {
			started.Add(1)
			ready <- struct{}{}
			<-r.Shutdown
		},
	}
	var shutdownChans []chan bool
	StartReconcileWorkers(config, Reconciler{}, &shutdownChans)

	// wait for each worker to report in
	for i := 0; i < 3; i++ {
		select {
		case <-ready:
		case <-time.After(time.Second):
			t.Fatalf("worker %d did not start", i+1)
		}
	}
	if got := started.Load(); got != 3 {
		t.Fatalf("started %d workers, want 3", got)
	}
	if len(shutdownChans) != 3 {
		t.Fatalf("got %d shutdown channels, want 3", len(shutdownChans))
	}

	// shut each worker down on its own channel
	seen := map[chan bool]struct{}{}
	for _, shutdownChan := range shutdownChans {
		if _, ok := seen[shutdownChan]; ok {
			t.Fatal("workers shared a shutdown channel")
		}
		seen[shutdownChan] = struct{}{}
		shutdownChan <- true
	}
}

// TestStartReconcileWorkersTreatsZeroAsOne covers a non-positive count
// still starting a single worker.
func TestStartReconcileWorkersTreatsZeroAsOne(t *testing.T) {
	started := &atomic.Int32{}
	ready := make(chan struct{}, 1)

	// start with a zero count so the helper must substitute one
	config := ReconcilerConfig{
		ConcurrentReconciles: 0,
		ReconcileFunc: func(r *Reconciler) {
			started.Add(1)
			ready <- struct{}{}
			<-r.Shutdown
		},
	}
	var shutdownChans []chan bool
	StartReconcileWorkers(config, Reconciler{}, &shutdownChans)

	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}

	if got := started.Load(); got != 1 {
		t.Fatalf("started %d workers, want 1", got)
	}
	if len(shutdownChans) != 1 {
		t.Fatalf("got %d shutdown channels, want 1", len(shutdownChans))
	}
	shutdownChans[0] <- true
}
