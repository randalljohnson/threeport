//go:build perf

package v0

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// perfNATSURLEnv is the environment variable a caller sets to point the
// pub/sub throughput bench at a live NATS server. Pub/sub throughput is
// only meaningful against a real broker, so the bench is skipped unless a
// URL is provided.
const perfNATSURLEnv = "PERF_NATS_URL"

// perfNATSSubjectPrefix scopes the bench's subject namespace so a
// concurrent run against the same broker cannot deliver messages across
// harnesses. Each run appends the process pid to keep subjects unique.
const perfNATSSubjectPrefix = "threeport.perf.notif"

// perfNATSMessageCount sizes the message batch each pub/sub iteration
// publishes and consumes. Large enough to smooth per-message variance;
// small enough to keep a full iteration under a couple of seconds against
// a local nats-server.
const perfNATSMessageCount = 5000

// perfNATSDrainTimeout caps how long the subscriber waits for the
// published batch to arrive before the iteration is considered stalled.
// A regression in delivery throughput surfaces as a stall here rather
// than a subtle rate change.
const perfNATSDrainTimeout = 10 * time.Second

// perfNATSMinRate is the per-subject publish+deliver throughput floor
// asserted by TestNATSPubSubThroughputPerSubject. The 3x-baseline
// convention keeps CI variance from flipping the threshold while still
// catching a real regression: a healthy local nats-server carries this
// payload shape well above the floor.
const perfNATSMinRate = 5000.0

// perfNATSThresholdIterations sizes the sample count for the threshold
// test. A handful of runs is enough for a stable p50 read on the rate.
const perfNATSThresholdIterations = 5

// newPerfNATSConn returns a connection to the URL named by
// perfNATSURLEnv, or skips the test when the env var is unset. Callers
// close the connection with tb.Cleanup so a failed iteration doesn't
// leak a subscription.
func newPerfNATSConn(tb testing.TB) *nats.Conn {
	tb.Helper()
	url := os.Getenv(perfNATSURLEnv)
	if url == "" {
		tb.Skipf("skipping NATS pub/sub throughput bench: %s not set", perfNATSURLEnv)
	}
	nc, err := nats.Connect(url, nats.Name("threeport-perf"))
	if err != nil {
		tb.Fatalf("failed to connect to NATS at %s: %v", url, err)
	}
	tb.Cleanup(func() { nc.Close() })
	return nc
}

// newPerfNotificationPayload builds a Notification payload sized to a
// realistic reconciler message: the fixed envelope fields plus an Object
// map big enough that json overhead dominates the per-message cost the
// way it does on the real wire.
func newPerfNotificationPayload(seq int) []byte {
	creationTime := time.Now().UnixNano()
	notif := Notification{
		Operation:     NotificationOperationCreated,
		CreationTime:  &creationTime,
		ObjectVersion: "v0",
		Object: map[string]interface{}{
			"ID":   seq,
			"Name": fmt.Sprintf("workload-%d", seq),
			"Type": "threeport.io/v0.KubernetesWorkloadInstance",
			"Note": "workload reconciled successfully",
		},
	}
	data, err := json.Marshal(notif)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal perf notification payload: %v", err))
	}
	return data
}

