package provider

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// peakWatcher samples inFlightCount in a tight loop and records the highest
// value seen, so a test can assert the concurrency cap was never exceeded at
// any instant rather than only at the sampling points.
type peakWatcher struct {
	peak int64
	stop chan struct{}
	done chan struct{}
}

func startPeakWatcher() *peakWatcher {
	w := &peakWatcher{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(w.done)
		for {
			select {
			case <-w.stop:
				return
			default:
				cur := inFlightCount()
				for {
					old := atomic.LoadInt64(&w.peak)
					if cur <= old || atomic.CompareAndSwapInt64(&w.peak, old, cur) {
						break
					}
				}
			}
		}
	}()
	return w
}

func (w *peakWatcher) stopAndPeak() int64 {
	close(w.stop)
	<-w.done
	return atomic.LoadInt64(&w.peak)
}

// TestHandleInfraCreate_NoDoubleKick is the headline create gate. Observe is
// held at provisioning after the first absent pass, modelling a create that
// is already in flight. The handler is driven through many requeue passes
// from concurrent goroutines sharing one infra; exactly one apply must ever
// fire, proving a create already underway is never kicked a second time no
// matter how the requeue loop races. Run under -race -count=5.
func TestHandleInfraCreate_NoDoubleKick(t *testing.T) {
	const (
		workers = 8
		passes  = 50
	)
	withInfraConcurrency(t, 5)

	fi := newFakeInfra()
	// first observe reports absent so the create kicks once; every later
	// observe reports provisioning so no further kick fires
	fi.setObservations(
		Observation{Phase: PhaseAbsent},
		Observation{Phase: PhaseProvisioning, State: validStackState()},
	)
	fi.setApply(applySucceed, nil)
	fi.setGetStackState(validStackState(), nil)

	fl := newFakeLifecycle()
	fl.setInfra(fi)

	log := newTestLogger()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := 0; p < passes; p++ {
				requeue, err := HandleInfraCreate(fl, log)
				if err != nil {
					t.Errorf("create handler errored: %v", err)
					return
				}
				// a kick or a persist requeues for provisioning; a confirm
				// returns zero; nothing else on this path
				assert.Contains(t, []int64{0, int64(requeueProvisioning)}, requeue)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, fi.applyCallCount(),
		"a create already in flight must be kicked exactly once across all requeue passes")
}

// TestHandleInfraDelete_NoDoubleKick is the symmetric headline destroy gate.
// Observe is held at deleting after the first present pass; the handler is
// driven through many requeue passes from concurrent goroutines sharing one
// infra; exactly one destroy must ever fire. Run under -race -count=5.
func TestHandleInfraDelete_NoDoubleKick(t *testing.T) {
	const (
		workers = 8
		passes  = 50
	)
	withInfraConcurrency(t, 5)

	fi := newFakeInfra()
	// first observe reports resources present so the destroy kicks once;
	// every later observe reports deleting so no further kick fires
	fi.setObservations(
		Observation{Phase: PhaseReady, State: validStackState()},
		Observation{Phase: PhaseDeleting, State: validStackState()},
	)
	fi.setDestroy(applySucceed, nil)
	fi.setGetStackState(validStackState(), nil)

	fl := newFakeLifecycle(scheduledSnap())
	fl.setInfra(fi)

	log := newTestLogger()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := 0; p < passes; p++ {
				requeue, err := HandleInfraDelete(fl, log)
				if err != nil {
					t.Errorf("delete handler errored: %v", err)
					return
				}
				assert.Contains(t, []int64{0, int64(requeueDeleting)}, requeue)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, fi.destroyCallCount(),
		"a teardown already in flight must be kicked exactly once across all requeue passes")
}

// TestInfraConcurrency_CapNeverExceeded is the headline scale test. It models
// the reconciler requeue loop driving many instances whose apply blocks, so
// every semaphore slot fills. The watcher proves the count of concurrently
// executing apply steps never exceeds the injected capacity, and a reconcile
// that cannot acquire a slot requeues without launching. Run under -race.
func TestInfraConcurrency_CapNeverExceeded(t *testing.T) {
	const (
		n = 200
		k = 5
	)
	withInfraConcurrency(t, k)

	fis := make([]*fakeInfra, n)
	fls := make([]*fakeLifecycle, n)
	for i := range fis {
		fi := newFakeInfra()
		fi.setObservations(Observation{Phase: PhaseAbsent})
		fi.setApply(applyBlock, nil)
		fis[i] = fi
		fl := newFakeLifecycle()
		fl.setInfra(fi)
		fls[i] = fl
	}
	// release every blocked apply before the drain cleanup runs (cleanups
	// are LIFO, and withInfraConcurrency registered the drain first)
	t.Cleanup(func() {
		for _, fi := range fis {
			fi.releaseApply()
		}
	})

	watcher := startPeakWatcher()
	log := newTestLogger()

	// each instance gets its own goroutine so blocked applies pile up and the
	// cap is actually pressured; a launched instance holds a slot, the rest
	// requeue without launching
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			requeue, err := HandleInfraCreate(fls[i], log)
			if err != nil {
				t.Errorf("create handler errored: %v", err)
				return
			}
			assert.Equal(t, int64(requeueProvisioning), requeue)
		}(i)
	}

	// the k slots should saturate with blocked applies
	require.Eventually(t, func() bool {
		return inFlightCount() == int64(k)
	}, 10*time.Second, time.Millisecond,
		"expected exactly %d blocked apply steps, got %d", k, inFlightCount())

	peak := watcher.stopAndPeak()
	assert.LessOrEqual(t, peak, int64(k),
		"concurrently executing apply steps must never exceed the cap")
	assert.Equal(t, int64(k), peak,
		"with blocking applies the cap should saturate")

	// release the blocked applies and let the launched reconciles finish
	for _, fi := range fis {
		fi.releaseApply()
	}
	wg.Wait()
	waitForInFlightDrain(t)
}

// TestInfraConcurrency_FullCapRequeuesWithoutKicking covers the backpressure
// branch directly: with the only slot held by a blocked apply, a second
// create requeues for provisioning without kicking its own apply.
func TestInfraConcurrency_FullCapRequeuesWithoutKicking(t *testing.T) {
	withInfraConcurrency(t, 1)

	// first instance: a blocking apply that holds the single slot
	holder := newFakeInfra()
	holder.setObservations(Observation{Phase: PhaseAbsent})
	holder.setApply(applyBlock, nil)
	holderFl := newFakeLifecycle()
	holderFl.setInfra(holder)
	t.Cleanup(holder.releaseApply)

	log := newTestLogger()
	go func() {
		_, _ = HandleInfraCreate(holderFl, log)
	}()

	// wait until the holder occupies the slot
	require.Eventually(t, func() bool {
		return inFlightCount() == 1
	}, 10*time.Second, time.Millisecond, "holder never acquired the slot")

	// second instance: must requeue without kicking, the slot is full
	second := newFakeInfra()
	second.setObservations(Observation{Phase: PhaseAbsent})
	second.setApply(applySucceed, nil)
	secondFl := newFakeLifecycle()
	secondFl.setInfra(second)

	requeue, err := HandleInfraCreate(secondFl, log)
	require.NoError(t, err)
	assert.Equal(t, int64(requeueProvisioning), requeue)
	assert.Equal(t, 0, second.applyCallCount(),
		"a create that cannot acquire a slot must requeue without kicking")

	holder.releaseApply()
	waitForInFlightDrain(t)
}
