package v0

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/threeport/threeport/pkg/encryption/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// setupGcpValidateDB returns a fresh in-memory sqlite gorm.DB with the
// GcpProvider and GcpGkeKubernetesRuntimeInstance tables migrated so the
// GcpProvider.beforeDelete dependent-query executes against a real schema.
func setupGcpValidateDB(t *testing.T) *gorm.DB {
	t.Helper()
	key, err := encryption.GenerateKey()
	require.NoError(t, err)
	t.Setenv(encryption.KeyEnvVar, key)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&GcpProvider{},
		&GcpGkeKubernetesRuntimeInstance{},
		&AttachedObjectReference{},
	))
	return db
}

// TestGcpProviderBeforeDelete_NoDependents covers the happy path of the
// GcpProvider delete guard: when no GcpGkeKubernetesRuntimeInstance rows
// reference the provider, the hook returns nil and the delete may proceed.
func TestGcpProviderBeforeDelete_NoDependents(t *testing.T) {
	// arrange a provider row with no dependent runtime instances
	db := setupGcpValidateDB(t)
	provider := &GcpProvider{
		Name:          util.Ptr("gcp-1"),
		ProjectID:     util.Ptr("proj-1"),
		DefaultRegion: util.Ptr("us-central1"),
	}
	require.NoError(t, db.Create(provider).Error)

	// invoke the delete hook directly on the provider
	err := provider.beforeDelete(db)

	// assert the hook returns nil so the delete can proceed
	assert.NoError(t, err)
}

// TestGcpProviderBeforeDelete_WithDependents rejects a GcpProvider delete
// when one or more GcpGkeKubernetesRuntimeInstance rows still reference it.
// The hook returns a 400 bad-request error naming the provider so the API
// caller can see which provider is still in use.
func TestGcpProviderBeforeDelete_WithDependents(t *testing.T) {
	// arrange a provider row plus a dependent runtime instance
	db := setupGcpValidateDB(t)
	provider := &GcpProvider{
		Name:          util.Ptr("gcp-dep"),
		ProjectID:     util.Ptr("proj-dep"),
		DefaultRegion: util.Ptr("us-central1"),
	}
	require.NoError(t, db.Create(provider).Error)
	instance := &GcpGkeKubernetesRuntimeInstance{
		Instance:                            Instance{Name: util.Ptr("gke-dep")},
		GcpProviderID:                       provider.ID,
		GcpGkeKubernetesRuntimeDefinitionID: util.Ptr(uint(1)),
		KubernetesRuntimeInstanceID:         util.Ptr(uint(1)),
	}
	require.NoError(t, db.Create(instance).Error)

	// invoke the delete hook with a dependent present
	err := provider.beforeDelete(db)

	// assert the hook surfaces a bad-request error naming the provider
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gcp-dep")
	assert.Contains(t, err.Error(), "cannot be deleted")

	// unwrap to *util.HttpError and confirm the 400 status
	var httpErr *util.HttpError
	require.True(t, errors.As(err, &httpErr))
	assert.Equal(t, 400, httpErr.StatusCode)
}

// TestGcpProviderBeforeDelete_QueryError exercises the query-failure branch
// by handing the hook a fresh DB with no schema migrated. The dependent-
// instance query fails and the hook surfaces the wrapping "failed to query"
// message naming the provider, without ever reaching the 400 branch.
func TestGcpProviderBeforeDelete_QueryError(t *testing.T) {
	// arrange a DB without the runtime-instance table so the query fails
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	provider := &GcpProvider{
		Common: Common{ID: util.Ptr(uint(42))},
		Name:   util.Ptr("no-schema"),
	}

	// invoke the delete hook against the schemaless DB
	hookErr := provider.beforeDelete(db)

	// assert the hook surfaces the query-failure message naming the provider
	require.Error(t, hookErr)
	assert.Contains(t, hookErr.Error(), "failed to query")
	assert.Contains(t, hookErr.Error(), "no-schema")
}

// TestGcpProviderStubHooks_ReturnNil asserts every GcpProvider hook other
// than beforeDelete is a scaffold stub that returns nil under a live
// transaction and a zero-value receiver.
func TestGcpProviderStubHooks_ReturnNil(t *testing.T) {
	db := setupGcpValidateDB(t)
	p := &GcpProvider{}

	// each case exercises one stub hook and asserts nil is returned
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return p.beforeCreate(db) }},
		{"beforeUpdate", func() error { return p.beforeUpdate(db) }},
		{"afterCreate", func() error { return p.afterCreate(db) }},
		{"afterUpdate", func() error { return p.afterUpdate(db) }},
		{"afterDelete", func() error { return p.afterDelete(db) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// stub hook must return nil under the standard live-tx call shape
			assert.NoError(t, tc.call())
		})
	}
}

