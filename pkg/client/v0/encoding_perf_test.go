//go:build perf

package v0

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// perfClientRoundtripPageSize sizes the object slice each encoding
// roundtrip serializes and re-decodes. Mirrors a full list-response
// page the client sees on the wire so the bench matches a real
// endpoint's decode shape.
const perfClientRoundtripPageSize = 100

// perfClientRoundtripIterations sizes the sample count for the
// threshold test. Enough for a stable p95 while keeping the perf run
// brisk.
const perfClientRoundtripIterations = 50

// perfClientRoundtripThreshold caps the p95 wall-clock for one client
// encode + decode roundtrip on a perfClientRoundtripPageSize page. The
// 3x-baseline headroom absorbs CI variance while still catching a
// regression that would slow every client-side list read.
const perfClientRoundtripThreshold = 40 * time.Millisecond

// newPerfClientResponse builds an api_server response envelope populated
// with n Event objects, matching what a real list endpoint writes to the
// wire. The Event surface is used because it carries the pointer-heavy
// json shape most threeport objects have; a marshal + Response-envelope
// unmarshal + typed re-decode against it is representative of the
// client-side path.
func newPerfClientResponse(n int) apiserver_lib.Response {
	now := time.Now()
	data := make([]apiserver_lib.Object, n)
	for i := 0; i < n; i++ {
		id := uint(i + 1)
		data[i] = api_v0.Event{
			Common: api_v0.Common{
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
		}
	}
	return apiserver_lib.Response{
		Meta: apiserver_lib.Meta{
			Pagination:  apiserver_lib.Pagination{Limit: int64(n), HasMore: false},
			ObjectCount: int64(n),
		},
		Type: "Event",
		Data: data,
	}
}

// runClientEncodingRoundtrip replays the encode + decode path a client
// pays on a list read: marshal the response envelope into wire bytes,
// decode those bytes back into a Response with json.Number semantics,
// then re-marshal + re-decode each Data element into its typed struct
// the way GetModuleObjectsWithModuleApiRoutes and its peers do. Returns
// the wall-clock across the whole roundtrip.
func runClientEncodingRoundtrip(tb testing.TB, resp *apiserver_lib.Response) time.Duration {
	tb.Helper()
	start := time.Now()

	// server-side marshal produces the wire bytes the client will read
	wire, err := json.Marshal(resp)
	if err != nil {
		tb.Fatalf("wire marshal failed: %v", err)
	}

	// client-side envelope decode with UseNumber so numeric ids survive
	// the roundtrip without float64 precision loss (matches response.go)
	var decoded apiserver_lib.Response
	envDecoder := json.NewDecoder(bytes.NewReader(wire))
	envDecoder.UseNumber()
	if err := envDecoder.Decode(&decoded); err != nil {
		tb.Fatalf("envelope decode failed: %v", err)
	}

	// per-element re-decode mirrors the pattern the module-object helpers
	// use: marshal the interface back to bytes, decode into the typed
	// destination via a UseNumber decoder so the numeric fields land in
	// their real Go types
	typed := make([]api_v0.Event, 0, len(decoded.Data))
	for i := range decoded.Data {
		elemBytes, err := json.Marshal(decoded.Data[i])
		if err != nil {
			tb.Fatalf("element marshal failed: %v", err)
		}
		var e api_v0.Event
		d := json.NewDecoder(bytes.NewReader(elemBytes))
		d.UseNumber()
		if err := d.Decode(&e); err != nil {
			tb.Fatalf("element decode failed: %v", err)
		}
		typed = append(typed, e)
	}
	if len(typed) != len(decoded.Data) {
		tb.Fatalf("typed re-decode dropped elements: got %d, want %d", len(typed), len(decoded.Data))
	}
	return time.Since(start)
}

// perfClientPercentileIndex returns the sorted-slice index for the given
// percentile (0..100) over n samples, clamped to [0, n-1].
func perfClientPercentileIndex(n, pct int) int {
	if n <= 0 {
		return 0
	}
	i := n * pct / 100
	if i >= n {
		i = n - 1
	}
	return i
}

// reportClientPercentiles sorts durations and emits p50 and p95 via
// b.ReportMetric so `go test -bench` output carries the percentile
// numbers rather than only the mean ns/op figure.
func reportClientPercentiles(b *testing.B, durations []time.Duration) {
	b.Helper()
	if len(durations) == 0 {
		return
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[perfClientPercentileIndex(len(durations), 50)]
	p95 := durations[perfClientPercentileIndex(len(durations), 95)]
	b.ReportMetric(float64(p50.Microseconds()), "p50-us")
	b.ReportMetric(float64(p95.Microseconds()), "p95-us")
}

// BenchmarkClientEncodingRoundtrip measures the wall-clock cost of one
// client-side encode + decode roundtrip against a perfClientRoundtripPageSize
// response page. Reports p50 and p95 via b.ReportMetric so CI can trend
// a regression before it crosses the TestClientEncodingRoundtripThreshold
// ceiling.
func BenchmarkClientEncodingRoundtrip(b *testing.B) {
	resp := newPerfClientResponse(perfClientRoundtripPageSize)
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		durations = append(durations, runClientEncodingRoundtrip(b, &resp))
	}
	b.StopTimer()
	reportClientPercentiles(b, durations)
}

// TestClientEncodingRoundtripThreshold asserts the client-side encoding
// roundtrip p95 stays under perfClientRoundtripThreshold. Guards a
// regression on the encode + decode path every client-side list read
// pays.
func TestClientEncodingRoundtripThreshold(t *testing.T) {
	resp := newPerfClientResponse(perfClientRoundtripPageSize)

	// warm-up: prime json's reflect-cached type info so the first
	// sample doesn't dominate the p95
	_ = runClientEncodingRoundtrip(t, &resp)

	// timed runs: collect perfClientRoundtripIterations samples for
	// the p95 read
	samples := make([]time.Duration, perfClientRoundtripIterations)
	for i := range samples {
		samples[i] = runClientEncodingRoundtrip(t, &resp)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[perfClientPercentileIndex(len(samples), 95)]

	if p95 > perfClientRoundtripThreshold {
		t.Fatalf(
			"client encoding roundtrip p95 %s exceeds threshold %s (page size %d); "+
				"check for regression in marshal, decode, or per-element re-decode",
			p95, perfClientRoundtripThreshold, perfClientRoundtripPageSize,
		)
	}
}
