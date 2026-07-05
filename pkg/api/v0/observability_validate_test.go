package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupObservabilityTestDB returns an in-memory sqlite db. The observability
// lifecycle hooks under test are stubs that return nil regardless of tx
// state, so no schema migration is needed; a live *gorm.DB is still passed
// to exercise the real hook signatures.
func setupObservabilityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

// observabilityHookCase pairs a hook name with a closure that invokes it.
type observabilityHookCase struct {
	name string
	call func() error
}

// runReturnNilCases asserts each hook returns nil under a live transaction.
func runReturnNilCases(t *testing.T, cases []observabilityHookCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must return nil under the standard live-tx call shape
			assert.NoError(t, tc.call())
		})
	}
}

// runNilTxCases asserts each hook tolerates a nil *gorm.DB without panicking.
func runNilTxCases(t *testing.T, cases []observabilityHookCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must not panic and must return nil on a nil tx
			assert.NotPanics(t, func() { _ = tc.call() })
			assert.NoError(t, tc.call())
		})
	}
}

// TestLoggingDefinitionLifecycleHooks_ReturnNil asserts every
// LoggingDefinition lifecycle hook returns nil for a zero-value receiver and
// a live transaction. These hooks are scaffold stubs; the contract they
// expose to callers is "never error".
func TestLoggingDefinitionLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupObservabilityTestDB(t)
	l := &LoggingDefinition{}

	// each case exercises one hook and asserts nil is returned
	runReturnNilCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return l.beforeCreate(db) }},
		{"beforeUpdate", func() error { return l.beforeUpdate(db) }},
		{"beforeDelete", func() error { return l.beforeDelete(db) }},
		{"afterCreate", func() error { return l.afterCreate(db) }},
		{"afterUpdate", func() error { return l.afterUpdate(db) }},
		{"afterDelete", func() error { return l.afterDelete(db) }},
	})
}

// TestLoggingDefinitionLifecycleHooks_NilTx asserts the LoggingDefinition
// lifecycle hooks tolerate a nil *gorm.DB. Since the stubs do not
// dereference tx, a nil transaction must not panic and must still return
// nil.
func TestLoggingDefinitionLifecycleHooks_NilTx(t *testing.T) {
	l := &LoggingDefinition{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	runNilTxCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return l.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return l.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return l.beforeDelete(nil) }},
		{"afterCreate", func() error { return l.afterCreate(nil) }},
		{"afterUpdate", func() error { return l.afterUpdate(nil) }},
		{"afterDelete", func() error { return l.afterDelete(nil) }},
	})
}

// TestLoggingInstanceLifecycleHooks_ReturnNil asserts every LoggingInstance
// lifecycle hook returns nil for a zero-value receiver and a live
// transaction. These hooks are scaffold stubs; the contract they expose to
// callers is "never error".
func TestLoggingInstanceLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupObservabilityTestDB(t)
	l := &LoggingInstance{}

	// each case exercises one hook and asserts nil is returned
	runReturnNilCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return l.beforeCreate(db) }},
		{"beforeUpdate", func() error { return l.beforeUpdate(db) }},
		{"beforeDelete", func() error { return l.beforeDelete(db) }},
		{"afterCreate", func() error { return l.afterCreate(db) }},
		{"afterUpdate", func() error { return l.afterUpdate(db) }},
		{"afterDelete", func() error { return l.afterDelete(db) }},
	})
}

// TestLoggingInstanceLifecycleHooks_NilTx asserts the LoggingInstance
// lifecycle hooks tolerate a nil *gorm.DB. Since the stubs do not
// dereference tx, a nil transaction must not panic and must still return
// nil.
func TestLoggingInstanceLifecycleHooks_NilTx(t *testing.T) {
	l := &LoggingInstance{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	runNilTxCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return l.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return l.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return l.beforeDelete(nil) }},
		{"afterCreate", func() error { return l.afterCreate(nil) }},
		{"afterUpdate", func() error { return l.afterUpdate(nil) }},
		{"afterDelete", func() error { return l.afterDelete(nil) }},
	})
}

// TestMetricsDefinitionLifecycleHooks_ReturnNil asserts every
// MetricsDefinition lifecycle hook returns nil for a zero-value receiver and
// a live transaction. These hooks are scaffold stubs; the contract they
// expose to callers is "never error".
func TestMetricsDefinitionLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupObservabilityTestDB(t)
	m := &MetricsDefinition{}

	// each case exercises one hook and asserts nil is returned
	runReturnNilCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return m.beforeCreate(db) }},
		{"beforeUpdate", func() error { return m.beforeUpdate(db) }},
		{"beforeDelete", func() error { return m.beforeDelete(db) }},
		{"afterCreate", func() error { return m.afterCreate(db) }},
		{"afterUpdate", func() error { return m.afterUpdate(db) }},
		{"afterDelete", func() error { return m.afterDelete(db) }},
	})
}