// TestGcpProviderStubHooks_NilTx asserts the GcpProvider stub hooks (every
// hook except beforeDelete) tolerate a nil *gorm.DB without panicking. The
// stubs never dereference tx, so a nil transaction must round-trip nil.
func TestGcpProviderStubHooks_NilTx(t *testing.T) {
	p := &GcpProvider{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return p.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return p.beforeUpdate(nil) }},
		{"afterCreate", func() error { return p.afterCreate(nil) }},
		{"afterUpdate", func() error { return p.afterUpdate(nil) }},
		{"afterDelete", func() error { return p.afterDelete(nil) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// stub hook must not panic and must return nil on a nil tx
			assert.NotPanics(t, func() { _ = tc.call() })
			assert.NoError(t, tc.call())
		})
	}
}

// TestGcpGkeKubernetesRuntimeDefinitionHooks_ReturnNil asserts every
// GcpGkeKubernetesRuntimeDefinition lifecycle hook returns nil for a
// zero-value receiver and a live transaction. All six hooks are scaffold
// stubs; the contract they expose is "never error".
func TestGcpGkeKubernetesRuntimeDefinitionHooks_ReturnNil(t *testing.T) {
	db := setupGcpValidateDB(t)
	d := &GcpGkeKubernetesRuntimeDefinition{}

	// each case exercises one hook and asserts nil is returned
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return d.beforeCreate(db) }},
		{"beforeUpdate", func() error { return d.beforeUpdate(db) }},
		{"beforeDelete", func() error { return d.beforeDelete(db) }},
		{"afterCreate", func() error { return d.afterCreate(db) }},
		{"afterUpdate", func() error { return d.afterUpdate(db) }},
		{"afterDelete", func() error { return d.afterDelete(db) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// stub hook must return nil under the standard live-tx call shape
			assert.NoError(t, tc.call())
		})
	}
}

// TestGcpGkeKubernetesRuntimeDefinitionHooks_NilTx confirms the definition
// stub hooks tolerate a nil *gorm.DB. Since the stubs do not dereference
// tx, a nil transaction must not panic and must still return nil.
func TestGcpGkeKubernetesRuntimeDefinitionHooks_NilTx(t *testing.T) {
	d := &GcpGkeKubernetesRuntimeDefinition{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return d.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return d.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return d.beforeDelete(nil) }},
		{"afterCreate", func() error { return d.afterCreate(nil) }},
		{"afterUpdate", func() error { return d.afterUpdate(nil) }},
		{"afterDelete", func() error { return d.afterDelete(nil) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// stub hook must not panic and must return nil on a nil tx
			assert.NotPanics(t, func() { _ = tc.call() })
			assert.NoError(t, tc.call())
		})
	}
}

// TestGcpGkeKubernetesRuntimeInstanceHooks_ReturnNil asserts every
// GcpGkeKubernetesRuntimeInstance lifecycle hook returns nil for a
// zero-value receiver and a live transaction. All six hooks are scaffold
// stubs; the contract they expose is "never error".
func TestGcpGkeKubernetesRuntimeInstanceHooks_ReturnNil(t *testing.T) {
	db := setupGcpValidateDB(t)
	i := &GcpGkeKubernetesRuntimeInstance{}

	// each case exercises one hook and asserts nil is returned
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return i.beforeCreate(db) }},
		{"beforeUpdate", func() error { return i.beforeUpdate(db) }},
		{"beforeDelete", func() error { return i.beforeDelete(db) }},
		{"afterCreate", func() error { return i.afterCreate(db) }},
		{"afterUpdate", func() error { return i.afterUpdate(db) }},
		{"afterDelete", func() error { return i.afterDelete(db) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// stub hook must return nil under the standard live-tx call shape
			assert.NoError(t, tc.call())
		})
	}
}

// TestGcpGkeKubernetesRuntimeInstanceHooks_NilTx confirms the instance stub
// hooks tolerate a nil *gorm.DB. Since the stubs do not dereference tx, a
// nil transaction must not panic and must still return nil.
func TestGcpGkeKubernetesRuntimeInstanceHooks_NilTx(t *testing.T) {
	i := &GcpGkeKubernetesRuntimeInstance{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeCreate", func() error { return i.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return i.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return i.beforeDelete(nil) }},
		{"afterCreate", func() error { return i.afterCreate(nil) }},
		{"afterUpdate", func() error { return i.afterUpdate(nil) }},
		{"afterDelete", func() error { return i.afterDelete(nil) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// stub hook must not panic and must return nil on a nil tx
			assert.NotPanics(t, func() { _ = tc.call() })
			assert.NoError(t, tc.call())
		})
	}
}