// runPerfPubSubBatch publishes perfNATSMessageCount messages on subject
// and returns the wall-clock elapsed from first publish to last delivery.
// A dedicated subject per call isolates the batch from any prior run.
func runPerfPubSubBatch(tb testing.TB, nc *nats.Conn, subject string) time.Duration {
	tb.Helper()

	// received counts deliveries to the async subscriber so the caller
	// can wait until the whole batch has landed before reading elapsed
	var received int64
	done := make(chan struct{})
	sub, err := nc.Subscribe(subject, func(_ *nats.Msg) {
		if atomic.AddInt64(&received, 1) == perfNATSMessageCount {
			close(done)
		}
	})
	if err != nil {
		tb.Fatalf("failed to subscribe to %s: %v", subject, err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	// ensure the subscription is registered on the server before the
	// publisher starts so no early publishes are dropped
	if err := nc.Flush(); err != nil {
		tb.Fatalf("failed to flush subscription registration: %v", err)
	}

	// publish the batch and time from first publish to last delivery
	start := time.Now()
	for i := 0; i < perfNATSMessageCount; i++ {
		if err := nc.Publish(subject, newPerfNotificationPayload(i)); err != nil {
			tb.Fatalf("failed to publish message %d: %v", i, err)
		}
	}
	if err := nc.Flush(); err != nil {
		tb.Fatalf("failed to flush publisher: %v", err)
	}

	// wait for the async subscriber to see the full batch before reading
	// elapsed, so the rate reflects deliver-side throughput not publish
	select {
	case <-done:
	case <-time.After(perfNATSDrainTimeout):
		tb.Fatalf("subscriber received only %d of %d messages within %s", atomic.LoadInt64(&received), perfNATSMessageCount, perfNATSDrainTimeout)
	}
	return time.Since(start)
}

// perfNATSPercentileIndex returns the sorted-slice index for the given
// percentile (0..100) over n samples, clamped to [0, n-1].
func perfNATSPercentileIndex(n, pct int) int {
	if n <= 0 {
		return 0
	}
	i := n * pct / 100
	if i >= n {
		i = n - 1
	}
	return i
}

// reportNATSPercentiles sorts rates and emits p50 and p95 via
// b.ReportMetric so `go test -bench` output carries the throughput
// numbers rather than only the mean ns/op figure.
func reportNATSPercentiles(b *testing.B, rates []float64) {
	b.Helper()
	if len(rates) == 0 {
		return
	}
	sort.Float64s(rates)
	p50 := rates[perfNATSPercentileIndex(len(rates), 50)]
	p95 := rates[perfNATSPercentileIndex(len(rates), 95)]
	b.ReportMetric(p50, "p50-msg/s")
	b.ReportMetric(p95, "p95-msg/s")
}

// BenchmarkNATSPubSubThroughputPerSubject measures the per-subject
// publish + deliver throughput of a batch of realistic notification
// payloads. Reports p50 and p95 msg/s so CI can trend a regression
// before it crosses the perfNATSMinRate floor asserted below.
func BenchmarkNATSPubSubThroughputPerSubject(b *testing.B) {
	nc := newPerfNATSConn(b)
	rates := make([]float64, 0, b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// unique subject per iteration keeps the batch counter isolated
		// from any prior subscriber that may still be draining
		subject := fmt.Sprintf("%s.%d.%d", perfNATSSubjectPrefix, os.Getpid(), i)
		elapsed := runPerfPubSubBatch(b, nc, subject)
		rates = append(rates, float64(perfNATSMessageCount)/elapsed.Seconds())
	}
	b.StopTimer()

	reportNATSPercentiles(b, rates)
}

// TestNATSPubSubThroughputPerSubjectFloor asserts the per-subject rate
// stays above perfNATSMinRate over perfNATSThresholdIterations runs.
// Guards a regression in the pub/sub path a controller pays on every
// requeue and status change.
func TestNATSPubSubThroughputPerSubjectFloor(t *testing.T) {
	nc := newPerfNATSConn(t)

	// warm-up: prime the server's per-connection buffers so the first
	// iteration doesn't drag the p50 rate down
	warmSubject := fmt.Sprintf("%s.%d.warm", perfNATSSubjectPrefix, os.Getpid())
	_ = runPerfPubSubBatch(t, nc, warmSubject)

	// timed runs: collect perfNATSThresholdIterations rate samples
	rates := make([]float64, perfNATSThresholdIterations)
	for i := range rates {
		subject := fmt.Sprintf("%s.%d.thr.%d", perfNATSSubjectPrefix, os.Getpid(), i)
		elapsed := runPerfPubSubBatch(t, nc, subject)
		rates[i] = float64(perfNATSMessageCount) / elapsed.Seconds()
	}
	sort.Float64s(rates)
	p50 := rates[perfNATSPercentileIndex(len(rates), 50)]

	if p50 < perfNATSMinRate {
		t.Fatalf(
			"NATS pub/sub p50 rate %.0f msg/s below floor %.0f (batch %d, subject prefix %s); "+
				"check for regression in nats.Conn buffering or subscription delivery",
			p50, perfNATSMinRate, perfNATSMessageCount, perfNATSSubjectPrefix,
		)
	}
}
