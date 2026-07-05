package v0

import (
	"strings"
	"testing"

	api_v0 "github.com/threeport/threeport/pkg/api/v0"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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
