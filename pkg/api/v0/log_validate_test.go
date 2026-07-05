package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupLogTestDB returns an in-memory sqlite db. The log lifecycle hooks under
// test are stubs that return nil regardless of tx state, so no schema migration
// is needed; a live *gorm.DB is still passed to exercise the real hook
// signatures.
func setupLogTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

// logHookCase names a single lifecycle hook invocation.
type logHookCase struct {
	name string
	call func() error
}

// assertLogHooksReturnNil runs each hook and asserts nil is returned.
func assertLogHooksReturnNil(t *testing.T, cases []logHookCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must return nil under the standard call shape
			assert.NoError(t, tc.call())
		})
	}
}

// assertLogHooksNilTxSafe runs each hook with a nil tx and asserts it neither
// panics nor errors.
func assertLogHooksNilTxSafe(t *testing.T, cases []logHookCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must not panic and must return nil on a nil tx
			assert.NotPanics(t, func() { _ = tc.call() })
			assert.NoError(t, tc.call())
		})
	}
}

// TestLogBackendLifecycleHooks_ReturnNil asserts every LogBackend lifecycle
// hook returns nil for a zero-value receiver and a live transaction. These
// hooks are scaffold stubs; the contract they expose to callers is "never
// error".
func TestLogBackendLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupLogTestDB(t)
	l := &LogBackend{}

	// each case exercises one hook and asserts nil is returned
	assertLogHooksReturnNil(t, []logHookCase{
		{"beforeCreate", func() error { return l.beforeCreate(db) }},
		{"beforeUpdate", func() error { return l.beforeUpdate(db) }},
		{"beforeDelete", func() error { return l.beforeDelete(db) }},
		{"afterCreate", func() error { return l.afterCreate(db) }},
		{"afterUpdate", func() error { return l.afterUpdate(db) }},
		{"afterDelete", func() error { return l.afterDelete(db) }},
	})
}

// TestLogBackendLifecycleHooks_NilTx asserts the LogBackend lifecycle hooks
// tolerate a nil *gorm.DB. Since the stubs do not dereference tx, a nil
// transaction must not panic and must still return nil.
func TestLogBackendLifecycleHooks_NilTx(t *testing.T) {
	l := &LogBackend{}

	// each hook is invoked with a nil transaction to confirm the stub never
	// dereferences the tx pointer
	assertLogHooksNilTxSafe(t, []logHookCase{
		{"beforeCreate", func() error { return l.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return l.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return l.beforeDelete(nil) }},
		{"afterCreate", func() error { return l.afterCreate(nil) }},
		{"afterUpdate", func() error { return l.afterUpdate(nil) }},
		{"afterDelete", func() error { return l.afterDelete(nil) }},
	})
}

// TestLogStorageDefinitionLifecycleHooks_ReturnNil asserts every
// LogStorageDefinition lifecycle hook returns nil for a zero-value receiver and
// a live transaction.
func TestLogStorageDefinitionLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupLogTestDB(t)
	l := &LogStorageDefinition{}

	// each case exercises one hook and asserts nil is returned
	assertLogHooksReturnNil(t, []logHookCase{
		{"beforeCreate", func() error { return l.beforeCreate(db) }},
		{"beforeUpdate", func() error { return l.beforeUpdate(db) }},
		{"beforeDelete", func() error { return l.beforeDelete(db) }},
		{"afterCreate", func() error { return l.afterCreate(db) }},
		{"afterUpdate", func() error { return l.afterUpdate(db) }},
		{"afterDelete", func() error { return l.afterDelete(db) }},
	})
}

// TestLogStorageDefinitionLifecycleHooks_NilTx asserts the LogStorageDefinition
// lifecycle hooks tolerate a nil *gorm.DB.
func TestLogStorageDefinitionLifecycleHooks_NilTx(t *testing.T) {
	l := &LogStorageDefinition{}

	// each hook is invoked with a nil transaction to confirm the stub never
	// dereferences the tx pointer
	assertLogHooksNilTxSafe(t, []logHookCase{
		{"beforeCreate", func() error { return l.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return l.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return l.beforeDelete(nil) }},
		{"afterCreate", func() error { return l.afterCreate(nil) }},
		{"afterUpdate", func() error { return l.afterUpdate(nil) }},
		{"afterDelete", func() error { return l.afterDelete(nil) }},
	})
}

// TestLogStorageInstanceLifecycleHooks_ReturnNil asserts every
// LogStorageInstance lifecycle hook returns nil for a zero-value receiver and a
// live transaction.
func TestLogStorageInstanceLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupLogTestDB(t)
	l := &LogStorageInstance{}

	// each case exercises one hook and asserts nil is returned
	assertLogHooksReturnNil(t, []logHookCase{
		{"beforeCreate", func() error { return l.beforeCreate(db) }},
		{"beforeUpdate", func() error { return l.beforeUpdate(db) }},
		{"beforeDelete", func() error { return l.beforeDelete(db) }},
		{"afterCreate", func() error { return l.afterCreate(db) }},
		{"afterUpdate", func() error { return l.afterUpdate(db) }},
		{"afterDelete", func() error { return l.afterDelete(db) }},
	})
}

// TestLogStorageInstanceLifecycleHooks_NilTx asserts the LogStorageInstance
// lifecycle hooks tolerate a nil *gorm.DB.
func TestLogStorageInstanceLifecycleHooks_NilTx(t *testing.T) {
	l := &LogStorageInstance{}

	// each hook is invoked with a nil transaction to confirm the stub never
	// dereferences the tx pointer
	assertLogHooksNilTxSafe(t, []logHookCase{
		{"beforeCreate", func() error { return l.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return l.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return l.beforeDelete(nil) }},
		{"afterCreate", func() error { return l.afterCreate(nil) }},
		{"afterUpdate", func() error { return l.afterUpdate(nil) }},
		{"afterDelete", func() error { return l.afterDelete(nil) }},
	})
}