// TestMetricsDefinitionLifecycleHooks_NilTx asserts the MetricsDefinition
// lifecycle hooks tolerate a nil *gorm.DB. Since the stubs do not
// dereference tx, a nil transaction must not panic and must still return
// nil.
func TestMetricsDefinitionLifecycleHooks_NilTx(t *testing.T) {
	m := &MetricsDefinition{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	runNilTxCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return m.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return m.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return m.beforeDelete(nil) }},
		{"afterCreate", func() error { return m.afterCreate(nil) }},
		{"afterUpdate", func() error { return m.afterUpdate(nil) }},
		{"afterDelete", func() error { return m.afterDelete(nil) }},
	})
}

// TestMetricsInstanceLifecycleHooks_ReturnNil asserts every MetricsInstance
// lifecycle hook returns nil for a zero-value receiver and a live
// transaction. These hooks are scaffold stubs; the contract they expose to
// callers is "never error".
func TestMetricsInstanceLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupObservabilityTestDB(t)
	m := &MetricsInstance{}

	// each case exercises one hook and asserts nil is returned
	runReturnNilCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return m.beforeCreate(db) }},
		{"beforeUpdate", func() error { return m.beforeUpdate(db) }},
		{"beforeDelete", func() error { return m.beforeDelete(db) }},
		{"afterCreate", func() error { return m.afterCreate(db) }},
		{"afterUpdate", func() error { return m.afterUpdate(db) }},
		{"afterDelete", func() error { return m.afterDelete(db) }},
	})
}

// TestMetricsInstanceLifecycleHooks_NilTx asserts the MetricsInstance
// lifecycle hooks tolerate a nil *gorm.DB. Since the stubs do not
// dereference tx, a nil transaction must not panic and must still return
// nil.
func TestMetricsInstanceLifecycleHooks_NilTx(t *testing.T) {
	m := &MetricsInstance{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	runNilTxCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return m.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return m.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return m.beforeDelete(nil) }},
		{"afterCreate", func() error { return m.afterCreate(nil) }},
		{"afterUpdate", func() error { return m.afterUpdate(nil) }},
		{"afterDelete", func() error { return m.afterDelete(nil) }},
	})
}

// TestObservabilityDashboardDefinitionLifecycleHooks_ReturnNil asserts every
// ObservabilityDashboardDefinition lifecycle hook returns nil for a
// zero-value receiver and a live transaction. These hooks are scaffold
// stubs; the contract they expose to callers is "never error".
func TestObservabilityDashboardDefinitionLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupObservabilityTestDB(t)
	o := &ObservabilityDashboardDefinition{}

	// each case exercises one hook and asserts nil is returned
	runReturnNilCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return o.beforeCreate(db) }},
		{"beforeUpdate", func() error { return o.beforeUpdate(db) }},
		{"beforeDelete", func() error { return o.beforeDelete(db) }},
		{"afterCreate", func() error { return o.afterCreate(db) }},
		{"afterUpdate", func() error { return o.afterUpdate(db) }},
		{"afterDelete", func() error { return o.afterDelete(db) }},
	})
}

// TestObservabilityDashboardDefinitionLifecycleHooks_NilTx asserts the
// ObservabilityDashboardDefinition lifecycle hooks tolerate a nil *gorm.DB.
// Since the stubs do not dereference tx, a nil transaction must not panic
// and must still return nil.
func TestObservabilityDashboardDefinitionLifecycleHooks_NilTx(t *testing.T) {
	o := &ObservabilityDashboardDefinition{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	runNilTxCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return o.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return o.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return o.beforeDelete(nil) }},
		{"afterCreate", func() error { return o.afterCreate(nil) }},
		{"afterUpdate", func() error { return o.afterUpdate(nil) }},
		{"afterDelete", func() error { return o.afterDelete(nil) }},
	})
}

