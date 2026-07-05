package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	util_v0 "github.com/threeport/threeport/pkg/util/v0"
)

// newModuleValidateDB returns an in-memory sqlite db with the module
// schemas migrated. The hooks under test interrogate the transaction with
// GORM queries, so a real live db is the most faithful setup.
// AttachedObjectReference is included because the generated
// ProcessCoreTaggedFields hooks fan out to it and expect its table to
// exist.
func newModuleValidateDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&ModuleApi{},
		&ModuleApiRoute{},
		&ModuleController{},
		&ModuleObject{},
		&AttachedObjectReference{},
	))
	return db
}

// seedDB returns a db session with all GORM hooks skipped so tests can
// insert setup rows without triggering the hooks under test.
func seedDB(db *gorm.DB) *gorm.DB {
	return db.Session(&gorm.Session{SkipHooks: true})
}

// newUnmigratedDB returns an in-memory sqlite db with no tables so the
// hooks under test hit a real GORM query error path.
func newUnmigratedDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

// uPtr returns a pointer to a uint literal for concise fixture setup.
func uPtr(v uint) *uint { return &v }

// sPtr returns a pointer to a string literal for concise fixture setup.
func sPtr(v string) *string { return &v }

// bPtr returns a pointer to a bool literal for concise fixture setup.
func bPtr(v bool) *bool { return &v }

// TestModuleApi_beforeCreate_ReturnsNil covers the no-op beforeCreate hook
// for ModuleApi: it must not reject any input.
func TestModuleApi_beforeCreate_ReturnsNil(t *testing.T) {
	// setup: fresh db, arbitrary ModuleApi
	db := newModuleValidateDB(t)
	m := &ModuleApi{Name: sPtr("api"), Endpoint: sPtr("svc:80")}

	// action: call the no-op hook directly
	err := m.beforeCreate(db)

	// assert: hook returns nil
	assert.NoError(t, err, "beforeCreate is a no-op and must return nil")
}

// TestModuleApi_beforeUpdate_ReturnsNil covers the no-op beforeUpdate hook
// for ModuleApi: it must not reject any input.
func TestModuleApi_beforeUpdate_ReturnsNil(t *testing.T) {
	// setup: fresh db, arbitrary ModuleApi
	db := newModuleValidateDB(t)
	m := &ModuleApi{Name: sPtr("api"), Endpoint: sPtr("svc:80")}

	// action: call the no-op hook directly
	err := m.beforeUpdate(db)

	// assert: hook returns nil
	assert.NoError(t, err, "beforeUpdate is a no-op and must return nil")
}

// TestModuleApi_beforeDelete covers the delete guard that blocks a
// ModuleApi delete when any ModuleApiRoute still references it. The three
// branches under test: the happy no-associated-routes path, the block
// path when routes exist, and the db-error path when the query fails.
func TestModuleApi_beforeDelete(t *testing.T) {
	t.Run("passes when no associated routes exist", func(t *testing.T) {
		// setup: create a ModuleApi with no routes
		db := newModuleValidateDB(t)
		api := &ModuleApi{Name: sPtr("api"), Endpoint: sPtr("svc:80"), Core: bPtr(true)}
		require.NoError(t, seedDB(db).Create(api).Error)

		// action: invoke the pre-delete guard
		err := api.beforeDelete(db)

		// assert: guard returns nil when no routes reference the api
		assert.NoError(t, err)
	})

	t.Run("blocks delete when associated routes exist", func(t *testing.T) {
		// setup: create a ModuleApi and one route that references it
		db := newModuleValidateDB(t)
		api := &ModuleApi{Name: sPtr("api"), Endpoint: sPtr("svc:80"), Core: bPtr(true)}
		require.NoError(t, seedDB(db).Create(api).Error)
		route := &ModuleApiRoute{Path: sPtr("/v0/things"), ModuleApiID: api.ID}
		require.NoError(t, seedDB(db).Create(route).Error)

		// action: invoke the pre-delete guard
		err := api.beforeDelete(db)

		// assert: guard rejects delete and names the id in the message
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be deleted")
		assert.Contains(t, err.Error(), "has associated routes")
	})

	t.Run("wraps the query error when lookup fails", func(t *testing.T) {
		// setup: db with no tables so the Find call fails
		db := newUnmigratedDB(t)
		api := &ModuleApi{Common: Common{ID: uPtr(42)}}

		// action: invoke the pre-delete guard against a broken db
		err := api.beforeDelete(db)

		// assert: guard surfaces the query error with the wrap prefix
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to retrieve routes")
	})
}

