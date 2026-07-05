//go:build perf

package handlers

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	api "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// perfHarnessSubjectType is the fully qualified type used for seeded AOR
// subject rows: a core kind so the join and filter paths match what the
// events endpoint sees in a running api server.
const perfHarnessSubjectType = "threeport.io/v0.KubernetesWorkloadInstance"

// newPerfDB() returns an in-memory sqlite gorm.DB with the tables the
// events perf path reads through: v0_events for the outer scan and
// v0_attached_object_references for the polymorphic join.
func newPerfDB(tb testing.TB) *gorm.DB {
	tb.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(tb, err)
	require.NoError(tb, db.AutoMigrate(&api.Event{}, &api.AttachedObjectReference{}))
	return db
}

// seedPerfEvents() inserts n events plus a matching AOR per event so the
// events JOIN produces n live rows. SkipHooks bypasses the AfterCreate
// path so the harness doesn't depend on core-type registration.
func seedPerfEvents(tb testing.TB, db *gorm.DB, n int) {
	tb.Helper()
	fullyQualifiedEventType := (&api.Event{}).GetFullyQualifiedType()
	now := time.Now()
	skipHooks := db.Session(&gorm.Session{SkipHooks: true})
	for i := 0; i < n; i++ {
		subjectID := uint(i + 1)
		e := &api.Event{
			Reason:              util.Ptr(fmt.Sprintf("R%d", i)),
			Note:                util.Ptr("n"),
			Type:                util.Ptr("Normal"),
			Count:               util.Ptr(uint(1)),
			EventTime:           &now,
			LastObservedTime:    &now,
			ReportingController: util.Ptr("perf"),
		}
		require.NoError(tb, skipHooks.Create(e).Error)
		aor := &api.AttachedObjectReference{
			ObjectType:         util.Ptr(perfHarnessSubjectType),
			ObjectID:           util.Ptr(subjectID),
			AttachedObjectType: util.Ptr(fullyQualifiedEventType),
			AttachedObjectID:   e.ID,
		}
		require.NoError(tb, skipHooks.Create(aor).Error)
	}
}

// perfPercentileIndex() returns the sorted-slice index for the given
// percentile (0..100) over n samples, clamped to [0, n-1]. Peer of the
// helper in events_bench_test.go; kept in this file so perf-only builds
// pick up one definition.
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

// timeSamples() runs fn iters times, records each wall-clock duration,
// sorts the samples, and returns the sorted slice for percentile reads.
func timeSamples(tb testing.TB, iters int, fn func() error) []time.Duration {
	tb.Helper()
	samples := make([]time.Duration, iters)
	for i := 0; i < iters; i++ {
		start := time.Now()
		if err := fn(); err != nil {
			tb.Fatalf("perf iteration %d failed: %v", i, err)
		}
		samples[i] = time.Since(start)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples
}
