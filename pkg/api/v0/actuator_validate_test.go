package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupActuatorTestDB returns an in-memory sqlite db. The Profile and Tier
// lifecycle hooks under test are stubs that return nil regardless of tx
// state, so no schema migration is needed; a live *gorm.DB is still passed
// to exercise the real hook signatures.
func setupActuatorTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

// TestProfileLifecycleHooks_ReturnNil asserts every Profile lifecycle hook
// returns nil for a zero-value receiver and a live transaction. These hooks
// are scaffold stubs; the contract they expose to callers is "never error".
func TestProfileLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupActuatorTestDB(t)
	p := &Profile{}

	// each case exercises one hook and asserts nil is returned
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return p.beforeCreate(db) }},
		{"beforeUpdate", func() error { return p.beforeUpdate(db) }},
		{"beforeDelete", func() error { return p.beforeDelete(db) }},
		{"afterCreate", func() error { return p.afterCreate(db) }},
		{"afterUpdate", func() error { return p.afterUpdate(db) }},
		{"afterDelete", func() error { return p.afterDelete(db) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must return nil under the standard live-tx call shape
			assert.NoError(t, tc.call())
		})
	}
}

// TestProfileLifecycleHooks_NilTx asserts the Profile lifecycle hooks
// tolerate a nil *gorm.DB. Since the stubs do not dereference tx, a nil
// transaction must not panic and must still return nil.
func TestProfileLifecycleHooks_NilTx(t *testing.T) {
	p := &Profile{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return p.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return p.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return p.beforeDelete(nil) }},
		{"afterCreate", func() error { return p.afterCreate(nil) }},
		{"afterUpdate", func() error { return p.afterUpdate(nil) }},
		{"afterDelete", func() error { return p.afterDelete(nil) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must not panic and must return nil on a nil tx
			assert.NotPanics(t, func() { _ = tc.call() })
			assert.NoError(t, tc.call())
		})
	}
}

// TestTierLifecycleHooks_ReturnNil asserts every Tier lifecycle hook
// returns nil for a zero-value receiver and a live transaction. These hooks
// are scaffold stubs; the contract they expose to callers is "never error".
func TestTierLifecycleHooks_ReturnNil(t *testing.T) {
	db := setupActuatorTestDB(t)
	tier := &Tier{}

	// each case exercises one hook and asserts nil is returned
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return tier.beforeCreate(db) }},
		{"beforeUpdate", func() error { return tier.beforeUpdate(db) }},
		{"beforeDelete", func() error { return tier.beforeDelete(db) }},
		{"afterCreate", func() error { return tier.afterCreate(db) }},
		{"afterUpdate", func() error { return tier.afterUpdate(db) }},
		{"afterDelete", func() error { return tier.afterDelete(db) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must return nil under the standard live-tx call shape
			assert.NoError(t, tc.call())
		})
	}
}

// TestTierLifecycleHooks_NilTx asserts the Tier lifecycle hooks tolerate a
// nil *gorm.DB. Since the stubs do not dereference tx, a nil transaction
// must not panic and must still return nil.
func TestTierLifecycleHooks_NilTx(t *testing.T) {
	tier := &Tier{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return tier.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return tier.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return tier.beforeDelete(nil) }},
		{"afterCreate", func() error { return tier.afterCreate(nil) }},
		{"afterUpdate", func() error { return tier.afterUpdate(nil) }},
		{"afterDelete", func() error { return tier.afterDelete(nil) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// hook must not panic and must return nil on a nil tx
			assert.NotPanics(t, func() { _ = tc.call() })
			assert.NoError(t, tc.call())
		})
	}
}
