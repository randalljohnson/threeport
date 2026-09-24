package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/threeport/threeport/pkg/encryption/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// setupKubernetesRuntimeValidateDB returns an in-memory sqlite gorm.DB with
// the KubernetesRuntimeDefinition and KubernetesRuntimeInstance tables
// migrated, plus the encryption key set in the env so the encrypt hooks
// invoked by ProcessCoreTaggedFieldsBeforeUpdate on the instance side can
// resolve their key without erroring.
func setupKubernetesRuntimeValidateDB(t *testing.T) *gorm.DB {
	t.Helper()
	key, err := encryption.GenerateKey()
	require.NoError(t, err)
	t.Setenv(encryption.KeyEnvVar, key)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&KubernetesRuntimeDefinition{},
		&KubernetesRuntimeInstance{},
		&AttachedObjectReference{},
	))
	return db
}

// TestSupportedInfraProviders asserts every documented infra provider is
// present in the returned slice and no extras are included. The set is the
// contract every validator and consumer key off, so a drift here would
// silently reject valid providers or allow removed ones.
func TestSupportedInfraProviders(t *testing.T) {
	// invoke the accessor and collect the returned providers
	got := SupportedInfraProviders()

	// assert both the count and the exact membership of the returned set
	require.Len(t, got, 4)
	assert.ElementsMatch(t,
		[]KubernetesRuntimeInfraProvider{
			KubernetesRuntimeInfraProviderKind,
			KubernetesRuntimeInfraProviderEKS,
			KubernetesRuntimeInfraProviderOKE,
			KubernetesRuntimeInfraProviderGKE,
		},
		got,
	)
}

