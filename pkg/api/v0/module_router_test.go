package v0

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// resetModRouter clears the process-global ModRouter so tests do not observe
// leaked entries from prior test runs or from other tests in this package.
func resetModRouter(t *testing.T) {
	t.Helper()
	ModRouter.routes.Range(func(k, _ interface{}) bool {
		ModRouter.routes.Delete(k)
		return true
	})
}

// newTestEchoContext builds an echo.Context whose request URL path is set to
// requestPath so ServeModuleRoutes can dispatch against it.
func newTestEchoContext(requestPath string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodGet, requestPath, nil)
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)
	return c, rec
}

// TestModuleRouter_AddAndRemoveRoute covers that AddRoute() stores a handler
// under the given path and RemoveRoute() deletes it.
func TestModuleRouter_AddAndRemoveRoute(t *testing.T) {
	// start with an isolated router so tests do not touch the global
	r := &ModuleRouter{routes: sync.Map{}}

	// action under test: register a handler for a path
	handler := func(c echo.Context) error { return nil }
	r.AddRoute("/v0/example", handler)

	// verify the handler is present in the underlying map
	got, ok := r.routes.Load("/v0/example")
	if !ok {
		t.Fatalf("AddRoute did not persist the path")
	}
	if got == nil {
		t.Fatalf("AddRoute stored nil handler")
	}

	// action under test: remove the handler
	r.RemoveRoute("/v0/example")

	// verify the handler was deleted
	if _, ok := r.routes.Load("/v0/example"); ok {
		t.Fatalf("RemoveRoute did not delete the path")
	}
}

// TestModuleRouter_RemoveRouteMissing covers that RemoveRoute() is a no-op
// when the path was never registered.
func TestModuleRouter_RemoveRouteMissing(t *testing.T) {
	// setup: empty router
	r := &ModuleRouter{routes: sync.Map{}}

	// action: remove a path that was never added; should not panic
	r.RemoveRoute("/v0/never-added")

	// assertion: map remains empty
	count := 0
	r.routes.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	if count != 0 {
		t.Fatalf("expected empty routes map, got %d entries", count)
	}
}

// TestModuleRouter_AddRouteOverwrites covers that AddRoute() replaces the
// handler for a path when called twice with the same path.
func TestModuleRouter_AddRouteOverwrites(t *testing.T) {
	// setup: register a first handler that marks a flag
	r := &ModuleRouter{routes: sync.Map{}}
	firstCalled := false
	secondCalled := false
	r.AddRoute("/v0/dup", func(c echo.Context) error {
		firstCalled = true
		return nil
	})

	// action: register a second handler at the same path
	r.AddRoute("/v0/dup", func(c echo.Context) error {
		secondCalled = true
		return nil
	})

	// verify only the second handler is stored
	got, _ := r.routes.Load("/v0/dup")
	if got == nil {
		t.Fatalf("handler missing after overwrite")
	}
	c, _ := newTestEchoContext("/v0/dup")
	if err := got.(echo.HandlerFunc)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if firstCalled {
		t.Fatalf("original handler was called after overwrite")
	}
	if !secondCalled {
		t.Fatalf("replacement handler was not called after overwrite")
	}
}

