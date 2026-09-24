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

// setupOciValidateDB returns a fresh in-memory sqlite gorm.DB with the
// OciProvider and OciOkeKubernetesRuntimeInstance tables migrated so the
// OciProvider.beforeDelete dependent-query runs against a real schema.
func setupOciValidateDB(t *testing.T) *gorm.DB {
	t.Helper()
	key, err := encryption.GenerateKey()
	require.NoError(t, err)
	t.Setenv(encryption.KeyEnvVar, key)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&OciProvider{},
		&OciOkeKubernetesRuntimeInstance{},
		&AttachedObjectReference{},
	))
	return db
}

// TestOciProviderBeforeDelete_NoDependents covers the happy path of the
// OciProvider delete guard: when no OciOkeKubernetesRuntimeInstance rows
// reference the provider, the hook returns nil and the delete may proceed.
func TestOciProviderBeforeDelete_NoDependents(t *testing.T) {
	// arrange a provider row with no dependent runtime instances
	db := setupOciValidateDB(t)
	provider := &OciProvider{
		Name:            util.Ptr("oci-1"),
		UserOCID:        util.Ptr("ocid1.user.oc1..user"),
		TenancyOCID:     util.Ptr("ocid1.tenancy.oc1..tenancy"),
		CompartmentOCID: util.Ptr("ocid1.compartment.oc1..cmp"),
		DefaultRegion:   util.Ptr("us-phoenix-1"),
		KeyFingerprint:  util.Ptr("aa:bb:cc"),
		PrivateKey:      util.Ptr("private-key-value"),
	}
	require.NoError(t, db.Create(provider).Error)

	// invoke the delete hook directly on the provider
	err := provider.beforeDelete(db)

	// assert the hook returns nil so the delete can proceed
	assert.NoError(t, err)
}

// TestOciProviderBeforeDelete_WithDependents rejects an OciProvider delete
// when one or more OciOkeKubernetesRuntimeInstance rows still reference it.
// The hook returns a 409 conflict error naming the number of active
// instances so the API caller can see why the delete was blocked.
func TestOciProviderBeforeDelete_WithDependents(t *testing.T) {
	// arrange a provider row plus a dependent runtime instance
	db := setupOciValidateDB(t)
	provider := &OciProvider{
		Name:            util.Ptr("oci-dep"),
		UserOCID:        util.Ptr("ocid1.user.oc1..dep"),
		TenancyOCID:     util.Ptr("ocid1.tenancy.oc1..dep"),
		CompartmentOCID: util.Ptr("ocid1.compartment.oc1..dep"),
		DefaultRegion:   util.Ptr("us-phoenix-1"),
		KeyFingerprint:  util.Ptr("dd:ee:ff"),
		PrivateKey:      util.Ptr("private-key-dep"),
	}
	require.NoError(t, db.Create(provider).Error)
	instance := &OciOkeKubernetesRuntimeInstance{
		Instance:                            Instance{Name: util.Ptr("oke-dep")},
		OciProviderID:                       provider.ID,
		OciOkeKubernetesRuntimeDefinitionID: util.Ptr(uint(1)),
		KubernetesRuntimeInstanceID:         util.Ptr(uint(1)),
	}
	require.NoError(t, db.Create(instance).Error)

	// invoke the delete hook with a dependent present
	err := provider.beforeDelete(db)

	// assert the hook surfaces a conflict error mentioning the active count
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 active OKE runtime instance")
	assert.Contains(t, err.Error(), "cannot be deleted")

	// unwrap to *util.HttpError and confirm the 409 status
	var httpErr *util.HttpError
	require.True(t, errors.As(err, &httpErr))
	assert.Equal(t, 409, httpErr.StatusCode)
}

// TestOciProviderBeforeDelete_QueryError exercises the query-failure branch
// by handing the hook a fresh DB with no schema migrated. The dependent-
// instance query fails and the hook surfaces the wrapping "failed to check"
// message without ever reaching the 409 branch.
func TestOciProviderBeforeDelete_QueryError(t *testing.T) {
	// arrange a DB without the runtime-instance table so the query fails
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	provider := &OciProvider{
		Common: Common{ID: util.Ptr(uint(42))},
		Name:   util.Ptr("no-schema"),
	}

	// invoke the delete hook against the schemaless DB
	hookErr := provider.beforeDelete(db)

	// assert the hook surfaces the query-failure wrapping message
	require.Error(t, hookErr)
	assert.Contains(t, hookErr.Error(), "failed to check for OCI OKE runtime instances")
}

// TestOciProviderStubHooks_ReturnNil asserts every OciProvider hook other
// than beforeDelete is a scaffold stub that returns nil under a live
// transaction and a zero-value receiver.
func TestOciProviderStubHooks_ReturnNil(t *testing.T) {
	db := setupOciValidateDB(t)
	p := &OciProvider{}

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

// TestOciProviderStubHooks_NilTx asserts the OciProvider stub hooks (every
// hook except beforeDelete) tolerate a nil *gorm.DB without panicking. The
// stubs never dereference tx, so a nil transaction must round-trip nil.
func TestOciProviderStubHooks_NilTx(t *testing.T) {
	p := &OciProvider{}

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

// TestOciOkeKubernetesRuntimeDefinitionHooks_ReturnNil asserts every
// OciOkeKubernetesRuntimeDefinition lifecycle hook returns nil for a
// zero-value receiver and a live transaction. All six hooks are scaffold
// stubs; the contract they expose is "never error".
func TestOciOkeKubernetesRuntimeDefinitionHooks_ReturnNil(t *testing.T) {
	db := setupOciValidateDB(t)
	d := &OciOkeKubernetesRuntimeDefinition{}

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

// TestOciOkeKubernetesRuntimeDefinitionHooks_NilTx confirms the definition
// stub hooks tolerate a nil *gorm.DB. Since the stubs do not dereference
// tx, a nil transaction must not panic and must still return nil.
func TestOciOkeKubernetesRuntimeDefinitionHooks_NilTx(t *testing.T) {
	d := &OciOkeKubernetesRuntimeDefinition{}

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

// TestOciOkeKubernetesRuntimeInstanceHooks_ReturnNil asserts every
// OciOkeKubernetesRuntimeInstance lifecycle hook returns nil for a
// zero-value receiver and a live transaction. All six hooks are scaffold
// stubs; the contract they expose is "never error".
func TestOciOkeKubernetesRuntimeInstanceHooks_ReturnNil(t *testing.T) {
	db := setupOciValidateDB(t)
	i := &OciOkeKubernetesRuntimeInstance{}

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

// TestOciOkeKubernetesRuntimeInstanceHooks_NilTx confirms the instance stub
// hooks tolerate a nil *gorm.DB. Since the stubs do not dereference tx, a
// nil transaction must not panic and must still return nil.
func TestOciOkeKubernetesRuntimeInstanceHooks_NilTx(t *testing.T) {
	i := &OciOkeKubernetesRuntimeInstance{}

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
