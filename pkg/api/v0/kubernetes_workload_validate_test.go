package v0

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupKubernetesWorkloadTestDB returns an in-memory sqlite db. The
// kubernetes workload lifecycle hooks under test are stubs that return nil
// regardless of tx state, so no schema migration is needed; a live *gorm.DB
// is still passed to exercise the real hook signatures.
func setupKubernetesWorkloadTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

// TestKubernetesWorkloadDefinitionLifecycleHooks_ReturnNil asserts every
// KubernetesWorkloadDefinition lifecycle hook returns nil for a zero-value
// receiver and a live transaction. These hooks are scaffold stubs; the
// contract they expose to callers is "never error".
func TestKubernetesWorkloadDefinitionLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupKubernetesWorkloadTestDB(t)
	k := &KubernetesWorkloadDefinition{}

	// each case exercises one hook and asserts nil is returned
	assertHooksReturnNil(t, []gatewayHookCase{
		{"beforeCreate", func() error { return k.beforeCreate(db) }},
		{"beforeUpdate", func() error { return k.beforeUpdate(db) }},
		{"beforeDelete", func() error { return k.beforeDelete(db) }},
		{"afterCreate", func() error { return k.afterCreate(db) }},
		{"afterUpdate", func() error { return k.afterUpdate(db) }},
		{"afterDelete", func() error { return k.afterDelete(db) }},
	})
}

// TestKubernetesWorkloadDefinitionLifecycleHooks_NilTx asserts the
// KubernetesWorkloadDefinition lifecycle hooks tolerate a nil *gorm.DB.
// Since the stubs do not dereference tx, a nil transaction must not panic
// and must still return nil.
func TestKubernetesWorkloadDefinitionLifecycleHooks_NilTx(t *testing.T) {
	k := &KubernetesWorkloadDefinition{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	assertHooksNilTxSafe(t, []gatewayHookCase{
		{"beforeCreate", func() error { return k.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return k.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return k.beforeDelete(nil) }},
		{"afterCreate", func() error { return k.afterCreate(nil) }},
		{"afterUpdate", func() error { return k.afterUpdate(nil) }},
		{"afterDelete", func() error { return k.afterDelete(nil) }},
	})
}

// TestKubernetesWorkloadInstanceLifecycleHooks_ReturnNil asserts every
// KubernetesWorkloadInstance lifecycle hook returns nil for a zero-value
// receiver and a live transaction.
func TestKubernetesWorkloadInstanceLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupKubernetesWorkloadTestDB(t)
	k := &KubernetesWorkloadInstance{}

	// each case exercises one hook and asserts nil is returned
	assertHooksReturnNil(t, []gatewayHookCase{
		{"beforeCreate", func() error { return k.beforeCreate(db) }},
		{"beforeUpdate", func() error { return k.beforeUpdate(db) }},
		{"beforeDelete", func() error { return k.beforeDelete(db) }},
		{"afterCreate", func() error { return k.afterCreate(db) }},
		{"afterUpdate", func() error { return k.afterUpdate(db) }},
		{"afterDelete", func() error { return k.afterDelete(db) }},
	})
}

// TestKubernetesWorkloadInstanceLifecycleHooks_NilTx asserts the
// KubernetesWorkloadInstance lifecycle hooks tolerate a nil *gorm.DB.
func TestKubernetesWorkloadInstanceLifecycleHooks_NilTx(t *testing.T) {
	k := &KubernetesWorkloadInstance{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	assertHooksNilTxSafe(t, []gatewayHookCase{
		{"beforeCreate", func() error { return k.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return k.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return k.beforeDelete(nil) }},
		{"afterCreate", func() error { return k.afterCreate(nil) }},
		{"afterUpdate", func() error { return k.afterUpdate(nil) }},
		{"afterDelete", func() error { return k.afterDelete(nil) }},
	})
}

// TestKubernetesWorkloadResourceDefinitionLifecycleHooks_ReturnNil asserts
// every KubernetesWorkloadResourceDefinition lifecycle hook returns nil for
// a zero-value receiver and a live transaction.
func TestKubernetesWorkloadResourceDefinitionLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupKubernetesWorkloadTestDB(t)
	k := &KubernetesWorkloadResourceDefinition{}

	// each case exercises one hook and asserts nil is returned
	assertHooksReturnNil(t, []gatewayHookCase{
		{"beforeCreate", func() error { return k.beforeCreate(db) }},
		{"beforeUpdate", func() error { return k.beforeUpdate(db) }},
		{"beforeDelete", func() error { return k.beforeDelete(db) }},
		{"afterCreate", func() error { return k.afterCreate(db) }},
		{"afterUpdate", func() error { return k.afterUpdate(db) }},
		{"afterDelete", func() error { return k.afterDelete(db) }},
	})
}

// TestKubernetesWorkloadResourceDefinitionLifecycleHooks_NilTx asserts the
// KubernetesWorkloadResourceDefinition lifecycle hooks tolerate a nil
// *gorm.DB.
func TestKubernetesWorkloadResourceDefinitionLifecycleHooks_NilTx(t *testing.T) {
	k := &KubernetesWorkloadResourceDefinition{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	assertHooksNilTxSafe(t, []gatewayHookCase{
		{"beforeCreate", func() error { return k.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return k.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return k.beforeDelete(nil) }},
		{"afterCreate", func() error { return k.afterCreate(nil) }},
		{"afterUpdate", func() error { return k.afterUpdate(nil) }},
		{"afterDelete", func() error { return k.afterDelete(nil) }},
	})
}

// TestKubernetesWorkloadResourceInstanceLifecycleHooks_ReturnNil asserts
// every KubernetesWorkloadResourceInstance lifecycle hook returns nil for a
// zero-value receiver and a live transaction.
func TestKubernetesWorkloadResourceInstanceLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupKubernetesWorkloadTestDB(t)
	k := &KubernetesWorkloadResourceInstance{}

	// each case exercises one hook and asserts nil is returned
	assertHooksReturnNil(t, []gatewayHookCase{
		{"beforeCreate", func() error { return k.beforeCreate(db) }},
		{"beforeUpdate", func() error { return k.beforeUpdate(db) }},
		{"beforeDelete", func() error { return k.beforeDelete(db) }},
		{"afterCreate", func() error { return k.afterCreate(db) }},
		{"afterUpdate", func() error { return k.afterUpdate(db) }},
		{"afterDelete", func() error { return k.afterDelete(db) }},
	})
}

// TestKubernetesWorkloadResourceInstanceLifecycleHooks_NilTx asserts the
// KubernetesWorkloadResourceInstance lifecycle hooks tolerate a nil
// *gorm.DB.
func TestKubernetesWorkloadResourceInstanceLifecycleHooks_NilTx(t *testing.T) {
	k := &KubernetesWorkloadResourceInstance{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	assertHooksNilTxSafe(t, []gatewayHookCase{
		{"beforeCreate", func() error { return k.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return k.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return k.beforeDelete(nil) }},
		{"afterCreate", func() error { return k.afterCreate(nil) }},
		{"afterUpdate", func() error { return k.afterUpdate(nil) }},
		{"afterDelete", func() error { return k.afterDelete(nil) }},
	})
}