// TestServeModuleRoutes_DispatchAndFallthrough covers both dispatch behaviors:
// a request matching a registered path runs the matched handler, and a
// request not matching any registered path falls through to next.
func TestServeModuleRoutes_DispatchAndFallthrough(t *testing.T) {
	tests := []struct {
		name           string
		registered     string
		requested      string
		wantMatched    bool
		wantNextCalled bool
	}{
		{
			name:           "exact match dispatches handler",
			registered:     "/v0/widgets",
			requested:      "/v0/widgets",
			wantMatched:    true,
			wantNextCalled: false,
		},
		{
			name:           "prefix match dispatches handler",
			registered:     "/v0/widgets",
			requested:      "/v0/widgets/42",
			wantMatched:    true,
			wantNextCalled: false,
		},
		{
			name:           "unrelated path falls through to next",
			registered:     "/v0/widgets",
			requested:      "/v0/gadgets",
			wantMatched:    false,
			wantNextCalled: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// setup: build an isolated router and register the test's path
			r := &ModuleRouter{routes: sync.Map{}}
			matched := false
			r.AddRoute(tc.registered, func(c echo.Context) error {
				matched = true
				return nil
			})

			// setup: capture whether the fallthrough next handler is invoked
			nextCalled := false
			next := func(c echo.Context) error {
				nextCalled = true
				return nil
			}

			// action: invoke the middleware against a request for the test path
			c, _ := newTestEchoContext(tc.requested)
			if err := r.ServeModuleRoutes(next)(c); err != nil {
				t.Fatalf("ServeModuleRoutes returned error: %v", err)
			}

			// verify dispatch went where expected
			if matched != tc.wantMatched {
				t.Fatalf("matched handler called=%v want=%v", matched, tc.wantMatched)
			}
			if nextCalled != tc.wantNextCalled {
				t.Fatalf("next handler called=%v want=%v", nextCalled, tc.wantNextCalled)
			}
		})
	}
}

// TestServeModuleRoutes_PropagatesHandlerError covers that an error returned
// by the matched handler is propagated to the caller.
func TestServeModuleRoutes_PropagatesHandlerError(t *testing.T) {
	// setup: register a handler that always errors
	r := &ModuleRouter{routes: sync.Map{}}
	sentinel := errors.New("handler failure")
	r.AddRoute("/v0/thing", func(c echo.Context) error { return sentinel })

	// action: invoke the middleware against the registered path
	c, _ := newTestEchoContext("/v0/thing")
	err := r.ServeModuleRoutes(func(c echo.Context) error { return nil })(c)

	// verify the handler's error surfaces to the caller
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

// TestServeModuleRoutes_NoRoutesRegistered covers that with an empty router
// every request falls through to next.
func TestServeModuleRoutes_NoRoutesRegistered(t *testing.T) {
	// setup: empty router
	r := &ModuleRouter{routes: sync.Map{}}

	// setup: track whether next was invoked
	nextCalled := false
	next := func(c echo.Context) error {
		nextCalled = true
		return nil
	}

	// action: call the middleware
	c, _ := newTestEchoContext("/anything")
	if err := r.ServeModuleRoutes(next)(c); err != nil {
		t.Fatalf("ServeModuleRoutes returned error: %v", err)
	}

	// verify the request fell through
	if !nextCalled {
		t.Fatalf("expected fallthrough to next when no routes are registered")
	}
}

// TestMatchRoute covers the segment-prefix matching semantics used by
// ServeModuleRoutes: registered path segments must match the leading segments
// of the requested path exactly, and extra request segments are ignored.
func TestMatchRoute(t *testing.T) {
	tests := []struct {
		name       string
		registered string
		requested  string
		want       bool
	}{
		{
			name:       "identical paths match",
			registered: "/v0/widgets",
			requested:  "/v0/widgets",
			want:       true,
		},
		{
			name:       "request with trailing id matches",
			registered: "/v0/widgets",
			requested:  "/v0/widgets/42",
			want:       true,
		},
		{
			name:       "request with subpath matches",
			registered: "/v0/widgets",
			requested:  "/v0/widgets/42/sub",
			want:       true,
		},
		{
			name:       "mismatched final segment does not match",
			registered: "/v0/widgets",
			requested:  "/v0/gadgets",
			want:       false,
		},
		{
			name:       "mismatched prefix does not match",
			registered: "/v0/widgets",
			requested:  "/v1/widgets",
			want:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// action: compare the registered and requested paths
			got := matchRoute(tc.registered, tc.requested)

			// verify the boolean result
			if got != tc.want {
				t.Fatalf("matchRoute(%q, %q) = %v want %v",
					tc.registered, tc.requested, got, tc.want)
			}
		})
	}
}

