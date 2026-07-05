//go:build perf

package v0

import (
	"encoding/json"
	"sort"
	"testing"
	"time"
)

// perfLargeSetSize sizes the enriched-event slice a marshal/unmarshal
// iteration serializes and deserializes. Chosen to represent a bulk
// export or a controller cache warm, both of which push far past the
// per-page marshal shape covered by BenchmarkEventMarshalEnrichedPage.
const perfLargeSetSize = 1000

// perfLargeSetIterations sizes the sample count for the threshold tests.
// Enough for a stable p95 while keeping the perf run brisk.
const perfLargeSetIterations = 20

// perfLargeMarshalThreshold caps the p95 marshal wall-clock for one
// perfLargeSetSize event set. json.Marshal on 1000 pointer-heavy
// structs runs well under this ceiling on modest hardware; the
// 3x-baseline headroom absorbs CI variance while still catching a
// regression that would slow every bulk-export handler.
const perfLargeMarshalThreshold = 150 * time.Millisecond

// perfLargeUnmarshalThreshold caps the p95 unmarshal wall-clock for one
// perfLargeSetSize event set. Unmarshal costs a reflect-based decode
// per field so the ceiling sits above the marshal ceiling to reflect
// the higher steady-state cost.
const perfLargeUnmarshalThreshold = 250 * time.Millisecond

// perfLargePercentileIndex returns the sorted-slice index for the given
// percentile (0..100) over n samples, clamped to [0, n-1]. Peer of the
// helper in events_perf_test.go; kept here so a package-scoped rename
// of one does not silently break the other.
func perfLargePercentileIndex(n, pct int) int {
	if n <= 0 {
		return 0
	}
	i := n * pct / 100
	if i >= n {
		i = n - 1
	}
	return i
}

// reportLargeSetPercentiles sorts durations and emits p50 and p95 via
// b.ReportMetric so `go test -bench` output carries the percentile
// numbers rather than only the mean ns/op figure.
func reportLargeSetPercentiles(b *testing.B, durations []time.Duration) {
	b.Helper()
	if len(durations) == 0 {
		return
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[perfLargePercentileIndex(len(durations), 50)]
	p95 := durations[perfLargePercentileIndex(len(durations), 95)]
	b.ReportMetric(float64(p50.Milliseconds()), "p50-ms")
	b.ReportMetric(float64(p95.Milliseconds()), "p95-ms")
}

// BenchmarkEventMarshalLargeSet measures the wall-clock cost of one
// json.Marshal over a perfLargeSetSize event slice. Reports p50 and
// p95 via b.ReportMetric so CI can trend a regression before it crosses
// the TestEventMarshalLargeSetThreshold ceiling.
func BenchmarkEventMarshalLargeSet(b *testing.B) {
	set := newPerfEnrichedEvents(perfLargeSetSize)
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		out, err := json.Marshal(set)
		if err != nil {
			b.Fatalf("marshal failed: %v", err)
		}
		if len(out) == 0 {
			b.Fatalf("empty marshal output")
		}
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()
	reportLargeSetPercentiles(b, durations)
}

// BenchmarkEventUnmarshalLargeSet measures the wall-clock cost of one
// json.Unmarshal into a perfLargeSetSize event slice from the same wire
// bytes produced by BenchmarkEventMarshalLargeSet. Reports p50 and p95
// via b.ReportMetric so CI can trend a regression before it crosses
// the TestEventUnmarshalLargeSetThreshold ceiling.
func BenchmarkEventUnmarshalLargeSet(b *testing.B) {
	set := newPerfEnrichedEvents(perfLargeSetSize)
	payload, err := json.Marshal(set)
	if err != nil {
		b.Fatalf("failed to build unmarshal payload: %v", err)
	}
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var got []Event
		start := time.Now()
		if err := json.Unmarshal(payload, &got); err != nil {
			b.Fatalf("unmarshal failed: %v", err)
		}
		if len(got) != perfLargeSetSize {
			b.Fatalf("unmarshal produced %d events, want %d", len(got), perfLargeSetSize)
		}
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()
	reportLargeSetPercentiles(b, durations)
}

// TestEventMarshalLargeSetThreshold asserts the marshal p95 over
// perfLargeSetIterations runs stays under perfLargeMarshalThreshold.
// Guards a regression on the bulk-export write path where every export
// pays this cost once per set.
func TestEventMarshalLargeSetThreshold(t *testing.T) {
	set := newPerfEnrichedEvents(perfLargeSetSize)

	// warm-up: prime json's reflect-cached type info so the first
	// sample doesn't dominate the p95
	if _, err := json.Marshal(set); err != nil {
		t.Fatalf("warm-up marshal failed: %v", err)
	}

	// timed runs: collect perfLargeSetIterations samples for the p95 read
	samples := make([]time.Duration, perfLargeSetIterations)
	for i := range samples {
		start := time.Now()
		out, err := json.Marshal(set)
		if err != nil {
			t.Fatalf("marshal iteration %d failed: %v", i, err)
		}
		if len(out) == 0 {
			t.Fatalf("iteration %d: empty marshal output", i)
		}
		samples[i] = time.Since(start)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[perfLargePercentileIndex(len(samples), 95)]

	if p95 > perfLargeMarshalThreshold {
		t.Fatalf(
			"event marshal p95 %s exceeds threshold %s (set size %d, enriched); "+
				"check for regression in Event serialization or added marshal hops",
			p95, perfLargeMarshalThreshold, perfLargeSetSize,
		)
	}
}

// TestEventUnmarshalLargeSetThreshold asserts the unmarshal p95 over
// perfLargeSetIterations runs stays under perfLargeUnmarshalThreshold.
// Guards a regression on the client decode path where every list read
// pays this cost once per response.
func TestEventUnmarshalLargeSetThreshold(t *testing.T) {
	set := newPerfEnrichedEvents(perfLargeSetSize)
	payload, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("failed to build unmarshal payload: %v", err)
	}

	// warm-up: prime json's reflect-cached type info so the first
	// sample doesn't dominate the p95
	var warm []Event
	if err := json.Unmarshal(payload, &warm); err != nil {
		t.Fatalf("warm-up unmarshal failed: %v", err)
	}

	// timed runs: collect perfLargeSetIterations samples for the p95 read
	samples := make([]time.Duration, perfLargeSetIterations)
	for i := range samples {
		var got []Event
		start := time.Now()
		if err := json.Unmarshal(payload, &got); err != nil {
			t.Fatalf("unmarshal iteration %d failed: %v", i, err)
		}
		if len(got) != perfLargeSetSize {
			t.Fatalf("iteration %d: unmarshal produced %d events, want %d", i, len(got), perfLargeSetSize)
		}
		samples[i] = time.Since(start)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[perfLargePercentileIndex(len(samples), 95)]

	if p95 > perfLargeUnmarshalThreshold {
		t.Fatalf(
			"event unmarshal p95 %s exceeds threshold %s (set size %d, enriched); "+
				"check for regression in Event deserialization or added decode hops",
			p95, perfLargeUnmarshalThreshold, perfLargeSetSize,
		)
	}
}
