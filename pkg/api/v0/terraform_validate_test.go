package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTerraformTestDB returns an in-memory sqlite db. The TerraformDefinition
// and TerraformInstance lifecycle hooks under test are stubs that return nil
// regardless of tx state, so no schema migration is needed; a live *gorm.DB is
// still passed to exercise the real hook signatures.
func setupTerraformTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

// TestTerraformDefinitionLifecycleHooks_ReturnNil asserts every
// TerraformDefinition lifecycle hook returns nil for a zero-value receiver and
// a live transaction. These hooks are scaffold stubs; the contract they expose
// to callers is "never error".
func TestTerraformDefinitionLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupTerraformTestDB(t)
	td := &TerraformDefinition{}

	// each case exercises one hook and asserts nil is returned
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return td.beforeCreate(db) }},
		{"beforeUpdate", func() error { return td.beforeUpdate(db) }},
		{"beforeDelete", func() error { return td.beforeDelete(db) }},
		{"afterCreate", func() error { return td.afterCreate(db) }},
		{"afterUpdate", func() error { return td.afterUpdate(db) }},
		{"afterDelete", func() error { return td.afterDelete(db) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must return nil under the standard live-tx call shape
			assert.NoError(t, tc.call())
		})
	}
}

// TestTerraformDefinitionLifecycleHooks_NilTx asserts the TerraformDefinition
// lifecycle hooks tolerate a nil *gorm.DB. Since the stubs do not dereference
// tx, a nil transaction must not panic and must still return nil.
func TestTerraformDefinitionLifecycleHooks_NilTx(t *testing.T) {
	td := &TerraformDefinition{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return td.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return td.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return td.beforeDelete(nil) }},
		{"afterCreate", func() error { return td.afterCreate(nil) }},
		{"afterUpdate", func() error { return td.afterUpdate(nil) }},
		{"afterDelete", func() error { return td.afterDelete(nil) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must not panic and must return nil on a nil tx
			assert.NotPanics(t, func() { _ = tc.call() })
			assert.NoError(t, tc.call())
		})
	}
}

// TestTerraformInstanceLifecycleHooks_ReturnNil asserts every
// TerraformInstance lifecycle hook returns nil for a zero-value receiver and a
// live transaction. These hooks are scaffold stubs; the contract they expose
// to callers is "never error".
func TestTerraformInstanceLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupTerraformTestDB(t)
	ti := &TerraformInstance{}

	// each case exercises one hook and asserts nil is returned
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return ti.beforeCreate(db) }},
		{"beforeUpdate", func() error { return ti.beforeUpdate(db) }},
		{"beforeDelete", func() error { return ti.beforeDelete(db) }},
		{"afterCreate", func() error { return ti.afterCreate(db) }},
		{"afterUpdate", func() error { return ti.afterUpdate(db) }},
		{"afterDelete", func() error { return ti.afterDelete(db) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must return nil under the standard live-tx call shape
			assert.NoError(t, tc.call())
		})
	}
}

// TestTerraformInstanceLifecycleHooks_NilTx asserts the TerraformInstance
// lifecycle hooks tolerate a nil *gorm.DB. Since the stubs do not dereference
// tx, a nil transaction must not panic and must still return nil.
func TestTerraformInstanceLifecycleHooks_NilTx(t *testing.T) {
	ti := &TerraformInstance{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return ti.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return ti.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return ti.beforeDelete(nil) }},
		{"afterCreate", func() error { return ti.afterCreate(nil) }},
		{"afterUpdate", func() error { return ti.afterUpdate(nil) }},
		{"afterDelete", func() error { return ti.afterDelete(nil) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must not panic and must return nil on a nil tx
			assert.NotPanics(t, func() { _ = tc.call() })
			assert.NoError(t, tc.call())
		})
	}
}
