package v0

import (
	"fmt"
	"reflect"

	"gorm.io/gorm"

	lib "github.com/threeport/threeport/pkg/api/lib/v0"
	auth "github.com/threeport/threeport/pkg/auth/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// Relationship classifies how an AttachedObjectReference relates the base
// object to the attached object. Drives lifecycle behavior (deletion and
// update blocking).
type Relationship string

const (
	RelationshipDescribes Relationship = "describes"
	RelationshipRequires  Relationship = "requires"
	RelationshipOwns      Relationship = "owns"
	RelationshipMarries   Relationship = "marries"
)

// RelationshipTaggedForeignKey describes a *uint ID field on an API type
// tagged with a `relationship:` value. The SDK generates a
// RelationshipTaggedForeignKeys method per type that returns one entry
// per such field, so runtime hooks read the list directly instead of
// walking struct tags via reflection.
type RelationshipTaggedForeignKey struct {
	FieldName    string
	ObjectType   string // e.g. "WorkloadInstance"
	Relationship Relationship
	ObjectID     *uint
}

// RelationshipTaggedForeignKeyProvider is implemented by every API type
// with at least one relationship-tagged foreign key.
type RelationshipTaggedForeignKeyProvider interface {
	RelationshipTaggedForeignKeys() []RelationshipTaggedForeignKey
}

// relationshipTaggedForeignKeysFor returns the tagged foreign keys of obj, or nil.
func relationshipTaggedForeignKeysFor(obj interface{}) []RelationshipTaggedForeignKey {
	p, ok := obj.(RelationshipTaggedForeignKeyProvider)
	if !ok {
		return nil
	}
	return p.RelationshipTaggedForeignKeys()
}

// insertAttachedObjectReference creates an AOR row for foreignKey. The
// `owns` and `requires` edges are immutable so this runs only on first set.
func insertAttachedObjectReference(
	tx *gorm.DB,
	foreignKey RelationshipTaggedForeignKey,
	attachedObjectType string,
	attachedObjectID uint,
) error {
	if err := tx.Create(&AttachedObjectReference{
		ObjectID:           foreignKey.ObjectID,
		ObjectType:         &foreignKey.ObjectType,
		AttachedObjectID:   &attachedObjectID,
		AttachedObjectType: &attachedObjectType,
		Relationship:       &foreignKey.Relationship,
	}).Error; err != nil {
		return fmt.Errorf(
			"failed to create attached object reference for %s.%s: %w",
			attachedObjectType, foreignKey.FieldName, err,
		)
	}
	return nil
}

// processRelationshipTaggedFieldsAfterCreate inserts an attached object
// reference for each foreign key that has a value.
func processRelationshipTaggedFieldsAfterCreate(tx *gorm.DB, obj interface{}) error {
	foreignKeys := relationshipTaggedForeignKeysFor(obj)
	if len(foreignKeys) == 0 {
		return nil
	}
	objType := util.ObjectTypeName(obj)
	objID := util.ObjectID(obj)
	if objID == nil {
		return fmt.Errorf(
			"cannot create attached object reference for %s: ID is nil after create",
			objType,
		)
	}
	for _, foreignKey := range foreignKeys {
		if foreignKey.ObjectID == nil {
			continue
		}
		if err := insertAttachedObjectReference(tx, foreignKey, objType, *objID); err != nil {
			return err
		}
	}
	return nil
}

// processRelationshipTaggedFieldsBeforeUpdate rejects updates that change
// a set foreign key, or any update to a row owned or married by another
// object.
func processRelationshipTaggedFieldsBeforeUpdate(tx *gorm.DB, obj interface{}) error {
	// reject reassignment of any relationship FK whose value is already set;
	// the owns/requires/marries edges are intentionally immutable
	for _, foreignKey := range relationshipTaggedForeignKeysFor(obj) {
		if foreignKey.ObjectID != nil && tx.Statement.Changed(string(foreignKey.FieldName)) {
			return util.NewBadRequestError(fmt.Sprintf(
				"%s is immutable; tear down and recreate the holder to undo this relationship",
				foreignKey.FieldName,
			))
		}
	}

	// look up incoming owns/marries refs to decide whether this row's
	// non-FK fields can be updated by the caller
	objType := util.ObjectTypeName(obj)
	objID := util.ObjectID(obj)
	if objID == nil {
		return nil
	}
	var ownedOrMarriedRefs []AttachedObjectReference
	if err := tx.
		Where(
			"object_type = ? AND object_id = ? AND relationship IN ?",
			objType, objID, []Relationship{RelationshipOwns, RelationshipMarries},
		).
		Find(&ownedOrMarriedRefs).Error; err != nil {
		return fmt.Errorf(
			"failed to look up owning attached object references for %s/%d: %w",
			objType, *objID, err,
		)
	}

	// no incoming owns/marries refs means anyone can update this row
	if len(ownedOrMarriedRefs) == 0 {
		return nil
	}

	// the owner/partner controller is allowed to update its owned/married
	// row; any control-plane caller bypasses the block
	if lib.Caller(tx.Statement.Context).OrganizationalUnit == auth.OUControlPlane {
		return nil
	}

	// external caller hit a row that's owned or married; reject the update
	// and tell them which object holds the lifecycle
	owner := "another object"
	if ownedOrMarriedRefs[0].AttachedObjectType != nil && ownedOrMarriedRefs[0].AttachedObjectID != nil {
		owner = fmt.Sprintf("%s/%d", *ownedOrMarriedRefs[0].AttachedObjectType, *ownedOrMarriedRefs[0].AttachedObjectID)
	}
	return util.NewBadRequestError(fmt.Sprintf(
		"object is owned by %s and cannot be updated externally; tear down the owner first",
		owner,
	))
}

// processRelationshipTaggedFieldsAfterUpdate syncs attached object
// references with foreign-key transitions. A foreign key that just
// cleared has its reference removed; one that just became set gets a
// new reference inserted.
func processRelationshipTaggedFieldsAfterUpdate(tx *gorm.DB, obj interface{}) error {
	if len(relationshipTaggedForeignKeysFor(obj)) == 0 {
		return nil
	}
	objType := util.ObjectTypeName(obj)
	objID := util.ObjectID(obj)
	if objID == nil {
		return fmt.Errorf(
			"cannot sync attached object references for %s: ID is nil after update",
			objType,
		)
	}

	// GORM's Model(&existing).Updates(...) pattern leaves the receiver's
	// FK fields at their pre-update values; reload into a fresh instance
	// so we can read post-update FK values without mutating obj (mutating
	// obj would null out tx.Statement.Changed's diff detection).
	freshObj := reflect.New(reflect.TypeOf(obj).Elem()).Interface()
	if err := tx.Session(&gorm.Session{NewDB: true}).First(freshObj, *objID).Error; err != nil {
		return fmt.Errorf(
			"failed to reload %s/%d after update: %w",
			objType, *objID, err,
		)
	}

	// state-based sync: GORM's Statement.Changed() is unreliable in
	// AfterUpdate, so reconcile the AOR table against the post-update FK
	// values directly. For each tagged FK, the AOR exists iff FK is set.
	for _, foreignKey := range relationshipTaggedForeignKeysFor(freshObj) {
		var count int64
		if err := tx.Session(&gorm.Session{NewDB: true}).
			Model(&AttachedObjectReference{}).
			Where(
				"object_type = ? AND attached_object_type = ? AND attached_object_id = ?",
				foreignKey.ObjectType, objType, *objID,
			).
			Count(&count).Error; err != nil {
			return fmt.Errorf(
				"failed to count attached object references for %s.%s: %w",
				objType, foreignKey.FieldName, err,
			)
		}

		switch {
		case foreignKey.ObjectID == nil && count > 0:
			if err := tx.Session(&gorm.Session{NewDB: true}).
				Where(
					"object_type = ? AND attached_object_type = ? AND attached_object_id = ?",
					foreignKey.ObjectType, objType, *objID,
				).
				Delete(&AttachedObjectReference{}).Error; err != nil {
				return fmt.Errorf(
					"failed to remove attached object reference for %s.%s: %w",
					objType, foreignKey.FieldName, err,
				)
			}
		case foreignKey.ObjectID != nil && count == 0:
			if err := insertAttachedObjectReference(tx, foreignKey, objType, *objID); err != nil {
				return err
			}
		}
	}
	return nil
}

// processRelationshipTaggedFieldsBeforeDelete rejects deletion when an
// incoming reference blocks it, then removes the row's outgoing references.
func processRelationshipTaggedFieldsBeforeDelete(tx *gorm.DB, obj interface{}) error {
	if err := CheckBlockingAttachedObjectReferences(tx, obj); err != nil {
		return err
	}
	objType := util.ObjectTypeName(obj)
	objID := util.ObjectID(obj)
	if objID == nil {
		return nil
	}

	// clean up outgoing references where this object is the attacher.
	// Those rows become orphans once this row is gone, so delete them
	// in the same transaction.
	if err := tx.
		Where("attached_object_type = ? AND attached_object_id = ?", objType, objID).
		Delete(&AttachedObjectReference{}).Error; err != nil {
		return fmt.Errorf(
			"failed to clean up outgoing attached object references for %s/%d: %w",
			objType, *objID, err,
		)
	}
	return nil
}
