//go:build perf

package v0

import (
	"fmt"
	"regexp"
	"sort"
	"testing"
	"time"
)

// perfSweepViewCount sizes the synthetic view-name list the sweep bench
// parses. Chosen to overshoot a busy control plane's steady-state view
// population so the per-view regex + parse cost dominates iteration
// wall-clock rather than harness overhead.
const perfSweepViewCount = 2000

// perfSweepTTLMinutes matches the ttl the cleanup goroutine runs with in
// production and drives roughly half the synthetic names to the drop
// bucket, exercising both the skip and the collect branches.
const perfSweepTTLMinutes = 30

// perfSweepThreshold caps the p95 CPU cost of parsing perfSweepViewCount
// view names once. Set well above the current baseline so CI variance
// doesn't flip the threshold, while still catching a regex or timestamp
// parse regression that pushes the sweep into control-plane visible
// territory.
const perfSweepThreshold = 50 * time.Millisecond

// makeSweepViewNames builds n view names in the exact shape
// CleanupMaterializedViews expects: PaginationViewPrefix, a
// YYYYMMDDhhmmss stamp, and a random-looking suffix. Half the stamps sit
// well past the TTL so the drop bucket populates, the other half sit
// under the TTL so the skip branch stays warm.
func makeSweepViewNames(n int) []string {
	now := time.Now()
	staleStamp := now.Add(-2 * time.Hour).Format("20060102150405")
	freshStamp := now.Add(-1 * time.Minute).Format("20060102150405")
	names := make([]string, n)
	for i := 0; i < n; i++ {
		stamp := freshStamp
		if i%2 == 0 {
			stamp = staleStamp
		}
		names[i] = fmt.Sprintf("%s_%s_query%08d", PaginationViewPrefix, stamp, i)
	}
	return names
}

// runViewSweep mirrors the per-view work dropPaginationViews performs
// after the information_schema query returns. Extracts the regex + parse
// + TTL comparison so a perf bench can measure it without a CRDB round
// trip (sqlite has no information_schema and no MATERIALIZED VIEW).
func runViewSweep(names []string, ttlMinutes int) []string {
	// compile once per sweep, matching how the production function
	// scopes the compilation to a single sweep pass
	re := regexp.MustCompile(fmt.Sprintf(`%s_(\d{14})_.*`, PaginationViewPrefix))
	now := time.Now()
	ttl := time.Duration(ttlMinutes) * time.Minute
	var toDrop []string
	for _, name := range names {
		// extract the stamp from the view name; the regex enforces the
		// stamp is a 14-digit run so a malformed name is skipped
		matches := re.FindStringSubmatch(name)
		if len(matches) != 2 {
			continue
		}
		// reject stamps that don't parse as YYYYMMDDhhmmss so a
		// pathological name can't stall the sweep
		ts, err := time.Parse("20060102150405", matches[1])
		if err != nil {
			continue
		}
		// collect names whose stamp is older than the TTL
		if now.Sub(ts) > ttl {
			toDrop = append(toDrop, name)
		}
	}
	return toDrop
}

// BenchmarkDropPaginationViewsSweep measures the CPU cost of the sweep's
// per-view regex + parse + TTL comparison over perfSweepViewCount view
// names. Reports p50 and p95 via b.ReportMetric so CI can trend the CPU
// portion of the cleanup goroutine before it crosses the
// TestDropPaginationViewsSweepThresholdMs ceiling.
func BenchmarkDropPaginationViewsSweep(b *testing.B) {
	names := makeSweepViewNames(perfSweepViewCount)
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		_ = runViewSweep(names, perfSweepTTLMinutes)
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()
	reportPaginationPercentiles(b, durations)
}

// TestDropPaginationViewsSweepThresholdMs asserts the sweep p95 over 20
// runs stays under perfSweepThreshold at perfSweepViewCount view names.
// Guards a regex or timestamp parse regression that would push the
// cleanup goroutine into per-tick CPU territory a controller loop would
// notice.
func TestDropPaginationViewsSweepThresholdMs(t *testing.T) {
	names := makeSweepViewNames(perfSweepViewCount)

	// warm-up: prime the compiled regex cache so the first sample
	// doesn't dominate the p95
	_ = runViewSweep(names, perfSweepTTLMinutes)

	// timed runs: collect 20 samples for the p95 read
	samples := make([]time.Duration, 20)
	for i := 0; i < len(samples); i++ {
		start := time.Now()
		_ = runViewSweep(names, perfSweepTTLMinutes)
		samples[i] = time.Since(start)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[perfPercentileIndex(len(samples), 95)]

	if p95 > perfSweepThreshold {
		t.Fatalf(
			"pagination sweep p95 %s exceeds threshold %s (%d views, ttl %dm); "+
				"check for regression in the regex + timestamp parse loop",
			p95, perfSweepThreshold, perfSweepViewCount, perfSweepTTLMinutes,
		)
	}
}

// perfPercentileIndex returns the sorted-slice index for the given
// percentile (0..100) over n samples, clamped to [0, n-1]. Local to the
// perf build tag so the non-perf build has no unused helper.
func perfPercentileIndex(n, pct int) int {
	if n <= 0 {
		return 0
	}
	i := n * pct / 100
	if i >= n {
		i = n - 1
	}
	return i
}

// reportPaginationPercentiles sorts durations and emits p50 and p95 via
// b.ReportMetric so `go test -bench` output carries the percentile
// numbers rather than only the mean ns/op figure.
func reportPaginationPercentiles(b *testing.B, durations []time.Duration) {
	b.Helper()
	if len(durations) == 0 {
		return
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[perfPercentileIndex(len(durations), 50)]
	p95 := durations[perfPercentileIndex(len(durations), 95)]
	b.ReportMetric(float64(p50.Microseconds()), "p50-us")
	b.ReportMetric(float64(p95.Microseconds()), "p95-us")
}
