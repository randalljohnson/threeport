//go:build perf

package handlers

import (
	"testing"
	"time"

	"gorm.io/gorm"

	api "github.com/threeport/threeport/pkg/api/v0"
)

// perfEventCount sizes the seeded events table for the cold and warm
// benches. Small enough that sqlite finishes each iteration in well
// under the assertion thresholds; large enough that the join+count+find
// shape dominates iteration cost rather than harness overhead.
const perfEventCount = 200

// perfPageLimit mirrors the api server's default page size so the
// scanned prefix matches what a real client would receive.
const perfPageLimit = 100

// coldPathThreshold is the p95 ceiling for the cold-path bench. The
// 378 ms baseline was captured against a real CRDB with a materialized
// view create + count; the ceiling here allows headroom for CI variance.
const coldPathThreshold = 1000 * time.Millisecond

// warmPathThreshold is the p50 ceiling for the warm-path bench. The
// 202 ms baseline was captured against a real CRDB continuation read
// against an existing view; the ceiling here allows headroom for CI
// variance.
const warmPathThreshold = 400 * time.Millisecond

// runEventsColdPath() reproduces the cold-path DB work the events
// endpoint does on a first-page request: a Count over the polymorphic
// join, followed by a Find of the first page. The materialized view /
// AS OF SYSTEM TIME wrappers the endpoint layers on top are CRDB-only,
// so this fallback measures only the portion that runs against sqlite.
func runEventsColdPath(db *gorm.DB) error {
	fullyQualifiedEventType := (&api.Event{}).GetFullyQualifiedType()

	// count matches the endpoint's HasMore probe on a fresh request
	var totalCount int64
	if err := JoinEventsToAttachedObjectReferences(
		db.Model(&api.Event{}),
		fullyQualifiedEventType,
	).Count(&totalCount).Error; err != nil {
		return err
	}

	// find pulls the first page in the same JOIN shape the count used
	var page []api.Event
	if err := JoinEventsToAttachedObjectReferences(
		db.Order("v0_events.id asc").Limit(perfPageLimit),
		fullyQualifiedEventType,
	).Find(&page).Error; err != nil {
		return err
	}
	return nil
}

// runEventsWarmPath() reproduces the warm-path DB work a continuation
// request does: a single Find scoped to `ID > cursor` off the same
// polymorphic join. No count, no view create; every page after the
// first takes this shape.
func runEventsWarmPath(db *gorm.DB, cursor uint) error {
	fullyQualifiedEventType := (&api.Event{}).GetFullyQualifiedType()
	var page []api.Event
	return JoinEventsToAttachedObjectReferences(
		db.Where("v0_events.id > ?", cursor).Order("v0_events.id asc").Limit(perfPageLimit),
		fullyQualifiedEventType,
	).Find(&page).Error
}

// BenchmarkEventsGetCold measures the cold-path DB work per iteration
// with a freshly seeded table. Reports p50 and p95 via b.ReportMetric so
// CI can trend regressions before they cross the assertion ceiling in
// TestEventsColdPathThresholdMs.
func BenchmarkEventsGetCold(b *testing.B) {
	db := newPerfDB(b)
	seedPerfEvents(b, db, perfEventCount)

	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if err := runEventsColdPath(db); err != nil {
			b.Fatalf("cold iteration failed: %v", err)
		}
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()

	reportPercentiles(b, durations)
}

// BenchmarkEventsGetWarm measures the warm-path DB work per iteration
// against the same seeded table, resuming from cursor 0 so every
// iteration scans an identical prefix. Reports p50 and p95 via
// b.ReportMetric so CI can trend regressions before they cross the
// assertion ceiling in TestEventsWarmPathThresholdMs.
func BenchmarkEventsGetWarm(b *testing.B) {
	db := newPerfDB(b)
	seedPerfEvents(b, db, perfEventCount)

	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if err := runEventsWarmPath(db, 0); err != nil {
			b.Fatalf("warm iteration failed: %v", err)
		}
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()

	reportPercentiles(b, durations)
}

// TestEventsColdPathThresholdMs asserts the cold-path p95 over 5 runs
// stays under coldPathThreshold. Guards against a regression that
// pushes the count + first-page find beyond the 378 ms CRDB baseline.
func TestEventsColdPathThresholdMs(t *testing.T) {
	db := newPerfDB(t)
	seedPerfEvents(t, db, perfEventCount)

	// warm-up: prime gorm's statement cache so the first sample doesn't
	// dominate the p95
	if err := runEventsColdPath(db); err != nil {
		t.Fatalf("warm-up failed: %v", err)
	}

	// timed runs: collect 5 samples for the p95 read
	samples := timeSamples(t, 5, func() error {
		return runEventsColdPath(db)
	})
	p95 := samples[perfPercentileIndex(len(samples), 95)]

	if p95 > coldPathThreshold {
		t.Fatalf(
			"events cold-path p95 %s exceeds threshold %s (baseline 378 ms, %d events, limit %d); "+
				"check for regression in the count + first-page find on the polymorphic join",
			p95, coldPathThreshold, perfEventCount, perfPageLimit,
		)
	}
}

// TestEventsWarmPathThresholdMs asserts the warm-path p50 over 20 runs
// stays under warmPathThreshold. Guards against a regression that
// pushes the continuation find beyond the 202 ms CRDB baseline.
func TestEventsWarmPathThresholdMs(t *testing.T) {
	db := newPerfDB(t)
	seedPerfEvents(t, db, perfEventCount)

	// warm-up: prime gorm's statement cache so the first sample doesn't
	// pull the p50 up
	if err := runEventsWarmPath(db, 0); err != nil {
		t.Fatalf("warm-up failed: %v", err)
	}

	// timed runs: collect 20 samples for the p50 read
	samples := timeSamples(t, 20, func() error {
		return runEventsWarmPath(db, 0)
	})
	p50 := samples[perfPercentileIndex(len(samples), 50)]

	if p50 > warmPathThreshold {
		t.Fatalf(
			"events warm-path p50 %s exceeds threshold %s (baseline 202 ms, %d events, limit %d); "+
				"check for regression in the continuation find on the polymorphic join",
			p50, warmPathThreshold, perfEventCount, perfPageLimit,
		)
	}
}

// reportPercentiles() sorts durations and emits p50 and p95 via
// b.ReportMetric so `go test -bench` output carries the percentile
// numbers rather than only the mean ns/op figure.
func reportPercentiles(b *testing.B, durations []time.Duration) {
	b.Helper()
	if len(durations) == 0 {
		return
	}
	// sort in place; caller doesn't need the original order back
	for i := 1; i < len(durations); i++ {
		for j := i; j > 0 && durations[j-1] > durations[j]; j-- {
			durations[j-1], durations[j] = durations[j], durations[j-1]
		}
	}
	p50 := durations[perfPercentileIndex(len(durations), 50)]
	p95 := durations[perfPercentileIndex(len(durations), 95)]
	b.ReportMetric(float64(p50.Milliseconds()), "p50-ms")
	b.ReportMetric(float64(p95.Milliseconds()), "p95-ms")
}
