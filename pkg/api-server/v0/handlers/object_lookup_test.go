package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// fakeModuleServer returns an httptest server that fakes the module
// CRUD endpoint. handler controls per-test responses; the server URL
// is split into the endpoint host and a captured path prefix so the
// caller can pass them into the lookup functions exactly as the
// real api-server does.
func fakeModuleServer(t *testing.T, handler http.HandlerFunc) (endpoint, host string, restore func()) {
	t.Helper()
	srv := httptest.NewServer(handler)

	// GetResponse prepends "http://" to whatever endpoint we pass, so
	// strip the scheme from the test-server URL to mirror the
	// production endpoint shape (bare DNS, no scheme).
	host = strings.TrimPrefix(srv.URL, "http://")

	// override the package's moduleHTTPClient so the test doesn't need
	// any special transport - a vanilla client speaks plain HTTP to the
	// httptest server.
	prev := moduleHTTPClient
	moduleHTTPClient = &http.Client{}

	restore = func() {
		moduleHTTPClient = prev
		srv.Close()
	}
	return host, host, restore
}

// writeJSONResponse marshals data into the apiserver_lib.Response
// envelope and writes it to w. Mirrors what a real module API server
// returns for list and by-id GETs.
func writeJSONResponse(t *testing.T, w http.ResponseWriter, data []apiserver_lib.Object) {
	t.Helper()
	body, err := json.Marshal(apiserver_lib.Response{Data: data})
	require.NoError(t, err)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// TestGetNamesFromModule_HappyPath drives one GET per id and folds
// each module response's Name field into the returned map.
func TestGetNamesFromModule_HappyPath(t *testing.T) {
	var gotURLs []string
	endpoint, _, restore := fakeModuleServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotURLs = append(gotURLs, r.URL.String())
		switch r.URL.Path {
		case "/example.com/v0/widgets/1":
			writeJSONResponse(t, w, []apiserver_lib.Object{map[string]interface{}{"Name": "widget-one"}})
		case "/example.com/v0/widgets/2":
			writeJSONResponse(t, w, []apiserver_lib.Object{map[string]interface{}{"Name": "widget-two"}})
		default:
			http.NotFound(w, r)
		}
	})
	defer restore()

	out, err := getNamesFromModule(endpoint, "/example.com/v0/widgets", []uint{1, 2}, false)
	require.NoError(t, err)
	assert.Equal(t, map[uint]string{1: "widget-one", 2: "widget-two"}, out)
	assert.Len(t, gotURLs, 2, "exactly one GET per id")
}

// TestGetNamesFromModule_IncludeDeleted appends the
// IncludeDeleted query param when the caller opts in. Soft-delete
// gating is otherwise transparent.
func TestGetNamesFromModule_IncludeDeleted(t *testing.T) {
	var gotQuery string
	endpoint, _, restore := fakeModuleServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		writeJSONResponse(t, w, []apiserver_lib.Object{map[string]interface{}{"Name": "widget-one"}})
	})
	defer restore()

	_, err := getNamesFromModule(endpoint, "/example.com/v0/widgets", []uint{1}, true)
	require.NoError(t, err)
	assert.Equal(t, apiserver_lib.QueryParamIncludeDeleted+"=true", gotQuery)
}

// TestGetNamesFromModule_PartialFailure skips ids whose module
// lookups fail or return empty payloads, returning a map with just
// the successful entries rather than failing the whole batch.
func TestGetNamesFromModule_PartialFailure(t *testing.T) {
	endpoint, _, restore := fakeModuleServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/example.com/v0/widgets/1":
			writeJSONResponse(t, w, []apiserver_lib.Object{map[string]interface{}{"Name": "widget-one"}})
		case "/example.com/v0/widgets/2":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "/example.com/v0/widgets/3":
			writeJSONResponse(t, w, []apiserver_lib.Object{})
		default:
			http.NotFound(w, r)
		}
	})
	defer restore()

	out, err := getNamesFromModule(endpoint, "/example.com/v0/widgets", []uint{1, 2, 3}, false)
	require.NoError(t, err)
	assert.Equal(t, map[uint]string{1: "widget-one"}, out, "only successful lookups appear")
}

// TestGetNamesFromModule_DropsEmptyName drops ids whose response
// has a non-string or empty Name. The caller renders id-only when
// the name isn't present.
func TestGetNamesFromModule_DropsEmptyName(t *testing.T) {
	endpoint, _, restore := fakeModuleServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/example.com/v0/widgets/1":
			writeJSONResponse(t, w, []apiserver_lib.Object{map[string]interface{}{"Name": ""}})
		case "/example.com/v0/widgets/2":
			writeJSONResponse(t, w, []apiserver_lib.Object{map[string]interface{}{"NotName": "x"}})
		default:
			http.NotFound(w, r)
		}
	})
	defer restore()

	out, err := getNamesFromModule(endpoint, "/example.com/v0/widgets", []uint{1, 2}, false)
	require.NoError(t, err)
	assert.Empty(t, out)
}

