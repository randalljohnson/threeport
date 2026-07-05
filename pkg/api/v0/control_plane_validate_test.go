package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupControlPlaneTestDB returns an in-memory sqlite db. The
// ControlPlaneDefinition and ControlPlaneInstance lifecycle hooks under test
// are stubs that return nil regardless of tx state, so no schema migration is
// needed; a live *gorm.DB is still passed to exercise the real hook signatures.
func setupControlPlaneTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

// TestControlPlaneDefinitionLifecycleHooks_ReturnNil asserts every
// ControlPlaneDefinition lifecycle hook returns nil for a zero-value receiver
// and a live transaction. These hooks are scaffold stubs; the contract they
// expose to callers is "never error".
func TestControlPlaneDefinitionLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupControlPlaneTestDB(t)
	cpd := &ControlPlaneDefinition{}

	// each case exercises one hook and asserts nil is returned
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return cpd.beforeCreate(db) }},
		{"beforeUpdate", func() error { return cpd.beforeUpdate(db) }},
		{"beforeDelete", func() error { return cpd.beforeDelete(db) }},
		{"afterCreate", func() error { return cpd.afterCreate(db) }},
		{"afterUpdate", func() error { return cpd.afterUpdate(db) }},
		{"afterDelete", func() error { return cpd.afterDelete(db) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must return nil under the standard live-tx call shape
			assert.NoError(t, tc.call())
		})
	}
}

// TestControlPlaneDefinitionLifecycleHooks_NilTx asserts the
// ControlPlaneDefinition lifecycle hooks tolerate a nil *gorm.DB. Since the
// stubs do not dereference tx, a nil transaction must not panic and must
// still return nil.
func TestControlPlaneDefinitionLifecycleHooks_NilTx(t *testing.T) {
	cpd := &ControlPlaneDefinition{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return cpd.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return cpd.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return cpd.beforeDelete(nil) }},
		{"afterCreate", func() error { return cpd.afterCreate(nil) }},
		{"afterUpdate", func() error { return cpd.afterUpdate(nil) }},
		{"afterDelete", func() error { return cpd.afterDelete(nil) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must not panic and must return nil on a nil tx
			assert.NotPanics(t, func() { _ = tc.call() })
			assert.NoError(t, tc.call())
		})
	}
}

// TestControlPlaneInstanceLifecycleHooks_ReturnNil asserts every
// ControlPlaneInstance lifecycle hook returns nil for a zero-value receiver
// and a live transaction. These hooks are scaffold stubs; the contract they
// expose to callers is "never error".
func TestControlPlaneInstanceLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupControlPlaneTestDB(t)
	cpi := &ControlPlaneInstance{}

	// each case exercises one hook and asserts nil is returned
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return cpi.beforeCreate(db) }},
		{"beforeUpdate", func() error { return cpi.beforeUpdate(db) }},
		{"beforeDelete", func() error { return cpi.beforeDelete(db) }},
		{"afterCreate", func() error { return cpi.afterCreate(db) }},
		{"afterUpdate", func() error { return cpi.afterUpdate(db) }},
		{"afterDelete", func() error { return cpi.afterDelete(db) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must return nil under the standard live-tx call shape
			assert.NoError(t, tc.call())
		})
	}
}

// TestControlPlaneInstanceLifecycleHooks_NilTx asserts the
// ControlPlaneInstance lifecycle hooks tolerate a nil *gorm.DB. Since the
// stubs do not dereference tx, a nil transaction must not panic and must
// still return nil.
func TestControlPlaneInstanceLifecycleHooks_NilTx(t *testing.T) {
	cpi := &ControlPlaneInstance{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return cpi.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return cpi.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return cpi.beforeDelete(nil) }},
		{"afterCreate", func() error { return cpi.afterCreate(nil) }},
		{"afterUpdate", func() error { return cpi.afterUpdate(nil) }},
		{"afterDelete", func() error { return cpi.afterDelete(nil) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must not panic and must return nil on a nil tx
			assert.NotPanics(t, func() { _ = tc.call() })
			assert.NoError(t, tc.call())
		})
	}
}
