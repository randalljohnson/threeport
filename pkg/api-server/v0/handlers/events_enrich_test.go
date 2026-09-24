package handlers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	api "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// setupEnrichTestDB returns an in-memory sqlite db with every table
// enrichEventsWithObjectInfo touches migrated: the Event source rows,
// the AttachedObjectReference join source, KubernetesWorkloadInstance
// as one core name-resolver hit, and the module-registry tables so
// GetModuleRouteForType finds an empty registry (rather than erroring)
// for unknown types.
func setupEnrichTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&api.Event{},
		&api.AttachedObjectReference{},
		&api.KubernetesWorkloadInstance{},
		// module-registry tables let GetModuleRouteForType return an
		// empty result (no owning module) instead of erroring when the
		// object type isn't a known core type.
		&api.ModuleApi{},
		&api.ModuleApiRoute{},
		&api.ModuleObject{},
	))
	return db
}

// makeEvent builds a persisted Event whose Event.afterCreate hook
// inserts the matching AOR pointing at (objectType, objectID).
func makeEvent(t *testing.T, db *gorm.DB, objectType string, objectID uint) *api.Event {
	t.Helper()
	now := time.Now()
	e := &api.Event{
		Reason:              util.Ptr("R"),
		Note:                util.Ptr("n"),
		Type:                util.Ptr("Normal"),
		Count:               util.Ptr(uint(1)),
		EventTime:           &now,
		LastObservedTime:    &now,
		ReportingController: util.Ptr("test"),
		ObjectType:          util.Ptr(objectType),
		ObjectID:            util.Ptr(objectID),
	}
	require.NoError(t, db.Create(e).Error)
	return e
}

// TestEnrich_EmptyReturnsNil covers the early exit when there are no
// events to enrich: no db activity and no error.
func TestEnrich_EmptyReturnsNil(t *testing.T) {
	db := setupEnrichTestDB(t)

	// nil-safe: no logger call is made because the function bails
	// before any lookup runs, so a nop logger is enough
	err := enrichEventsWithObjectInfo(db, nil, zap.NewNop())
	require.NoError(t, err)
}

// TestEnrich_SkipsEventsWithNilID guards the nil-dedupe pass: an event
// slice made up entirely of rows without IDs returns cleanly rather
// than dereffing a nil pointer or issuing an AOR query with an empty
// id list.
func TestEnrich_SkipsEventsWithNilID(t *testing.T) {
	db := setupEnrichTestDB(t)

	// two hand-built events with no ID; enrich must skip them both
	events := []api.Event{{}, {}}
	err := enrichEventsWithObjectInfo(db, events, zap.NewNop())
	require.NoError(t, err)

	// nothing projected onto either row
	assert.Nil(t, events[0].ObjectType)
	assert.Nil(t, events[1].ObjectType)
}

// TestEnrich_ProjectsObjectTypeAndIDFromAOR verifies the first
// projection pass: AOR rows joined by (attached_object_type,
// attached_object_id) fill ObjectType and ObjectID on each event. Uses
// an object type unknown to core so the name-resolution step
// intentionally leaves ObjectName nil - this test isolates the AOR
// projection from the name lookup.
func TestEnrich_ProjectsObjectTypeAndIDFromAOR(t *testing.T) {
	db := setupEnrichTestDB(t)

	// two events pointing at distinct (unknown) subjects; each Event.afterCreate
	// inserts the matching AOR that enrich reads back
	e1 := makeEvent(t, db, "example.com/v0.Widget", 42)
	e2 := makeEvent(t, db, "example.com/v0.Widget", 99)

	// re-load events without the gorm:"-" projection fields so
	// enrichEventsWithObjectInfo has to fill them from the AOR side
	events := []api.Event{
		{Common: api.Common{ID: e1.ID}},
		{Common: api.Common{ID: e2.ID}},
	}

	// action under test: enrichment projects AOR fields onto each event row
	require.NoError(t, enrichEventsWithObjectInfo(db, events, zap.NewNop()))

	// each event carries the AOR's (ObjectType, ObjectID) back to the caller
	require.NotNil(t, events[0].ObjectType)
	assert.Equal(t, "example.com/v0.Widget", *events[0].ObjectType)
	require.NotNil(t, events[0].ObjectID)
	assert.Equal(t, uint(42), *events[0].ObjectID)

	require.NotNil(t, events[1].ObjectType)
	assert.Equal(t, "example.com/v0.Widget", *events[1].ObjectType)
	require.NotNil(t, events[1].ObjectID)
	assert.Equal(t, uint(99), *events[1].ObjectID)

	// unknown type + no owning module -> ObjectName stays nil
	assert.Nil(t, events[0].ObjectName)
	assert.Nil(t, events[1].ObjectName)
}