// TestGetIDsFromModuleByName_HappyPath issues a single name-filtered
// GET and pulls out every row's ID. The name lookup is the inverse
// of the id lookup and is used by `tptctl events --for foo=name`.
func TestGetIDsFromModuleByName_HappyPath(t *testing.T) {
	var gotURL string
	endpoint, _, restore := fakeModuleServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		writeJSONResponse(t, w, []apiserver_lib.Object{
			map[string]interface{}{"ID": float64(7), "Name": "widget-seven"},
			map[string]interface{}{"ID": float64(9), "Name": "widget-seven"},
		})
	})
	defer restore()

	ids, err := getIDsFromModuleByName(endpoint, "/example.com/v0/widgets", "example.com/v0.widget", "widget-seven")
	require.NoError(t, err)
	assert.Equal(t, []uint{7, 9}, ids, "every matching row's id is collected")
	assert.Equal(t, "/example.com/v0/widgets?name=widget-seven", gotURL)
}

// TestGetIDsFromModuleByName_Empty handles the empty-result case as a
// successful zero-length slice, not an error - "no rows" is a
// legitimate answer for `name=missing`.
func TestGetIDsFromModuleByName_Empty(t *testing.T) {
	endpoint, _, restore := fakeModuleServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(t, w, []apiserver_lib.Object{})
	})
	defer restore()

	ids, err := getIDsFromModuleByName(endpoint, "/example.com/v0/widgets", "example.com/v0.widget", "ghost")
	require.NoError(t, err)
	assert.Empty(t, ids)
}

// TestGetIDsFromModuleByName_ModuleError surfaces a real transport
// or non-200 error to the caller with the object type in the message
// so the api-server log line points at the right type.
func TestGetIDsFromModuleByName_ModuleError(t *testing.T) {
	endpoint, _, restore := fakeModuleServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	defer restore()

	_, err := getIDsFromModuleByName(endpoint, "/example.com/v0/widgets", "example.com/v0.widget", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "example.com/v0.widget", "object type appears in error")
}

// TestParseRowID exercises the ID-field shape coverage directly. The
// httptest tests above exercise the float64 path through GetResponse
// (which uses json.Unmarshal without UseNumber). This test covers the
// json.Number branch the resolver defends against in case a caller's
// decoder ever swaps to UseNumber, plus the unrecognized-shape fallback.
func TestParseRowID(t *testing.T) {
	cases := []struct {
		name           string
		input          interface{}
		wantID         uint
		wantRecognized bool
		wantErr        bool
	}{
		{name: "float64 (default decoder shape)", input: float64(42), wantID: 42, wantRecognized: true},
		{name: "json.Number numeric", input: json.Number("7"), wantID: 7, wantRecognized: true},
		{name: "json.Number non-integer surfaces parse error", input: json.Number("not-a-number"), wantRecognized: true, wantErr: true},
		{name: "string is unrecognized, no error", input: "42"},
		{name: "nil is unrecognized, no error", input: nil},
		{name: "bool is unrecognized, no error", input: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, recognized, err := parseRowID(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				assert.True(t, recognized, "json.Number stays recognized even on parse error")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantRecognized, recognized)
			assert.Equal(t, tc.wantID, id)
		})
	}
}

// setupObjectLookupDB migrates every table GetObjectIDsByName may
// consult: a core lookup type (ControlPlaneDefinition) plus the module
// registry tables (v0_module_apis, v0_module_api_routes,
// v0_module_objects) that GetModuleRouteForType joins over. The
// AttachedObjectReference table is present because the core type's
// afterCreate hooks reference it under a non-SkipHooks session; seeds
// below use SkipHooks so it can stay empty.
func setupObjectLookupDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newHandlersTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&api_v0.ControlPlaneDefinition{},
		&api_v0.AttachedObjectReference{},
		&api_v0.ModuleApi{},
		&api_v0.ModuleApiRoute{},
		&api_v0.ModuleObject{},
	))
	return db
}

// TestGetObjectIDsByName_CoreType covers the core-SQL branch: a
// registered core object type resolves via GetCoreObjectIDsByName
// without touching the module registry. Two rows with the same name are
// seeded so the multi-id return path is exercised - name uniqueness is
// not enforced at the database layer.
func TestGetObjectIDsByName_CoreType(t *testing.T) {
	// setup: DB with core + registry tables, three rows (two "shared",
	// one "other") on ControlPlaneDefinition
	db := setupObjectLookupDB(t)
	tx := db.Session(&gorm.Session{SkipHooks: true})
	for _, name := range []string{"shared", "shared", "other"} {
		require.NoError(t, tx.Create(&api_v0.ControlPlaneDefinition{
			Definition: api_v0.Definition{Name: util.Ptr(name)},
		}).Error)
	}

	// action: look up the shared name under the core-owned qualified type
	ids, err := GetObjectIDsByName(db, "threeport.io/v0.ControlPlaneDefinition", "shared")

	// assert: both matching ids come back; the "other" row is excluded
	require.NoError(t, err)
	assert.Len(t, ids, 2, "both rows named shared are returned")
}

