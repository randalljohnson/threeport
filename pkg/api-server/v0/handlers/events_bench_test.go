package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	zap "go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	api "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// events perf harness knobs. eventCount * moduleLatency is the wall-clock
// cost of the sequential per-id fan-out mode; the fixed handler resolves
// the same batch well under perfLatencyCeiling by parallelizing or
// batching the module lookups.
const (
	perfEventCount     = 20
	perfModuleLatency  = 200 * time.Millisecond
	perfLatencyCeiling = 2 * time.Second
	perfIterations     = 5
)

// setupEventsPerfHarness stands up an in-memory sqlite db, a fake module
// api that sleeps perfModuleLatency per response, and perfEventCount
// events whose subject is a module-owned Widget. The returned enrich
// closure runs enrichEventsWithObjectInfo against a fresh copy of the
// seeded event slice so repeated iterations start from the same state.
func setupEventsPerfHarness(tb testing.TB) (
	db *gorm.DB,
	enrich func() error,
	requestCount *int64,
	teardown func(),
) {
	tb.Helper()

	// fake module api serving GET /example-com/v0/widgets/{id} with a
	// one-row Data payload; sleeps to make the per-id sequential fan-out
	// mode expensive enough to distinguish from the parallel path.
	var counter int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&counter, 1)
		time.Sleep(perfModuleLatency)
		segs := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
		idStr := segs[len(segs)-1]
		row := map[string]interface{}{"ID": idStr, "Name": "widget-" + idStr}
		body := map[string]interface{}{
			"Data":   []interface{}{row},
			"Status": map[string]interface{}{"Code": 200, "Message": "OK"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))

	// in-memory sqlite mirrors the schema tables the enrich path reads:
	// events + AORs for the join, module_apis + module_api_routes +
	// module_objects + the m2m junction for the type->endpoint lookup.
	d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(tb, err)
	require.NoError(tb, d.AutoMigrate(
		&api.Event{},
		&api.AttachedObjectReference{},
		&api.ModuleApi{},
		&api.ModuleApiRoute{},
		&api.ModuleObject{},
	))

	// module route lookup stores host:port; the client prepends http://
	endpoint := strings.TrimPrefix(server.URL, "http://")

	// seed the module owning example.com/v0.Widget with a CRUD route
	// pointing at the fake server. GetModuleRouteForType joins across
	// v0_module_apis, v0_module_api_routes, v0_module_objects, and the
	// m2m junction, so every one gets a row.
	skipHooks := d.Session(&gorm.Session{SkipHooks: true})
	modApi := &api.ModuleApi{
		Name:         util.Ptr("widget-module"),
		Core:         util.Ptr(false),
		ApiNamespace: util.Ptr("example.com"),
		Endpoint:     util.Ptr(endpoint),
	}
	require.NoError(tb, skipHooks.Create(modApi).Error)
	obj := &api.ModuleObject{
		Name:        util.Ptr("Widget"),
		Version:     util.Ptr("v0"),
		ModuleApiID: modApi.ID,
	}
	require.NoError(tb, skipHooks.Create(obj).Error)
	route := &api.ModuleApiRoute{
		Path:        util.Ptr("/example-com/v0/widgets"),
		ModuleApiID: modApi.ID,
	}
	require.NoError(tb, skipHooks.Create(route).Error)
	// direct junction insert so BeforeCreate hooks don't reject the
	// already-persisted ModuleObject as duplicate.
	require.NoError(tb, skipHooks.Exec(
		"INSERT INTO v0_module_api_routes_module_objects (module_api_route_id, module_object_id) VALUES (?, ?)",
		route.ID, obj.ID,
	).Error)

	// seed events + AORs directly (SkipHooks bypasses the AfterCreate
	// path so the harness doesn't depend on core-type registration for
	// Widget).
	fullyQualifiedEventType := (&api.Event{}).GetFullyQualifiedType()
	now := time.Now()
	seeds := make([]api.Event, perfEventCount)
	for i := 0; i < perfEventCount; i++ {
		widgetID := uint(i + 1)
		e := &api.Event{
			Reason:              util.Ptr(fmt.Sprintf("R%d", i)),
			Note:                util.Ptr("n"),
			Type:                util.Ptr("Normal"),
			Count:               util.Ptr(uint(1)),
			EventTime:           &now,
			LastObservedTime:    &now,
			ReportingController: util.Ptr("test"),
		}
		require.NoError(tb, d.Session(&gorm.Session{SkipHooks: true}).Create(e).Error)
		aor := &api.AttachedObjectReference{
			ObjectType:         util.Ptr("example.com/v0.Widget"),
			ObjectID:           util.Ptr(widgetID),
			AttachedObjectType: util.Ptr(fullyQualifiedEventType),
			AttachedObjectID:   e.ID,
		}
		require.NoError(tb, d.Session(&gorm.Session{SkipHooks: true}).Create(aor).Error)
		seeds[i] = *e
	}

	logger := zap.NewNop()

	// each enrich call gets a shallow copy of the seed slice with the
	// projection-only fields reset so a prior iteration's populated
	// ObjectName can't short-circuit the module lookup on the next.
	enrich = func() error {
		fresh := make([]api.Event, len(seeds))
		copy(fresh, seeds)
		for i := range fresh {
			fresh[i].ObjectType = nil
			fresh[i].ObjectID = nil
			fresh[i].ObjectName = nil
		}
		return enrichEventsWithObjectInfo(context.Background(), d, fresh, logger)
	}
	teardown = func() { server.Close() }
	return d, enrich, &counter, teardown
}