// TestInitModuleRouter_LoadsNonCoreRoutes covers that InitModuleRouter reads
// non-core ModuleApi rows from the database, registers each associated route
// path against the shared ModRouter, and installs itself as middleware on the
// echo instance.
func TestInitModuleRouter_LoadsNonCoreRoutes(t *testing.T) {
	// setup: start with a clean global router and restore it after the test
	resetModRouter(t)
	t.Cleanup(func() { resetModRouter(t) })

	// setup: open an in-memory database with the module tables
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&ModuleApi{}, &ModuleApiRoute{}, &AttachedObjectReference{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// setup: seed one non-core module API with two routes and one core row
	// that should be skipped by the WHERE core = false clause
	name := "widgets-api"
	endpoint := "widgets.example:8080"
	core := false
	nonCore := ModuleApi{
		Name:     &name,
		Core:     &core,
		Endpoint: &endpoint,
	}
	if err := db.Create(&nonCore).Error; err != nil {
		t.Fatalf("create module api: %v", err)
	}
	p1 := "/v0/widgets"
	p2 := "/v0/gizmos"
	if err := db.Create(&ModuleApiRoute{Path: &p1, ModuleApiID: nonCore.ID}).Error; err != nil {
		t.Fatalf("create route 1: %v", err)
	}
	if err := db.Create(&ModuleApiRoute{Path: &p2, ModuleApiID: nonCore.ID}).Error; err != nil {
		t.Fatalf("create route 2: %v", err)
	}
	coreName := "core-api"
	coreEndpoint := "core.example:8080"
	coreFlag := true
	coreApi := ModuleApi{
		Name:     &coreName,
		Core:     &coreFlag,
		Endpoint: &coreEndpoint,
	}
	if err := db.Create(&coreApi).Error; err != nil {
		t.Fatalf("create core api: %v", err)
	}
	corePath := "/v0/core-only"
	if err := db.Create(&ModuleApiRoute{Path: &corePath, ModuleApiID: coreApi.ID}).Error; err != nil {
		t.Fatalf("create core route: %v", err)
	}

	// action: initialize the module router against the seeded database
	e := echo.New()
	if err := InitModuleRouter(db, e); err != nil {
		t.Fatalf("InitModuleRouter: %v", err)
	}

	// verify each non-core route was registered in the global ModRouter
	for _, want := range []string{p1, p2} {
		if _, ok := ModRouter.routes.Load(want); !ok {
			t.Fatalf("expected route %q registered, missing", want)
		}
	}

	// verify the core-only route was skipped by the non-core filter
	if _, ok := ModRouter.routes.Load(corePath); ok {
		t.Fatalf("core route %q should not have been registered", corePath)
	}
}

// TestInitModuleRouter_EmptyDatabase covers the boundary case where no module
// APIs are present: InitModuleRouter returns nil and leaves the router empty.
func TestInitModuleRouter_EmptyDatabase(t *testing.T) {
	// setup: clean global router
	resetModRouter(t)
	t.Cleanup(func() { resetModRouter(t) })

	// setup: empty in-memory database with the module schema
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&ModuleApi{}, &ModuleApiRoute{}, &AttachedObjectReference{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// action: initialize against the empty database
	e := echo.New()
	if err := InitModuleRouter(db, e); err != nil {
		t.Fatalf("InitModuleRouter on empty db: %v", err)
	}

	// verify no routes were registered
	count := 0
	ModRouter.routes.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	if count != 0 {
		t.Fatalf("expected empty router, got %d routes", count)
	}
}

// TestInitModuleRouter_QueryError covers that InitModuleRouter wraps and
// returns a database error when the ModuleApi query fails (e.g. the schema is
// not migrated).
func TestInitModuleRouter_QueryError(t *testing.T) {
	// setup: database with no tables migrated so the query fails
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	// action: initialize against a database without the module schema
	e := echo.New()
	err = InitModuleRouter(db, e)

	// verify the error is surfaced
	if err == nil {
		t.Fatalf("expected error from missing schema, got nil")
	}
}
