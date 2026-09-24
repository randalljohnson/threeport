//go:build perf

package handlers

import (
	"testing"
	"time"

	api "github.com/threeport/threeport/pkg/api/v0"
)

// perfDBRowCount sizes the seeded table for the single-row, list, and
// join benches. Large enough that the query shape dominates iteration
// cost rather than harness overhead; small enough that sqlite finishes
// each iteration well under the assertion thresholds.
const perfDBRowCount = 500

// perfDBListLimit mirrors the api server's default page size so the
// scanned prefix matches what a real list handler would return.
const perfDBListLimit = 100

// perfDBSamples sizes the sample count for each pattern's threshold
// test. Enough for a stable p50 read while keeping the perf run brisk.
const perfDBSamples = 20

// perfDBSingleRowThreshold caps the p50 wall-clock for a single-row
// lookup by primary key. sqlite's primary-key path runs well under this
// ceiling on modest hardware; the 3x-baseline headroom absorbs CI
// variance while still catching a regression that would slow every
// get-by-id handler on the read path.
const perfDBSingleRowThreshold = 3 * time.Millisecond

// perfDBListThreshold caps the p50 wall-clock for a limited list scan.
// The ceiling stays well above the sqlite baseline for a
// perfDBListLimit-row page against a perfDBRowCount-row table.
const perfDBListThreshold = 30 * time.Millisecond

// perfDBJoinThreshold caps the p50 wall-clock for a two-table join
// scoped to the same list-page shape. Matches the query shape the
// events endpoint pays on every continuation read.
const perfDBJoinThreshold = 60 * time.Millisecond

// BenchmarkDBSingleRowLookup measures the wall-clock cost of a single
// primary-key lookup on the events table. Reports p50 and p95 via
// b.ReportMetric so CI can trend a regression before it crosses the
// TestDBSingleRowLookupThreshold ceiling.
func BenchmarkDBSingleRowLookup(b *testing.B) {
	db := newPerfDB(b)
	seedPerfEvents(b, db, perfDBRowCount)

	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// cycle through ids so the query planner cannot cache a single
		// row's page in the buffer pool and skew successive iterations
		targetID := uint((i % perfDBRowCount) + 1)
		var got api.Event
		start := time.Now()
		if err := db.First(&got, targetID).Error; err != nil {
			b.Fatalf("single-row lookup id=%d failed: %v", targetID, err)
		}
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()
	reportPercentiles(b, durations)
}

// BenchmarkDBListLimit measures the wall-clock cost of a limited list
// scan on the events table: the shape a list handler pays before the
// join wrapper is layered on.
func BenchmarkDBListLimit(b *testing.B) {
	db := newPerfDB(b)
	seedPerfEvents(b, db, perfDBRowCount)

	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var page []api.Event
		start := time.Now()
		if err := db.Order("id asc").Limit(perfDBListLimit).Find(&page).Error; err != nil {
			b.Fatalf("list scan failed: %v", err)
		}
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()
	reportPercentiles(b, durations)
}

// BenchmarkDBJoinListLimit measures the wall-clock cost of a limited
// list scan joined to the attached object reference table: the shape
// the events endpoint pays on every continuation read.
func BenchmarkDBJoinListLimit(b *testing.B) {
	db := newPerfDB(b)
	seedPerfEvents(b, db, perfDBRowCount)

	fullyQualifiedEventType := (&api.Event{}).GetFullyQualifiedType()
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var page []api.Event
		start := time.Now()
		if err := JoinEventsToAttachedObjectReferences(
			db.Order("v0_events.id asc").Limit(perfDBListLimit),
			fullyQualifiedEventType,
		).Find(&page).Error; err != nil {
			b.Fatalf("join list scan failed: %v", err)
		}
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()
	reportPercentiles(b, durations)
}

// TestDBSingleRowLookupThreshold asserts the primary-key lookup p50
// stays under perfDBSingleRowThreshold. Guards a regression that would
// slow every get-by-id handler on the read path.
func TestDBSingleRowLookupThreshold(t *testing.T) {
	db := newPerfDB(t)
	seedPerfEvents(t, db, perfDBRowCount)

	// warm-up: prime gorm's statement cache so the first sample does not
	// dominate the p50
	var warm api.Event
	if err := db.First(&warm, uint(1)).Error; err != nil {
		t.Fatalf("warm-up single-row lookup failed: %v", err)
	}

	// timed runs: collect perfDBSamples samples for the p50 read
	samples := timeSamples(t, perfDBSamples, func() error {
		var got api.Event
		return db.First(&got, uint(1)).Error
	})
	p50 := samples[perfPercentileIndex(len(samples), 50)]

	if p50 > perfDBSingleRowThreshold {
		t.Fatalf(
			"single-row lookup p50 %s exceeds threshold %s (%d rows); "+
				"check for regression in the primary-key read path",
			p50, perfDBSingleRowThreshold, perfDBRowCount,
		)
	}
}

// TestDBListLimitThreshold asserts the list-scan p50 stays under
// perfDBListThreshold. Guards a regression on the base list read.
func TestDBListLimitThreshold(t *testing.T) {
	db := newPerfDB(t)
	seedPerfEvents(t, db, perfDBRowCount)

	// warm-up: prime gorm's statement cache so the first sample does not
	// dominate the p50
	var warm []api.Event
	if err := db.Order("id asc").Limit(perfDBListLimit).Find(&warm).Error; err != nil {
		t.Fatalf("warm-up list scan failed: %v", err)
	}

	samples := timeSamples(t, perfDBSamples, func() error {
		var page []api.Event
		return db.Order("id asc").Limit(perfDBListLimit).Find(&page).Error
	})
	p50 := samples[perfPercentileIndex(len(samples), 50)]

	if p50 > perfDBListThreshold {
		t.Fatalf(
			"list-scan p50 %s exceeds threshold %s (%d rows, limit %d); "+
				"check for regression in the base list read path",
			p50, perfDBListThreshold, perfDBRowCount, perfDBListLimit,
		)
	}
}

// TestDBJoinListLimitThreshold asserts the join list-scan p50 stays
// under perfDBJoinThreshold. Guards a regression on the join read that
// backs every events continuation page.
func TestDBJoinListLimitThreshold(t *testing.T) {
	db := newPerfDB(t)
	seedPerfEvents(t, db, perfDBRowCount)

	fullyQualifiedEventType := (&api.Event{}).GetFullyQualifiedType()

	// warm-up: prime gorm's statement cache so the first sample does not
	// dominate the p50
	var warm []api.Event
	if err := JoinEventsToAttachedObjectReferences(
		db.Order("v0_events.id asc").Limit(perfDBListLimit),
		fullyQualifiedEventType,
	).Find(&warm).Error; err != nil {
		t.Fatalf("warm-up join list scan failed: %v", err)
	}

	samples := timeSamples(t, perfDBSamples, func() error {
		var page []api.Event
		return JoinEventsToAttachedObjectReferences(
			db.Order("v0_events.id asc").Limit(perfDBListLimit),
			fullyQualifiedEventType,
		).Find(&page).Error
	})
	p50 := samples[perfPercentileIndex(len(samples), 50)]

	if p50 > perfDBJoinThreshold {
		t.Fatalf(
			"join list-scan p50 %s exceeds threshold %s (%d rows, limit %d); "+
				"check for regression in the polymorphic join or ordering",
			p50, perfDBJoinThreshold, perfDBRowCount, perfDBListLimit,
		)
	}
}

