package v0

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	lib "github.com/threeport/threeport/pkg/api/lib/v0"
	auth "github.com/threeport/threeport/pkg/auth/v0"
)

// testCoreHolder mirrors testHolder from relationship_hooks_test.go but
// wires its gorm hooks straight through the exported
// ProcessCoreTaggedFields* wrappers. Exercising the wrappers via real
// gorm operations lets each test observe the delegated processor's
// side effects (AOR inserts, immutability rejections, cleanup) end to
// end, which is the only way to prove the wrapper delegates correctly.
type testCoreHolder struct {
	ID         *uint `gorm:"primaryKey"`
	Name       *string
	RequiresFK *uint `relationship:"requires"`
	OwnsFK     *uint `relationship:"owns"`
}

const testCoreHolderFQT = "test.local/v0.TestCoreHolder"

func (h *testCoreHolder) GetFullyQualifiedType() string { return testCoreHolderFQT }

func (h *testCoreHolder) RelationshipTaggedForeignKeys() []RelationshipTaggedForeignKey {
	return []RelationshipTaggedForeignKey{
		{FieldName: "RequiresFK", ObjectID: h.RequiresFK, ObjectType: "test.local/v0.RequiresTarget", Relationship: RelationshipRequires},
		{FieldName: "OwnsFK", ObjectID: h.OwnsFK, ObjectType: "test.local/v0.OwnsTarget", Relationship: RelationshipOwns},
	}
}

func (h *testCoreHolder) BeforeCreate(tx *gorm.DB) error {
	return ProcessCoreTaggedFieldsBeforeCreate(tx, h)
}

func (h *testCoreHolder) BeforeUpdate(tx *gorm.DB) error {
	return ProcessCoreTaggedFieldsBeforeUpdate(tx, h)
}

func (h *testCoreHolder) BeforeDelete(tx *gorm.DB) error {
	return ProcessCoreTaggedFieldsBeforeDelete(tx, h)
}

func (h *testCoreHolder) AfterCreate(tx *gorm.DB) error {
	return ProcessCoreTaggedFieldsAfterCreate(tx, h)
}

func (h *testCoreHolder) AfterUpdate(tx *gorm.DB) error {
	return ProcessCoreTaggedFieldsAfterUpdate(tx, h)
}

// setupCoreTaggedTestDB stands up an in-memory sqlite db with the
// testCoreHolder table and AttachedObjectReference migrated. Every
// wrapper test uses this fixture; the shared helper keeps the tests
// terse.
func setupCoreTaggedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&testCoreHolder{}, &AttachedObjectReference{}))
	return db
}

func coreUintPtr(v uint) *uint      { return &v }
func coreStrPtr(s string) *string   { return &s }

// countCoreAORs returns the number of AttachedObjectReference rows
// whose attacher is the given testCoreHolder id. Used to assert
// after-create and after-update AOR sync side effects.
func countCoreAORs(t *testing.T, db *gorm.DB, attacherID uint) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.
		Model(&AttachedObjectReference{}).
		Where("attached_object_type = ? AND attached_object_id = ?", testCoreHolderFQT, attacherID).
		Count(&n).Error)
	return n
}

// TestProcessCoreTaggedFieldsBeforeCreate_NonProviderReturnsNil covers
// the happy path where the object implements neither
// EncryptedFieldProvider nor PersistFalseFieldProvider: both delegated
// processors short-circuit to nil so the wrapper returns nil.
func TestProcessCoreTaggedFieldsBeforeCreate_NonProviderReturnsNil(t *testing.T) {
	db := setupCoreTaggedTestDB(t)

	// call the wrapper directly with a plain struct that implements no providers
	err := ProcessCoreTaggedFieldsBeforeCreate(db, &struct{}{})

	// assert no error propagates from either delegated processor
	require.NoError(t, err)
}

// TestProcessCoreTaggedFieldsBeforeCreate_ViaCreateHookSucceeds runs
// the wrapper through gorm's actual BeforeCreate callback on
// testCoreHolder, which is the production wiring shape. A successful
// Create proves the wrapper's return value flows through the gorm hook
// chain.
func TestProcessCoreTaggedFieldsBeforeCreate_ViaCreateHookSucceeds(t *testing.T) {
	db := setupCoreTaggedTestDB(t)

	// insert a row whose BeforeCreate hook delegates to the wrapper
	h := &testCoreHolder{Name: coreStrPtr("plain")}
	err := db.Create(h).Error

	// assert the row was persisted and the wrapper did not block create
	require.NoError(t, err)
	require.NotNil(t, h.ID)
}

// TestProcessCoreTaggedFieldsAfterCreate_InsertsAORsForSetFKs proves
// the wrapper delegates to processRelationshipTaggedFieldsAfterCreate:
// each set FK on the created row must yield one AOR.
func TestProcessCoreTaggedFieldsAfterCreate_InsertsAORsForSetFKs(t *testing.T) {
	db := setupCoreTaggedTestDB(t)

	// seed a target row so the FKs point at real ids, then create the holder
	// with both FKs set; AfterCreate fires and delegates to the wrapper
	target := &testCoreHolder{Name: coreStrPtr("target")}
	require.NoError(t, db.Create(target).Error)

	h := &testCoreHolder{
		Name:       coreStrPtr("holder"),
		RequiresFK: target.ID,
		OwnsFK:     target.ID,
	}
	require.NoError(t, db.Create(h).Error)

	// assert one AOR per set FK; the wrapper delegated to the relationship processor
	assert.Equal(t, int64(2), countCoreAORs(t, db, *h.ID))
}

