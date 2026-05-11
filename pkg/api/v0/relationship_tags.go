package v0

import (
	"bytes"
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/iancoleman/strcase"
	"gorm.io/gorm"

	sdk "github.com/threeport/threeport/pkg/sdk/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// foreignKey describes a *uint ID field on an API type tagged
// `relationship:"owns"` or `relationship:"requires"`.
type foreignKey struct {
	fieldName    string
	objectType   string // e.g. "WorkloadInstance"
	relationship string // sdk.RelationshipOwns or sdk.RelationshipRequires
	objectID     *uint
}

// foreignKeyProvider is implemented by every API type with at
// least one tagged foreign key. The SDK generates the method so runtime
// hooks read it directly instead of reflecting over struct tags.
type foreignKeyProvider interface {
	ForeignKeys() []foreignKey
}

// foreignKeysFor returns the tagged foreign keys of obj, or nil.
func foreignKeysFor(obj interface{}) []foreignKey {
	p, ok := obj.(foreignKeyProvider)
	if !ok {
		return nil
	}
	return p.ForeignKeys()
}

// insertAttachedObjectReference creates an attached object reference for
// foreignKey. `owns` and `requires` edges are immutable, so this only runs
// on first set.
func insertAttachedObjectReference(
	tx *gorm.DB,
	foreignKey foreignKey,
	attachedObjectType string,
	attachedObjectID uint,
) error {
	if err := tx.Create(&AttachedObjectReference{
		ObjectID:           foreignKey.objectID,
		ObjectType:         &foreignKey.objectType,
		AttachedObjectID:   &attachedObjectID,
		AttachedObjectType: &attachedObjectType,
		Relationship:       &foreignKey.relationship,
	}).Error; err != nil {
		return fmt.Errorf(
			"failed to create attached object reference for %s.%s: %w",
			attachedObjectType, foreignKey.fieldName, err,
		)
	}
	return nil
}

// processRelationshipTaggedFieldsAfterCreate inserts an attached object
// reference for each foreign key that has a value.
func processRelationshipTaggedFieldsAfterCreate(tx *gorm.DB, obj interface{}) error {
	foreignKeys := foreignKeysFor(obj)
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
		if foreignKey.objectID == nil {
			continue
		}
		if err := insertAttachedObjectReference(tx, foreignKey, objType, *objID); err != nil {
			return err
		}
	}
	return nil
}

// processRelationshipTaggedFieldsBeforeUpdate rejects updates that change a
// foreign key once it has a value, and rejects updates to a row owned by
// another object.
func processRelationshipTaggedFieldsBeforeUpdate(tx *gorm.DB, obj interface{}) error {
	// reject any update that changes a foreign key with a value already set;
	// fall through when no foreign key on the row is being changed
	for _, foreignKey := range foreignKeysFor(obj) {
		if foreignKey.objectID != nil && tx.Statement.Changed(foreignKey.fieldName) {
			return util.NewBadRequestError(fmt.Sprintf(
				"%s is immutable; tear down and recreate the holder to undo this relationship",
				foreignKey.fieldName,
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
		Where("object_type = ? AND object_id = ? AND relationship = ?", objType, objID, sdk.RelationshipOwns).
		Find(&ownedRefs).Error; err != nil {
		return fmt.Errorf(
			"failed to look up owning attached object references for %s/%d: %w",
			objType, *objID, err,
		)
	}
	if len(ownedRefs) > 0 {
		owner := "another object"
		if ownedRefs[0].AttachedObjectType != nil && ownedRefs[0].AttachedObjectID != nil {
			owner = fmt.Sprintf("%s/%d", *ownedRefs[0].AttachedObjectType, *ownedRefs[0].AttachedObjectID)
		}
		return util.NewBadRequestError(fmt.Sprintf(
			"object is owned by %s and cannot be updated externally; tear down the owner first",
			owner,
		))
	}
	return nil
}

// processRelationshipTaggedFieldsAfterUpdate inserts an attached object
// reference for each foreign key that just transitioned from nil to a value.
// Other transitions are rejected by BeforeUpdate.
func processRelationshipTaggedFieldsAfterUpdate(tx *gorm.DB, obj interface{}) error {
	foreignKeys := foreignKeysFor(obj)
	if len(foreignKeys) == 0 {
		return nil
	}
	objType := util.ObjectTypeName(obj)
	objID := util.ObjectID(obj)
	if objID == nil {
		return nil
	}
	for _, foreignKey := range foreignKeys {
		if foreignKey.objectID == nil || !tx.Statement.Changed(foreignKey.fieldName) {
			continue
		}
		if err := insertAttachedObjectReference(tx, foreignKey, objType, *objID); err != nil {
			return err
		}
	}
	return nil
}

// processRelationshipTaggedFieldsBeforeDelete rejects deletion when an
// incoming reference blocks it, then deletes outgoing references for this
// row.
func processRelationshipTaggedFieldsBeforeDelete(tx *gorm.DB, obj interface{}) error {
	objType := util.ObjectTypeName(obj)
	objID := util.ObjectID(obj)
	if objID == nil {
		return nil
	}

	blockingRefs, err := findBlockingAttachedObjectReferences(tx, objType, objID)
	if err != nil {
		return err
	}
	if len(blockingRefs) > 0 {
		return util.NewConflictError(
			formatBlockingAttachedObjectReferencesError(blockingRefs).Error(),
		)
	}

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

// findBlockingAttachedObjectReferences returns attached object references
// that target the given object with a relationship that blocks deletion
// ("requires" or "owns"). "describes" references are excluded.
func findBlockingAttachedObjectReferences(
	db *gorm.DB,
	objectType string,
	objectID *uint,
) ([]AttachedObjectReference, error) {
	var attachedObjectReferences []AttachedObjectReference
	if err := db.
		Where(
			"object_type = ? AND object_id = ? AND relationship IN ?",
			objectType, objectID, []string{sdk.RelationshipRequires, sdk.RelationshipOwns},
		).
		Find(&attachedObjectReferences).Error; err != nil {
		return nil, fmt.Errorf("failed to list blocking attached object references: %w", err)
	}
	return attachedObjectReferences, nil
}

// formatBlockingAttachedObjectReferencesError returns an error describing the
// blocking attached object references as an aligned two-column table,
// suitable for a 409 response body.
func formatBlockingAttachedObjectReferencesError(
	attachedObjectReferences []AttachedObjectReference,
) error {
	baseType := "object"
	if len(attachedObjectReferences) > 0 && attachedObjectReferences[0].ObjectType != nil {
		baseType = strcase.ToDelimited(*attachedObjectReferences[0].ObjectType, ' ')
	}

	var buf bytes.Buffer
	writer := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "  TYPE\tID")
	for _, attachedObjectReference := range attachedObjectReferences {
		attachedObjectType := "<unknown>"
		attachedObjectID := "<unknown>"
		if attachedObjectReference.AttachedObjectType != nil {
			attachedObjectType = *attachedObjectReference.AttachedObjectType
		}
		if attachedObjectReference.AttachedObjectID != nil {
			attachedObjectID = fmt.Sprintf("%d", *attachedObjectReference.AttachedObjectID)
		}
		fmt.Fprintf(writer, "  %s\t%s\n", attachedObjectType, attachedObjectID)
	}
	writer.Flush()

	msg := fmt.Sprintf(
		"%s cannot be deleted while %d object(s) still reference it:\n\n%s\nRemove dependents first.",
		baseType, len(attachedObjectReferences), buf.String(),
	)
	return errors.New(msg)
}