// TestModuleApi_afterHooks_ReturnNil covers the three no-op after-hooks
// on ModuleApi: they must not reject or mutate anything.
func TestModuleApi_afterHooks_ReturnNil(t *testing.T) {
	// setup: fresh db, arbitrary ModuleApi
	db := newModuleValidateDB(t)
	m := &ModuleApi{Name: sPtr("api"), Endpoint: sPtr("svc:80")}

	// assert: each no-op after-hook returns nil
	assert.NoError(t, m.afterCreate(db), "afterCreate is a no-op")
	assert.NoError(t, m.afterUpdate(db), "afterUpdate is a no-op")
	assert.NoError(t, m.afterDelete(db), "afterDelete is a no-op")
}

// TestModuleApiRoute_beforeCreate covers the uniqueness check that
// prevents two routes from sharing the same path. The three branches
// under test: no existing route (returns nil), an existing route on the
// path (returns a 409 conflict), and a db lookup failure (wrapped error).
func TestModuleApiRoute_beforeCreate(t *testing.T) {
	t.Run("passes when no existing route on the path", func(t *testing.T) {
		// setup: empty db, new route
		db := newModuleValidateDB(t)
		route := &ModuleApiRoute{Path: sPtr("/v0/first"), ModuleApiID: uPtr(1)}

		// action: invoke the uniqueness guard
		err := route.beforeCreate(db)

		// assert: guard permits an unused path
		assert.NoError(t, err)
	})

	t.Run("returns a 409 conflict when the path is already used", func(t *testing.T) {
		// setup: existing route occupies the target path
		db := newModuleValidateDB(t)
		existing := &ModuleApiRoute{Path: sPtr("/v0/taken"), ModuleApiID: uPtr(1)}
		require.NoError(t, seedDB(db).Create(existing).Error)
		route := &ModuleApiRoute{Path: sPtr("/v0/taken"), ModuleApiID: uPtr(2)}

		// action: invoke the uniqueness guard
		err := route.beforeCreate(db)

		// assert: guard returns a conflict typed error naming the path
		require.Error(t, err)
		httpErr, ok := err.(*util_v0.HttpError)
		require.True(t, ok, "expected *util_v0.HttpError, got %T", err)
		assert.Equal(t, 409, httpErr.StatusCode)
		assert.Contains(t, httpErr.Message, "/v0/taken")
	})

	t.Run("wraps the query error when lookup fails", func(t *testing.T) {
		// setup: db with no tables so the First call fails
		db := newUnmigratedDB(t)
		route := &ModuleApiRoute{Path: sPtr("/v0/x"), ModuleApiID: uPtr(1)}

		// action: invoke the uniqueness guard against a broken db
		err := route.beforeCreate(db)

		// assert: guard surfaces the query error with the wrap prefix
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to look up API routes")
	})
}

// TestModuleApiRoute_beforeUpdate_ReturnsNil covers the no-op
// beforeUpdate hook on ModuleApiRoute.
func TestModuleApiRoute_beforeUpdate_ReturnsNil(t *testing.T) {
	// setup: fresh db, arbitrary route
	db := newModuleValidateDB(t)
	r := &ModuleApiRoute{Path: sPtr("/v0/x"), ModuleApiID: uPtr(1)}

	// assert: hook returns nil
	assert.NoError(t, r.beforeUpdate(db))
}

// TestModuleApiRoute_beforeDelete_ReturnsNil covers the no-op
// beforeDelete hook on ModuleApiRoute.
func TestModuleApiRoute_beforeDelete_ReturnsNil(t *testing.T) {
	// setup: fresh db, arbitrary route
	db := newModuleValidateDB(t)
	r := &ModuleApiRoute{Path: sPtr("/v0/x"), ModuleApiID: uPtr(1)}

	// assert: hook returns nil
	assert.NoError(t, r.beforeDelete(db))
}

