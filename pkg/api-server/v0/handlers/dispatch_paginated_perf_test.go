//go:build perf

package handlers

import (
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
)

// perfDispatchDSNEnv is the environment variable a caller sets to point
// the AOST-vs-MV bench at a CRDB instance. Both DispatchGetPaginatedRecords
// modes depend on CRDB-specific SQL (AS OF SYSTEM TIME, MATERIALIZED
// VIEW) that sqlite does not implement, so the bench is skipped unless a
// real CRDB is available.
const perfDispatchDSNEnv = "PERF_CRDB_DSN"

// perfDispatchRowCount sizes the source table the two modes page over.
// Chosen to make first-page work dominate iteration wall-clock while
// keeping the seed fast on the local dev crdb the perf lane runs
// against.
const perfDispatchRowCount = 5000

// perfDispatchPageLimit mirrors the api server's default page size so
// the scanned prefix matches what a real client would receive.
const perfDispatchPageLimit = 100

// perfDispatchSamples sizes the sample count for each mode's threshold
// test. Enough for a stable p95 read while keeping the perf run brisk.
const perfDispatchSamples = 5

// perfDispatchAOSTThreshold caps the AOST-mode p95 for a first-page
// dispatch on perfDispatchRowCount rows. Set well above the current
// baseline so CI variance does not flip the threshold, while still
// catching a regression that would surface as user-visible list-page
// latency.
const perfDispatchAOSTThreshold = 500 * time.Millisecond

// perfDispatchMVThreshold caps the MV-mode p95 for a first-page dispatch
// on perfDispatchRowCount rows. MV mode pays for a CREATE MATERIALIZED
// VIEW + CREATE INDEX plus the first-page find, so the ceiling sits
// above the AOST ceiling to reflect the different work profile.
const perfDispatchMVThreshold = 2 * time.Second

// perfDispatchRow is a minimal gorm model backing the source table the
// two modes read through. A pk + one text column keeps the row-scan
// cost dominated by SELECT overhead rather than column decode.
type perfDispatchRow struct {
	ID   uint `gorm:"primaryKey"`
	Note string
}

// TableName pins the source table name so both modes share the same
// queryTable string when DispatchGetPaginatedRecords is invoked below.
func (perfDispatchRow) TableName() string { return "v0_perf_dispatch_rows" }

// newPerfDispatchDB returns a gorm.DB pointed at a CRDB instance if
// PERF_CRDB_DSN is set. Skips the test otherwise so the perf lane can
// run on machines without CRDB while still exercising the CRDB-specific
// paths when one is available.
func newPerfDispatchDB(tb testing.TB) *gorm.DB {
	tb.Helper()
	dsn := os.Getenv(perfDispatchDSNEnv)
	if dsn == "" {
		tb.Skipf("skipping AOST vs MV dispatch bench: %s not set (both modes require CRDB syntax)", perfDispatchDSNEnv)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(tb, err)
	require.NoError(tb, db.AutoMigrate(&perfDispatchRow{}))
	return db
}

// seedPerfDispatchRows resets the source table and inserts n rows in
// one batch so both modes read from the same fixture. Called from each
// bench and threshold test so a stale prior run cannot leak state.
func seedPerfDispatchRows(tb testing.TB, db *gorm.DB, n int) {
	tb.Helper()
	// drop-and-recreate keeps the source table small so the AOST HLC
	// capture and MV create do not stall on GC of prior perf runs
	require.NoError(tb, db.Migrator().DropTable(&perfDispatchRow{}))
	require.NoError(tb, db.AutoMigrate(&perfDispatchRow{}))

	rows := make([]perfDispatchRow, n)
	for i := 0; i < n; i++ {
		rows[i] = perfDispatchRow{Note: fmt.Sprintf("note-%d", i)}
	}
	// CreateInBatches keeps a single INSERT statement from ballooning
	// past the CRDB wire limit at large row counts
	require.NoError(tb, db.CreateInBatches(rows, 500).Error)
}

// dispatchOneFirstPage runs one first-page dispatch in the given mode
// against the seeded source table and returns the elapsed wall-clock.
// Both modes hit DispatchGetPaginatedRecords with an empty QueryId so
// the initial-page branch runs, matching the endpoint's cold path.
func dispatchOneFirstPage(tb testing.TB, h Handler, mode apiserver_lib.PaginationMode) time.Duration {
	tb.Helper()
	var rows []perfDispatchRow
	pageParams := &apiserver_lib.PageRequestParams{Cursor: 0, Limit: perfDispatchPageLimit}
	start := time.Now()
	_, _, err := h.DispatchGetPaginatedRecords(mode, &rows, "v0_perf_dispatch_rows", pageParams)
	elapsed := time.Since(start)
	if err != nil {
		tb.Fatalf("%s dispatch failed: %v", mode, err)
	}
	if len(rows) == 0 {
		tb.Fatalf("%s dispatch returned zero rows", mode)
	}
	return elapsed
}

// perfDispatchPercentileIndex returns the sorted-slice index for the given
// percentile (0..100) over n samples, clamped to [0, n-1].
func perfDispatchPercentileIndex(n, pct int) int {
	if n <= 0 {
		return 0
	}
	i := n * pct / 100
	if i >= n {
		i = n - 1
	}
	return i
}

// reportDispatchPercentiles sorts durations and emits p50 and p95 via
// b.ReportMetric so `go test -bench` output carries the percentile
// numbers rather than only the mean ns/op figure.
func reportDispatchPercentiles(b *testing.B, durations []time.Duration) {
	b.Helper()
	if len(durations) == 0 {
		return
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[perfDispatchPercentileIndex(len(durations), 50)]
	p95 := durations[perfDispatchPercentileIndex(len(durations), 95)]
	b.ReportMetric(float64(p50.Milliseconds()), "p50-ms")
	b.ReportMetric(float64(p95.Milliseconds()), "p95-ms")
}

// BenchmarkDispatchGetPaginatedRecordsAOST measures the wall-clock cost
// of one first-page dispatch in AsOfSystemTime mode against a seeded
// source table. Reports p50 and p95 so CI can trend a regression
// before it crosses the TestDispatchAOSTFirstPageThresholdMs ceiling.
// Requires PERF_CRDB_DSN because AS OF SYSTEM TIME is CRDB-only.
func BenchmarkDispatchGetPaginatedRecordsAOST(b *testing.B) {
	db := newPerfDispatchDB(b)
	seedPerfDispatchRows(b, db, perfDispatchRowCount)
	h := New(db, nil, nil, zap.NewNop(), apiserver_lib.PaginationModeAsOfSystemTime)

	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		durations = append(durations, dispatchOneFirstPage(b, h, apiserver_lib.PaginationModeAsOfSystemTime))
	}
	b.StopTimer()
	reportDispatchPercentiles(b, durations)
}

// BenchmarkDispatchGetPaginatedRecordsMV measures the wall-clock cost of
// one first-page dispatch in MaterializedView mode against a seeded
// source table. Peer of BenchmarkDispatchGetPaginatedRecordsAOST so
// mode-vs-mode regression trends are directly comparable.
// Requires PERF_CRDB_DSN because CREATE MATERIALIZED VIEW is CRDB-only.
func BenchmarkDispatchGetPaginatedRecordsMV(b *testing.B) {
	db := newPerfDispatchDB(b)
	seedPerfDispatchRows(b, db, perfDispatchRowCount)
	h := New(db, nil, nil, zap.NewNop(), apiserver_lib.PaginationModeMaterializedView)

	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		durations = append(durations, dispatchOneFirstPage(b, h, apiserver_lib.PaginationModeMaterializedView))
	}
	b.StopTimer()
	reportDispatchPercentiles(b, durations)
}

