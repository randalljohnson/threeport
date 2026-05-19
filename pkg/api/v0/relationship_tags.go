package v0

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"text/tabwriter"

	"github.com/iancoleman/strcase"
	"gorm.io/gorm"

	auth "github.com/threeport/threeport/pkg/auth/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// BlockedDeleteError reports a delete rejected by one or more attached
// object references. Error() renders the message with id-only paths; the
// API server upgrades to name-resolved paths when it can. AttachedRefs
// is always non-empty.
type BlockedDeleteError struct {
	AttachedRefs []AttachedObjectReference
}

func (e *BlockedDeleteError) Error() string {
	return FormatBlockedDelete(e, nil)
}

// FormatBlockedDelete renders the 409 message body. namesByType holds
// id->name for each object type that the caller resolved; pass nil for
// id-only output.
func FormatBlockedDelete(e *BlockedDeleteError, namesByType map[string]map[uint]string) string {
	baseType := *e.AttachedRefs[0].ObjectType
	baseID := *e.AttachedRefs[0].ObjectID
	baseLabel := FormatObjectPath(baseType, baseID, namesByType[baseType])

	var buf bytes.Buffer
	writer := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	for _, ref := range e.AttachedRefs {
		fmt.Fprintf(writer, "  - %s\n", FormatObjectPath(
			*ref.AttachedObjectType, *ref.AttachedObjectID, namesByType[*ref.AttachedObjectType],
		))
	}
	writer.Flush()

	return fmt.Sprintf(
		"%s cannot be deleted while %d object(s) still reference it:\n\n%sRemove dependents first.",
		baseLabel, len(e.AttachedRefs), buf.String(),
	)
}

// FormatObjectPath renders <namespace>/<kebab-kind>/<name> for module
// types and <kebab-kind>/<name> for core, falling back to id when no name
// is available.
func FormatObjectPath(rawType string, id uint, names map[uint]string) string {
	namespace := ""
	versionedName := rawType
	if slashIdx := strings.Index(rawType, "/"); slashIdx >= 0 {
		namespace = rawType[:slashIdx]
		versionedName = rawType[slashIdx+1:]
	}
	typeName := versionedName
	if dotIdx := strings.LastIndex(versionedName, "."); dotIdx >= 0 {
		typeName = versionedName[dotIdx+1:]
	}
	kind := strcase.ToKebab(typeName)

	tail := kind
	if namespace != "" {
		tail = fmt.Sprintf("%s/%s", namespace, kind)
	}

	if name, ok := names[id]; ok && name != "" {
		return fmt.Sprintf("%s/%s", tail, name)
	}
	return fmt.Sprintf("%s/%d", tail, id)
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
// a set foreign key, or any update to a row owned by another object.
func processRelationshipTaggedFieldsBeforeUpdate(tx *gorm.DB, obj interface{}) error {
	// reject any update that changes a foreign key with a value already set;
	// fall through when no foreign key on the row is being changed
	for _, foreignKey := range relationshipTaggedForeignKeysFor(obj) {
		if foreignKey.ObjectID != nil && tx.Statement.Changed(string(foreignKey.FieldName)) {
			return util.NewBadRequestError(fmt.Sprintf(
				"%s is immutable; tear down and recreate the holder to undo this relationship",
				foreignKey.FieldName,
			))
		}
	}

	objType := util.ObjectTypeName(obj)
	objID := util.ObjectID(obj)
	if objID == nil {
		return nil
	}
	var ownedRefs []AttachedObjectReference
	if err := tx.
		Where(
			"object_type = ? AND object_id = ? AND relationship IN ?",
			objType, objID, []Relationship{RelationshipOwns, RelationshipMarries},
		).
		Find(&ownedRefs).Error; err != nil {
		return fmt.Errorf(
			"failed to look up owning attached object references for %s/%d: %w",
			objType, *objID, err,
		)
	}
	if len(ownedRefs) == 0 {
		return nil
	}

	// any control-plane caller bypasses the block
	if Caller(tx.Statement.Context).OrganizationalUnit == auth.OUControlPlane {
		return nil
	}

	owner := "another object"
	if ownedRefs[0].AttachedObjectType != nil && ownedRefs[0].AttachedObjectID != nil {
		owner = fmt.Sprintf("%s/%d", *ownedRefs[0].AttachedObjectType, *ownedRefs[0].AttachedObjectID)
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
		return nil
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

// CheckBlockingAttachedObjectReferences returns a BlockedDeleteError if obj
// has any incoming references that block its deletion. Handlers call this
// synchronously before scheduling a delete so the caller sees the 409
// immediately instead of the controller looping on the BeforeDelete hook.
func CheckBlockingAttachedObjectReferences(tx *gorm.DB, obj interface{}) error {
	objType := util.ObjectTypeName(obj)
	objID := util.ObjectID(obj)
	if objID == nil {
		return nil
	}

	callerOU := Caller(tx.Statement.Context).OrganizationalUnit
	blockingRefs, err := findBlockingAttachedObjectReferences(tx, objType, objID, callerOU)
	if err != nil {
		return err
	}
	if len(blockingRefs) > 0 {
		return &BlockedDeleteError{AttachedRefs: blockingRefs}
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

// findBlockingAttachedObjectReferences returns the attached object
// references that block deletion of the given object. "requires" always
// blocks; "owns" and "marries" block unless the caller is a control-plane
// component.
func findBlockingAttachedObjectReferences(
	db *gorm.DB,
	objectType string,
	objectID *uint,
	callerOU string,
) ([]AttachedObjectReference, error) {
	var attachedObjectReferences []AttachedObjectReference
	if err := db.
		Where(
			"object_type = ? AND object_id = ? AND relationship IN ?",
			objectType, objectID, []Relationship{RelationshipRequires, RelationshipOwns, RelationshipMarries},
		).
		Find(&attachedObjectReferences).Error; err != nil {
		return nil, fmt.Errorf("failed to list blocking attached object references: %w", err)
	}

	if callerOU != auth.OUControlPlane {
		return attachedObjectReferences, nil
	}

	var blockingRefs []AttachedObjectReference
	for _, ref := range attachedObjectReferences {
		if ref.Relationship != nil && *ref.Relationship == RelationshipRequires {
			blockingRefs = append(blockingRefs, ref)
		}
	}
	return blockingRefs, nil
}

