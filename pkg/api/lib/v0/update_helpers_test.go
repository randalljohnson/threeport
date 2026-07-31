package v0

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// testUpdateRow exercises the IncomingValues + IsFieldChanged helpers.
// Its BeforeUpdate hook captures the helpers' output onto exported
// fields tagged gorm:"-" so the captures don't get persisted.
type testUpdateRow struct {
	ID    *uint `gorm:"primaryKey"`
	Name  *string
	Count *int
	Color *string

	SawIncomingPtr  interface{} `gorm:"-"`
	SawNameChanged  bool        `gorm:"-"`
	SawCountChanged bool        `gorm:"-"`
	SawColorChanged bool        `gorm:"-"`
	SawErr          error       `gorm:"-"`
}

func (t *testUpdateRow) BeforeUpdate(tx *gorm.DB) error {
	t.SawIncomingPtr = IncomingValues(tx)
	var err error
	if t.SawNameChanged, err = IsFieldChanged(tx, "Name"); err != nil {
		t.SawErr = err
		return err
	}
	if t.SawCountChanged, err = IsFieldChanged(tx, "Count"); err != nil {
		t.SawErr = err
		return err
	}
	if t.SawColorChanged, err = IsFieldChanged(tx, "Color"); err != nil {
		t.SawErr = err
		return err
	}
	return nil
}

// testTypoRow exists only to drive the missing-field error path; its
// hook asks IsFieldChanged for a field that doesn't exist on the type.
type testTypoRow struct {
	ID     *uint `gorm:"primaryKey"`
	Name   *string
	SawErr error `gorm:"-"`
}

func (t *testTypoRow) BeforeUpdate(tx *gorm.DB) error {
	_, t.SawErr = IsFieldChanged(tx, "NoSuchField")
	return t.SawErr
}

func setupUpdateHooksTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&testUpdateRow{}, &testTypoRow{}))
	return db
}

func updateTestStrPtr(s string) *string { return &s }
func updateTestIntPtr(n int) *int       { return &n }
func updateTestUintPtr(n uint) *uint    { return &n }

// TestIncomingValues_UnderPATCH confirms the helper returns
// tx.Statement.Dest (the patch struct) under db.Model(...).Updates(...).
// The hook receiver in that call shape is the loaded row, not the
// patch; the whole point of IncomingValues is to redirect to the
// inbound payload so callers don't read stale loaded values by mistake.
func TestIncomingValues_UnderPATCH(t *testing.T) {
	db := setupUpdateHooksTestDB(t)
	require.NoError(t, db.Create(&testUpdateRow{
		ID: updateTestUintPtr(1), Name: updateTestStrPtr("orig"), Count: updateTestIntPtr(5),
	}).Error)

	var loaded testUpdateRow
	require.NoError(t, db.First(&loaded, 1).Error)

	patch := &testUpdateRow{Name: updateTestStrPtr("renamed")}
	require.NoError(t, db.Model(&loaded).Updates(patch).Error)

	assert.Same(t, patch, loaded.SawIncomingPtr,
		"under PATCH the receiver is the loaded row but IncomingValues must return the patch (Statement.Dest)")
}

// TestIncomingValues_UnderPUT confirms the helper returns the receiver
// itself under db.Save(...). Under that call shape Model == Dest, so
// the receiver already IS the caller's new values and the redirect is
// a no-op; IncomingValues hands the receiver back unchanged.
func TestIncomingValues_UnderPUT(t *testing.T) {
	db := setupUpdateHooksTestDB(t)
	require.NoError(t, db.Create(&testUpdateRow{
		ID: updateTestUintPtr(1), Name: updateTestStrPtr("orig"), Count: updateTestIntPtr(5),
	}).Error)

	obj := &testUpdateRow{
		ID: updateTestUintPtr(1), Name: updateTestStrPtr("renamed"), Count: updateTestIntPtr(7),
	}
	require.NoError(t, db.Save(obj).Error)

	assert.Same(t, obj, obj.SawIncomingPtr,
		"under PUT the receiver is the inbound object itself (receiver == Statement.Dest); IncomingValues hands it back")
}

// TestIsFieldChanged_PATCH_OnlyPatchFieldsThatDiffer confirms the PATCH
// fast-path (tx.Statement.Changed) reports a field only when it's in
// the patch payload AND differs from the loaded row. Fields absent
// from the patch (zero in Dest) are not flagged as changed even
// though the loaded row has different values.
func TestIsFieldChanged_PATCH_OnlyPatchFieldsThatDiffer(t *testing.T) {
	db := setupUpdateHooksTestDB(t)
	require.NoError(t, db.Create(&testUpdateRow{
		ID:    updateTestUintPtr(1),
		Name:  updateTestStrPtr("orig"),
		Count: updateTestIntPtr(5),
		Color: updateTestStrPtr("red"),
	}).Error)

	var loaded testUpdateRow
	require.NoError(t, db.First(&loaded, 1).Error)

	// patch sets Name (different) and Color (same value); Count omitted.
	patch := &testUpdateRow{
		Name:  updateTestStrPtr("renamed"),
		Color: updateTestStrPtr("red"),
	}
	require.NoError(t, db.Model(&loaded).Updates(patch).Error)

	assert.True(t, loaded.SawNameChanged, "Name differs from loaded → must be flagged")
	assert.False(t, loaded.SawCountChanged, "Count absent from patch → must not be flagged")
	assert.False(t, loaded.SawColorChanged, "Color same-value → must not be flagged")
}

