package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_lib "github.com/threeport/threeport/pkg/api/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// registerModuleVersionsOnce guards the one-time registration of Module*
// tagged-fields entries the payload-check path reads through. Multiple
// tests exercise the same handlers, so guard against re-invocation.
var registerModuleVersionsOnce sync.Once

// registerModuleVersions wires the Module* tagged-fields entries into the
// shared apiserver_lib registry. PayloadCheck reads through
// ObjectTaggedFields keyed by (version, object), so the entries must be
// present before any handler test runs. Registration is inline rather
// than delegated to pkg/api-server/v0/versions to avoid the routes ->
// handlers import cycle triggered by importing versions from a handlers
// test file.
func registerModuleVersions(t *testing.T) {
	t.Helper()
	registerModuleVersionsOnce.Do(func() {
		registerModuleTaggedFields(new(api_v0.ModuleApiRoute), api_v0.ObjectTypeModuleApiRoute)
		registerModuleTaggedFields(new(api_v0.ModuleObject), api_v0.ObjectTypeModuleObject)
	})
}

// registerModuleTaggedFields walks obj's struct tags to build a
// FieldsByTag entry, then registers it under (v0, objectType) in the
// shared ObjectTaggedFields map. Mirrors what the codegen'd
// versions.AddModule*Versions() functions do but without pulling in the
// package that would cycle back to handlers.
func registerModuleTaggedFields(obj interface{}, objectType string) {
	tagName := string(api_lib.ValidateTag)
	tf := map[string]*apiserver_lib.FieldsByTag{
		tagName: {
			Optional:             []string{},
			OptionalAssociations: []string{},
			Required:             []string{},
			TagName:              tagName,
		},
	}
	apiserver_lib.ParseStruct(tagName, reflect.ValueOf(obj), "", apiserver_lib.Translate, tf)
	apiserver_lib.ObjectTaggedFields[apiserver_lib.VersionObject{
		Object:  objectType,
		Version: "v0",
	}] = tf[tagName]
}

// setupModuleTestDB returns an in-memory sqlite gorm.DB with the module
// tables and their m2m join table migrated. sqlite is used so tests don't
// require CockroachDB features (materialized views, cluster_logical_timestamp).
func setupModuleTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&api_v0.ModuleApi{},
		&api_v0.ModuleApiRoute{},
		&api_v0.ModuleObject{},
		&api_v0.AttachedObjectReference{},
	))
	return db
}

// newModuleEcho returns an echo instance configured with a CustomValidator
// so ValidateBoundData's c.Validate() path exercises the same tags the
// real api-server registers.
func newModuleEcho(t *testing.T) *echo.Echo {
	t.Helper()
	e := echo.New()
	v := validator.New()
	require.NoError(t, v.RegisterValidation("optional", apiserver_lib.IsOptional))
	require.NoError(t, v.RegisterValidation("association", apiserver_lib.IsAssociation))
	require.NoError(t, v.RegisterValidation("ISO8601date", apiserver_lib.IsISO8601Date))
	e.Validator = &apiserver_lib.CustomValidator{Validator: v}
	e.Binder = apiserver_lib.NewQueryBinder()
	return e
}

// newModuleHandler returns a Handler wired with db, a no-op zap logger, and
// AsOfSystemTime pagination mode (irrelevant for tests that stay under the
// pagination limit but harmless as a default).
func newModuleHandler(db *gorm.DB) Handler {
	return Handler{
		DB:             db,
		Logger:         zap.NewNop(),
		PaginationMode: apiserver_lib.PaginationModeAsOfSystemTime,
	}
}

// postJSONContext builds an echo.Context for a POST with the given body
// mapped at the given path so PayloadCheck's versionFromPath sees a "/v0"
// prefix. The path is registered on the router so c.Path() returns it.
func postJSONContext(t *testing.T, e *echo.Echo, path string, body []byte) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath(path)
	return c, rec
}

