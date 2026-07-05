package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupSecretTestDB returns an in-memory sqlite db. The secret lifecycle hooks
// under test are stubs that return nil regardless of tx state, so no schema
// migration is needed; a live *gorm.DB is still passed to exercise the real
// hook signatures.
func setupSecretTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

// secretHookCase names a single lifecycle hook invocation.
type secretHookCase struct {
	name string
	call func() error
}

// assertSecretHooksReturnNil runs each hook and asserts nil is returned.
func assertSecretHooksReturnNil(t *testing.T, cases []secretHookCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must return nil under the standard call shape
			assert.NoError(t, tc.call())
		})
	}
}

// assertSecretHooksNilTxSafe runs each hook with a nil tx and asserts it
// neither panics nor errors.
func assertSecretHooksNilTxSafe(t *testing.T, cases []secretHookCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must not panic and must return nil on a nil tx
			assert.NotPanics(t, func() { _ = tc.call() })
			assert.NoError(t, tc.call())
		})
	}
}

// TestSecretDefinitionLifecycleHooks_ReturnNil asserts every SecretDefinition
// lifecycle hook returns nil for a zero-value receiver and a live transaction.
// These hooks are scaffold stubs; the contract they expose to callers is
// "never error".
func TestSecretDefinitionLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupSecretTestDB(t)
	s := &SecretDefinition{}

	// each case exercises one hook and asserts nil is returned
	assertSecretHooksReturnNil(t, []secretHookCase{
		{"beforeCreate", func() error { return s.beforeCreate(db) }},
		{"beforeUpdate", func() error { return s.beforeUpdate(db) }},
		{"beforeDelete", func() error { return s.beforeDelete(db) }},
		{"afterCreate", func() error { return s.afterCreate(db) }},
		{"afterUpdate", func() error { return s.afterUpdate(db) }},
		{"afterDelete", func() error { return s.afterDelete(db) }},
	})
}

// TestSecretDefinitionLifecycleHooks_NilTx asserts the SecretDefinition
// lifecycle hooks tolerate a nil *gorm.DB. Since the stubs do not dereference
// tx, a nil transaction must not panic and must still return nil.
func TestSecretDefinitionLifecycleHooks_NilTx(t *testing.T) {
	s := &SecretDefinition{}

	// each hook is invoked with a nil transaction to confirm the stub never
	// dereferences the tx pointer
	assertSecretHooksNilTxSafe(t, []secretHookCase{
		{"beforeCreate", func() error { return s.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return s.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return s.beforeDelete(nil) }},
		{"afterCreate", func() error { return s.afterCreate(nil) }},
		{"afterUpdate", func() error { return s.afterUpdate(nil) }},
		{"afterDelete", func() error { return s.afterDelete(nil) }},
	})
}

// TestSecretInstanceLifecycleHooks_ReturnNil asserts every SecretInstance
// lifecycle hook returns nil for a zero-value receiver and a live transaction.
// These hooks are scaffold stubs; the contract they expose to callers is
// "never error".
func TestSecretInstanceLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupSecretTestDB(t)
	s := &SecretInstance{}

	// each case exercises one hook and asserts nil is returned
	assertSecretHooksReturnNil(t, []secretHookCase{
		{"beforeCreate", func() error { return s.beforeCreate(db) }},
		{"beforeUpdate", func() error { return s.beforeUpdate(db) }},
		{"beforeDelete", func() error { return s.beforeDelete(db) }},
		{"afterCreate", func() error { return s.afterCreate(db) }},
		{"afterUpdate", func() error { return s.afterUpdate(db) }},
		{"afterDelete", func() error { return s.afterDelete(db) }},
	})
}

// TestSecretInstanceLifecycleHooks_NilTx asserts the SecretInstance lifecycle
// hooks tolerate a nil *gorm.DB. Since the stubs do not dereference tx, a nil
// transaction must not panic and must still return nil.
func TestSecretInstanceLifecycleHooks_NilTx(t *testing.T) {
	s := &SecretInstance{}

	// each hook is invoked with a nil transaction to confirm the stub never
	// dereferences the tx pointer
	assertSecretHooksNilTxSafe(t, []secretHookCase{
		{"beforeCreate", func() error { return s.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return s.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return s.beforeDelete(nil) }},
		{"afterCreate", func() error { return s.afterCreate(nil) }},
		{"afterUpdate", func() error { return s.afterUpdate(nil) }},
		{"afterDelete", func() error { return s.afterDelete(nil) }},
	})
}
