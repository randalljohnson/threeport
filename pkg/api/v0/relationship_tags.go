package v0

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/iancoleman/strcase"
	"gorm.io/gorm"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// controllerForType returns the controller name registered for typeKey, or "" if none.
func controllerForType(tx *gorm.DB, typeKey string) (string, error) {
	version, name, ok := strings.Cut(typeKey, ".")
	if !ok {
		return "", nil
	}
	var moduleController ModuleController
	if err := tx.
		Joins(
			"JOIN v0_module_objects ON v0_module_objects.module_controller_id = v0_module_controllers.id",
		).
		Where("v0_module_objects.name = ? AND v0_module_objects.version = ?", name, version).
		First(&moduleController).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("failed to look up controller for type %s: %w", typeKey, err)
	}
	if moduleController.Name == nil {
		return "", nil
	}
	return *moduleController.Name, nil
}

// foreignKeysFor returns the tagged foreign keys of obj, or nil.
func foreignKeysFor(obj interface{}) []ForeignKey {
	p, ok := obj.(ForeignKeyProvider)
	if !ok {
		return nil
	}
	return p.ForeignKeys()
}

// insertAttachedObjectReference creates an AOR row for foreignKey. The
// `owns` and `requires` edges are immutable so this runs only on first set.
func insertAttachedObjectReference(
	tx *gorm.DB,
	foreignKey ForeignKey,
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
	for _, foreignKey := range foreignKeysFor(obj) {
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
		Where("object_type = ? AND object_id = ? AND relationship = ?", objType, objID, RelationshipOwns).
		Find(&ownedRefs).Error; err != nil {
		return fmt.Errorf(
			"failed to look up owning attached object references for %s/%d: %w",
			objType, *objID, err,
		)
	}
	if len(ownedRefs) == 0 {
		return nil
	}

	// owner can update its own state; non-owners cannot
	callerCN := CallerCN(tx.Statement.Context)
	if callerCN != "" {
		for _, ref := range ownedRefs {
			if ref.AttachedObjectType == nil {
				continue
			}
			ownerCN, err := controllerForType(tx, *ref.AttachedObjectType)
			if err != nil {
				return err
			}
			if ownerCN != "" && ownerCN == callerCN {
				return nil
			}
		}
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
		if !tx.Statement.Changed(string(foreignKey.FieldName)) {
			continue
		}
		if foreignKey.ObjectID == nil {
			if err := tx.
				Where(
					"attached_object_type = ? AND attached_object_id = ? AND object_type = ?",
					objType, *objID, foreignKey.ObjectType,
				).
				Delete(&AttachedObjectReference{}).Error; err != nil {
				return fmt.Errorf(
					"failed to remove attached object reference for %s.%s after foreign key cleared: %w",
					objType, foreignKey.FieldName, err,
				)
			}
			continue
		}
		if err := insertAttachedObjectReference(tx, foreignKey, objType, *objID); err != nil {
			return err
		}
	}
	return nil
}

// processRelationshipTaggedFieldsBeforeDelete rejects deletion when an
// incoming reference blocks it, then deletes the row's outgoing references.
func processRelationshipTaggedFieldsBeforeDelete(tx *gorm.DB, obj interface{}) error {
	objType := util.ObjectTypeName(obj)
	objID := util.ObjectID(obj)
	if objID == nil {
		return nil
	}

	callerCN := CallerCN(tx.Statement.Context)
	blockingRefs, err := findBlockingAttachedObjectReferences(tx, objType, objID, callerCN)
	if err != nil {
		return err
	}
	if len(blockingRefs) > 0 {
		return util.NewConflictError(
			formatBlockingAttachedObjectReferencesError(blockingRefs).Error(),
		)
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
// blocks; "owns" blocks unless callerCN matches the owner type's
// registered controller.
func findBlockingAttachedObjectReferences(
	db *gorm.DB,
	objectType string,
	objectID *uint,
	callerCN string,
) ([]AttachedObjectReference, error) {
	var attachedObjectReferences []AttachedObjectReference
	if err := db.
		Where(
			"object_type = ? AND object_id = ? AND relationship IN ?",
			objectType, objectID, []Relationship{RelationshipRequires, RelationshipOwns},
		).
		Find(&attachedObjectReferences).Error; err != nil {
		return nil, fmt.Errorf("failed to list blocking attached object references: %w", err)
	}

	if callerCN == "" {
		return attachedObjectReferences, nil
	}

	var blockingRefs []AttachedObjectReference
	for _, ref := range attachedObjectReferences {
		if ref.Relationship == nil || *ref.Relationship != RelationshipOwns || ref.AttachedObjectType == nil {
			blockingRefs = append(blockingRefs, ref)
			continue
		}
		ownerCN, err := controllerForType(db, *ref.AttachedObjectType)
		if err != nil {
			return nil, err
		}
		if ownerCN == "" || ownerCN != callerCN {
			blockingRefs = append(blockingRefs, ref)
		}
	}
	return blockingRefs, nil
}

// formatBlockingAttachedObjectReferencesError returns an error with the
// blocking attached object references rendered as a two-column 409 body.
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