// TestModuleApiRoute_afterUpdate_ReturnsNil covers the no-op afterUpdate
// hook on ModuleApiRoute.
func TestModuleApiRoute_afterUpdate_ReturnsNil(t *testing.T) {
	// setup: fresh db, arbitrary route
	db := newModuleValidateDB(t)
	r := &ModuleApiRoute{Path: sPtr("/v0/x"), ModuleApiID: uPtr(1)}

	// assert: hook returns nil
	assert.NoError(t, r.afterUpdate(db))
}

// TestModuleApiRoute_afterCreate covers the after-create hook that
// registers a proxy route with the module router. The three branches
// under test: the parent api lookup fails (wrapped error), the parent is
// core (no route registered), and the parent is non-core (route
// registered).
func TestModuleApiRoute_afterCreate(t *testing.T) {
	t.Run("wraps error when parent module api lookup fails", func(t *testing.T) {
		// setup: parent api id points at nothing
		db := newModuleValidateDB(t)
		route := &ModuleApiRoute{Path: sPtr("/v0/orphan"), ModuleApiID: uPtr(999)}

		// action: invoke afterCreate with a missing parent
		err := route.afterCreate(db)

		// assert: hook surfaces the lookup error with the wrap prefix
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to retrieve module API")
	})

	t.Run("does not register a route when parent is core", func(t *testing.T) {
		// setup: reset router, create core parent, prepare route
		resetModRouter(t)
		db := newModuleValidateDB(t)
		api := &ModuleApi{Name: sPtr("core"), Endpoint: sPtr("svc:80"), Core: bPtr(true)}
		require.NoError(t, seedDB(db).Create(api).Error)
		route := &ModuleApiRoute{Path: sPtr("/v0/core-skip"), ModuleApiID: api.ID}

		// action: invoke afterCreate
		err := route.afterCreate(db)

		// assert: hook returns nil and no route was registered
		require.NoError(t, err)
		assert.False(t, modRouterHas("/v0/core-skip"), "core api routes must be skipped")
	})

	t.Run("registers a proxy route when parent is not core", func(t *testing.T) {
		// setup: reset router, create non-core parent, prepare route
		resetModRouter(t)
		db := newModuleValidateDB(t)
		api := &ModuleApi{Name: sPtr("mod"), Endpoint: sPtr("mod-svc:8080"), Core: bPtr(false)}
		require.NoError(t, seedDB(db).Create(api).Error)
		route := &ModuleApiRoute{Path: sPtr("/v0/module-route"), ModuleApiID: api.ID}

		// action: invoke afterCreate
		err := route.afterCreate(db)

		// assert: hook returns nil and the route is registered
		require.NoError(t, err)
		assert.True(t, modRouterHas("/v0/module-route"), "non-core route must be added to router")
	})
}

// TestModuleApiRoute_afterDelete removes the route from the module
// router. The two branches under test: removing an existing route leaves
// the router empty, and removing an unknown route is a no-op.
func TestModuleApiRoute_afterDelete(t *testing.T) {
	t.Run("removes a previously registered route", func(t *testing.T) {
		// setup: pre-populate the router with a route
		resetModRouter(t)
		db := newModuleValidateDB(t)
		ModRouter.AddRoute("/v0/gone", nil)
		require.True(t, modRouterHas("/v0/gone"))
		route := &ModuleApiRoute{Path: sPtr("/v0/gone")}

		// action: invoke afterDelete
		err := route.afterDelete(db)

		// assert: hook returns nil and the route is removed
		require.NoError(t, err)
		assert.False(t, modRouterHas("/v0/gone"), "afterDelete must remove the route")
	})

	t.Run("no-op on unknown route", func(t *testing.T) {
		// setup: empty router
		resetModRouter(t)
		db := newModuleValidateDB(t)
		route := &ModuleApiRoute{Path: sPtr("/v0/never-registered")}

		// action: invoke afterDelete against a router that never had the path
		err := route.afterDelete(db)

		// assert: hook returns nil and router remains empty
		require.NoError(t, err)
		assert.False(t, modRouterHas("/v0/never-registered"))
	})
}

// TestModuleController_hooks_ReturnNil covers the six no-op hooks on
// ModuleController: all must return nil.
func TestModuleController_hooks_ReturnNil(t *testing.T) {
	// setup: fresh db, arbitrary controller
	db := newModuleValidateDB(t)
	c := &ModuleController{Name: sPtr("c"), DeploymentName: sPtr("c-deploy"), ModuleApiID: uPtr(1)}

	// assert: every no-op hook returns nil
	assert.NoError(t, c.beforeCreate(db))
	assert.NoError(t, c.beforeUpdate(db))
	assert.NoError(t, c.beforeDelete(db))
	assert.NoError(t, c.afterCreate(db))
	assert.NoError(t, c.afterUpdate(db))
	assert.NoError(t, c.afterDelete(db))
}

