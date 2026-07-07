package handlers

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	api "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// setupEventJoinTestDB returns an in-memory sqlite db with Event and
// AttachedObjectReference tables migrated. Used to exercise the
// polymorphic JOIN shape the events handler relies on.
func setupEventJoinTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&api.Event{}, &api.AttachedObjectReference{}))
	return db
}

// TestEventsJoin_ExcludesSoftDeletedAOR verifies the JOIN shape used
// by GetEventsJoinAttachedObjectReferences applies
// apiserver_lib.LiveRowsFilter to keep soft-deleted AOR rows out of
// the result. Without the explicit filter, the raw .Joins() form
// would surface stale AORs that don't represent the current state.
func TestEventsJoin_ExcludesSoftDeletedAOR(t *testing.T) {
	db := setupEventJoinTestDB(t)
	fullyQualifiedEventType := (&api.Event{}).GetFullyQualifiedType()

	// create two events, each with its own AOR linking back to a synthetic
	// KubernetesWorkloadInstance subject. Going through Create fires the Event
	// afterCreate hook which inserts the matching AOR.
	now := time.Now()
	e1 := &api.Event{
		Reason:              util.Ptr("R1"),
		Note:                util.Ptr("n1"),
		Type:                util.Ptr("Normal"),
		Count:               util.Ptr(uint(1)),
		EventTime:           &now,
		LastObservedTime:    &now,
		ReportingController: util.Ptr("test"),
		ObjectType:          util.Ptr("threeport.io/v0.KubernetesWorkloadInstance"),
		ObjectID:            util.Ptr(uint(1)),
	}
	e2 := &api.Event{
		Reason:              util.Ptr("R2"),
		Note:                util.Ptr("n2"),
		Type:                util.Ptr("Normal"),
		Count:               util.Ptr(uint(1)),
		EventTime:           &now,
		LastObservedTime:    &now,
		ReportingController: util.Ptr("test"),
		ObjectType:          util.Ptr("threeport.io/v0.KubernetesWorkloadInstance"),
		ObjectID:            util.Ptr(uint(2)),
	}
	require.NoError(t, db.Create(e1).Error)
	require.NoError(t, db.Create(e2).Error)

	// confirm baseline: the JOIN returns both events when nothing is
	// soft-deleted.
	runJoin := func() []api.Event {
		var rows []api.Event
		require.NoError(t, JoinEventsToAttachedObjectReferences(
			db.Model(&api.Event{}),
			fullyQualifiedEventType,
		).Find(&rows).Error)
		return rows
	}
	require.Len(t, runJoin(), 2, "baseline: both events visible with their live AORs")

	// soft-delete e1's AOR directly. e1's event row stays live, but its
	// AOR should drop out of the JOIN.
	require.NoError(t, db.
		Where("attached_object_type = ? AND attached_object_id = ?", fullyQualifiedEventType, *e1.ID).
		Delete(&api.AttachedObjectReference{}).Error)

	rows := runJoin()
	require.Len(t, rows, 1, "soft-deleted AOR must be excluded from the JOIN")
	assert.Equal(t, *e2.ID, *rows[0].ID, "remaining event is e2 (whose AOR is still live)")
}

// TestEventsJoin_CrossTypeFilterSurfacesAllMatches pins the intended
// (Cartesian-product) behavior of applyObjectIdFilter. When a bare
// kind resolves to multiple FQTs (e.g. "Widget" registered in both
// core and a module, each with a record named "foo"), every matching
// (type, id) pair surfaces - the caller asked about "Widget/foo"
// without disambiguating the namespace, so all "Widget/foo"s come
// back. Callers narrow via objectnamespace / objectversion up front,
// not at this filter layer.
func TestEventsJoin_CrossTypeFilterSurfacesAllMatches(t *testing.T) {
	db := setupEventJoinTestDB(t)
	fullyQualifiedEventType := (&api.Event{}).GetFullyQualifiedType()
	now := time.Now()

	// build a small fixture: three events about widgets, spanning two
	// FQTs and including an id that exists under both. The filter
	// should surface every event whose subject is in the resolved
	// (type, id) cross-product.
	subjects := []struct {
		objectType string
		objectID   uint
	}{
		{"threeport.io/v0.Widget", 42},
		{"example.com/v0.Widget", 99},
		{"threeport.io/v0.Widget", 99}, // same id under a different type - intentionally included
	}
	for i, s := range subjects {
		require.NoError(t, db.Create(&api.Event{
			Reason: util.Ptr(fmt.Sprintf("R%d", i)),
			Note:   util.Ptr("n"), Type: util.Ptr("Normal"),
			Count: util.Ptr(uint(1)), EventTime: &now, LastObservedTime: &now,
			ReportingController: util.Ptr("test"),
			ObjectType:          util.Ptr(s.objectType),
			ObjectID:            util.Ptr(s.objectID),
		}).Error)
	}

	// Caller resolution yielded types from both Widget registrations
	// and ids from each (name lookup hit "foo" under each FQT).
	types := []string{"threeport.io/v0.Widget", "example.com/v0.Widget"}
	ids := []uint{42, 99}

	var rows []api.Event
	require.NoError(t, JoinEventsToAttachedObjectReferences(
		db.Model(&api.Event{}),
		fullyQualifiedEventType,
	).
		Where("v0_attached_object_references.object_type IN ?", types).
		Where("v0_attached_object_references.object_id IN ?", ids).
		Find(&rows).Error)

	assert.Len(t, rows, 3, "Cartesian filter surfaces every event whose subject is one of the resolved (type, id) pairs")
}

