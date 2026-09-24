package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newValidateDB returns an in-memory sqlite db with AttachedObjectReference
// migrated. The hooks under test operate on tx.Statement, so a real
// transaction against a live db is the most faithful setup.
func newValidateDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&AttachedObjectReference{}))
	return db
}

// TestAttachedObjectReference_beforeCreate_ReturnsNil covers the no-op
// beforeCreate hook: it currently has no rejection logic and must not
// interfere with a well-formed insert. The assertion checks both the
// direct method return and that a Create succeeds end-to-end.
func TestAttachedObjectReference_beforeCreate_ReturnsNil(t *testing.T) {
	// setup: fresh db, minimal well-formed row
	db := newValidateDB(t)
	a := newAOR("threeport.io/v0.Workload", 1, "threeport.io/v0.Pod", 2, RelationshipDescribes)

	// action: call the hook directly with the db as tx
	err := a.beforeCreate(db)

	// assert: direct call returns nil
	assert.NoError(t, err, "beforeCreate is a no-op and must return nil")

	// assert: end-to-end create succeeds so the hook wired through
	// the exported BeforeCreate wrapper does not block the insert
	require.NoError(t, db.Create(a).Error)
}

// TestAttachedObjectReference_beforeUpdate_ImmutableRelationship covers
// the only branch with rejection logic. When the Relationship column is
// marked changed on the update statement, the hook must return a bad
// request error naming the field. When it is not changed, the hook must
// pass through.
func TestAttachedObjectReference_beforeUpdate_ImmutableRelationship(t *testing.T) {
	const (
		baseType     = "threeport.io/v0.Workload"
		baseID       = uint(7)
		attacherType = "threeport.io/v0.Pod"
		attacherID   = uint(99)
	)

	cases := []struct {
		name      string
		updateRel *Relationship // nil = payload omits the field entirely
		wantErr   bool
		wantSub   string
	}{
		{
			name:      "unchanged update passes through",
			updateRel: nil,
		},
		{
			name:      "same-value update resolves Changed=false and passes",
			updateRel: rPtr(RelationshipDescribes),
		},
		{
			name:      "widening describes to owns is rejected",
			updateRel: rPtr(RelationshipOwns),
			wantErr:   true,
			wantSub:   "immutable",
		},
		{
			name:      "narrowing describes to requires is rejected",
			updateRel: rPtr(RelationshipRequires),
			wantErr:   true,
			wantSub:   "immutable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// setup: seed a describes-relationship row and reload it
			db := newValidateDB(t)
			existing := newAOR(baseType, baseID, attacherType, attacherID, RelationshipDescribes)
			require.NoError(t, db.Create(existing).Error)

			var loaded AttachedObjectReference
			require.NoError(t, db.First(&loaded, *existing.ID).Error)

			// action: apply the update through gorm so BeforeUpdate fires
			// with a real tx.Statement carrying the change set
			inbound := &AttachedObjectReference{}
			if tc.updateRel != nil {
				inbound.Relationship = tc.updateRel
			}
			err := db.Model(&loaded).Updates(inbound).Error

			if tc.wantErr {
				// assert: hook rejects with a bad-request error naming the field
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantSub)
				return
			}
			// assert: no-op or same-value update flows through
			require.NoError(t, err)
		})
	}
}

// TestAttachedObjectReference_beforeDelete_ReturnsNil covers the no-op
// beforeDelete hook: it must not block a soft-delete on a well-formed
// row.
func TestAttachedObjectReference_beforeDelete_ReturnsNil(t *testing.T) {
	// setup: seed a row so Delete has something to soft-delete
	db := newValidateDB(t)
	a := newAOR("threeport.io/v0.Workload", 1, "threeport.io/v0.Pod", 2, RelationshipDescribes)
	require.NoError(t, db.Create(a).Error)

	// action: direct hook call
	err := a.beforeDelete(db)

	// assert: no-op hook returns nil
	assert.NoError(t, err, "beforeDelete is a no-op and must return nil")

	// assert: full delete round-trip also succeeds
	require.NoError(t, db.Delete(a).Error)
}

// TestAttachedObjectReference_afterCreate_ReturnsNil covers the no-op
// afterCreate hook. It must not roll back a successful insert.
func TestAttachedObjectReference_afterCreate_ReturnsNil(t *testing.T) {
	// setup: fresh db, well-formed row already persisted
	db := newValidateDB(t)
	a := newAOR("threeport.io/v0.Workload", 1, "threeport.io/v0.Pod", 2, RelationshipDescribes)
	require.NoError(t, db.Create(a).Error)

	// action: direct hook call
	err := a.afterCreate(db)

	// assert: no-op hook returns nil
	assert.NoError(t, err, "afterCreate is a no-op and must return nil")
}

// TestAttachedObjectReference_afterUpdate_ReturnsNil covers the no-op
// afterUpdate hook. It must not roll back a successful update.
func TestAttachedObjectReference_afterUpdate_ReturnsNil(t *testing.T) {
	// setup: fresh db, unbound receiver
	db := newValidateDB(t)
	a := &AttachedObjectReference{}

	// action: direct hook call
	err := a.afterUpdate(db)

	// assert: no-op hook returns nil
	assert.NoError(t, err, "afterUpdate is a no-op and must return nil")
}

// TestAttachedObjectReference_afterDelete_ReturnsNil covers the no-op
// afterDelete hook. It must not roll back a successful delete.
func TestAttachedObjectReference_afterDelete_ReturnsNil(t *testing.T) {
	// setup: fresh db, unbound receiver
	db := newValidateDB(t)
	a := &AttachedObjectReference{}

	// action: direct hook call
	err := a.afterDelete(db)

	// assert: no-op hook returns nil
	assert.NoError(t, err, "afterDelete is a no-op and must return nil")
}