// BenchmarkGetEventsWithFakeModules measures wall-clock cost of the
// enrichment path (module name lookup) with perfEventCount events
// pointing at a slow module. Reports p50 and p95 via b.ReportMetric so
// CI can trend regressions before they cross the hard ceiling asserted
// by TestGetEventsEnrich_LatencyCeiling.
func BenchmarkGetEventsWithFakeModules(b *testing.B) {
	_, enrich, _, teardown := setupEventsPerfHarness(b)
	defer teardown()

	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if err := enrich(); err != nil {
			b.Fatalf("enrich failed: %v", err)
		}
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[percentileIndex(len(durations), 50)]
	p95 := durations[percentileIndex(len(durations), 95)]
	b.ReportMetric(float64(p50.Milliseconds()), "p50-ms")
	b.ReportMetric(float64(p95.Milliseconds()), "p95-ms")
}

// TestGetEventsEnrich_LatencyCeiling fails when the module name lookup
// falls back to a sequential per-id fan-out. With perfEventCount events
// and a perfModuleLatency-per-request fake module, a sequential run
// costs perfEventCount * perfModuleLatency (well over perfLatencyCeiling);
// a parallel or batched implementation finishes under the ceiling. This
// is the regression guard for the 51s events endpoint mode observed in
// production.
func TestGetEventsEnrich_LatencyCeiling(t *testing.T) {
	_, enrich, _, teardown := setupEventsPerfHarness(t)
	defer teardown()

	// warm run: primes the DB path and validates the harness compiles a
	// real call before we start timing.
	require.NoError(t, enrich())

	// timed runs: collect perfIterations samples, sort, take p95.
	durations := make([]time.Duration, perfIterations)
	for i := 0; i < perfIterations; i++ {
		start := time.Now()
		require.NoError(t, enrich())
		durations[i] = time.Since(start)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[percentileIndex(len(durations), 95)]

	// hard ceiling: sequential fan-out would take
	// perfEventCount*perfModuleLatency which is well above perfLatencyCeiling.
	if p95 > perfLatencyCeiling {
		t.Fatalf(
			"events enrich p95 %s exceeds ceiling %s (workload: %d events, %s per module lookup); "+
				"check for sequential per-id fan-out in module name resolution",
			p95, perfLatencyCeiling, perfEventCount, perfModuleLatency,
		)
	}
}

// percentileIndex returns the sorted-slice index for the given
// percentile (0..100) over n samples, clamped to [0, n-1].
func percentileIndex(n, pct int) int {
	if n <= 0 {
		return 0
	}
	i := n * pct / 100
	if i >= n {
		i = n - 1
	}
	return i
}
