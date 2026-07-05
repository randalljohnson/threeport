package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupGatewayTestDB returns an in-memory sqlite db. The gateway lifecycle
// hooks under test are stubs that return nil regardless of tx state, so no
// schema migration is needed; a live *gorm.DB is still passed to exercise
// the real hook signatures.
func setupGatewayTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

// gatewayHookCase names a single lifecycle hook invocation.
type gatewayHookCase struct {
	name string
	call func() error
}

// assertHooksReturnNil runs each hook and asserts nil is returned.
func assertHooksReturnNil(t *testing.T, cases []gatewayHookCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must return nil under the standard call shape
			assert.NoError(t, tc.call())
		})
	}
}

// assertHooksNilTxSafe runs each hook with a nil tx and asserts it neither
// panics nor errors.
func assertHooksNilTxSafe(t *testing.T, cases []gatewayHookCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must not panic and must return nil on a nil tx
			assert.NotPanics(t, func() { _ = tc.call() })
			assert.NoError(t, tc.call())
		})
	}
}

// TestDomainNameDefinitionLifecycleHooks_ReturnNil asserts every
// DomainNameDefinition lifecycle hook returns nil for a zero-value receiver
// and a live transaction. These hooks are scaffold stubs; the contract they
// expose to callers is "never error".
func TestDomainNameDefinitionLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupGatewayTestDB(t)
	d := &DomainNameDefinition{}

	// each case exercises one hook and asserts nil is returned
	assertHooksReturnNil(t, []gatewayHookCase{
		{"beforeCreate", func() error { return d.beforeCreate(db) }},
		{"beforeUpdate", func() error { return d.beforeUpdate(db) }},
		{"beforeDelete", func() error { return d.beforeDelete(db) }},
		{"afterCreate", func() error { return d.afterCreate(db) }},
		{"afterUpdate", func() error { return d.afterUpdate(db) }},
		{"afterDelete", func() error { return d.afterDelete(db) }},
	})
}

// TestDomainNameDefinitionLifecycleHooks_NilTx asserts the
// DomainNameDefinition lifecycle hooks tolerate a nil *gorm.DB. Since the
// stubs do not dereference tx, a nil transaction must not panic and must
// still return nil.
func TestDomainNameDefinitionLifecycleHooks_NilTx(t *testing.T) {
	d := &DomainNameDefinition{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	assertHooksNilTxSafe(t, []gatewayHookCase{
		{"beforeCreate", func() error { return d.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return d.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return d.beforeDelete(nil) }},
		{"afterCreate", func() error { return d.afterCreate(nil) }},
		{"afterUpdate", func() error { return d.afterUpdate(nil) }},
		{"afterDelete", func() error { return d.afterDelete(nil) }},
	})
}

// TestDomainNameInstanceLifecycleHooks_ReturnNil asserts every
// DomainNameInstance lifecycle hook returns nil for a zero-value receiver
// and a live transaction. These hooks are scaffold stubs; the contract they
// expose to callers is "never error".
func TestDomainNameInstanceLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupGatewayTestDB(t)
	d := &DomainNameInstance{}

	// each case exercises one hook and asserts nil is returned
	assertHooksReturnNil(t, []gatewayHookCase{
		{"beforeCreate", func() error { return d.beforeCreate(db) }},
		{"beforeUpdate", func() error { return d.beforeUpdate(db) }},
		{"beforeDelete", func() error { return d.beforeDelete(db) }},
		{"afterCreate", func() error { return d.afterCreate(db) }},
		{"afterUpdate", func() error { return d.afterUpdate(db) }},
		{"afterDelete", func() error { return d.afterDelete(db) }},
	})
}

// TestDomainNameInstanceLifecycleHooks_NilTx asserts the DomainNameInstance
// lifecycle hooks tolerate a nil *gorm.DB.
func TestDomainNameInstanceLifecycleHooks_NilTx(t *testing.T) {
	d := &DomainNameInstance{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	assertHooksNilTxSafe(t, []gatewayHookCase{
		{"beforeCreate", func() error { return d.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return d.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return d.beforeDelete(nil) }},
		{"afterCreate", func() error { return d.afterCreate(nil) }},
		{"afterUpdate", func() error { return d.afterUpdate(nil) }},
		{"afterDelete", func() error { return d.afterDelete(nil) }},
	})
}

// TestGatewayDefinitionLifecycleHooks_ReturnNil asserts every
// GatewayDefinition lifecycle hook returns nil for a zero-value receiver and
// a live transaction.
func TestGatewayDefinitionLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupGatewayTestDB(t)
	g := &GatewayDefinition{}

	// each case exercises one hook and asserts nil is returned
	assertHooksReturnNil(t, []gatewayHookCase{
		{"beforeCreate", func() error { return g.beforeCreate(db) }},
		{"beforeUpdate", func() error { return g.beforeUpdate(db) }},
		{"beforeDelete", func() error { return g.beforeDelete(db) }},
		{"afterCreate", func() error { return g.afterCreate(db) }},
		{"afterUpdate", func() error { return g.afterUpdate(db) }},
		{"afterDelete", func() error { return g.afterDelete(db) }},
	})
}

