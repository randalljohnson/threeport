package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupAORValidateDB returns an in-memory sqlite DB with the
// AttachedObjectReference table migrated. The validate hook fires on
// every update so no other tables are needed for these tests.
func setupAORValidateDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&AttachedObjectReference{}))
	return db
}

// createValidAOR inserts an AttachedObjectReference with the given
// relationship and returns the persisted row reloaded from DB. The
// reload mirrors the PATCH handler's flow, which loads existing before
// calling Updates.
func createValidAOR(t *testing.T, db *gorm.DB, rel Relationship) AttachedObjectReference {
	t.Helper()
	objType := "threeport.io/v0.TestBase"
	objID := uint(1)
	attachedType := "threeport.io/v0.TestAttacher"
	attachedID := uint(2)
	relCopy := rel
	existing := &AttachedObjectReference{
		ObjectType:         &objType,
		ObjectID:           &objID,
		AttachedObjectType: &attachedType,
		AttachedObjectID:   &attachedID,
		Relationship:       &relCopy,
	}
	require.NoError(t, db.Create(existing).Error)

	var loaded AttachedObjectReference
	require.NoError(t, db.First(&loaded, *existing.ID).Error)
	return loaded
}

// TestAttachedObjectReference_beforeUpdate_RejectsPatchRelationship
// pins the canonical PATCH-shape immutability check: changing
// Relationship via Updates() must be rejected because the relationship
// lifecycle (owns, marries, requires, describes) determines blocking
// behavior of existing references, and silently widening or narrowing
// it would change those guarantees post-create.
func TestAttachedObjectReference_beforeUpdate_RejectsPatchRelationship(t *testing.T) {
	db := setupAORValidateDB(t)
	loaded := createValidAOR(t, db, RelationshipRequires)

	newRel := RelationshipOwns
	patch := &AttachedObjectReference{Relationship: &newRel}
	err := db.Model(&loaded).Updates(patch).Error

	require.Error(t, err)
	assert.Contains(t, err.Error(), "AttachedObjectReference.Relationship is immutable")
}

// TestAttachedObjectReference_beforeUpdate_RejectsPutRelationship is
// the regression guard for the GORM Statement.Changed PUT-blind spot.
// Under Save() the receiver IS the inbound row, so Statement.Changed
// reports nothing changed even when the caller flips Relationship.
// The fix routes through IsFieldChanged, which loads the committed row
// and diffs it against the inbound values. This test fails against
// the pre-fix code and passes against the fixed code.
func TestAttachedObjectReference_beforeUpdate_RejectsPutRelationship(t *testing.T) {
	db := setupAORValidateDB(t)
	loaded := createValidAOR(t, db, RelationshipRequires)

	full := loaded
	newRel := RelationshipOwns
	full.Relationship = &newRel
	err := db.Save(&full).Error

	require.Error(t, err)
	assert.Contains(t, err.Error(), "AttachedObjectReference.Relationship is immutable")
}

// TestAttachedObjectReference_beforeUpdate_AllowsNoOpUpdate confirms a
// patch that doesn't touch Relationship passes through. This pins the
// "no spurious rejection" branch — without it a regression that
// always-returns-error on the field check would still pass the
// rejection tests above but break legitimate updates.
func TestAttachedObjectReference_beforeUpdate_AllowsNoOpUpdate(t *testing.T) {
	db := setupAORValidateDB(t)
	loaded := createValidAOR(t, db, RelationshipRequires)

	// PATCH with no Relationship field present in the payload
	patch := &AttachedObjectReference{}
	err := db.Model(&loaded).Updates(patch).Error

	require.NoError(t, err, "no-op update should pass; Relationship absent from patch must not trigger rejection")
}

// TestAttachedObjectReference_beforeUpdate_AllowsSameRelationshipPut
// confirms a PUT that writes the SAME Relationship value back passes
// through. Without the IsFieldChanged fix, a naive "any nil-vs-non-nil
// diff" check could fire even when the caller is sending the same
// value, breaking idempotent writes.
func TestAttachedObjectReference_beforeUpdate_AllowsSameRelationshipPut(t *testing.T) {
	db := setupAORValidateDB(t)
	loaded := createValidAOR(t, db, RelationshipRequires)

	full := loaded
	// reassign with the same value via a fresh pointer (simulates the
	// generated handler which binds the request body into a new struct)
	sameRel := RelationshipRequires
	full.Relationship = &sameRel
	err := db.Save(&full).Error

	require.NoError(t, err, "PUT writing the same Relationship back should pass; same value must not be flagged as changed")
}