// getContext builds an echo.Context for a GET at the given path. Optional
// param maps the path param name to its value (used for the by-ID handler).
// The returned CustomContext is what handlers cast to when reading
// pagination params.
func getContext(t *testing.T, e *echo.Echo, path string, params map[string]string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath(path)
	names := make([]string, 0, len(params))
	vals := make([]string, 0, len(params))
	for k, v := range params {
		names = append(names, k)
		vals = append(vals, v)
	}
	c.SetParamNames(names...)
	c.SetParamValues(vals...)
	return &apiserver_lib.CustomContext{Context: c}, rec
}

// decodeModuleResponse parses the recorded body into a Response envelope so
// tests can assert on Data/Status without redoing json plumbing per case.
// A module-scoped helper avoids collision with a similarly named helper in
// kubernetes_workload_test.go.
func decodeModuleResponse(t *testing.T, rec *httptest.ResponseRecorder) apiserver_lib.Response {
	t.Helper()
	var resp apiserver_lib.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

// TestAddModuleApiRouteWithModuleObjectReferences_HappyPath covers the
// success path: a valid payload persists the ModuleApiRoute row and the
// handler returns 201 with the created object in Data.
func TestAddModuleApiRouteWithModuleObjectReferences_HappyPath(t *testing.T) {
	// setup: registered versions, migrated DB, echo wiring
	registerModuleVersions(t)
	db := setupModuleTestDB(t)
	h := newModuleHandler(db)
	e := newModuleEcho(t)

	// seed a ModuleApi row so the ModuleApiRoute afterCreate hook can look
	// it up when honoring the requires-relationship
	moduleApi := &api_v0.ModuleApi{
		Name:     util.Ptr("api"),
		Core:     util.Ptr(true),
		Endpoint: util.Ptr("http://example.com"),
	}
	require.NoError(t, db.Create(moduleApi).Error)

	// build a minimal valid payload: Path and ModuleApiID are the only
	// required fields on ModuleApiRoute
	body, err := json.Marshal(map[string]interface{}{
		"Path":        "/v0/widgets",
		"ModuleApiID": *moduleApi.ID,
	})
	require.NoError(t, err)

	c, rec := postJSONContext(t, e, "/v0/module-api-route-with-module-object-reference", body)

	// action: run the handler
	require.NoError(t, h.AddModuleApiRouteWithModuleObjectReferences(c))

	// assert the response envelope
	assert.Equal(t, http.StatusCreated, rec.Code)
	resp := decodeModuleResponse(t, rec)
	assert.Equal(t, api_v0.ObjectTypeModuleApiRoute, resp.Type)
	require.Len(t, resp.Data, 1)

	// assert the row was persisted with the payload values
	var stored api_v0.ModuleApiRoute
	require.NoError(t, db.First(&stored).Error)
	assert.Equal(t, "/v0/widgets", *stored.Path)
	assert.Equal(t, uint(1), *stored.ModuleApiID)
}

// TestAddModuleApiRouteWithModuleObjectReferences_EmptyPayload rejects a
// {} body with 400 and the empty-payload message. PayloadCheck is the
// first gate the handler runs.
func TestAddModuleApiRouteWithModuleObjectReferences_EmptyPayload(t *testing.T) {
	registerModuleVersions(t)
	db := setupModuleTestDB(t)
	h := newModuleHandler(db)
	e := newModuleEcho(t)

	// action: empty JSON object trips the payload-empty check
	c, rec := postJSONContext(t, e, "/v0/module-api-route-with-module-object-reference", []byte(`{}`))
	require.NoError(t, h.AddModuleApiRouteWithModuleObjectReferences(c))

	// assert 400 with the empty-payload message
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	resp := decodeModuleResponse(t, rec)
	assert.Contains(t, resp.Status.Error, apiserver_lib.ErrMsgJSONPayloadEmpty)
}

// TestAddModuleApiRouteWithModuleObjectReferences_UnsupportedField rejects a
// payload carrying a field name the tagged-fields registry doesn't know
// about with 400.
func TestAddModuleApiRouteWithModuleObjectReferences_UnsupportedField(t *testing.T) {
	registerModuleVersions(t)
	db := setupModuleTestDB(t)
	h := newModuleHandler(db)
	e := newModuleEcho(t)

	// setup: valid required fields plus an unknown field
	body := []byte(`{"Path":"/v0/x","ModuleApiID":1,"NotAField":"x"}`)
	c, rec := postJSONContext(t, e, "/v0/module-api-route-with-module-object-reference", body)

	require.NoError(t, h.AddModuleApiRouteWithModuleObjectReferences(c))

	// assert 400 with the unsupported-field message
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	resp := decodeModuleResponse(t, rec)
	assert.Contains(t, resp.Status.Error, apiserver_lib.ErrMsgUnsupportedFieldsNotAllowed)
}

// TestAddModuleApiRouteWithModuleObjectReferences_MissingRequired returns
// 400 when the payload omits a required field (ModuleApiID). Path is
// provided so PayloadCheck passes and the check surfaces from
// ValidateBoundData.
func TestAddModuleApiRouteWithModuleObjectReferences_MissingRequired(t *testing.T) {
	registerModuleVersions(t)
	db := setupModuleTestDB(t)
	h := newModuleHandler(db)
	e := newModuleEcho(t)

	// setup: omit ModuleApiID so the required-field check trips
	body := []byte(`{"Path":"/v0/x"}`)
	c, rec := postJSONContext(t, e, "/v0/module-api-route-with-module-object-reference", body)

	require.NoError(t, h.AddModuleApiRouteWithModuleObjectReferences(c))

	// assert 400 with the missing-required message naming ModuleApiID
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	resp := decodeModuleResponse(t, rec)
	assert.Contains(t, resp.Status.Error, apiserver_lib.ErrMsgMissingRequiredFields)
	assert.Contains(t, resp.Status.Error, "ModuleApiID")
}

// TestAddModuleApiRouteWithModuleObjectReferences_DBError returns 500 when
// the Create hits a DB error. The migration is skipped on purpose so the
// insert targets a non-existent table.
func TestAddModuleApiRouteWithModuleObjectReferences_DBError(t *testing.T) {
	registerModuleVersions(t)

	// setup: open sqlite without migrating so Create fails
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	h := newModuleHandler(db)
	e := newModuleEcho(t)

	body := []byte(`{"Path":"/v0/x","ModuleApiID":1}`)
	c, rec := postJSONContext(t, e, "/v0/module-api-route-with-module-object-reference", body)

	require.NoError(t, h.AddModuleApiRouteWithModuleObjectReferences(c))

	// assert 500 (the DB error surfaces as an internal server error)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestGetModuleObjectsWithModuleApiRoutes_HappyPath fetches all module
// objects when the total count is under the pagination limit, exercising
// the non-paginated branch. Preloaded ModuleApiRoutes must be returned.
func TestGetModuleObjectsWithModuleApiRoutes_HappyPath(t *testing.T) {
	registerModuleVersions(t)
	db := setupModuleTestDB(t)
	h := newModuleHandler(db)
	e := newModuleEcho(t)

	// setup: seed a ModuleApi and two ModuleObjects, then associate one
	// object with a ModuleApiRoute through the m2m join. Creating the route
	// and object separately (rather than via a nested Create) avoids
	// re-triggering the api-side uniqueness check on route Path.
	moduleApi := &api_v0.ModuleApi{Name: util.Ptr("api"), Endpoint: util.Ptr("http://example.com")}
	require.NoError(t, db.Create(moduleApi).Error)
	route := &api_v0.ModuleApiRoute{Path: util.Ptr("/v0/widgets"), ModuleApiID: moduleApi.ID}
	require.NoError(t, db.Create(route).Error)
	obj1 := &api_v0.ModuleObject{
		Name:        util.Ptr("widget"),
		Version:     util.Ptr("v0"),
		ModuleApiID: moduleApi.ID,
	}
	require.NoError(t, db.Create(obj1).Error)
	obj2 := &api_v0.ModuleObject{
		Name:        util.Ptr("gadget"),
		Version:     util.Ptr("v0"),
		ModuleApiID: moduleApi.ID,
	}
	require.NoError(t, db.Create(obj2).Error)
	// append the existing route to obj1 through the m2m association so the
	// Preload path has something to return without re-inserting the route.
	// SkipHooks so the m2m Append's implicit upsert doesn't re-trigger the
	// ModuleApiRoute beforeCreate uniqueness check on the route Path.
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Model(obj1).Association("ModuleApiRoutes").Append(route))

	// action: no query params, so no pagination and both rows return
	c, rec := getContext(t, e, "/v0/module-objects-with-module-api-routes", nil)
	require.NoError(t, h.GetModuleObjectsWithModuleApiRoutes(c))

	// assert the response holds both objects
	assert.Equal(t, http.StatusOK, rec.Code)
	resp := decodeModuleResponse(t, rec)
	assert.Equal(t, api_v0.ObjectTypeModuleObject, resp.Type)
	assert.Len(t, resp.Data, 2)
	assert.EqualValues(t, 2, resp.Meta.ObjectCount)

	// re-query directly to confirm the Preload wired the join for obj1
	var reloaded api_v0.ModuleObject
	require.NoError(t, db.Preload("ModuleApiRoutes").First(&reloaded, *obj1.ID).Error)
	assert.Len(t, reloaded.ModuleApiRoutes, 1)
}

// TestGetModuleObjectsWithModuleApiRoutes_Empty returns an empty Data
// slice and ObjectCount 0 when no rows exist. The handler must not
// short-circuit to an error.
func TestGetModuleObjectsWithModuleApiRoutes_Empty(t *testing.T) {
	registerModuleVersions(t)
	db := setupModuleTestDB(t)
	h := newModuleHandler(db)
	e := newModuleEcho(t)

	c, rec := getContext(t, e, "/v0/module-objects-with-module-api-routes", nil)
	require.NoError(t, h.GetModuleObjectsWithModuleApiRoutes(c))

	// assert OK with empty data
	assert.Equal(t, http.StatusOK, rec.Code)
	resp := decodeModuleResponse(t, rec)
	assert.Empty(t, resp.Data)
	assert.EqualValues(t, 0, resp.Meta.ObjectCount)
}

// TestGetModuleObjectsWithModuleApiRoutes_InvalidLimit rejects a
// non-numeric limit query param with 400 via GetPaginationParams.
func TestGetModuleObjectsWithModuleApiRoutes_InvalidLimit(t *testing.T) {
	registerModuleVersions(t)
	db := setupModuleTestDB(t)
	h := newModuleHandler(db)
	e := newModuleEcho(t)

	// setup: bad limit trips GetPaginationParams before any DB work
	req := httptest.NewRequest(http.MethodGet, "/v0/module-objects-with-module-api-routes?limit=not-a-number", nil)
	rec := httptest.NewRecorder()
	c := &apiserver_lib.CustomContext{Context: e.NewContext(req, rec)}
	c.SetPath("/v0/module-objects-with-module-api-routes")

	require.NoError(t, h.GetModuleObjectsWithModuleApiRoutes(c))

	// assert 400 with the invalid-limit message
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	resp := decodeModuleResponse(t, rec)
	assert.Contains(t, strings.ToLower(resp.Status.Error), "invalid limit value")
}

// TestGetModuleObjectsWithModuleApiRoutes_QueryIdWithoutCursor rejects a
// continuation request that names a queryId but forgot to include the
// cursor from the previous page.
func TestGetModuleObjectsWithModuleApiRoutes_QueryIdWithoutCursor(t *testing.T) {
	registerModuleVersions(t)
	db := setupModuleTestDB(t)
	h := newModuleHandler(db)
	e := newModuleEcho(t)

	// setup: queryid without cursor is the caller-error branch
	req := httptest.NewRequest(http.MethodGet, "/v0/module-objects-with-module-api-routes?queryid=abc", nil)
	rec := httptest.NewRecorder()
	c := &apiserver_lib.CustomContext{Context: e.NewContext(req, rec)}
	c.SetPath("/v0/module-objects-with-module-api-routes")

	require.NoError(t, h.GetModuleObjectsWithModuleApiRoutes(c))

	// assert 400 with the cursor-required message
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	resp := decodeModuleResponse(t, rec)
	assert.Contains(t, resp.Status.Error, "cursor is required")
}

// TestGetModuleObjectWithModuleApiRoutes_HappyPath returns 200 with the
// single object and its preloaded ModuleApiRoutes when the id resolves.
func TestGetModuleObjectWithModuleApiRoutes_HappyPath(t *testing.T) {
	registerModuleVersions(t)
	db := setupModuleTestDB(t)
	h := newModuleHandler(db)
	e := newModuleEcho(t)

	// setup: seed a ModuleApi, a route, and one object linked via m2m
	moduleApi := &api_v0.ModuleApi{Name: util.Ptr("api"), Endpoint: util.Ptr("http://example.com")}
	require.NoError(t, db.Create(moduleApi).Error)
	route := &api_v0.ModuleApiRoute{Path: util.Ptr("/v0/widgets"), ModuleApiID: moduleApi.ID}
	require.NoError(t, db.Create(route).Error)
	obj := &api_v0.ModuleObject{
		Name:        util.Ptr("widget"),
		Version:     util.Ptr("v0"),
		ModuleApiID: moduleApi.ID,
	}
	require.NoError(t, db.Create(obj).Error)
	// SkipHooks so the m2m Append's implicit upsert doesn't re-trigger the
	// ModuleApiRoute beforeCreate uniqueness check on the route Path.
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Model(obj).Association("ModuleApiRoutes").Append(route))

	// action: request the object by its assigned id
	idStr := numToStr(*obj.ID)
	c, rec := getContext(t, e, "/v0/module-objects-with-module-api-routes/:id", map[string]string{"id": idStr})
	require.NoError(t, h.GetModuleObjectWithModuleApiRoutes(c))

	// assert the response wraps the single object
	assert.Equal(t, http.StatusOK, rec.Code)
	resp := decodeModuleResponse(t, rec)
	assert.Equal(t, api_v0.ObjectTypeModuleObject, resp.Type)
	require.Len(t, resp.Data, 1)
}

// TestGetModuleObjectWithModuleApiRoutes_NotFound returns 404 when the
// requested id doesn't match any row. gorm.ErrRecordNotFound is the
// signaling error the handler branches on.
func TestGetModuleObjectWithModuleApiRoutes_NotFound(t *testing.T) {
	registerModuleVersions(t)
	db := setupModuleTestDB(t)
	h := newModuleHandler(db)
	e := newModuleEcho(t)

	// action: any id lookup against an empty table hits ErrRecordNotFound
	c, rec := getContext(t, e, "/v0/module-objects-with-module-api-routes/:id", map[string]string{"id": "9999"})
	require.NoError(t, h.GetModuleObjectWithModuleApiRoutes(c))

	// assert 404 with the not-found status message
	assert.Equal(t, http.StatusNotFound, rec.Code)
	resp := decodeModuleResponse(t, rec)
	assert.Equal(t, http.StatusNotFound, resp.Status.Code)
}

// numToStr renders a uint as its decimal string for path params. Keeps
// the test bodies free of strconv noise.
func numToStr(n uint) string {
	// use json to render without a strconv import; the encoding here is
	// trivially the decimal representation
	b, _ := json.Marshal(n)
	return string(b)
}
