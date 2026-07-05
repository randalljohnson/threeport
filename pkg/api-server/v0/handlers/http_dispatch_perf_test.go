//go:build perf

package handlers

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// perfHTTPRoute is the path the echo router registers for the dispatch
// bench. A shallow static route matches the shape most threeport
// endpoints expose: a version prefix plus a static resource segment.
const perfHTTPRoute = "/v0/perf-dispatch"

// perfHTTPMethodRoute is a second registered path with a path parameter,
// used to measure the extra cost the router pays walking the tree with a
// bound parameter versus a purely static match.
const perfHTTPMethodRoute = "/v0/perf-dispatch/:id"

// perfHTTPIterations sizes the sample count for the threshold test.
// Enough samples for a stable p95 while keeping the perf run brisk.
const perfHTTPIterations = 200

// perfHTTPStaticThreshold caps the p95 wall-clock for one static-route
// dispatch through echo. echo's radix-tree lookup + context alloc runs
// well under this ceiling on modest hardware; the 3x-baseline headroom
// absorbs CI variance while still catching a regression that would
// slow every handler dispatch.
const perfHTTPStaticThreshold = 200 * time.Microsecond

// perfHTTPParamThreshold caps the p95 wall-clock for one parameterized
// route dispatch. Sits above the static ceiling to reflect the extra
// path-param bind work echo does when a route carries `:name` segments.
const perfHTTPParamThreshold = 300 * time.Microsecond

// newPerfEcho returns an echo.Echo pre-registered with the two dispatch
// routes and a no-op handler. Wiring only the router + one handler
// isolates the dispatch cost from any body-parse or middleware overhead.
func newPerfEcho() *echo.Echo {
	e := echo.New()
	// disable the built-in logger/recover so their middleware doesn't
	// dominate the per-request cost this bench is trying to measure
	e.HideBanner = true
	e.HidePort = true
	noop := func(c echo.Context) error { return c.NoContent(http.StatusOK) }
	e.GET(perfHTTPRoute, noop)
	e.GET(perfHTTPMethodRoute, noop)
	return e
}

// dispatchOneRequest fires one GET at target through the echo router,
// records the wall-clock, and asserts the response reached the no-op
// handler so a router misconfiguration cannot silently zero the samples.
func dispatchOneRequest(tb testing.TB, e *echo.Echo, target string) time.Duration {
	tb.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	start := time.Now()
	e.ServeHTTP(rec, req)
	elapsed := time.Since(start)
	if rec.Code != http.StatusOK {
		tb.Fatalf("dispatch %s produced status %d, want %d", target, rec.Code, http.StatusOK)
	}
	return elapsed
}

// perfHTTPPercentileIndex returns the sorted-slice index for the given
// percentile (0..100) over n samples, clamped to [0, n-1].
func perfHTTPPercentileIndex(n, pct int) int {
	if n <= 0 {
		return 0
	}
	i := n * pct / 100
	if i >= n {
		i = n - 1
	}
	return i
}

// reportHTTPPercentiles sorts durations and emits p50 and p95 via
// b.ReportMetric so `go test -bench` output carries the percentile
// numbers rather than only the mean ns/op figure.
func reportHTTPPercentiles(b *testing.B, durations []time.Duration) {
	b.Helper()
	if len(durations) == 0 {
		return
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[perfHTTPPercentileIndex(len(durations), 50)]
	p95 := durations[perfHTTPPercentileIndex(len(durations), 95)]
	b.ReportMetric(float64(p50.Microseconds()), "p50-us")
	b.ReportMetric(float64(p95.Microseconds()), "p95-us")
}

// BenchmarkHTTPDispatchStaticRoute measures the wall-clock cost of one
// echo router dispatch to a static route with a no-op handler. Reports
// p50 and p95 so CI can trend a regression before it crosses the
// TestHTTPDispatchStaticRouteThreshold ceiling.
func BenchmarkHTTPDispatchStaticRoute(b *testing.B) {
	e := newPerfEcho()
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		durations = append(durations, dispatchOneRequest(b, e, perfHTTPRoute))
	}
	b.StopTimer()
	reportHTTPPercentiles(b, durations)
}

// BenchmarkHTTPDispatchParamRoute measures the wall-clock cost of one
// echo router dispatch to a parameterized route with a no-op handler.
// Peer of BenchmarkHTTPDispatchStaticRoute so param-vs-static regression
// trends are directly comparable.
func BenchmarkHTTPDispatchParamRoute(b *testing.B) {
	e := newPerfEcho()
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		durations = append(durations, dispatchOneRequest(b, e, "/v0/perf-dispatch/42"))
	}
	b.StopTimer()
	reportHTTPPercentiles(b, durations)
}

// TestHTTPDispatchStaticRouteThreshold asserts the static-route dispatch
// p95 over perfHTTPIterations runs stays under perfHTTPStaticThreshold.
// Guards a regression that would raise the per-request floor every
// handler pays.
func TestHTTPDispatchStaticRouteThreshold(t *testing.T) {
	e := newPerfEcho()

	// warm-up: prime echo's context pool + radix tree so the first
	// sample doesn't dominate the p95
	_ = dispatchOneRequest(t, e, perfHTTPRoute)

	// timed runs: collect perfHTTPIterations samples for the p95 read
	samples := make([]time.Duration, perfHTTPIterations)
	for i := range samples {
		samples[i] = dispatchOneRequest(t, e, perfHTTPRoute)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[perfHTTPPercentileIndex(len(samples), 95)]

	if p95 > perfHTTPStaticThreshold {
		t.Fatalf(
			"static-route dispatch p95 %s exceeds threshold %s (route %s); "+
				"check for regression in echo routing or context pool",
			p95, perfHTTPStaticThreshold, perfHTTPRoute,
		)
	}
}

// TestHTTPDispatchParamRouteThreshold asserts the parameterized-route
// dispatch p95 stays under perfHTTPParamThreshold. Guards a regression
// that would raise the per-request floor every parameterized handler
// pays.
func TestHTTPDispatchParamRouteThreshold(t *testing.T) {
	e := newPerfEcho()

	// warm-up: prime echo's context pool + radix tree so the first
	// sample doesn't dominate the p95
	_ = dispatchOneRequest(t, e, "/v0/perf-dispatch/42")

	// timed runs: collect perfHTTPIterations samples for the p95 read
	samples := make([]time.Duration, perfHTTPIterations)
	for i := range samples {
		samples[i] = dispatchOneRequest(t, e, "/v0/perf-dispatch/42")
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[perfHTTPPercentileIndex(len(samples), 95)]

	if p95 > perfHTTPParamThreshold {
		t.Fatalf(
			"param-route dispatch p95 %s exceeds threshold %s (route %s); "+
				"check for regression in echo path-param binding or context pool",
			p95, perfHTTPParamThreshold, perfHTTPMethodRoute,
		)
	}
}