// TestGatewayDefinitionLifecycleHooks_NilTx asserts the GatewayDefinition
// lifecycle hooks tolerate a nil *gorm.DB.
func TestGatewayDefinitionLifecycleHooks_NilTx(t *testing.T) {
	g := &GatewayDefinition{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	assertHooksNilTxSafe(t, []gatewayHookCase{
		{"beforeCreate", func() error { return g.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return g.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return g.beforeDelete(nil) }},
		{"afterCreate", func() error { return g.afterCreate(nil) }},
		{"afterUpdate", func() error { return g.afterUpdate(nil) }},
		{"afterDelete", func() error { return g.afterDelete(nil) }},
	})
}

// TestGatewayHttpPortLifecycleHooks_ReturnNil asserts every GatewayHttpPort
// lifecycle hook returns nil for a zero-value receiver and a live tx.
func TestGatewayHttpPortLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupGatewayTestDB(t)
	g := &GatewayHttpPort{}

	// each case exercises one hook and asserts nil is returned
	assertHooksReturnNil(t, []gatewayHookCase{
		{"beforeCreate", func() error { return g.beforeCreate(db) }},
		{"beforeUpdate", func() error { return g.beforeUpdate(db) }},
		{"beforeDelete", func() error { return g.beforeDelete(db) }},
		{"afterCreate", func() error { return g.afterCreate(db) }},
		{"afterUpdate", func() error { return g.afterUpdate(db) }},
		{"afterDelete", func() error { return g.afterDelete(db) }},
	})
}

// TestGatewayHttpPortLifecycleHooks_NilTx asserts the GatewayHttpPort
// lifecycle hooks tolerate a nil *gorm.DB.
func TestGatewayHttpPortLifecycleHooks_NilTx(t *testing.T) {
	g := &GatewayHttpPort{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	assertHooksNilTxSafe(t, []gatewayHookCase{
		{"beforeCreate", func() error { return g.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return g.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return g.beforeDelete(nil) }},
		{"afterCreate", func() error { return g.afterCreate(nil) }},
		{"afterUpdate", func() error { return g.afterUpdate(nil) }},
		{"afterDelete", func() error { return g.afterDelete(nil) }},
	})
}

// TestGatewayInstanceLifecycleHooks_ReturnNil asserts every GatewayInstance
// lifecycle hook returns nil for a zero-value receiver and a live tx.
func TestGatewayInstanceLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupGatewayTestDB(t)
	g := &GatewayInstance{}

	// each case exercises one hook and asserts nil is returned
	assertHooksReturnNil(t, []gatewayHookCase{
		{"beforeCreate", func() error { return g.beforeCreate(db) }},
		{"beforeUpdate", func() error { return g.beforeUpdate(db) }},
		{"beforeDelete", func() error { return g.beforeDelete(db) }},
		{"afterCreate", func() error { return g.afterCreate(db) }},
		{"afterUpdate", func() error { return g.afterUpdate(db) }},
		{"afterDelete", func() error { return g.afterDelete(db) }},
	})
}

// TestGatewayInstanceLifecycleHooks_NilTx asserts the GatewayInstance
// lifecycle hooks tolerate a nil *gorm.DB.
func TestGatewayInstanceLifecycleHooks_NilTx(t *testing.T) {
	g := &GatewayInstance{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	assertHooksNilTxSafe(t, []gatewayHookCase{
		{"beforeCreate", func() error { return g.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return g.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return g.beforeDelete(nil) }},
		{"afterCreate", func() error { return g.afterCreate(nil) }},
		{"afterUpdate", func() error { return g.afterUpdate(nil) }},
		{"afterDelete", func() error { return g.afterDelete(nil) }},
	})
}

// TestGatewayTcpPortLifecycleHooks_ReturnNil asserts every GatewayTcpPort
// lifecycle hook returns nil for a zero-value receiver and a live tx.
func TestGatewayTcpPortLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupGatewayTestDB(t)
	g := &GatewayTcpPort{}

	// each case exercises one hook and asserts nil is returned
	assertHooksReturnNil(t, []gatewayHookCase{
		{"beforeCreate", func() error { return g.beforeCreate(db) }},
		{"beforeUpdate", func() error { return g.beforeUpdate(db) }},
		{"beforeDelete", func() error { return g.beforeDelete(db) }},
		{"afterCreate", func() error { return g.afterCreate(db) }},
		{"afterUpdate", func() error { return g.afterUpdate(db) }},
		{"afterDelete", func() error { return g.afterDelete(db) }},
	})
}

// TestGatewayTcpPortLifecycleHooks_NilTx asserts the GatewayTcpPort lifecycle
// hooks tolerate a nil *gorm.DB.
func TestGatewayTcpPortLifecycleHooks_NilTx(t *testing.T) {
	g := &GatewayTcpPort{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	assertHooksNilTxSafe(t, []gatewayHookCase{
		{"beforeCreate", func() error { return g.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return g.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return g.beforeDelete(nil) }},
		{"afterCreate", func() error { return g.afterCreate(nil) }},
		{"afterUpdate", func() error { return g.afterUpdate(nil) }},
		{"afterDelete", func() error { return g.afterDelete(nil) }},
	})
}
