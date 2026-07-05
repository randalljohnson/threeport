package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// setupAwsValidateDB returns a fresh in-memory sqlite gorm.DB. The AWS
// lifecycle hooks under test either return nil unconditionally or only
// touch the passed *gorm.DB's Statement, so no schema migration is needed
// for direct-call tests.
func setupAwsValidateDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

// TestAwsProviderBeforeCreate_KeyPairValidation covers the AccessKeyID and
// SecretAccessKey pairing rule: both must be set together or both unset,
// otherwise the hook returns a bad-request error.
func TestAwsProviderBeforeCreate_KeyPairValidation(t *testing.T) {
	// each case names the receiver shape and the expected outcome
	cases := []struct {
		name        string
		provider    *AwsProvider
		wantErr     bool
		wantErrText string
	}{
		{
			name:     "neither key set accepts",
			provider: &AwsProvider{Name: util.Ptr("p")},
			wantErr:  false,
		},
		{
			name: "both keys set accepts",
			provider: &AwsProvider{
				Name:            util.Ptr("p"),
				AccessKeyID:     util.Ptr("AKIA..."),
				SecretAccessKey: util.Ptr("secret"),
			},
			wantErr: false,
		},
		{
			name: "only access key id rejects",
			provider: &AwsProvider{
				Name:        util.Ptr("p"),
				AccessKeyID: util.Ptr("AKIA..."),
			},
			wantErr:     true,
			wantErrText: "both access key id and secret access key must be set",
		},
		{
			name: "only secret access key rejects",
			provider: &AwsProvider{
				Name:            util.Ptr("p"),
				SecretAccessKey: util.Ptr("secret"),
			},
			wantErr:     true,
			wantErrText: "both access key id and secret access key must be set",
		},
		{
			name: "empty-string access key id treated as unset",
			provider: &AwsProvider{
				Name:        util.Ptr("p"),
				AccessKeyID: util.Ptr(""),
			},
			wantErr: false,
		},
		{
			name: "empty-string secret access key treated as unset",
			provider: &AwsProvider{
				Name:            util.Ptr("p"),
				SecretAccessKey: util.Ptr(""),
			},
			wantErr: false,
		},
		{
			name: "access key id set with empty secret rejects",
			provider: &AwsProvider{
				Name:            util.Ptr("p"),
				AccessKeyID:     util.Ptr("AKIA..."),
				SecretAccessKey: util.Ptr(""),
			},
			wantErr:     true,
			wantErrText: "both access key id and secret access key must be set",
		},
	}

	db := setupAwsValidateDB(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke the hook directly on a fresh DB session
			err := tc.provider.beforeCreate(db)

			// assert the pass/fail outcome, and (when failing) the surfaced message
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrText)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestAwsProviderBeforeCreate_ZeroValue confirms a zero-value receiver
// (all pointers nil) passes validation. Nil fields are skipped by the
// util.IsNonNilPtr guard so neither key is considered set.
func TestAwsProviderBeforeCreate_ZeroValue(t *testing.T) {
	db := setupAwsValidateDB(t)
	p := &AwsProvider{}

	// zero-value receiver exercises the nil-pointer skip branch
	assert.NoError(t, p.beforeCreate(db))
}

// TestAwsProviderStubHooks_ReturnNil asserts every AwsProvider hook other
// than beforeCreate is a scaffold stub that returns nil for a zero-value
// receiver and a live transaction.
func TestAwsProviderStubHooks_ReturnNil(t *testing.T) {
	db := setupAwsValidateDB(t)
	p := &AwsProvider{}

	// each case exercises one stub hook and asserts nil is returned
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeUpdate", func() error { return p.beforeUpdate(db) }},
		{"beforeDelete", func() error { return p.beforeDelete(db) }},
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

// TestAwsProviderStubHooks_NilTx asserts the AwsProvider stub hooks (all
// hooks except beforeCreate) tolerate a nil *gorm.DB without panicking.
// The stubs never dereference tx, so a nil transaction must round-trip nil.
func TestAwsProviderStubHooks_NilTx(t *testing.T) {
	p := &AwsProvider{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	cases := []struct {
		name string
		call func() error
	}{
		{"beforeUpdate", func() error { return p.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return p.beforeDelete(nil) }},
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

// TestAwsEksKubernetesRuntimeDefinitionHooks_ReturnNil asserts every
// AwsEksKubernetesRuntimeDefinition lifecycle hook returns nil for a
// zero-value receiver and a live transaction. All six hooks are scaffold
// stubs; the contract they expose is "never error".
func TestAwsEksKubernetesRuntimeDefinitionHooks_ReturnNil(t *testing.T) {
	db := setupAwsValidateDB(t)
	d := &AwsEksKubernetesRuntimeDefinition{}

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

// TestAwsEksKubernetesRuntimeDefinitionHooks_NilTx confirms the definition
// stub hooks tolerate a nil *gorm.DB. Since the stubs do not dereference
// tx, a nil transaction must not panic and must still return nil.
func TestAwsEksKubernetesRuntimeDefinitionHooks_NilTx(t *testing.T) {
	d := &AwsEksKubernetesRuntimeDefinition{}

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

// TestAwsEksKubernetesRuntimeInstanceHooks_ReturnNil asserts every
// AwsEksKubernetesRuntimeInstance lifecycle hook returns nil for a
// zero-value receiver and a live transaction. All six hooks are scaffold
// stubs; the contract they expose is "never error".
func TestAwsEksKubernetesRuntimeInstanceHooks_ReturnNil(t *testing.T) {
	db := setupAwsValidateDB(t)
	i := &AwsEksKubernetesRuntimeInstance{}

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

// TestAwsEksKubernetesRuntimeInstanceHooks_NilTx confirms the instance
// stub hooks tolerate a nil *gorm.DB. Since the stubs do not dereference
// tx, a nil transaction must not panic and must still return nil.
func TestAwsEksKubernetesRuntimeInstanceHooks_NilTx(t *testing.T) {
	i := &AwsEksKubernetesRuntimeInstance{}

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