// rawRow builds an event carrying the aggregation-relevant fields
// keyed off a base instant. Reason, ObjectType, ObjectID, Note form
// the bucket key; EventTime and LastObservedTime drive the window
// walk. Callers pass the raw row's ID so IDs the bucket inherits are
// deterministic in the assertions.
func rawRow(id uint, reason, objectType string, objectID uint, note string, base time.Time, eventTimeOffset, lastObservedOffset time.Duration) api.Event {
	eventTime := base.Add(eventTimeOffset)
	lastObserved := base.Add(lastObservedOffset)
	return api.Event{
		Common:              api.Common{ID: util.Ptr(id)},
		Reason:              util.Ptr(reason),
		Note:                util.Ptr(note),
		Type:                util.Ptr("Warning"),
		Count:               util.Ptr(uint(1)),
		EventTime:           &eventTime,
		LastObservedTime:    &lastObserved,
		ReportingController: util.Ptr("test"),
		ObjectType:          util.Ptr(objectType),
		ObjectID:            util.Ptr(objectID),
	}
}

// TestAggregateEvents_CoversDedupKeyCollapseWithinWindow inserts three
// rows sharing the dedup key inside a 10-minute span; every row folds
// into one bucket, Count reflects the raw-row total, EventTime is the
// earliest row's time, LastObservedTime is the latest row's time.
func TestAggregateEvents_CoversDedupKeyCollapseWithinWindow(t *testing.T) {
	// three raw rows sharing (Reason, ObjectType, ObjectID, Note),
	// spaced under the window so the walk keeps extending one bucket.
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rows := []api.Event{
		rawRow(1, "ScriptFailed", "threeport.io/v0.MachineWorkloadInstance", 42, "n", base, 0, 0),
		rawRow(2, "ScriptFailed", "threeport.io/v0.MachineWorkloadInstance", 42, "n", base, 2*time.Minute, 2*time.Minute),
		rawRow(3, "ScriptFailed", "threeport.io/v0.MachineWorkloadInstance", 42, "n", base, 5*time.Minute, 5*time.Minute),
	}

	// aggregate and assert one bucket carrying the three raw rows.
	out := aggregateEvents(rows)
	require.Len(t, out, 1, "identical-key rows inside the window collapse into one bucket")
	got := out[0]
	require.NotNil(t, got.Count)
	assert.Equal(t, uint(3), *got.Count, "Count reflects raw-row total in the bucket")
	require.NotNil(t, got.EventTime)
	assert.True(t, got.EventTime.Equal(base), "EventTime carries the first raw row's timestamp")
	require.NotNil(t, got.LastObservedTime)
	assert.True(t, got.LastObservedTime.Equal(base.Add(5*time.Minute)), "LastObservedTime slides to the last raw row's timestamp")
	require.NotNil(t, got.ID)
	assert.Equal(t, uint(1), *got.ID, "bucket ID copies the first raw row so the pagination cursor stays stable")
}