// TestKubernetesRuntimeDefinition_beforeCreate covers the infra-provider
// validation branch: each supported provider accepts and any other string
// rejects with a bad-request error naming the invalid provider.
func TestKubernetesRuntimeDefinition_beforeCreate(t *testing.T) {
	db := setupKubernetesRuntimeValidateDB(t)

	// each case pairs an infra provider value with the expected outcome
	cases := []struct {
		name     string
		provider string
		wantErr  bool
	}{
		{"kind accepts", KubernetesRuntimeInfraProviderKind, false},
		{"eks accepts", KubernetesRuntimeInfraProviderEKS, false},
		{"oke accepts", KubernetesRuntimeInfraProviderOKE, false},
		{"gke accepts", KubernetesRuntimeInfraProviderGKE, false},
		{"unknown rejects", "azure", true},
		{"empty string rejects", "", true},
		{"case mismatch rejects", "Kind", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// arrange a definition carrying the case's infra provider
			def := &KubernetesRuntimeDefinition{
				InfraProvider: util.Ptr(tc.provider),
			}

			// invoke the create hook directly on the fresh session
			err := def.beforeCreate(db)

			// assert the pass/fail outcome, and (when failing) that the
			// error names the invalid provider and cites the valid set
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.provider)
				assert.Contains(t, err.Error(), "not valid")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestKubernetesRuntimeDefinition_beforeUpdate_RejectsInfraProviderChange
// covers the immutability guard: an update that flips InfraProvider must be
// rejected with a bad-request error citing the immutable field.
func TestKubernetesRuntimeDefinition_beforeUpdate_RejectsInfraProviderChange(t *testing.T) {
	db := setupKubernetesRuntimeValidateDB(t)

	// arrange a persisted definition with kind as the initial infra provider
	existing := &KubernetesRuntimeDefinition{
		Definition:    Definition{Name: util.Ptr("test-krd")},
		InfraProvider: util.Ptr(KubernetesRuntimeInfraProviderKind),
	}
	require.NoError(t, db.Create(existing).Error)

	var loaded KubernetesRuntimeDefinition
	require.NoError(t, db.First(&loaded, *existing.ID).Error)

	// action: attempt to change InfraProvider on the loaded row via Updates
	payload := &KubernetesRuntimeDefinition{
		InfraProvider: util.Ptr(KubernetesRuntimeInfraProviderEKS),
	}
	err := db.Model(&loaded).Updates(payload).Error

	// assert the update is rejected and cites the immutable field
	require.Error(t, err)
	assert.Contains(t, err.Error(), "infra provider cannot be changed")
}

// TestKubernetesRuntimeDefinition_beforeUpdate_RejectsHighAvailabilityChange
// covers the sibling immutability guard: HighAvailability is also fixed at
// creation time and any update that alters it must be rejected.
func TestKubernetesRuntimeDefinition_beforeUpdate_RejectsHighAvailabilityChange(t *testing.T) {
	db := setupKubernetesRuntimeValidateDB(t)

	// arrange a persisted definition with HighAvailability explicitly false
	existing := &KubernetesRuntimeDefinition{
		Definition:       Definition{Name: util.Ptr("test-krd-ha")},
		InfraProvider:    util.Ptr(KubernetesRuntimeInfraProviderKind),
		HighAvailability: util.Ptr(false),
	}
	require.NoError(t, db.Create(existing).Error)

	var loaded KubernetesRuntimeDefinition
	require.NoError(t, db.First(&loaded, *existing.ID).Error)

	// action: attempt to flip HighAvailability on the loaded row
	payload := &KubernetesRuntimeDefinition{
		HighAvailability: util.Ptr(true),
	}
	err := db.Model(&loaded).Updates(payload).Error

	// assert the update is rejected and cites the immutable field
	require.Error(t, err)
	assert.Contains(t, err.Error(), "high availability cannot be changed")
}

// TestKubernetesRuntimeDefinition_beforeUpdate_AcceptsMutableFieldChange
// covers the happy path of the immutability guard: an update that only
// touches non-immutable fields must pass without error.
func TestKubernetesRuntimeDefinition_beforeUpdate_AcceptsMutableFieldChange(t *testing.T) {
	db := setupKubernetesRuntimeValidateDB(t)

	// arrange a persisted definition with an initial NodeSize
	existing := &KubernetesRuntimeDefinition{
		Definition:    Definition{Name: util.Ptr("test-krd-mut")},
		InfraProvider: util.Ptr(KubernetesRuntimeInfraProviderKind),
		NodeSize:      util.Ptr("Medium"),
	}
	require.NoError(t, db.Create(existing).Error)

	var loaded KubernetesRuntimeDefinition
	require.NoError(t, db.First(&loaded, *existing.ID).Error)

	// action: update only the mutable NodeSize field
	payload := &KubernetesRuntimeDefinition{
		NodeSize: util.Ptr("Large"),
	}
	err := db.Model(&loaded).Updates(payload).Error

	// assert the update is accepted since no immutable field is touched
	assert.NoError(t, err)
}

// TestKubernetesRuntimeInstance_beforeCreate covers the location-validation
// branch: a supported location accepts and any other string rejects with a
// bad-request error naming the invalid location.
func TestKubernetesRuntimeInstance_beforeCreate(t *testing.T) {
	db := setupKubernetesRuntimeValidateDB(t)

	// each case pairs a location value with the expected outcome
	cases := []struct {
		name     string
		location string
		wantErr  bool
	}{
		{"local accepts", "Local", false},
		{"north america new york accepts", "NorthAmerica:NewYork", false},
		{"unknown rejects", "Middle-Earth:Rivendell", true},
		{"empty string rejects", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// arrange an instance carrying the case's location
			inst := &KubernetesRuntimeInstance{
				Location: util.Ptr(tc.location),
			}

			// invoke the create hook directly on the fresh session
			err := inst.beforeCreate(db)

			// assert the pass/fail outcome, and (when failing) that the
			// error names the invalid location
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.location)
				assert.Contains(t, err.Error(), "not supported")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestKubernetesRuntimeInstance_beforeUpdate_RejectsLocationChange covers
// the instance-side immutability guard: an update that changes Location
// must be rejected with a bad-request error citing the new location value.
func TestKubernetesRuntimeInstance_beforeUpdate_RejectsLocationChange(t *testing.T) {
	db := setupKubernetesRuntimeValidateDB(t)

	// arrange a parent definition so the instance FK constraint is satisfied
	def := &KubernetesRuntimeDefinition{
		Definition:    Definition{Name: util.Ptr("test-krd-loc-parent")},
		InfraProvider: util.Ptr(KubernetesRuntimeInfraProviderKind),
	}
	require.NoError(t, db.Create(def).Error)

	// arrange a persisted instance at Local so the update can flip it
	existing := &KubernetesRuntimeInstance{
		Instance:                      Instance{Name: util.Ptr("test-kri")},
		Location:                      util.Ptr("Local"),
		KubernetesRuntimeDefinitionID: def.ID,
	}
	require.NoError(t, db.Create(existing).Error)

	var loaded KubernetesRuntimeInstance
	require.NoError(t, db.First(&loaded, *existing.ID).Error)

	// action: attempt to move the instance to a new location
	payload := &KubernetesRuntimeInstance{
		Location: util.Ptr("NorthAmerica:NewYork"),
	}
	err := db.Model(&loaded).Updates(payload).Error

	// assert the update is rejected and the message cites immovability
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be moved")
	assert.Contains(t, err.Error(), "immutable")
}

// TestKubernetesRuntimeInstance_beforeUpdate_AcceptsMutableFieldChange
// covers the happy path of the instance-side immutability guard: an
// update that only touches non-immutable fields must pass without error.
func TestKubernetesRuntimeInstance_beforeUpdate_AcceptsMutableFieldChange(t *testing.T) {
	db := setupKubernetesRuntimeValidateDB(t)

	// arrange a parent definition so the instance FK constraint is satisfied
	def := &KubernetesRuntimeDefinition{
		Definition:    Definition{Name: util.Ptr("test-krd-mut-parent")},
		InfraProvider: util.Ptr(KubernetesRuntimeInfraProviderKind),
	}
	require.NoError(t, db.Create(def).Error)

	// arrange a persisted instance at Local
	existing := &KubernetesRuntimeInstance{
		Instance:                      Instance{Name: util.Ptr("test-kri-mut")},
		Location:                      util.Ptr("Local"),
		KubernetesRuntimeDefinitionID: def.ID,
	}
	require.NoError(t, db.Create(existing).Error)

	var loaded KubernetesRuntimeInstance
	require.NoError(t, db.First(&loaded, *existing.ID).Error)

	// action: update only the mutable APIEndpoint field
	payload := &KubernetesRuntimeInstance{
		APIEndpoint: util.Ptr("https://api.example"),
	}
	err := db.Model(&loaded).Updates(payload).Error

	// assert the update is accepted since Location is untouched
	assert.NoError(t, err)
}

// TestKubernetesRuntimeDefinitionStubHooks_ReturnNil asserts every
// KubernetesRuntimeDefinition scaffold hook (beforeDelete and every after*
// hook) returns nil for a zero-value receiver and a live transaction.
func TestKubernetesRuntimeDefinitionStubHooks_ReturnNil(t *testing.T) {
	db := setupKubernetesRuntimeValidateDB(t)
	d := &KubernetesRuntimeDefinition{}

	// each case exercises one stub hook and asserts nil is returned
	cases := []struct {
		name string
		call func() error
	}{
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

// TestKubernetesRuntimeDefinitionStubHooks_NilTx confirms the definition
// stub hooks tolerate a nil *gorm.DB. Since the stubs do not dereference
// tx, a nil transaction must not panic and must still return nil.
func TestKubernetesRuntimeDefinitionStubHooks_NilTx(t *testing.T) {
	d := &KubernetesRuntimeDefinition{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	cases := []struct {
		name string
		call func() error
	}{
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

// TestKubernetesRuntimeInstanceStubHooks_ReturnNil asserts every
// KubernetesRuntimeInstance scaffold hook (beforeDelete and every after*
// hook) returns nil for a zero-value receiver and a live transaction.
func TestKubernetesRuntimeInstanceStubHooks_ReturnNil(t *testing.T) {
	db := setupKubernetesRuntimeValidateDB(t)
	i := &KubernetesRuntimeInstance{}

	// each case exercises one stub hook and asserts nil is returned
	cases := []struct {
		name string
		call func() error
	}{
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

// TestKubernetesRuntimeInstanceStubHooks_NilTx confirms the instance stub
// hooks tolerate a nil *gorm.DB. Since the stubs do not dereference tx, a
// nil transaction must not panic and must still return nil.
func TestKubernetesRuntimeInstanceStubHooks_NilTx(t *testing.T) {
	i := &KubernetesRuntimeInstance{}

	// each hook is invoked with a nil transaction to confirm the stub
	// never dereferences the tx pointer
	cases := []struct {
		name string
		call func() error
	}{
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
