//go:build perf

package controller

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	notifications "github.com/threeport/threeport/pkg/notifications/v0"
)

// perfReconcileIterations sizes the sample count for the threshold
// test. Reconciler loops run at reasonable message rates so this needs
// to be large enough that a per-iteration regression shows up in the
// aggregate wall-clock rather than variance.
const perfReconcileIterations = 5000

// perfReconcileBackoffCreationTime is the notification creation-time
// offset the SetRequeueDelay call in the loop treats as the "previous
// requeue" epoch. Fixed here so the delay branch every iteration takes
// is stable across runs, isolating the parse cost from the backoff
// math.
const perfReconcileBackoffCreationTime = int64(5)

// perfReconcileLoopThreshold caps the total wall-clock for
// perfReconcileIterations of the parse + backoff + payload extraction
// work a reconciler pays per NATS message. The 3x-baseline headroom
// absorbs CI variance while still catching a regression that would
// slow every controller under load.
const perfReconcileLoopThreshold = 500 * time.Millisecond

// newPerfNotificationPayload builds a Notification wire payload sized
// to a realistic reconciler message: the fixed envelope fields plus an
// Object map big enough that json overhead dominates the per-message
// cost the way it does on the real wire.
func newPerfNotificationPayload(tb testing.TB, seq int) []byte {
	tb.Helper()
	creationTime := time.Now().Unix() - perfReconcileBackoffCreationTime
	payload := notifications.Notification{
		Operation:     notifications.NotificationOperationUpdated,
		CreationTime:  &creationTime,
		ObjectVersion: "v0",
		Object: map[string]interface{}{
			"ID":       seq,
			"Name":     fmt.Sprintf("workload-%d", seq),
			"Type":     "threeport.io/v0.KubernetesWorkloadInstance",
			"Note":     "workload reconciled successfully",
			"Reason":   "ReconcileRequeue",
			"Reporter": "kubernetes-workload-controller",
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		tb.Fatalf("failed to build perf notification payload: %v", err)
	}
	return data
}

// runOneReconcileIteration replays the parse + backoff + payload
// extraction work a reconciler does per NATS message: decode the wire
// bytes into a Notification, derive the next backoff delay, then reach
// into the Object payload the way a real reconciler does before it
// hands the object to the ReconcileFunc.
func runOneReconcileIteration(tb testing.TB, payload []byte) {
	tb.Helper()
	notif, err := notifications.ConsumeMessage(payload)
	if err != nil {
		tb.Fatalf("ConsumeMessage failed: %v", err)
	}
	// mimic the backoff calculation SetRequeueDelay does on the requeue
	// path every real reconcile takes when work isn't complete
	_ = SetRequeueDelay(notif.CreationTime)
	// touch the Object payload the way a real ReconcileFunc does so the
	// bench captures the map-index overhead the loop pays every time
	if obj, ok := notif.Object.(map[string]interface{}); ok {
		if _, ok := obj["ID"]; !ok {
			tb.Fatalf("notification Object missing ID")
		}
	}
}

// perfReconcilePercentileIndex returns the sorted-slice index for the
// given percentile (0..100) over n samples, clamped to [0, n-1].
func perfReconcilePercentileIndex(n, pct int) int {
	if n <= 0 {
		return 0
	}
	i := n * pct / 100
	if i >= n {
		i = n - 1
	}
	return i
}

// reportReconcilePercentiles sorts durations and emits p50 and p95 via
// b.ReportMetric so `go test -bench` output carries the percentile
// numbers rather than only the mean ns/op figure.
func reportReconcilePercentiles(b *testing.B, durations []time.Duration) {
	b.Helper()
	if len(durations) == 0 {
		return
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[perfReconcilePercentileIndex(len(durations), 50)]
	p95 := durations[perfReconcilePercentileIndex(len(durations), 95)]
	b.ReportMetric(float64(p50.Microseconds()), "p50-us")
	b.ReportMetric(float64(p95.Microseconds()), "p95-us")
}

// BenchmarkReconcileLoopIteration measures the wall-clock cost of one
// pass of the parse + backoff + payload extraction work every
// reconciler does per NATS message. Reports p50 and p95 via
// b.ReportMetric so CI can trend a regression before it crosses the
// TestReconcileLoopIterationThreshold ceiling.
func BenchmarkReconcileLoopIteration(b *testing.B) {
	payload := newPerfNotificationPayload(b, 1)
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		runOneReconcileIteration(b, payload)
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()
	reportReconcilePercentiles(b, durations)
}

// TestReconcileLoopIterationThreshold asserts the total wall-clock for
// perfReconcileIterations of the reconciler per-message work stays
// under perfReconcileLoopThreshold. Guards a regression on the hot
// loop every controller runs.
func TestReconcileLoopIterationThreshold(t *testing.T) {
	payload := newPerfNotificationPayload(t, 1)

	// warm-up: prime json's reflect-cached type info so the first
	// iteration doesn't dominate the total wall-clock
	runOneReconcileIteration(t, payload)

	// timed runs: aggregate wall-clock over perfReconcileIterations
	// iterations gives a stable read; the per-iteration budget is
	// derived by dividing the ceiling by the iteration count
	start := time.Now()
	for i := 0; i < perfReconcileIterations; i++ {
		runOneReconcileIteration(t, payload)
	}
	total := time.Since(start)

	if total > perfReconcileLoopThreshold {
		perIter := total / time.Duration(perfReconcileIterations)
		t.Fatalf(
			"reconcile loop total %s exceeds threshold %s over %d iterations (%.0f ns/iter); "+
				"check for regression in ConsumeMessage decode or SetRequeueDelay math",
			total, perfReconcileLoopThreshold, perfReconcileIterations, float64(perIter.Nanoseconds()),
		)
	}
}