// TestDispatchAOSTFirstPageThresholdMs asserts the AOST first-page p95
// stays under perfDispatchAOSTThreshold at perfDispatchRowCount rows.
// Guards a regression on the HLC capture + AS OF SYSTEM TIME read a
// list endpoint pays on the cold path.
func TestDispatchAOSTFirstPageThresholdMs(t *testing.T) {
	db := newPerfDispatchDB(t)
	seedPerfDispatchRows(t, db, perfDispatchRowCount)
	h := New(db, nil, nil, zap.NewNop(), apiserver_lib.PaginationModeAsOfSystemTime)

	// warm-up: prime gorm's statement cache so the first sample does not
	// dominate the p95
	_ = dispatchOneFirstPage(t, h, apiserver_lib.PaginationModeAsOfSystemTime)

	// timed runs: collect perfDispatchSamples samples for the p95 read
	samples := make([]time.Duration, perfDispatchSamples)
	for i := range samples {
		samples[i] = dispatchOneFirstPage(t, h, apiserver_lib.PaginationModeAsOfSystemTime)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[perfDispatchPercentileIndex(len(samples), 95)]

	if p95 > perfDispatchAOSTThreshold {
		t.Fatalf(
			"AOST first-page p95 %s exceeds threshold %s (%d rows, limit %d); "+
				"check for regression in HLC capture or AS OF SYSTEM TIME read",
			p95, perfDispatchAOSTThreshold, perfDispatchRowCount, perfDispatchPageLimit,
		)
	}
}

// TestDispatchMVFirstPageThresholdMs asserts the MV first-page p95 stays
// under perfDispatchMVThreshold at perfDispatchRowCount rows. Guards a
// regression on the CREATE MATERIALIZED VIEW + CREATE INDEX + first-page
// find the endpoint pays on the cold path.
func TestDispatchMVFirstPageThresholdMs(t *testing.T) {
	db := newPerfDispatchDB(t)
	seedPerfDispatchRows(t, db, perfDispatchRowCount)
	h := New(db, nil, nil, zap.NewNop(), apiserver_lib.PaginationModeMaterializedView)

	// warm-up: prime gorm's statement cache so the first sample does not
	// dominate the p95
	_ = dispatchOneFirstPage(t, h, apiserver_lib.PaginationModeMaterializedView)

	// timed runs: collect perfDispatchSamples samples for the p95 read
	samples := make([]time.Duration, perfDispatchSamples)
	for i := range samples {
		samples[i] = dispatchOneFirstPage(t, h, apiserver_lib.PaginationModeMaterializedView)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[perfDispatchPercentileIndex(len(samples), 95)]

	if p95 > perfDispatchMVThreshold {
		t.Fatalf(
			"MV first-page p95 %s exceeds threshold %s (%d rows, limit %d); "+
				"check for regression in CREATE MATERIALIZED VIEW or index build",
			p95, perfDispatchMVThreshold, perfDispatchRowCount, perfDispatchPageLimit,
		)
	}
}