// TestProcessCoreTaggedFieldsAfterCreate_NoFKsProducesNoAORs covers
// the boundary where every FK is nil: the delegated processor iterates
// but inserts nothing, so the wrapper returns nil and no AORs land.
func TestProcessCoreTaggedFieldsAfterCreate_NoFKsProducesNoAORs(t *testing.T) {
	db := setupCoreTaggedTestDB(t)

	// create a row with no FKs set; AfterCreate hook still fires
	h := &testCoreHolder{Name: coreStrPtr("no-fks")}
	require.NoError(t, db.Create(h).Error)

	// assert the wrapper produced no AOR side effects when every FK is nil
	assert.Equal(t, int64(0), countCoreAORs(t, db, *h.ID))
}

// TestProcessCoreTaggedFieldsBeforeUpdate_ImmutableFKRejected proves
// the wrapper delegates to processRelationshipTaggedFieldsBeforeUpdate
// first: a set->other transition on a relationship-tagged FK must be
// rejected with the "immutable once set" error.
func TestProcessCoreTaggedFieldsBeforeUpdate_ImmutableFKRejected(t *testing.T) {
	db := setupCoreTaggedTestDB(t)

	// seed two distinct target ids and a holder whose RequiresFK already points at target1
	target1 := &testCoreHolder{Name: coreStrPtr("t1")}
	require.NoError(t, db.Create(target1).Error)
	target2 := &testCoreHolder{Name: coreStrPtr("t2")}
	require.NoError(t, db.Create(target2).Error)

	holder := &testCoreHolder{Name: coreStrPtr("holder"), RequiresFK: target1.ID}
	require.NoError(t, db.Create(holder).Error)

	// attempt a set->other transition on the relationship-tagged FK
	var loaded testCoreHolder
	require.NoError(t, db.First(&loaded, *holder.ID).Error)
	err := db.Model(&loaded).Updates(&testCoreHolder{RequiresFK: target2.ID}).Error

	// assert the wrapper propagated the delegated processor's immutability rejection
	require.Error(t, err)
	assert.Contains(t, err.Error(), "immutable once set")
}

// TestProcessCoreTaggedFieldsBeforeUpdate_UnchangedRowSucceeds covers
// the happy path: a rename that doesn't touch any relationship FK and
// carries no incoming ownership should pass both delegated processors.
func TestProcessCoreTaggedFieldsBeforeUpdate_UnchangedRowSucceeds(t *testing.T) {
	db := setupCoreTaggedTestDB(t)

	// create a holder with no FKs and no incoming ownership refs
	holder := &testCoreHolder{Name: coreStrPtr("orig")}
	require.NoError(t, db.Create(holder).Error)

	// rename the holder; BeforeUpdate delegates through the wrapper
	var loaded testCoreHolder
	require.NoError(t, db.First(&loaded, *holder.ID).Error)
	err := db.Model(&loaded).Updates(&testCoreHolder{Name: coreStrPtr("renamed")}).Error

	// assert the wrapper allowed the update
	require.NoError(t, err)
}

// TestProcessCoreTaggedFieldsBeforeUpdate_OwnedRowBlocksExternal
// exercises the incoming-relationship guard inside the delegated
// processor: an AOR marking the row as owned makes external updates
// fail, while a control-plane caller bypasses the guard.
func TestProcessCoreTaggedFieldsBeforeUpdate_OwnedRowBlocksExternal(t *testing.T) {
	db := setupCoreTaggedTestDB(t)

	// seed a target row and plant an owns-relationship AOR pointing at it
	target := &testCoreHolder{Name: coreStrPtr("target")}
	require.NoError(t, db.Create(target).Error)
	owner := &testCoreHolder{Name: coreStrPtr("owner")}
	require.NoError(t, db.Create(owner).Error)
	rel := RelationshipOwns
	require.NoError(t, db.Create(&AttachedObjectReference{
		ObjectType:         coreStrPtr(testCoreHolderFQT),
		ObjectID:           target.ID,
		AttachedObjectType: coreStrPtr(testCoreHolderFQT),
		AttachedObjectID:   owner.ID,
		Relationship:       &rel,
	}).Error)

	// attempt an external update; wrapper must propagate the owned-by rejection
	var loaded testCoreHolder
	require.NoError(t, db.First(&loaded, *target.ID).Error)
	err := db.Model(&loaded).Updates(&testCoreHolder{Name: coreStrPtr("renamed")}).Error

	// assert the external caller was blocked with the owned-by message
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owned by")

	// same update issued via a control-plane caller must succeed;
	// bypass path is inside the delegated processor
	ctx := lib.WithCaller(context.Background(), lib.CallerIdentity{OrganizationalUnit: auth.OUControlPlane})
	var loaded2 testCoreHolder
	require.NoError(t, db.First(&loaded2, *target.ID).Error)
	require.NoError(t, db.WithContext(ctx).Model(&loaded2).Updates(&testCoreHolder{Name: coreStrPtr("cp-renamed")}).Error)
}

