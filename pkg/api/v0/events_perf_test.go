//go:build perf

package v0

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// perfMarshalPageSize sizes the event slice a marshal iteration
// serializes. Mirrors the default page a client receives from
// GetEventsJoinAttachedObjectReferences so the bench matches what the
// api server actually writes to the wire.
const perfMarshalPageSize = 100

// perfMarshalIterations sizes the sample count for the threshold test.
// Enough samples for a stable p95 while keeping the perf run brisk.
const perfMarshalIterations = 50

// perfMarshalThreshold caps the p95 marshal wall-clock for one enriched
// page. json.Marshal on 100 pointer-heavy structs runs well under this
// ceiling on modest hardware; the headroom absorbs CI variance while
// still catching a regression that would slow the response write path.
const perfMarshalThreshold = 20 * time.Millisecond

// newPerfEnrichedEvents returns n Event values populated as the events
// endpoint returns them after enrichEventsWithObjectInfo runs: the
// on-disk fields plus the projection-only fields (ObjectType, ObjectID,
// ObjectName) the join + name lookup fills in. Written here so the
// api/v0 package can bench the on-wire shape without depending on the
// handlers package.
func newPerfEnrichedEvents(n int) []Event {
	now := time.Now()
	events := make([]Event, n)
	for i := 0; i < n; i++ {
		id := uint(i + 1)
		events[i] = Event{
			Common: Common{
				ID:        util.Ptr(id),
				CreatedAt: util.Ptr(now),
				UpdatedAt: util.Ptr(now),
			},
			Reason:              util.Ptr(fmt.Sprintf("R%d", i)),
			Note:                util.Ptr("workload reconciled successfully"),
			Count:               util.Ptr(uint(1)),
			EventTime:           util.Ptr(now),
			LastObservedTime:    util.Ptr(now),
			Type:                util.Ptr("Normal"),
			ReportingController: util.Ptr("kubernetes-workload-controller"),
			// projection-only fields populated by enrichEventsWithObjectInfo:
			// the AOR-projected subject plus its resolved name.
			ObjectType: util.Ptr("threeport.io/v0.KubernetesWorkloadInstance"),
			ObjectID:   util.Ptr(id),
			ObjectName: util.Ptr(fmt.Sprintf("workload-%d", i)),
		}
	}
	return events
}

// perfMarshalPercentileIndex returns the sorted-slice index for the given
// percentile (0..100) over n samples, clamped to [0, n-1].
func perfMarshalPercentileIndex(n, pct int) int {
	if n <= 0 {
		return 0
	}
	i := n * pct / 100
	if i >= n {
		i = n - 1
	}
	return i
}

// reportMarshalPercentiles sorts durations and emits p50 and p95 via
// b.ReportMetric so `go test -bench` output carries the percentile
// numbers rather than only the mean ns/op figure.
func reportMarshalPercentiles(b *testing.B, durations []time.Duration) {
	b.Helper()
	if len(durations) == 0 {
		return
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[perfMarshalPercentileIndex(len(durations), 50)]
	p95 := durations[perfMarshalPercentileIndex(len(durations), 95)]
	b.ReportMetric(float64(p50.Microseconds()), "p50-us")
	b.ReportMetric(float64(p95.Microseconds()), "p95-us")
}

// BenchmarkEventMarshalEnrichedPage measures the wall-clock cost of
// json.Marshal on one enriched events page. Reports p50 and p95 via
// b.ReportMetric so CI can trend a regression before it crosses the
// TestEventMarshalEnrichedPageThresholdUs ceiling.
func BenchmarkEventMarshalEnrichedPage(b *testing.B) {
	page := newPerfEnrichedEvents(perfMarshalPageSize)
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		out, err := json.Marshal(page)
		if err != nil {
			b.Fatalf("marshal failed: %v", err)
		}
		// keep the compiler from optimizing the marshal away
		if len(out) == 0 {
			b.Fatalf("empty marshal output")
		}
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()
	reportMarshalPercentiles(b, durations)
}

// BenchmarkEventMarshalSingleEnriched isolates the per-event marshal
// cost so a regression in Event's json tag surface (a new field, a
// custom MarshalJSON, an added time-format hop) shows up separately
// from the page-loop overhead.
func BenchmarkEventMarshalSingleEnriched(b *testing.B) {
	events := newPerfEnrichedEvents(1)
	one := events[0]
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		out, err := json.Marshal(&one)
		if err != nil {
			b.Fatalf("marshal failed: %v", err)
		}
		if len(out) == 0 {
			b.Fatalf("empty marshal output")
		}
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()
	reportMarshalPercentiles(b, durations)
}

// TestEventMarshalEnrichedPageThresholdUs asserts the marshal p95 over
// perfMarshalIterations runs stays under perfMarshalThreshold for a
// perfMarshalPageSize-event page. Guards a regression on the response
// write path where every events page pays this cost.
func TestEventMarshalEnrichedPageThresholdUs(t *testing.T) {
	page := newPerfEnrichedEvents(perfMarshalPageSize)

	// warm-up: prime json's reflect-cached type info so the first
	// sample doesn't dominate the p95
	if _, err := json.Marshal(page); err != nil {
		t.Fatalf("warm-up marshal failed: %v", err)
	}

	// timed runs: collect perfMarshalIterations samples for the p95 read
	samples := make([]time.Duration, perfMarshalIterations)
	for i := range samples {
		start := time.Now()
		out, err := json.Marshal(page)
		if err != nil {
			t.Fatalf("marshal iteration %d failed: %v", i, err)
		}
		if len(out) == 0 {
			t.Fatalf("iteration %d: empty marshal output", i)
		}
		samples[i] = time.Since(start)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[perfMarshalPercentileIndex(len(samples), 95)]

	if p95 > perfMarshalThreshold {
		t.Fatalf(
			"event marshal p95 %s exceeds threshold %s (page size %d, enriched); "+
				"check for regression in Event serialization or added marshal hops",
			p95, perfMarshalThreshold, perfMarshalPageSize,
		)
	}
}