// TestAggregateEvents_CoversDedupKeySplitAcrossWindow places three
// key-identical rows so the third one falls outside the window,
// producing two buckets: the first two collapse and the third stands
// alone. Verifies the rolling-window rule: the window slides forward
// on the second emit, so the third's 16-minute offset is measured
// against the second's LastObservedTime, not the first's.
func TestAggregateEvents_CoversDedupKeySplitAcrossWindow(t *testing.T) {
	// rows at t=0, t=5min (within window of t=0), t=16min (outside
	// window of t=5min): first two form one bucket, third stands alone.
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rows := []api.Event{
		rawRow(1, "ScriptFailed", "threeport.io/v0.MachineWorkloadInstance", 42, "n", base, 0, 0),
		rawRow(2, "ScriptFailed", "threeport.io/v0.MachineWorkloadInstance", 42, "n", base, 5*time.Minute, 5*time.Minute),
		rawRow(3, "ScriptFailed", "threeport.io/v0.MachineWorkloadInstance", 42, "n", base, 16*time.Minute, 16*time.Minute),
	}

	// aggregate; two buckets, in EventTime-ascending order.
	out := aggregateEvents(rows)
	require.Len(t, out, 2, "row falling outside the rolling window starts a fresh bucket")

	// first bucket collapses rows 1 and 2.
	first := out[0]
	require.NotNil(t, first.Count)
	assert.Equal(t, uint(2), *first.Count, "first bucket sums the two in-window rows")
	assert.True(t, first.EventTime.Equal(base))
	assert.True(t, first.LastObservedTime.Equal(base.Add(5*time.Minute)))

	// second bucket carries row 3 alone.
	second := out[1]
	require.NotNil(t, second.Count)
	assert.Equal(t, uint(1), *second.Count, "post-window row stands alone in a fresh bucket")
	assert.True(t, second.EventTime.Equal(base.Add(16*time.Minute)))
	assert.True(t, second.LastObservedTime.Equal(base.Add(16*time.Minute)))
}

// TestAggregateEvents_RejectsCrossKeyCollapse pins the bucket-key
// isolation contract: two rows about the same object but with
// different Reason belong to different buckets, no collapse.
func TestAggregateEvents_RejectsCrossKeyCollapse(t *testing.T) {
	// identical object and window but different Reason values: the
	// dedup key differs, so no collapse.
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rows := []api.Event{
		rawRow(1, "ScriptFailed", "threeport.io/v0.MachineWorkloadInstance", 42, "n", base, 0, 0),
		rawRow(2, "ScriptTimedOut", "threeport.io/v0.MachineWorkloadInstance", 42, "n", base, 1*time.Minute, 1*time.Minute),
	}

	// each row lands in its own bucket.
	out := aggregateEvents(rows)
	require.Len(t, out, 2, "distinct Reason values do not share a bucket")
	for _, b := range out {
		require.NotNil(t, b.Count)
		assert.Equal(t, uint(1), *b.Count)
	}
}

// TestAggregateEvents_CoversCausalSortOldestFirst confirms the
// response order at the endpoint is oldest-first by EventTime,
// regardless of input order. Mixes rows across distinct objects so
// aggregation collapses nothing and the sort is the only effect.
func TestAggregateEvents_CoversCausalSortOldestFirst(t *testing.T) {
	// four rows across distinct objects, fed in newest-first order;
	// the aggregation pass should reorder them oldest-first.
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rows := []api.Event{
		rawRow(4, "R", "threeport.io/v0.MachineWorkloadInstance", 4, "n", base, 30*time.Minute, 30*time.Minute),
		rawRow(3, "R", "threeport.io/v0.MachineWorkloadInstance", 3, "n", base, 20*time.Minute, 20*time.Minute),
		rawRow(2, "R", "threeport.io/v0.MachineWorkloadInstance", 2, "n", base, 10*time.Minute, 10*time.Minute),
		rawRow(1, "R", "threeport.io/v0.MachineWorkloadInstance", 1, "n", base, 0, 0),
	}

	// aggregate and read out the EventTime order.
	out := aggregateEvents(rows)
	require.Len(t, out, 4)
	for i := 1; i < len(out); i++ {
		assert.True(t,
			out[i-1].EventTime.Before(*out[i].EventTime) || out[i-1].EventTime.Equal(*out[i].EventTime),
			"aggregation output is sorted by EventTime ascending; index %d violates ordering", i,
		)
	}
	// spot-check the first bucket is the oldest input.
	require.NotNil(t, out[0].ID)
	assert.Equal(t, uint(1), *out[0].ID, "oldest raw row's ID heads the response")
}

// TestAggregateEvents_CollapsesNilAndEmptyNoteTogether covers the
// nil-safe key rule: a nil Note and an empty-string Note produce the
// same bucket key so churn between the two representations does not
// spuriously split buckets.
func TestAggregateEvents_CollapsesNilAndEmptyNoteTogether(t *testing.T) {
	// two rows same key with one nil-Note row and one empty-Note row;
	// key derivation should collapse them.
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	row1 := rawRow(1, "R", "threeport.io/v0.MachineWorkloadInstance", 42, "", base, 0, 0)
	row1.Note = nil
	row2 := rawRow(2, "R", "threeport.io/v0.MachineWorkloadInstance", 42, "", base, 1*time.Minute, 1*time.Minute)

	// aggregate; one bucket, count 2.
	out := aggregateEvents([]api.Event{row1, row2})
	require.Len(t, out, 1, "nil Note and empty Note share the same bucket key")
	require.NotNil(t, out[0].Count)
	assert.Equal(t, uint(2), *out[0].Count)
}