// TestProcessCoreTaggedFieldsAfterUpdate_InsertsAORForNewFK verifies
// the wrapper delegates to processRelationshipTaggedFieldsAfterUpdate:
// a nil->set transition (allowed by BeforeUpdate) must add an AOR.
func TestProcessCoreTaggedFieldsAfterUpdate_InsertsAORForNewFK(t *testing.T) {
	db := setupCoreTaggedTestDB(t)

	// seed target and holder; holder starts with no FKs and no AORs
	target := &testCoreHolder{Name: coreStrPtr("target")}
	require.NoError(t, db.Create(target).Error)
	holder := &testCoreHolder{Name: coreStrPtr("holder")}
	require.NoError(t, db.Create(holder).Error)
	require.Equal(t, int64(0), countCoreAORs(t, db, *holder.ID))

	// perform an allowed nil->set FK transition; AfterUpdate delegates to the wrapper
	var loaded testCoreHolder
	require.NoError(t, db.First(&loaded, *holder.ID).Error)
	require.NoError(t, db.Model(&loaded).Updates(&testCoreHolder{RequiresFK: target.ID}).Error)

	// assert the wrapper produced the AOR sync side effect
	assert.Equal(t, int64(1), countCoreAORs(t, db, *holder.ID))
}

// TestProcessCoreTaggedFieldsBeforeDelete_RejectsWhenBlocked exercises
// the blocking-reference path inside the delegated processor: a
// requires-relationship AOR pointing at the row must reject delete
// regardless of caller.
func TestProcessCoreTaggedFieldsBeforeDelete_RejectsWhenBlocked(t *testing.T) {
	db := setupCoreTaggedTestDB(t)

	// seed a target and plant an incoming requires-relationship AOR
	target := &testCoreHolder{Name: coreStrPtr("target")}
	require.NoError(t, db.Create(target).Error)
	requirer := &testCoreHolder{Name: coreStrPtr("requirer")}
	require.NoError(t, db.Create(requirer).Error)
	rel := RelationshipRequires
	require.NoError(t, db.Create(&AttachedObjectReference{
		ObjectType:         coreStrPtr(testCoreHolderFQT),
		ObjectID:           target.ID,
		AttachedObjectType: coreStrPtr(testCoreHolderFQT),
		AttachedObjectID:   requirer.ID,
		Relationship:       &rel,
	}).Error)

	// load then delete the row; BeforeDelete delegates through the wrapper
	var loaded testCoreHolder
	require.NoError(t, db.First(&loaded, *target.ID).Error)
	err := db.Delete(&loaded).Error

	// assert the wrapper propagated the blocking-reference rejection
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be deleted")
}

// TestProcessCoreTaggedFieldsBeforeDelete_CleansOutgoingAORs verifies
// the wrapper delegates the outgoing-AOR cleanup: on an unblocked
// delete the row's outgoing AORs are removed in the same transaction.
func TestProcessCoreTaggedFieldsBeforeDelete_CleansOutgoingAORs(t *testing.T) {
	db := setupCoreTaggedTestDB(t)

	// seed target and holder with one outgoing AOR (RequiresFK set)
	target := &testCoreHolder{Name: coreStrPtr("target")}
	require.NoError(t, db.Create(target).Error)
	holder := &testCoreHolder{Name: coreStrPtr("holder"), RequiresFK: target.ID}
	require.NoError(t, db.Create(holder).Error)
	require.Equal(t, int64(1), countCoreAORs(t, db, *holder.ID))

	// delete the holder; BeforeDelete delegates through the wrapper
	var loaded testCoreHolder
	require.NoError(t, db.First(&loaded, *holder.ID).Error)
	require.NoError(t, db.Delete(&loaded).Error)

	// assert the wrapper drove the outgoing-AOR cleanup
	assert.Equal(t, int64(0), countCoreAORs(t, db, *holder.ID))
}

// TestProcessCoreTaggedFieldsAfterDelete_AlwaysReturnsNil pins the
// stub behavior: no work happens after delete, so the wrapper returns
// nil for any input including a plain struct that implements no
// providers.
func TestProcessCoreTaggedFieldsAfterDelete_AlwaysReturnsNil(t *testing.T) {
	cases := []struct {
		name string
		obj  interface{}
	}{
		{name: "plain struct", obj: &struct{}{}},
		{name: "provider holder", obj: &testCoreHolder{Name: coreStrPtr("h"), RequiresFK: coreUintPtr(1)}},
		{name: "nil interface", obj: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupCoreTaggedTestDB(t)

			// invoke the after-delete wrapper directly with each input shape
			err := ProcessCoreTaggedFieldsAfterDelete(db, tc.obj)

			// assert the wrapper is a pure no-op regardless of input
			require.NoError(t, err)
		})
	}
}