// TestEnrich_ResolvesNameForCoreType covers the name-projection pass
// end to end: a core-known object type dispatches to
// GetCoreObjectNamesByIDs, whose result fills ObjectName on every
// event whose subject id was found.
func TestEnrich_ResolvesNameForCoreType(t *testing.T) {
	db := setupEnrichTestDB(t)

	// seed the subject: a KubernetesWorkloadInstance the enrich pass
	// will look up by id. SkipHooks avoids the tagged-field hooks that
	// would require the full registry surrounding a real workload.
	kwi := &api.KubernetesWorkloadInstance{
		Instance:                       api.Instance{Name: util.Ptr("web-server")},
		KubernetesRuntimeInstanceID:    util.Ptr(uint(1)),
		KubernetesWorkloadDefinitionID: util.Ptr(uint(1)),
	}
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(kwi).Error)
	require.NotNil(t, kwi.ID)

	// event whose subject is the seeded workload instance; the AOR
	// afterCreate hook wires it up
	e := makeEvent(t, db, "threeport.io/v0.KubernetesWorkloadInstance", *kwi.ID)

	// only ID populated on the input row - enrich must fetch and set the rest
	events := []api.Event{{Common: api.Common{ID: e.ID}}}

	// action under test: enrichment resolves the name via the core resolver
	require.NoError(t, enrichEventsWithObjectInfo(db, events, zap.NewNop()))

	// AOR projection populated
	require.NotNil(t, events[0].ObjectType)
	assert.Equal(t, "threeport.io/v0.KubernetesWorkloadInstance", *events[0].ObjectType)
	require.NotNil(t, events[0].ObjectID)
	assert.Equal(t, *kwi.ID, *events[0].ObjectID)

	// name projection populated from the core resolver
	require.NotNil(t, events[0].ObjectName)
	assert.Equal(t, "web-server", *events[0].ObjectName)
}

// TestEnrich_DoesNotFailWhenTypeUnknown pins the graceful-degradation
// contract: when a subject's type isn't owned by core or any
// registered module, the AOR projection still happens but ObjectName
// stays nil and the enrich call returns nil (no error surfaced).
func TestEnrich_DoesNotFailWhenTypeUnknown(t *testing.T) {
	db := setupEnrichTestDB(t)

	// unknown type - not in core registry, no module rows planted
	e := makeEvent(t, db, "unregistered.io/v0.Ghost", 7)

	events := []api.Event{{Common: api.Common{ID: e.ID}}}

	// action under test: enrichment does not surface the unknown-type miss as an error
	require.NoError(t, enrichEventsWithObjectInfo(db, events, zap.NewNop()))

	// AOR fields populated even when the name lookup can't resolve
	require.NotNil(t, events[0].ObjectType)
	assert.Equal(t, "unregistered.io/v0.Ghost", *events[0].ObjectType)
	assert.Nil(t, events[0].ObjectName, "unresolvable type leaves ObjectName nil")
}

// TestEnrich_DedupesRepeatedEventIDs guards the seen-map pass: when
// the same event id appears more than once in the input slice (as
// happens under overlapping pagination retries), only one AOR row is
// fetched but every occurrence of that id in the slice still gets its
// projection fields filled.
func TestEnrich_DedupesRepeatedEventIDs(t *testing.T) {
	db := setupEnrichTestDB(t)

	// one persisted event; the input slice will reference it twice
	e := makeEvent(t, db, "example.com/v0.Widget", 42)

	// duplicated by id so the seen-map path runs
	events := []api.Event{
		{Common: api.Common{ID: e.ID}},
		{Common: api.Common{ID: e.ID}},
	}

	// action under test: enrichment tolerates duplicate ids across the slice
	require.NoError(t, enrichEventsWithObjectInfo(db, events, zap.NewNop()))

	// both occurrences carry the projected AOR fields; the dedupe pass
	// removes the second id from the AOR query but not from the projection loop
	for i, ev := range events {
		require.NotNilf(t, ev.ObjectType, "row %d ObjectType", i)
		assert.Equal(t, "example.com/v0.Widget", *ev.ObjectType)
		require.NotNilf(t, ev.ObjectID, "row %d ObjectID", i)
		assert.Equal(t, uint(42), *ev.ObjectID)
	}
}