// TestIsFieldChanged_PUT_FieldDiffsIncludingClears confirms the PUT
// path correctly compares every named field against the committed
// row, including the case where the inbound value is nil but the
// committed value was non-nil (an explicit clear under PUT).
func TestIsFieldChanged_PUT_FieldDiffsIncludingClears(t *testing.T) {
	db := setupUpdateHooksTestDB(t)
	require.NoError(t, db.Create(&testUpdateRow{
		ID:    updateTestUintPtr(1),
		Name:  updateTestStrPtr("orig"),
		Count: updateTestIntPtr(5),
		Color: updateTestStrPtr("red"),
	}).Error)

	// PUT changes Name, leaves Count, clears Color.
	obj := &testUpdateRow{
		ID:    updateTestUintPtr(1),
		Name:  updateTestStrPtr("renamed"),
		Count: updateTestIntPtr(5),
		Color: nil,
	}
	require.NoError(t, db.Save(obj).Error)

	assert.True(t, obj.SawNameChanged, "Name changed → must be flagged")
	assert.False(t, obj.SawCountChanged, "Count unchanged → must not be flagged")
	assert.True(t, obj.SawColorChanged, "Color cleared (nil under PUT) → must be flagged")
}

// TestIsFieldChanged_PUT_NoChangeReportsFalse confirms a PUT that
// writes the same row back unchanged reports every field as not
// changed. Catches a regression where same-value PUT was mis-flagged.
func TestIsFieldChanged_PUT_NoChangeReportsFalse(t *testing.T) {
	db := setupUpdateHooksTestDB(t)
	require.NoError(t, db.Create(&testUpdateRow{
		ID:    updateTestUintPtr(1),
		Name:  updateTestStrPtr("orig"),
		Count: updateTestIntPtr(5),
		Color: updateTestStrPtr("red"),
	}).Error)

	obj := &testUpdateRow{
		ID:    updateTestUintPtr(1),
		Name:  updateTestStrPtr("orig"),
		Count: updateTestIntPtr(5),
		Color: updateTestStrPtr("red"),
	}
	require.NoError(t, db.Save(obj).Error)

	assert.False(t, obj.SawNameChanged)
	assert.False(t, obj.SawCountChanged)
	assert.False(t, obj.SawColorChanged)
}

// TestIsFieldChanged_PATCH_NoChangeReportsFalse confirms a PATCH that
// rewrites the same values reports no changes. Pins the "same-value
// not flagged" branch of GORM's Statement.Changed.
func TestIsFieldChanged_PATCH_NoChangeReportsFalse(t *testing.T) {
	db := setupUpdateHooksTestDB(t)
	require.NoError(t, db.Create(&testUpdateRow{
		ID:    updateTestUintPtr(1),
		Name:  updateTestStrPtr("orig"),
		Count: updateTestIntPtr(5),
	}).Error)

	var loaded testUpdateRow
	require.NoError(t, db.First(&loaded, 1).Error)

	patch := &testUpdateRow{Name: updateTestStrPtr("orig")}
	require.NoError(t, db.Model(&loaded).Updates(patch).Error)

	assert.False(t, loaded.SawNameChanged)
	assert.False(t, loaded.SawCountChanged)
	assert.False(t, loaded.SawColorChanged)
}

// TestIsFieldChanged_MissingFieldReturnsError_UnderPUT confirms a typo'd
// field name surfaces as a returned error rather than silently returning
// false on the PUT path. The hook propagates the error so GORM aborts
// the operation.
func TestIsFieldChanged_MissingFieldReturnsError_UnderPUT(t *testing.T) {
	db := setupUpdateHooksTestDB(t)
	require.NoError(t, db.Create(&testTypoRow{
		ID: updateTestUintPtr(1), Name: updateTestStrPtr("orig"),
	}).Error)

	obj := &testTypoRow{ID: updateTestUintPtr(1), Name: updateTestStrPtr("renamed")}
	err := db.Save(obj).Error

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "NoSuchField"),
		"error message should name the missing field; got: %s", err.Error())
	require.Error(t, obj.SawErr)
	assert.True(t, strings.Contains(obj.SawErr.Error(), "NoSuchField"))
}

// TestIsFieldChanged_MissingFieldReturnsError_UnderPATCH confirms the
// same guard fires on the PATCH path. Without explicit schema lookup
// the PATCH branch would delegate to tx.Statement.Changed which
// returns false silently for unknown field names, letting a typo
// slip through any immutability check that depends on the helper.
func TestIsFieldChanged_MissingFieldReturnsError_UnderPATCH(t *testing.T) {
	db := setupUpdateHooksTestDB(t)
	require.NoError(t, db.Create(&testTypoRow{
		ID: updateTestUintPtr(1), Name: updateTestStrPtr("orig"),
	}).Error)

	var loaded testTypoRow
	require.NoError(t, db.First(&loaded, 1).Error)

	patch := &testTypoRow{Name: updateTestStrPtr("renamed")}
	err := db.Model(&loaded).Updates(patch).Error

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "NoSuchField"),
		"error message should name the missing field; got: %s", err.Error())
	require.Error(t, loaded.SawErr)
	assert.True(t, strings.Contains(loaded.SawErr.Error(), "NoSuchField"))
}