// TestModuleObject_beforeCreate covers the uniqueness check that
// prevents two ModuleObjects sharing (name, module_api_id). The three
// branches under test: no existing object (returns nil), an existing
// object collides on the pair (returns a 409 conflict), and a db lookup
// failure (wrapped error).
func TestModuleObject_beforeCreate(t *testing.T) {
	t.Run("passes when no existing object shares (name, module_api_id)", func(t *testing.T) {
		// setup: empty db, new object
		db := newModuleValidateDB(t)
		obj := &ModuleObject{Name: sPtr("Widget"), Version: sPtr("v0"), ModuleApiID: uPtr(1)}

		// action: invoke the uniqueness guard
		err := obj.beforeCreate(db)

		// assert: guard permits an unused (name, api) pair
		assert.NoError(t, err)
	})

	t.Run("passes when same name lives under a different module api", func(t *testing.T) {
		// setup: existing object with same name but a different api id
		db := newModuleValidateDB(t)
		existing := &ModuleObject{Name: sPtr("Widget"), Version: sPtr("v0"), ModuleApiID: uPtr(1)}
		require.NoError(t, seedDB(db).Create(existing).Error)
		obj := &ModuleObject{Name: sPtr("Widget"), Version: sPtr("v0"), ModuleApiID: uPtr(2)}

		// action: invoke the uniqueness guard
		err := obj.beforeCreate(db)

		// assert: uniqueness is scoped to the (name, module_api_id) pair
		assert.NoError(t, err)
	})

	t.Run("returns a 409 conflict when the pair already exists", func(t *testing.T) {
		// setup: existing object occupies (name, api)
		db := newModuleValidateDB(t)
		existing := &ModuleObject{Name: sPtr("Widget"), Version: sPtr("v0"), ModuleApiID: uPtr(1)}
		require.NoError(t, seedDB(db).Create(existing).Error)
		obj := &ModuleObject{Name: sPtr("Widget"), Version: sPtr("v0"), ModuleApiID: uPtr(1)}

		// action: invoke the uniqueness guard
		err := obj.beforeCreate(db)

		// assert: guard returns a conflict typed error naming the pair
		require.Error(t, err)
		httpErr, ok := err.(*util_v0.HttpError)
		require.True(t, ok, "expected *util_v0.HttpError, got %T", err)
		assert.Equal(t, 409, httpErr.StatusCode)
		assert.Contains(t, httpErr.Message, "Widget")
		assert.Contains(t, httpErr.Message, "1")
	})

	t.Run("wraps the query error when lookup fails", func(t *testing.T) {
		// setup: db with no tables so the First call fails
		db := newUnmigratedDB(t)
		obj := &ModuleObject{Name: sPtr("Widget"), Version: sPtr("v0"), ModuleApiID: uPtr(1)}

		// action: invoke the uniqueness guard against a broken db
		err := obj.beforeCreate(db)

		// assert: guard surfaces the query error with the wrap prefix
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to look up API objects")
	})
}

// TestModuleObject_otherHooks_ReturnNil covers the five no-op hooks on
// ModuleObject: they must return nil.
func TestModuleObject_otherHooks_ReturnNil(t *testing.T) {
	// setup: fresh db, arbitrary object
	db := newModuleValidateDB(t)
	o := &ModuleObject{Name: sPtr("o"), Version: sPtr("v0"), ModuleApiID: uPtr(1)}

	// assert: every remaining no-op hook returns nil
	assert.NoError(t, o.beforeUpdate(db))
	assert.NoError(t, o.beforeDelete(db))
	assert.NoError(t, o.afterCreate(db))
	assert.NoError(t, o.afterUpdate(db))
	assert.NoError(t, o.afterDelete(db))
}

// modRouterHas reports whether the given path is currently registered on
// the package-global ModRouter. resetModRouter is defined in
// module_router_test.go and shared across the router-touching tests.
func modRouterHas(path string) bool {
	_, ok := ModRouter.routes.Load(path)
	return ok
}

