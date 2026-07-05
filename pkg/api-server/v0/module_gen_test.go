package v0

import (
	"strings"
	"testing"

	api_v0 "github.com/threeport/threeport/pkg/api/v0"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openTestDB opens an in-memory sqlite database with the gorm logger silenced
// and migrates every model that upsertModuleControllersObjectsRoutes touches
// directly or through the relationship hook, so a full RegisterModule call can
// run against it.
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&api_v0.ModuleApi{},
		&api_v0.ModuleController{},
		&api_v0.ModuleObject{},
		&api_v0.ModuleApiRoute{},
		&api_v0.AttachedObjectReference{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// TestRegisterModuleRejectsMissingApiEndpoint asserts RegisterModule returns
// an error naming THREEPORT_API_ENDPOINT when the env var is unset, before
// touching the database.
func TestRegisterModuleRejectsMissingApiEndpoint(t *testing.T) {
	// force the endpoint env var to empty so the guard fires
	t.Setenv("THREEPORT_API_ENDPOINT", "")

	// invoke the entry point with a nil db to confirm the env-var guard runs
	// first and no gorm call is attempted
	err := RegisterModule(nil)

	// verify a non-nil error is returned
	if err == nil {
		t.Fatal("expected error for missing THREEPORT_API_ENDPOINT, got nil")
	}
	// verify the error names the missing env var so operators can act on it
	if !strings.Contains(err.Error(), "THREEPORT_API_ENDPOINT") {
		t.Errorf("error does not mention THREEPORT_API_ENDPOINT: %v", err)
	}
}

// TestUpsertModuleApiRejectsMissingEndpoint asserts upsertModuleApi returns
// an error with a nil module and no database access when the endpoint env var
// is unset.
func TestUpsertModuleApiRejectsMissingEndpoint(t *testing.T) {
	// force the endpoint env var to empty so the guard fires
	t.Setenv("THREEPORT_API_ENDPOINT", "")

	// invoke the helper with a nil db; the env-var guard runs before any gorm
	// call so nil is safe here
	got, err := upsertModuleApi(nil)

	// verify a non-nil error is returned
	if err == nil {
		t.Fatal("expected error for missing THREEPORT_API_ENDPOINT, got nil")
	}
	// verify the module pointer is nil when the guard fires
	if got != nil {
		t.Errorf("expected nil ModuleApi on error, got %+v", got)
	}
	// verify the error names the missing env var
	if !strings.Contains(err.Error(), "THREEPORT_API_ENDPOINT") {
		t.Errorf("error does not mention THREEPORT_API_ENDPOINT: %v", err)
	}
}

// TestUpsertModuleApiCreatesCoreModuleApiRecord asserts upsertModuleApi writes
// a ModuleApi row populated with the endpoint from the env, the core namespace
// constant, the core module name, and Core=true.
func TestUpsertModuleApiCreatesCoreModuleApiRecord(t *testing.T) {
	// setup: seed the endpoint env with a recognizable value
	endpoint := "https://threeport.example:1323"
	t.Setenv("THREEPORT_API_ENDPOINT", endpoint)

	// setup: open an in-memory sqlite database and migrate the ModuleApi table
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&api_v0.ModuleApi{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// action: invoke the helper against the empty database
	got, err := upsertModuleApi(db)
	if err != nil {
		t.Fatalf("upsertModuleApi: %v", err)
	}
	if got == nil {
		t.Fatal("expected ModuleApi pointer, got nil")
	}

	// verify the endpoint field reflects the env-var value
	if got.Endpoint == nil || *got.Endpoint != endpoint {
		t.Errorf("endpoint mismatch: got %v want %q", got.Endpoint, endpoint)
	}
	// verify Core is true so the record represents the core threeport api
	if got.Core == nil || !*got.Core {
		t.Errorf("expected Core=true, got %v", got.Core)
	}
	// verify Name is populated so the (Name, ApiNamespace) unique index has values
	if got.Name == nil || *got.Name == "" {
		t.Errorf("expected Name populated, got %v", got.Name)
	}
	// verify ApiNamespace is populated so the unique index pair is complete
	if got.ApiNamespace == nil || *got.ApiNamespace == "" {
		t.Errorf("expected ApiNamespace populated, got %v", got.ApiNamespace)
	}
	// verify the row was persisted with a primary key assigned
	if got.ID == nil || *got.ID == 0 {
		t.Errorf("expected non-zero ID after upsert, got %v", got.ID)
	}
}

// TestUpsertModuleApiIsIdempotent asserts a second upsertModuleApi call
// returns the existing row rather than inserting a duplicate, preserving the
// (Name, ApiNamespace) unique index invariant across restarts.
func TestUpsertModuleApiIsIdempotent(t *testing.T) {
	// setup: seed the endpoint env
	t.Setenv("THREEPORT_API_ENDPOINT", "https://threeport.example:1323")

	// setup: open an in-memory sqlite database and migrate the ModuleApi table
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&api_v0.ModuleApi{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// action: invoke twice against the same database
	first, err := upsertModuleApi(db)
	if err != nil {
		t.Fatalf("first upsertModuleApi: %v", err)
	}
	second, err := upsertModuleApi(db)
	if err != nil {
		t.Fatalf("second upsertModuleApi: %v", err)
	}

	// verify both calls returned the same primary key so the second call was
	// a lookup, not an insert
	if first.ID == nil || second.ID == nil || *first.ID != *second.ID {
		t.Errorf("upsert not idempotent: first=%v second=%v", first.ID, second.ID)
	}

	// verify the underlying table still contains a single row
	var count int64
	if err := db.Model(&api_v0.ModuleApi{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 ModuleApi row after two upserts, got %d", count)
	}
}

// TestUpsertModuleControllersObjectsRoutesRejectsMissingNamespace asserts the
// helper returns an error naming THREEPORT_CONTROL_PLANE_NAMESPACE when the
// env var is unset, before touching the database.
func TestUpsertModuleControllersObjectsRoutesRejectsMissingNamespace(t *testing.T) {
	// force the namespace env var to empty so the guard fires
	t.Setenv("THREEPORT_CONTROL_PLANE_NAMESPACE", "")

	// invoke the helper with nil arguments; the env-var guard runs before any
	// db or module dereference so nil is safe here
	err := upsertModuleControllersObjectsRoutes(nil, nil)

	// verify a non-nil error is returned
	if err == nil {
		t.Fatal("expected error for missing THREEPORT_CONTROL_PLANE_NAMESPACE, got nil")
	}
	// verify the error names the missing env var so operators can act on it
	if !strings.Contains(err.Error(), "THREEPORT_CONTROL_PLANE_NAMESPACE") {
		t.Errorf("error does not mention THREEPORT_CONTROL_PLANE_NAMESPACE: %v", err)
	}
}

// TestRegisterModuleRejectsMissingNamespace asserts RegisterModule returns an
// error naming THREEPORT_CONTROL_PLANE_NAMESPACE when the namespace env var is
// unset but the endpoint env var is set, so the second-stage guard fires after
// the module API upsert succeeds.
func TestRegisterModuleRejectsMissingNamespace(t *testing.T) {
	// setup: seed the endpoint env so upsertModuleApi succeeds
	t.Setenv("THREEPORT_API_ENDPOINT", "https://threeport.example:1323")
	// setup: clear the namespace env so the second-stage guard fires
	t.Setenv("THREEPORT_CONTROL_PLANE_NAMESPACE", "")

	// setup: open a database migrated for the ModuleApi upsert step
	db := openTestDB(t)

	// action: RegisterModule runs upsertModuleApi first, then hits the
	// namespace guard inside upsertModuleControllersObjectsRoutes
	err := RegisterModule(db)

	// verify the second-stage guard surfaces a non-nil error
	if err == nil {
		t.Fatal("expected error for missing THREEPORT_CONTROL_PLANE_NAMESPACE, got nil")
	}
	// verify the error names the missing env var so operators can act on it
	if !strings.Contains(err.Error(), "THREEPORT_CONTROL_PLANE_NAMESPACE") {
		t.Errorf("error does not mention THREEPORT_CONTROL_PLANE_NAMESPACE: %v", err)
	}
}

// TestRegisterModulePopulatesControllersObjectsRoutes exercises RegisterModule
// end-to-end against an in-memory database and asserts the module API,
// controllers, objects, and routes are all persisted. This drives the full
// upsertModuleControllersObjectsRoutes body which is otherwise unreachable
// without a running database.
func TestRegisterModulePopulatesControllersObjectsRoutes(t *testing.T) {
	// setup: seed both env vars so neither guard fires
	t.Setenv("THREEPORT_API_ENDPOINT", "https://threeport.example:1323")
	t.Setenv("THREEPORT_CONTROL_PLANE_NAMESPACE", "threeport-control-plane")

	// setup: open a database migrated for every model the registration touches
	db := openTestDB(t)

	// action: run the full registration flow
	if err := RegisterModule(db); err != nil {
		t.Fatalf("RegisterModule: %v", err)
	}

	// verify exactly one ModuleApi row exists so the endpoint upsert succeeded
	var moduleApis int64
	if err := db.Model(&api_v0.ModuleApi{}).Count(&moduleApis).Error; err != nil {
		t.Fatalf("count module apis: %v", err)
	}
	if moduleApis != 1 {
		t.Errorf("expected 1 ModuleApi row, got %d", moduleApis)
	}

	// verify multiple controllers were registered so the controller upsert loop ran
	var controllers int64
	if err := db.Model(&api_v0.ModuleController{}).Count(&controllers).Error; err != nil {
		t.Fatalf("count controllers: %v", err)
	}
	if controllers < 2 {
		t.Errorf("expected multiple ModuleController rows, got %d", controllers)
	}

	// verify multiple objects were registered so the object upsert loop ran
	var objects int64
	if err := db.Model(&api_v0.ModuleObject{}).Count(&objects).Error; err != nil {
		t.Fatalf("count objects: %v", err)
	}
	if objects < 2 {
		t.Errorf("expected multiple ModuleObject rows, got %d", objects)
	}

	// verify multiple routes were registered so the route upsert loop ran
	var routes int64
	if err := db.Model(&api_v0.ModuleApiRoute{}).Count(&routes).Error; err != nil {
		t.Fatalf("count routes: %v", err)
	}
	if routes < 2 {
		t.Errorf("expected multiple ModuleApiRoute rows, got %d", routes)
	}

	// verify controller deployment names carry the namespace prefix so the env
	// var was threaded through to the DeploymentName field
	var sample api_v0.ModuleController
	if err := db.First(&sample).Error; err != nil {
		t.Fatalf("read controller: %v", err)
	}
	if sample.DeploymentName == nil || !strings.HasPrefix(*sample.DeploymentName, "threeport-control-plane/") {
		t.Errorf("expected DeploymentName to be namespace-prefixed, got %v", sample.DeploymentName)
	}
}

// TestRegisterModuleIsIdempotent asserts running RegisterModule twice against
// the same database preserves the row counts, exercising the FirstOrCreate
// lookup branch across every controller, object, and route upsert.
func TestRegisterModuleIsIdempotent(t *testing.T) {
	// setup: seed both env vars so neither guard fires
	t.Setenv("THREEPORT_API_ENDPOINT", "https://threeport.example:1323")
	t.Setenv("THREEPORT_CONTROL_PLANE_NAMESPACE", "threeport-control-plane")

	// setup: open a database migrated for every model the registration touches
	db := openTestDB(t)

	// action: first run populates the tables
	if err := RegisterModule(db); err != nil {
		t.Fatalf("first RegisterModule: %v", err)
	}

	// capture per-model row counts after the first run so the second-run
	// counts can be compared
	countRows := func(model interface{}) int64 {
		var n int64
		if err := db.Model(model).Count(&n).Error; err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	apis1 := countRows(&api_v0.ModuleApi{})
	controllers1 := countRows(&api_v0.ModuleController{})
	objects1 := countRows(&api_v0.ModuleObject{})
	routes1 := countRows(&api_v0.ModuleApiRoute{})

	// action: second run should short-circuit every FirstOrCreate to a lookup
	if err := RegisterModule(db); err != nil {
		t.Fatalf("second RegisterModule: %v", err)
	}

	// verify no new rows were inserted, so every upsert took the lookup branch
	if got := countRows(&api_v0.ModuleApi{}); got != apis1 {
		t.Errorf("ModuleApi count changed: before=%d after=%d", apis1, got)
	}
	if got := countRows(&api_v0.ModuleController{}); got != controllers1 {
		t.Errorf("ModuleController count changed: before=%d after=%d", controllers1, got)
	}
	if got := countRows(&api_v0.ModuleObject{}); got != objects1 {
		t.Errorf("ModuleObject count changed: before=%d after=%d", objects1, got)
	}
	if got := countRows(&api_v0.ModuleApiRoute{}); got != routes1 {
		t.Errorf("ModuleApiRoute count changed: before=%d after=%d", routes1, got)
	}
}