// TestGetObjectIDsByName_UnknownTypeErrors covers the last-resort
// branch: a type unknown to core AND unowned by any registered module
// returns a hard error so the caller can't silently degrade a
// name-based lookup to no-op.
func TestGetObjectIDsByName_UnknownTypeErrors(t *testing.T) {
	// setup: registry tables present but empty, so GetModuleRouteForType
	// returns endpoint="" and the caller falls into the hard-error branch
	db := setupObjectLookupDB(t)

	// action: look up a type no one owns
	ids, err := GetObjectIDsByName(db, "example.com/v0.Widget", "any")

	// assert: nil result and an error message that names the missing owner
	require.Error(t, err)
	assert.Nil(t, ids)
	assert.Contains(t, err.Error(), "not owned by core or any registered module")
}

// TestGetObjectIDsByName_DispatchesToModule covers the module-owned
// branch: an unknown core type gets its owning module looked up in the
// registry, then a name-filtered list GET is dispatched to the module's
// CRUD endpoint. Returned IDs come from the module's response body.
func TestGetObjectIDsByName_DispatchesToModule(t *testing.T) {
	// setup: fake module server that returns two matching rows for the
	// name-filtered list request, capturing the URL so the test can pin
	// the CRUD path shape
	db := setupObjectLookupDB(t)
	var gotURL string
	endpoint, _, restore := fakeModuleServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		writeJSONResponse(t, w, []apiserver_lib.Object{
			map[string]interface{}{"ID": float64(3), "Name": "widget"},
			map[string]interface{}{"ID": float64(4), "Name": "widget"},
		})
	})
	defer restore()

	// seed a module registration for example.com/v0.Widget pointing at
	// the fake server. SkipHooks avoids tagged-field emitters unrelated
	// to the lookup under test.
	tx := db.Session(&gorm.Session{SkipHooks: true})
	moduleApi := &api_v0.ModuleApi{
		Name:         util.Ptr("example-com-api"),
		ApiNamespace: util.Ptr("example.com"),
		Endpoint:     util.Ptr(endpoint),
		Core:         util.Ptr(false),
	}
	require.NoError(t, tx.Create(moduleApi).Error)
	moduleObject := &api_v0.ModuleObject{
		Name:        util.Ptr("Widget"),
		Version:     util.Ptr("v0"),
		ModuleApiID: moduleApi.ID,
	}
	require.NoError(t, tx.Create(moduleObject).Error)
	crudRoute := &api_v0.ModuleApiRoute{
		Path:        util.Ptr("/example.com/v0/widgets"),
		ModuleApiID: moduleApi.ID,
	}
	require.NoError(t, tx.Create(crudRoute).Error)
	require.NoError(t, tx.Model(crudRoute).Association("ModuleObjects").Append(moduleObject))
	// register the /versions discovery route too so GetModuleRouteForType
	// exercises its CRUD-vs-versions filter
	versionsRoute := &api_v0.ModuleApiRoute{
		Path:        util.Ptr("/example.com/widgets/versions"),
		ModuleApiID: moduleApi.ID,
	}
	require.NoError(t, tx.Create(versionsRoute).Error)
	require.NoError(t, tx.Model(versionsRoute).Association("ModuleObjects").Append(moduleObject))

	// action: look up "widget" under a type owned by the registered module
	ids, err := GetObjectIDsByName(db, "example.com/v0.Widget", "widget")

	// assert: both ids come back and the fake server saw the name-filtered
	// CRUD path (not the /versions discovery path)
	require.NoError(t, err)
	assert.Equal(t, []uint{3, 4}, ids)
	assert.Equal(t, "/example.com/v0/widgets?name=widget", gotURL)
}

// TestGetObjectIDsByName_CoreLookupErrorSurfaces covers the
// non-"unknown core type" error path: when GetCoreObjectIDsByName
// returns an error other than ErrUnknownCoreType (e.g. the underlying
// SQL query fails), the error is surfaced verbatim without falling
// through to a module lookup.
func TestGetObjectIDsByName_CoreLookupErrorSurfaces(t *testing.T) {
	// setup: DB where only the registry tables are migrated. Looking up a
	// core type whose backing table is missing forces the core SQL query
	// to fail with a real error rather than ErrUnknownCoreType.
	db := newHandlersTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&api_v0.ModuleApi{},
		&api_v0.ModuleApiRoute{},
		&api_v0.ModuleObject{},
	))

	// action: look up a name under a core type whose table doesn't exist
	ids, err := GetObjectIDsByName(db, "threeport.io/v0.ControlPlaneDefinition", "any")

	// assert: the real DB error propagates (not silently swallowed as
	// unknown-type and rerouted through the module registry)
	require.Error(t, err)
	assert.Nil(t, ids)
	assert.Contains(t, err.Error(), "ControlPlaneDefinition")
}
