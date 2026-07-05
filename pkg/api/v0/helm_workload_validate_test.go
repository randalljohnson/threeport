package v0

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupHelmWorkloadTestDB returns an in-memory sqlite db. The helm workload
// lifecycle hooks under test are stubs that return nil regardless of tx state,
// so no schema migration is needed; a live *gorm.DB is still passed to exercise
// the real hook signatures.
func setupHelmWorkloadTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

// TestHelmWorkloadDefinitionLifecycleHooks_ReturnNil asserts every
// HelmWorkloadDefinition lifecycle hook returns nil for a zero-value receiver
// and a live transaction. These hooks are scaffold stubs; the contract they
// expose to callers is "never error".
func TestHelmWorkloadDefinitionLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupHelmWorkloadTestDB(t)
	h := &HelmWorkloadDefinition{}

	// each case exercises one hook and asserts nil is returned
	assertHooksReturnNil(t, []gatewayHookCase{
		{"beforeCreate", func() error { return h.beforeCreate(db) }},
		{"beforeUpdate", func() error { return h.beforeUpdate(db) }},
		{"beforeDelete", func() error { return h.beforeDelete(db) }},
		{"afterCreate", func() error { return h.afterCreate(db) }},
		{"afterUpdate", func() error { return h.afterUpdate(db) }},
		{"afterDelete", func() error { return h.afterDelete(db) }},
	})
}

// TestHelmWorkloadDefinitionLifecycleHooks_NilTx asserts the
// HelmWorkloadDefinition lifecycle hooks tolerate a nil *gorm.DB. Since the
// stubs do not dereference tx, a nil transaction must not panic and must
// still return nil.
func TestHelmWorkloadDefinitionLifecycleHooks_NilTx(t *testing.T) {
	h := &HelmWorkloadDefinition{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	assertHooksNilTxSafe(t, []gatewayHookCase{
		{"beforeCreate", func() error { return h.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return h.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return h.beforeDelete(nil) }},
		{"afterCreate", func() error { return h.afterCreate(nil) }},
		{"afterUpdate", func() error { return h.afterUpdate(nil) }},
		{"afterDelete", func() error { return h.afterDelete(nil) }},
	})
}

// TestHelmWorkloadInstanceLifecycleHooks_ReturnNil asserts every
// HelmWorkloadInstance lifecycle hook returns nil for a zero-value receiver
// and a live transaction.
func TestHelmWorkloadInstanceLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupHelmWorkloadTestDB(t)
	h := &HelmWorkloadInstance{}

	// each case exercises one hook and asserts nil is returned
	assertHooksReturnNil(t, []gatewayHookCase{
		{"beforeCreate", func() error { return h.beforeCreate(db) }},
		{"beforeUpdate", func() error { return h.beforeUpdate(db) }},
		{"beforeDelete", func() error { return h.beforeDelete(db) }},
		{"afterCreate", func() error { return h.afterCreate(db) }},
		{"afterUpdate", func() error { return h.afterUpdate(db) }},
		{"afterDelete", func() error { return h.afterDelete(db) }},
	})
}

// TestHelmWorkloadInstanceLifecycleHooks_NilTx asserts the HelmWorkloadInstance
// lifecycle hooks tolerate a nil *gorm.DB.
func TestHelmWorkloadInstanceLifecycleHooks_NilTx(t *testing.T) {
	h := &HelmWorkloadInstance{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	assertHooksNilTxSafe(t, []gatewayHookCase{
		{"beforeCreate", func() error { return h.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return h.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return h.beforeDelete(nil) }},
		{"afterCreate", func() error { return h.afterCreate(nil) }},
		{"afterUpdate", func() error { return h.afterUpdate(nil) }},
		{"afterDelete", func() error { return h.afterDelete(nil) }},
	})
}
