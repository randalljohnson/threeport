package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// peakWatcher samples inFlightCount and records the highest value it saw.
// Sampling can only miss a spike, never invent one, so a peak above the cap
// is proof the cap broke while a peak at or below it is corroborating
// evidence rather than a guarantee. The semaphore is what enforces the cap.
type peakWatcher struct {
	// peak is the highest in-flight count any sample observed.
	peak int64

	// stop closes to end the sampling loop.
	stop chan struct{}

	// done closes once the sampling goroutine has returned.
	done chan struct{}
}

// peakSampleInterval paces the sampling loop. Without it the watcher pins a
// core for the length of the test and competes with the goroutines it is
// measuring, which on a two-core runner changes the result it reports.
const peakSampleInterval = 100 * time.Microsecond

// startPeakWatcher launches the sampling goroutine and returns the watcher
// it samples into.
func startPeakWatcher() *peakWatcher {
	w := &peakWatcher{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(peakSampleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-ticker.C:
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

// stopAndPeak stops the sampling goroutine, waits for it to exit, and
// returns the highest in-flight count recorded.
func (w *peakWatcher) stopAndPeak() int64 {
	close(w.stop)
	<-w.done
	return atomic.LoadInt64(&w.peak)
}

// waitForInFlightZero polls until no infra operation is executing or the
// deadline passes.
func waitForInFlightZero(t *testing.T, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if inFlightCount() == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("in-flight operations did not drain to zero within %s (still %d)", within, inFlightCount())
}

// TestReconcilerLoop_2000Instances_SemaphoreCapped is the headline scale
// test. It models the reconciler requeue loop: the semaphore acquire is
// non-blocking, so a call when the pool is full returns (30, nil) without
// launching. Blocking deploys hold every slot so the cap is observable;
// the watcher proves the count of concurrently-executing operations never
// exceeds the injected capacity no matter how hard the loop drives.
func TestReconcilerLoop_2000Instances_SemaphoreCapped(t *testing.T) {
	const (
		n = 2000
		k = 5
	)
	configureSemaphoreTest(t, k)

	fis := make([]*fakeInfra, n)
	fls := make([]*fakeLifecycle, n)
	for i := range fis {
		fis[i] = newFakeInfra()
		fis[i].setDeploy(infraBlock, nil)
		fls[i] = newFakeLifecycle()
		fls[i].setInfra(fis[i])
	}
	// release before the drain cleanup runs (cleanups are LIFO, and
	// configureSemaphoreTest registered the drain wait first)
	t.Cleanup(func() {
		for _, fi := range fis {
			fi.releaseDeploy()
		}
	})

	watcher := startPeakWatcher()
	log := newTestLogger()

	// drive several full passes over every instance; with blocking
	// deploys only k slots ever fill and the rest requeue at 30
	for pass := 0; pass < 5; pass++ {
		for i := 0; i < n; i++ {
			requeue, err := HandleInfraCreate(fls[i], log)
			require.NoError(t, err)
			// a launched instance requeues at 120; a pool-full instance
			// at 30; never anything else on this path
			require.Contains(t, []int64{30, 120}, requeue)
		}
	}

	// the k blocked deploys should now hold every slot. the observed count
	// is recorded by the condition and reported after the wait: passing
	// inFlightCount() as a format argument reports the value from before
	// the wait started, which is always zero here. the store is atomic
	// because testify runs the condition on its own goroutine
	var lastCount atomic.Int64
	reached := assert.Eventually(t, func() bool {
		c := inFlightCount()
		lastCount.Store(c)
		return c == int64(k)
	}, 5*time.Second, time.Millisecond)
	require.True(t, reached,
		"expected exactly %d blocked operations, last saw %d", k, lastCount.Load())

	peak := watcher.stopAndPeak()
	assert.LessOrEqual(t, peak, int64(k), "concurrently executing operations must never exceed the semaphore capacity")
	assert.Equal(t, int64(k), peak, "with blocking deploys the pool should saturate at the capacity")

	// release and confirm the pool drains back to zero
	for _, fi := range fis {
		fi.releaseDeploy()
	}
	waitForInFlightZero(t, 10*time.Second)
}

// TestReconcilerLoop_2000CreateAndDelete_Mixed drives 1000 create and 1000
// delete launches interleaved under a small pool with fast-succeeding
// operations. It proves the cap holds under sustained mixed churn and that
// the loop never deadlocks, exercised under the race detector.
func TestReconcilerLoop_2000CreateAndDelete_Mixed(t *testing.T) {
	const (
		creates = 1000
		deletes = 1000
		k       = 25
	)
	configureSemaphoreTest(t, k)

	createFls := make([]*fakeLifecycle, creates)
	for i := range createFls {
		fi := newFakeInfra()
		fi.setDeploy(infraSucceed, nil)
		fl := newFakeLifecycle()
		fl.setInfra(fi)
		createFls[i] = fl
	}
	deleteFls := make([]*fakeLifecycle, deletes)
	for i := range deleteFls {
		fi := newFakeInfra()
		fi.setDestroy(infraSucceed, nil)
		fl := newFakeLifecycle(&ReconciliationSnapshot{
			DeletionScheduled: timePtr(deleteTestBase.Add(-time.Minute)),
		})
		fl.setInfra(fi)
		deleteFls[i] = fl
	}

	watcher := startPeakWatcher()
	log := newTestLogger()

	// the driver takes a stop channel and collects its own error rather
	// than calling t.Errorf: on the timeout path below the test goroutine
	// has already finished, and logging from a goroutine after its test
	// completes panics the whole binary
	stop := make(chan struct{})
	defer close(stop)

	done := make(chan struct{})
	var driverErr error
	go func() {
		defer close(done)
		for pass := 0; pass < 10; pass++ {
			for i := 0; i < creates; i++ {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := HandleInfraCreate(createFls[i], log); err != nil {
					driverErr = fmt.Errorf("create handler errored: %w", err)
					return
				}
				if _, err := HandleInfraDelete(deleteFls[i], log); err != nil {
					driverErr = fmt.Errorf("delete handler errored: %w", err)
					return
				}
			}
		}
	}()

	select {
	case <-done:
		require.NoError(t, driverErr)
	case <-time.After(60 * time.Second):
		t.Fatal("mixed reconciler loop deadlocked or made no progress within 60s")
	}

	peak := watcher.stopAndPeak()
	assert.LessOrEqual(t, peak, int64(k), "mixed create/delete churn must never exceed the semaphore capacity")

	waitForInFlightZero(t, 10*time.Second)
}

// TestPerInstanceStateDirIsolation builds many workspaces through the
// exported constructor with a shared temp root and asserts each resolves
// to a distinct state file path, then writes to all of them concurrently
// to prove the per-instance directories never collide. The writes go
// straight to the resolved paths so the test needs no Pulumi backend.
func TestPerInstanceStateDirIsolation(t *testing.T) {
	const n = 100
	root := t.TempDir()

	paths := make([]string, n)
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		w := NewPulumiWorkspace(fmt.Sprintf("inst-%d", i), "proj", WithStateDirRoot(root))
		p, err := w.GetStateFilePath()
		require.NoError(t, err)
		require.False(t, seen[p], "state file path collided across instances: %s", p)
		seen[p] = true
		paths[i] = p
	}

	// each goroutine reports through assert rather than require: require
	// calls t.FailNow, which stops only the goroutine it runs on, so the
	// read-back loop below would then fail on a file nobody wrote and bury
	// the real cause
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if !assert.NoError(t, os.MkdirAll(filepath.Dir(paths[i]), 0o755)) {
				return
			}
			content := fmt.Sprintf("state-for-inst-%d", i)
			assert.NoError(t, os.WriteFile(paths[i], []byte(content), 0o644))
		}(i)
	}
	wg.Wait()
	if t.Failed() {
		t.Fatal("concurrent state file writes failed; skipping read-back")
	}

	for i := 0; i < n; i++ {
		got, err := os.ReadFile(paths[i])
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("state-for-inst-%d", i), string(got),
			"each instance's state file must hold only its own content")
	}
}