// TestObservabilityDashboardInstanceLifecycleHooks_ReturnNil asserts every
// ObservabilityDashboardInstance lifecycle hook returns nil for a zero-value
// receiver and a live transaction. These hooks are scaffold stubs; the
// contract they expose to callers is "never error".
func TestObservabilityDashboardInstanceLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupObservabilityTestDB(t)
	o := &ObservabilityDashboardInstance{}

	// each case exercises one hook and asserts nil is returned
	runReturnNilCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return o.beforeCreate(db) }},
		{"beforeUpdate", func() error { return o.beforeUpdate(db) }},
		{"beforeDelete", func() error { return o.beforeDelete(db) }},
		{"afterCreate", func() error { return o.afterCreate(db) }},
		{"afterUpdate", func() error { return o.afterUpdate(db) }},
		{"afterDelete", func() error { return o.afterDelete(db) }},
	})
}

// TestObservabilityDashboardInstanceLifecycleHooks_NilTx asserts the
// ObservabilityDashboardInstance lifecycle hooks tolerate a nil *gorm.DB.
// Since the stubs do not dereference tx, a nil transaction must not panic
// and must still return nil.
func TestObservabilityDashboardInstanceLifecycleHooks_NilTx(t *testing.T) {
	o := &ObservabilityDashboardInstance{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	runNilTxCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return o.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return o.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return o.beforeDelete(nil) }},
		{"afterCreate", func() error { return o.afterCreate(nil) }},
		{"afterUpdate", func() error { return o.afterUpdate(nil) }},
		{"afterDelete", func() error { return o.afterDelete(nil) }},
	})
}

// TestObservabilityStackDefinitionLifecycleHooks_ReturnNil asserts every
// ObservabilityStackDefinition lifecycle hook returns nil for a zero-value
// receiver and a live transaction. These hooks are scaffold stubs; the
// contract they expose to callers is "never error".
func TestObservabilityStackDefinitionLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupObservabilityTestDB(t)
	o := &ObservabilityStackDefinition{}

	// each case exercises one hook and asserts nil is returned
	runReturnNilCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return o.beforeCreate(db) }},
		{"beforeUpdate", func() error { return o.beforeUpdate(db) }},
		{"beforeDelete", func() error { return o.beforeDelete(db) }},
		{"afterCreate", func() error { return o.afterCreate(db) }},
		{"afterUpdate", func() error { return o.afterUpdate(db) }},
		{"afterDelete", func() error { return o.afterDelete(db) }},
	})
}

// TestObservabilityStackDefinitionLifecycleHooks_NilTx asserts the
// ObservabilityStackDefinition lifecycle hooks tolerate a nil *gorm.DB.
// Since the stubs do not dereference tx, a nil transaction must not panic
// and must still return nil.
func TestObservabilityStackDefinitionLifecycleHooks_NilTx(t *testing.T) {
	o := &ObservabilityStackDefinition{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	runNilTxCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return o.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return o.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return o.beforeDelete(nil) }},
		{"afterCreate", func() error { return o.afterCreate(nil) }},
		{"afterUpdate", func() error { return o.afterUpdate(nil) }},
		{"afterDelete", func() error { return o.afterDelete(nil) }},
	})
}

// TestObservabilityStackInstanceLifecycleHooks_ReturnNil asserts every
// ObservabilityStackInstance lifecycle hook returns nil for a zero-value
// receiver and a live transaction. These hooks are scaffold stubs; the
// contract they expose to callers is "never error".
func TestObservabilityStackInstanceLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupObservabilityTestDB(t)
	o := &ObservabilityStackInstance{}

	// each case exercises one hook and asserts nil is returned
	runReturnNilCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return o.beforeCreate(db) }},
		{"beforeUpdate", func() error { return o.beforeUpdate(db) }},
		{"beforeDelete", func() error { return o.beforeDelete(db) }},
		{"afterCreate", func() error { return o.afterCreate(db) }},
		{"afterUpdate", func() error { return o.afterUpdate(db) }},
		{"afterDelete", func() error { return o.afterDelete(db) }},
	})
}

// TestObservabilityStackInstanceLifecycleHooks_NilTx asserts the
// ObservabilityStackInstance lifecycle hooks tolerate a nil *gorm.DB. Since
// the stubs do not dereference tx, a nil transaction must not panic and
// must still return nil.
func TestObservabilityStackInstanceLifecycleHooks_NilTx(t *testing.T) {
	o := &ObservabilityStackInstance{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	runNilTxCases(t, []observabilityHookCase{
		{"beforeCreate", func() error { return o.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return o.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return o.beforeDelete(nil) }},
		{"afterCreate", func() error { return o.afterCreate(nil) }},
		{"afterUpdate", func() error { return o.afterUpdate(nil) }},
		{"afterDelete", func() error { return o.afterDelete(nil) }},
	})
}
