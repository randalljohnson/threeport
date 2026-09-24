package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api "github.com/threeport/threeport/pkg/api/v0"
)

// setupEventsHandlerDB returns an in-memory sqlite DB with the tables the
// events handler touches: v0_events, v0_attached_object_references, and
// the module registration tables (v0_module_apis, v0_module_objects,
// v0_module_api_routes) that resolveQualifiedTypes() joins over.
func setupEventsHandlerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&api.Event{},
		&api.AttachedObjectReference{},
		&api.ModuleApi{},
		&api.ModuleApiRoute{},
		&api.ModuleObject{},
	))
	return db
}

// newEventsHandler wires a Handler at the given DB with a silent logger.
// PaginationMode defaults to MaterializedView; individual tests override
// when they need to exercise the AsOfSystemTime branch.
func newEventsHandler(db *gorm.DB) Handler {
	return Handler{
		DB:             db,
		Logger:         zap.NewNop(),
		PaginationMode: apiserver_lib.PaginationModeMaterializedView,
	}
}

// newEventsContext builds a CustomContext for a GET at the events-join
// route with the given raw query string. The path is set so
// PayloadCheck's versionFromPath extracts "v0"; the CustomContext wrap
// is what GetEventsJoinAttachedObjectReferences casts to when reading
// pagination params.
func newEventsContext(t *testing.T, rawQuery string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	e.Binder = apiserver_lib.NewQueryBinder()

	target := "/v0/events-join-attached-object-references"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/v0/events-join-attached-object-references")
	return &apiserver_lib.CustomContext{Context: c}, rec
}

