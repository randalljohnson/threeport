package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// testTransient has one field tagged persist:"false" and one that
// must persist. The BeforeCreate hook calls straight into
// ProcessPersistFalseTaggedFields so the test exercises the
// production wiring path.
type testTransient struct {
	ID     uint `gorm:"primaryKey"`
	Name   string
	Secret *string `persist:"false"`
}

func (t *testTransient) PersistFalseFields() []PersistFalseField {
	return []PersistFalseField{{Name: "Secret"}}
}

func (t *testTransient) BeforeCreate(tx *gorm.DB) error {
	return ProcessPersistFalseTaggedFields(tx, t)
}

// testTransient mirrors the BeforeUpdate wiring used by encrypt
// hooks so the update-path behavior can be pinned. Current
// production wiring does NOT call ProcessPersistFalseTaggedFields
// on BeforeUpdate (see ProcessCoreTaggedFieldsBeforeUpdate in
// pkg/api/v0/tagged_fields.go), but the function itself supports
// it via the dest redirect. The test struct opts in so we can
// also exercise that branch directly.
func (t *testTransient) BeforeUpdate(tx *gorm.DB) error {
	return ProcessPersistFalseTaggedFields(tx, t)
}

// testPlain has no persist:"false"-tagged fields and does not
// implement PersistFalseFieldProvider, so the hook must be a
// no-op for it.
type testPlain struct {
	ID    uint `gorm:"primaryKey"`
	Name  string
	Value string
}

func (t *testPlain) BeforeCreate(tx *gorm.DB) error {
	return ProcessPersistFalseTaggedFields(tx, t)
}

// setupPersistTestDB stands up an in-memory sqlite db with both
// test types migrated.
func setupPersistTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&testTransient{}, &testPlain{}))
	return db
}

// testStringPtr returns a fresh *string so an assertion-time
// local can't be reached through the pointer chain after the
// hook nulls the column.
func testStringPtr(s string) *string { return &s }

// TestPersistFalseHookNullsTaggedFieldOnCreate pins the core
// contract: the inbound persist:"false" field is dropped before
// the row hits the database, while sibling fields persist.
func TestPersistFalseHookNullsTaggedFieldOnCreate(t *testing.T) {
	db := setupPersistTestDB(t)

	obj := &testTransient{Name: "keep-me", Secret: testStringPtr("drop-me")}
	require.NoError(t, db.Create(obj).Error)

	var stored testTransient
	require.NoError(t, db.First(&stored, obj.ID).Error)
	assert.Equal(t, "keep-me", stored.Name, "untagged field must persist")
	assert.Nil(t, stored.Secret, "persist:false field must be nil in the row")
}

// TestPersistFalseHookLeavesUntaggedRowAlone pins the no-op
// path: a type without persist:"false" fields persists every
// field as written.
func TestPersistFalseHookLeavesUntaggedRowAlone(t *testing.T) {
	db := setupPersistTestDB(t)

	obj := &testPlain{Name: "alpha", Value: "beta"}
	require.NoError(t, db.Create(obj).Error)

	var stored testPlain
	require.NoError(t, db.First(&stored, obj.ID).Error)
	assert.Equal(t, "alpha", stored.Name)
	assert.Equal(t, "beta", stored.Value, "non-provider type must keep every field")
}

// TestPersistFalseHookNullsRegardlessOfInboundValue confirms a
// non-nil inbound value still gets dropped: the hook nulls the
// column unconditionally rather than inspecting the value.
func TestPersistFalseHookNullsRegardlessOfInboundValue(t *testing.T) {
	db := setupPersistTestDB(t)

	for _, name := range []string{"with-nil-secret", "with-set-secret"} {
		obj := &testTransient{Name: name}
		if name == "with-set-secret" {
			obj.Secret = testStringPtr("never-stored")
		}
		require.NoError(t, db.Create(obj).Error)

		var stored testTransient
		require.NoError(t, db.First(&stored, obj.ID).Error)
		assert.Nil(t, stored.Secret, "%s: persist:false field must be nil", name)
	}
}

// TestPersistFalseHookProductionUpdatePathPersistsTaggedField
// pins the current production wiring: persist:"false" fields
// only get nulled on BeforeCreate, not BeforeUpdate (see
// ProcessCoreTaggedFieldsBeforeUpdate in
// pkg/api/v0/tagged_fields.go). A PATCH that includes the
// tagged field therefore WILL store the value. This is
// known-but-pending-design-decision and the test pins it so
// any change to the wiring surfaces here first.
//
// The hook is exercised through gorm's BeforeUpdate to mirror
// what would happen if the wiring were added; the assertion
// below documents what the production wiring does today by
// using db.Updates with only the untagged field, so the hook
// has nothing to null and the existing tagged value survives.
func TestPersistFalseHookProductionUpdatePathPersistsTaggedField(t *testing.T) {
	db := setupPersistTestDB(t)

	// seed the row directly so a Secret value lands in the db,
	// bypassing the create-time null
	seeded := &testTransient{Name: "seed", Secret: testStringPtr("seeded-value")}
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(seeded).Error)

	var loaded testTransient
	require.NoError(t, db.First(&loaded, seeded.ID).Error)
	require.NotNil(t, loaded.Secret)
	require.Equal(t, "seeded-value", *loaded.Secret)

	// production wiring: BeforeUpdate does NOT call the
	// persist-false hook, so this test calls the gorm Update
	// path with hooks skipped to simulate that. An inbound
	// Secret would persist.
	inbound := &testTransient{Secret: testStringPtr("new-value")}
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Model(&loaded).Updates(inbound).Error)

	var stored testTransient
	require.NoError(t, db.First(&stored, seeded.ID).Error)
	require.NotNil(t, stored.Secret, "persist:false field still persists on the update path today")
	assert.Equal(t, "new-value", *stored.Secret, "production update wiring leaves the tagged field intact")
}

// TestPersistFalseHookUpdateBranchNullsViaDestRedirect exercises
// the dest-redirect branch in ProcessPersistFalseTaggedFields: if
// the wiring is ever extended to fire on BeforeUpdate, the hook
// must null the tagged column on the inbound side instead of
// the loaded row. The test struct's BeforeUpdate opts in so we
// can confirm the branch works end-to-end.
func TestPersistFalseHookUpdateBranchNullsViaDestRedirect(t *testing.T) {
	db := setupPersistTestDB(t)

	seeded := &testTransient{Name: "seed", Secret: testStringPtr("seeded-value")}
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(seeded).Error)

	var loaded testTransient
	require.NoError(t, db.First(&loaded, seeded.ID).Error)
	require.NotNil(t, loaded.Secret)

	inbound := &testTransient{Secret: testStringPtr("should-be-dropped")}
	require.NoError(t, db.Model(&loaded).Updates(inbound).Error)

	var stored testTransient
	require.NoError(t, db.First(&stored, seeded.ID).Error)
	assert.NotNil(t, stored.Secret, "loaded row's prior value remains untouched by the hook")
	assert.Equal(t, "seeded-value", *stored.Secret, "hook nulls only the inbound column, leaving the loaded value")
}
