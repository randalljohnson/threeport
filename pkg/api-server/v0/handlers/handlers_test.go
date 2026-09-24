package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// newHandlersTestDB returns a fresh in-memory sqlite gorm.DB used by
// the handlers tests. Callers migrate whichever tables the specific
// test needs.
func newHandlersTestDB(tb testing.TB) *gorm.DB {
	tb.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(tb, err)
	return db
}

// softDeleteRow is a minimal gorm model with a soft-delete column so
// RequestDB's scope behavior can be observed without pulling in the
// heavier api_v0 models and their hook machinery.
type softDeleteRow struct {
	ID        uint `gorm:"primaryKey"`
	Name      string
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// TestNew asserts the constructor forwards every argument onto the
// returned Handler and leaves nil fields nil.
func TestNew(t *testing.T) {
	// setup: dependencies passed into the constructor
	db := newHandlersTestDB(t)
	logger := zap.NewNop()

	// action: build a Handler with the AsOfSystemTime pagination mode
	h := New(db, nil, nil, logger, apiserver_lib.PaginationModeAsOfSystemTime)

	// assert: every field lands on the returned struct as-passed
	assert.Same(t, db, h.DB)
	assert.Nil(t, h.NC)
	assert.Nil(t, h.JS)
	assert.Same(t, logger, h.Logger)
	assert.Equal(t, apiserver_lib.PaginationModeAsOfSystemTime, h.PaginationMode)
}

// TestRequestDB covers the request-scoped session: by default the
// soft-delete filter hides deleted rows; with includedeleted=true the
// Unscoped scope surfaces them.
func TestRequestDB(t *testing.T) {
	// setup: soft-delete-capable table with one deleted row
	db := newHandlersTestDB(t)
	require.NoError(t, db.AutoMigrate(&softDeleteRow{}))
	row := &softDeleteRow{Name: "gone"}
	require.NoError(t, db.Create(row).Error)
	require.NoError(t, db.Delete(row).Error)

	h := New(db, nil, nil, zap.NewNop(), apiserver_lib.PaginationModeMaterializedView)
	e := echo.New()

	// default request: soft-delete filter is active, the deleted row is hidden
	reqDefault := httptest.NewRequest("GET", "/", nil)
	ctxDefault := e.NewContext(reqDefault, httptest.NewRecorder())
	var hiddenRows []softDeleteRow
	require.NoError(t, h.RequestDB(ctxDefault).Find(&hiddenRows).Error)
	assert.Len(t, hiddenRows, 0, "default request hides soft-deleted rows")

	// includedeleted=true: QueryScopes applies Unscoped and the row returns
	reqIncluded := httptest.NewRequest("GET", "/?includedeleted=true", nil)
	ctxIncluded := e.NewContext(reqIncluded, httptest.NewRecorder())
	var visibleRows []softDeleteRow
	require.NoError(t, h.RequestDB(ctxIncluded).Find(&visibleRows).Error)
	assert.Len(t, visibleRows, 1, "includedeleted=true surfaces soft-deleted rows")
}

// TestRequestDB_PropagatesRequestContext asserts the request context is
// attached to the returned session so downstream cancellation and hooks
// can read per-request state.
func TestRequestDB_PropagatesRequestContext(t *testing.T) {
	// setup: handler and echo context wrapping a stock request
	db := newHandlersTestDB(t)
	h := New(db, nil, nil, zap.NewNop(), apiserver_lib.PaginationModeMaterializedView)
	e := echo.New()
	req := httptest.NewRequest("GET", "/", nil)
	c := e.NewContext(req, httptest.NewRecorder())

	// action: fetch the request-scoped session
	scoped := h.RequestDB(c)

	// assert: the session's Statement carries the request context
	require.NotNil(t, scoped.Statement.Context)
	assert.Equal(t, req.Context(), scoped.Statement.Context)
}

// TestRespondBlockedDelete covers the 409 responder: it writes a
// Conflict status, includes the kebab-cased kind path for the base and
// the blocking attacher, and falls back to id when name resolution
// isn't available.
func TestRespondBlockedDelete(t *testing.T) {
	// setup: fresh DB (name resolution will short-circuit through the
	// error-swallowing fallback since sqlite has neither the core nor
	// module lookup tables), echo context capturing the response
	db := newHandlersTestDB(t)
	e := echo.New()
	req := httptest.NewRequest("DELETE", "/v0/things/42", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// build a BlockedDeleteError with a base object and one attacher
	// under an unregistered namespace so no name lookup succeeds
	blocked := &api_v0.BlockedDeleteError{
		AttachedRefs: []api_v0.AttachedObjectReference{
			{
				ObjectType:         util.Ptr("example.io/v0.Thing"),
				ObjectID:           util.Ptr(uint(42)),
				AttachedObjectType: util.Ptr("example.io/v0.Attacher"),
				AttachedObjectID:   util.Ptr(uint(7)),
			},
		},
	}

	// action: invoke the responder
	require.NoError(t, RespondBlockedDelete(c, db, blocked))

	// assert: status 409 with kebab paths and id fallbacks in the body
	assert.Equal(t, 409, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "example.io/thing/42", "base rendered with kebab kind and id fallback")
	assert.Contains(t, body, "example.io/attacher/7", "attacher rendered with kebab kind and id fallback")
}

// TestGenerateMaterializedViewName covers the produced name shape: the
// paginated prefix, a 14-char timestamp segment, and a 16-char query id
// that matches the returned queryId. Successive calls produce distinct
// query ids.
func TestGenerateMaterializedViewName(t *testing.T) {
	// action: generate two names back to back
	viewName, queryId := GenerateMaterializedViewName()
	_, queryId2 := GenerateMaterializedViewName()

	// assert: query id is the 16-char random alphanumeric
	assert.Len(t, queryId, 16)

	// assert: view name is "<prefix>_<timestamp>_<queryid>"
	assert.True(t, strings.HasPrefix(viewName, apiserver_lib.PaginationViewPrefix+"_"))
	assert.True(t, strings.HasSuffix(viewName, "_"+queryId))
	parts := strings.Split(viewName, "_")
	require.Len(t, parts, 3, "view name has prefix, timestamp, queryid segments")
	assert.Len(t, parts[1], 14, "timestamp segment is YYYYMMDDhhmmss")

	// assert: distinct query ids across calls
	assert.NotEqual(t, queryId, queryId2)
}

// TestGetMaterializedViewRecords covers the raw-select paging path:
// happy first-page and continuation reads, a query error, and the
// reflection guard that rejects a non-slice destination.
func TestGetMaterializedViewRecords(t *testing.T) {
	// setup: build a real sqlite view over a seed table so the raw SQL
	// path exercises the query build and the reflection-based row count
	db := newHandlersTestDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE src (id INTEGER PRIMARY KEY, name TEXT)`).Error)
	for i := 1; i <= 5; i++ {
		require.NoError(t, db.Exec(`INSERT INTO src (id, name) VALUES (?, ?)`, i, "n").Error)
	}
	require.NoError(t, db.Exec(`CREATE VIEW paginated_v AS SELECT * FROM src`).Error)

	h := New(db, nil, nil, zap.NewNop(), apiserver_lib.PaginationModeMaterializedView)

	type row struct {
		ID   uint
		Name string
	}

	// first-page path: cursor 0, limit 2 returns the first two rows
	var firstPage []row
	count, err := h.GetMaterializedViewRecords(
		&firstPage, "paginated_v",
		&apiserver_lib.PageRequestParams{Cursor: 0, Limit: 2},
	)
	require.NoError(t, err)
	assert.EqualValues(t, 2, count)
	require.Len(t, firstPage, 2)
	assert.EqualValues(t, 1, firstPage[0].ID)

	// continuation path: cursor > 0 uses the WHERE ID > cursor branch
	var nextPage []row
	count, err = h.GetMaterializedViewRecords(
		&nextPage, "paginated_v",
		&apiserver_lib.PageRequestParams{Cursor: firstPage[len(firstPage)-1].ID, Limit: 2},
	)
	require.NoError(t, err)
	assert.EqualValues(t, 2, count)
	require.Len(t, nextPage, 2)
	assert.EqualValues(t, 3, nextPage[0].ID)

	// error path: pointing at a nonexistent view surfaces a wrapped error
	var missing []row
	_, err = h.GetMaterializedViewRecords(
		&missing, "no_such_view",
		&apiserver_lib.PageRequestParams{Cursor: 0, Limit: 1},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "handler error: error finding records")

	// reflection guard: a non-slice pointer trips the "must be a slice" branch
	var notSlice row
	_, err = h.GetMaterializedViewRecords(
		&notSlice, "paginated_v",
		&apiserver_lib.PageRequestParams{Cursor: 0, Limit: 1},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "records must be a slice")
}

// TestCreateMaterializedView_ErrorPath asserts CREATE-time failures are
// wrapped with the handler-error prefix and returned with empty
// identifiers. sqlite rejects MATERIALIZED VIEW syntax, so the first
// Exec always errors.
func TestCreateMaterializedView_ErrorPath(t *testing.T) {
	// setup: handler over sqlite, which doesn't support materialized views
	db := newHandlersTestDB(t)
	h := New(db, nil, nil, zap.NewNop(), apiserver_lib.PaginationModeMaterializedView)

	// action: attempt to create a materialized view
	viewName, queryId, err := h.CreateMaterializedView("nonexistent_table")

	// assert: wrapped create error, empty identifiers
	require.Error(t, err)
	assert.Empty(t, viewName)
	assert.Empty(t, queryId)
	assert.Contains(t, err.Error(), "handler error: error creating materialized view")
}

// TestGetMaterializedViewName_ErrorPath asserts information_schema
// lookup failures are wrapped with the handler-error prefix. sqlite
// has no information_schema so the Raw scan always errors.
func TestGetMaterializedViewName_ErrorPath(t *testing.T) {
	// setup: handler over sqlite, which has no information_schema
	db := newHandlersTestDB(t)
	h := New(db, nil, nil, zap.NewNop(), apiserver_lib.PaginationModeMaterializedView)

	// action: look up a view name by a fabricated query id
	name, err := h.GetMaterializedViewName("abcd1234")

	// assert: wrapped lookup error, empty name
	require.Error(t, err)
	assert.Empty(t, name)
	assert.Contains(t, err.Error(), "handler error: error finding materialized view")
}

// TestGetPaginatedRecordsAsOfSystemTime covers the HLC injection guard
// and the HLC-capture error path.
func TestGetPaginatedRecordsAsOfSystemTime(t *testing.T) {
	// setup: handler over sqlite (no cluster_logical_timestamp available)
	db := newHandlersTestDB(t)
	h := New(db, nil, nil, zap.NewNop(), apiserver_lib.PaginationModeAsOfSystemTime)

	type row struct{ ID uint }

	// injection guard: a non-decimal caller-supplied queryId is rejected
	// before any DB interaction
	var rows []row
	hlc, count, err := h.GetPaginatedRecordsAsOfSystemTime(
		&rows, "src", "'; DROP TABLE src;--",
		&apiserver_lib.PageRequestParams{Cursor: 0, Limit: 5},
	)
	require.Error(t, err)
	assert.Empty(t, hlc)
	assert.EqualValues(t, 0, count)
	assert.Contains(t, err.Error(), "invalid queryid")

	// HLC-capture path: empty queryId triggers cluster_logical_timestamp()
	// which sqlite doesn't provide, so the Raw scan errors and gets wrapped
	_, _, err = h.GetPaginatedRecordsAsOfSystemTime(
		&rows, "src", "",
		&apiserver_lib.PageRequestParams{Cursor: 0, Limit: 5},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "capturing HLC snapshot")
}

// TestDispatchGetPaginatedRecords covers the mode switch: the unknown
// mode, the AsOfSystemTime branch's error routing, and both
// MaterializedView branches (initial-page create failure and
// continuation lookup failure).
func TestDispatchGetPaginatedRecords(t *testing.T) {
	db := newHandlersTestDB(t)
	h := New(db, nil, nil, zap.NewNop(), apiserver_lib.PaginationModeMaterializedView)

	type row struct{ ID uint }

	t.Run("unknown mode returns wrapped error", func(t *testing.T) {
		// action: dispatch with a mode value outside the switch
		var rows []row
		queryId, count, err := h.DispatchGetPaginatedRecords(
			apiserver_lib.PaginationMode("bogus"), &rows, "src",
			&apiserver_lib.PageRequestParams{Cursor: 0, Limit: 5},
		)

		// assert: wrapped error, no queryid or count
		require.Error(t, err)
		assert.Empty(t, queryId)
		assert.EqualValues(t, 0, count)
		assert.Contains(t, err.Error(), "unknown pagination mode")
	})

	t.Run("as-of-system-time surfaces invalid queryid", func(t *testing.T) {
		// action: dispatch with a bogus continuation queryId; should route
		// through GetPaginatedRecordsAsOfSystemTime's HLC guard
		var rows []row
		_, _, err := h.DispatchGetPaginatedRecords(
			apiserver_lib.PaginationModeAsOfSystemTime, &rows, "src",
			&apiserver_lib.PageRequestParams{QueryId: "bogus", Cursor: 0, Limit: 5},
		)

		// assert: the guard error is surfaced (TranslatePaginationSessionError
		// is a no-op for a non-GC error)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid queryid")
	})

	t.Run("materialized-view initial page: create failure", func(t *testing.T) {
		// action: dispatch with empty QueryId; the initial-page branch
		// tries CreateMaterializedView, which fails on sqlite
		var rows []row
		_, _, err := h.DispatchGetPaginatedRecords(
			apiserver_lib.PaginationModeMaterializedView, &rows, "src",
			&apiserver_lib.PageRequestParams{QueryId: "", Cursor: 0, Limit: 5},
		)

		// assert: the CreateMaterializedView error is surfaced verbatim
		require.Error(t, err)
		assert.Contains(t, err.Error(), "creating materialized view")
	})

	t.Run("materialized-view continuation: lookup failure echoes queryid", func(t *testing.T) {
		// action: dispatch with a non-empty QueryId; the continuation
		// branch calls GetMaterializedViewName, which fails on sqlite
		var rows []row
		queryId, count, err := h.DispatchGetPaginatedRecords(
			apiserver_lib.PaginationModeMaterializedView, &rows, "src",
			&apiserver_lib.PageRequestParams{QueryId: "abcd1234", Cursor: 0, Limit: 5},
		)

		// assert: the lookup error is surfaced and the caller-supplied
		// queryid is echoed back so the client can retry
		require.Error(t, err)
		assert.Equal(t, "abcd1234", queryId)
		assert.EqualValues(t, 0, count)
	})
}