// decodeEventsResponse parses the recorded body into the Response envelope.
func decodeEventsResponse(t *testing.T, rec *httptest.ResponseRecorder) apiserver_lib.Response {
	t.Helper()
	var resp apiserver_lib.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

// TestGetEventsJoinAttachedObjectReferences_InvalidLimit rejects a
// pagination limit that exceeds MaxPaginationLimitValue at the
// pagination-params gate, before touching the DB.
func TestGetEventsJoinAttachedObjectReferences_InvalidLimit(t *testing.T) {
	// setup: DB not required past construction; the pagination gate
	// short-circuits before any query runs
	db := setupEventsHandlerDB(t)
	h := newEventsHandler(db)

	// action: limit above the ceiling trips GetPaginationParams
	c, rec := newEventsContext(
		t,
		fmt.Sprintf("limit=%d", apiserver_lib.MaxPaginationLimitValue+1),
	)
	require.NoError(t, h.GetEventsJoinAttachedObjectReferences(c))

	// assert: 400 naming the ceiling constraint
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	resp := decodeEventsResponse(t, rec)
	assert.Contains(t, resp.Status.Error, "limit value is too large")
}

// TestGetEventsJoinAttachedObjectReferences_MissingObjectTypeName covers
// the type-required gate: filtering by objectname without objecttypename
// is ambiguous and returns 400.
func TestGetEventsJoinAttachedObjectReferences_MissingObjectTypeName(t *testing.T) {
	// setup: base handler; the type-required gate rejects before any
	// query touches the tables
	db := setupEventsHandlerDB(t)
	h := newEventsHandler(db)

	// action: supply objectname alone, no objecttypename
	c, rec := newEventsContext(t, "objectname=widget-one")
	require.NoError(t, h.GetEventsJoinAttachedObjectReferences(c))

	// assert: 400 naming the type requirement
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	resp := decodeEventsResponse(t, rec)
	assert.Contains(
		t, resp.Status.Error,
		"objecttypename is required",
		"handler explains that type is required to disambiguate",
	)
}

// TestGetEventsJoinAttachedObjectReferences_ObjectIDAndNameConflict
// covers the ambiguity gate: providing both objectid and objectname
// leaves the filter choice ambiguous and returns 400.
func TestGetEventsJoinAttachedObjectReferences_ObjectIDAndNameConflict(t *testing.T) {
	// setup: base handler; the ambiguity gate rejects before any query
	db := setupEventsHandlerDB(t)
	h := newEventsHandler(db)

	// action: supply both objectid and objectname alongside a valid
	// objecttypename so the id+name conflict branch is the trip
	c, rec := newEventsContext(
		t,
		"objecttypename=Widget&objectid=1&objectname=w",
	)
	require.NoError(t, h.GetEventsJoinAttachedObjectReferences(c))

	// assert: 400 naming the "one or the other" constraint
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	resp := decodeEventsResponse(t, rec)
	assert.Contains(t, resp.Status.Error, "either objectid or objectname, not both")
}

// TestGetEventsJoinAttachedObjectReferences_TypeOnly covers the default
// switch arm: objecttypename supplied without either objectid or
// objectname is unsupported and returns 400 spelling out the accepted
// shapes.
func TestGetEventsJoinAttachedObjectReferences_TypeOnly(t *testing.T) {
	// setup: base handler; default switch arm rejects before any query
	db := setupEventsHandlerDB(t)
	h := newEventsHandler(db)

	// action: supply objecttypename only
	c, rec := newEventsContext(t, "objecttypename=Widget")
	require.NoError(t, h.GetEventsJoinAttachedObjectReferences(c))

	// assert: 400 spelling out the accepted shapes so the caller knows
	// how to make the request valid
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	resp := decodeEventsResponse(t, rec)
	assert.Contains(t, resp.Status.Error, "objecttypename + objectid")
	assert.Contains(t, resp.Status.Error, "objecttypename + objectname")
}

// TestGetEventsJoinAttachedObjectReferences_TypeAndIDUnregisteredKind
// covers the 404 branch for the type+id shape: the id parses cleanly,
// but the bare kind resolves to no fully qualified types (module
// tables are empty and Widget isn't a core type), so the handler
// returns 404.
func TestGetEventsJoinAttachedObjectReferences_TypeAndIDUnregisteredKind(t *testing.T) {
	// setup: DB with the module tables migrated but no rows, so
	// GetObjectTypes() runs the JOIN cleanly and returns zero types
	db := setupEventsHandlerDB(t)
	h := newEventsHandler(db)

	// action: request a bare kind that no module or core type declares
	c, rec := newEventsContext(
		t,
		"objecttypename=NoSuchKind&objectid=1",
	)
	require.NoError(t, h.GetEventsJoinAttachedObjectReferences(c))

	// assert: 404 naming the unregistered kind so the caller can fix
	// the query
	assert.Equal(t, http.StatusNotFound, rec.Code)
	resp := decodeEventsResponse(t, rec)
	assert.Contains(t, resp.Status.Error, "NoSuchKind")
	assert.Contains(t, resp.Status.Error, "not registered")
}

// TestGetEventsJoinAttachedObjectReferences_TypeAndNameUnregisteredKind
// covers the 404 branch for the type+name shape: the bare kind
// resolves to no fully qualified types, so the handler returns 404
// before attempting any name lookup.
func TestGetEventsJoinAttachedObjectReferences_TypeAndNameUnregisteredKind(t *testing.T) {
	// setup: DB with the module tables migrated but no rows
	db := setupEventsHandlerDB(t)
	h := newEventsHandler(db)

	// action: request a bare kind that no module or core type declares
	// alongside an objectname so the type+name branch runs
	c, rec := newEventsContext(
		t,
		"objecttypename=NoSuchKind&objectname=foo",
	)
	require.NoError(t, h.GetEventsJoinAttachedObjectReferences(c))

	// assert: 404 naming the unregistered kind
	assert.Equal(t, http.StatusNotFound, rec.Code)
	resp := decodeEventsResponse(t, rec)
	assert.Contains(t, resp.Status.Error, "NoSuchKind")
	assert.Contains(t, resp.Status.Error, "not registered")
}

// TestGetEventsJoinAttachedObjectReferences_QueryIdWithoutCursor covers
// the continuation gate: a client that supplies a QueryId without a
// Cursor can't be resumed, so the handler returns 400 with the
// coherence message.
func TestGetEventsJoinAttachedObjectReferences_QueryIdWithoutCursor(t *testing.T) {
	// setup: base handler; the coherence gate rejects before any query
	db := setupEventsHandlerDB(t)
	h := newEventsHandler(db)

	// action: supply queryid but no cursor
	c, rec := newEventsContext(t, "queryid=abcd1234")
	require.NoError(t, h.GetEventsJoinAttachedObjectReferences(c))

	// assert: 400 explaining the pair-must-be-set constraint
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	resp := decodeEventsResponse(t, rec)
	assert.Contains(t, resp.Status.Error, "cursor is required")
}

// TestGetEventsJoinAttachedObjectReferences_ContinuationBadHLC covers
// the AsOfSystemTime continuation guard: a caller-supplied queryid
// that isn't a valid HLC decimal is rejected before the raw SQL query
// is built, so no injection can slip into AS OF SYSTEM TIME.
func TestGetEventsJoinAttachedObjectReferences_ContinuationBadHLC(t *testing.T) {
	// setup: handler in AsOfSystemTime mode so the continuation runs
	// through the HLC-validation guard
	db := setupEventsHandlerDB(t)
	h := newEventsHandler(db)
	h.PaginationMode = apiserver_lib.PaginationModeAsOfSystemTime

	// action: supply a non-decimal queryid with a cursor so the
	// continuation branch reads the guard
	c, rec := newEventsContext(t, "queryid=not-hlc&cursor=1")
	require.NoError(t, h.GetEventsJoinAttachedObjectReferences(c))

	// assert: 400 with the restart-hint the guard emits
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	resp := decodeEventsResponse(t, rec)
	assert.Contains(t, resp.Status.Error, "invalid queryid")
	assert.Contains(t, resp.Status.Error, "restart pagination")
}
