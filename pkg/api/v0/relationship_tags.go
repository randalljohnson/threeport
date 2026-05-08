package v0

import (
	"fmt"
	"reflect"
	"strings"

	"gorm.io/gorm"

	sdk "github.com/threeport/threeport/pkg/sdk/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// relationshipForeignKey describes one *uint <Target>ID field on an API
// object that is tagged `relationship:"owns"` or `relationship:"requires"`.
type relationshipForeignKey struct {
	fieldName    string
	targetType   string // e.g. "WorkloadInstance"
	relationship string // "owns" or "requires"
	value        *uint
}

// findRelationshipForeignKeys returns each *uint <Target>ID field on obj
// tagged with `relationship:"<kind>[;type:<TypeName>]"`.
func findRelationshipForeignKeys(obj interface{}) ([]relationshipForeignKey, error) {
	objVal := reflect.ValueOf(obj)
	if objVal.Kind() == reflect.Ptr {
		objVal = objVal.Elem()
	}
	if objVal.Kind() != reflect.Struct {
		return nil, nil
	}
	objType := objVal.Type()

	var fks []relationshipForeignKey
	for i := 0; i < objType.NumField(); i++ {
		field := objType.Field(i)
		rel := field.Tag.Get(sdk.RelationshipTag)
		if rel == "" {
			continue
		}
		// the FK convention is <TargetType>ID *uint; reject anything else so
		// a typo in the field name fails loudly instead of producing a wrong
		// target type
		if !strings.HasSuffix(field.Name, "ID") {
			return nil, fmt.Errorf(
				"field %s has relationship tag but does not end in 'ID'",
				field.Name,
			)
		}
		if field.Type.Kind() != reflect.Ptr || field.Type.Elem().Kind() != reflect.Uint {
			return nil, fmt.Errorf(
				"field %s has relationship tag but is not *uint",
				field.Name,
			)
		}

		// grammar: <kind>[;<key>:<value>...] — first part is the kind, rest
		// are modifiers
		parts := strings.Split(rel, ";")
		kind := parts[0]
		if kind != sdk.RelationshipOwns && kind != sdk.RelationshipRequires {
			return nil, fmt.Errorf(
				"field %s has invalid relationship tag %q: expected %q or %q",
				field.Name, kind, sdk.RelationshipOwns, sdk.RelationshipRequires,
			)
		}
		// default target = field name minus "ID"; the type modifier
		// overrides for fields whose name doesn't match the target struct
		targetType := strings.TrimSuffix(field.Name, "ID")
		for _, p := range parts[1:] {
			k, v, ok := strings.Cut(p, ":")
			if !ok {
				return nil, fmt.Errorf(
					"field %s has malformed relationship modifier %q: expected key:value",
					field.Name, p,
				)
			}
			switch k {
			case sdk.RelationshipTypeKey:
				targetType = v
			default:
				return nil, fmt.Errorf(
					"field %s has unknown relationship modifier %q", field.Name, k,
				)
			}
		}

		var v *uint
		fieldVal := objVal.Field(i)
		if !fieldVal.IsNil() {
			v = fieldVal.Interface().(*uint)
		}
		fks = append(fks, relationshipForeignKey{
			fieldName:    field.Name,
			targetType:   targetType,
			relationship: kind,
			value:        v,
		})
	}
	return fks, nil
}

// insertAttachedObjectReference creates an attached object reference for fk.
// Both `owns` and `requires` edges are immutable once set, so this only ever
// runs on fresh edges.
func insertAttachedObjectReference(tx *gorm.DB, fk relationshipForeignKey, attachedID *uint, attachedType string) error {
	targetType := util.TargetTypeName(attachedType, fk.targetType)
	relationship := fk.relationship
	if err := tx.Create(&AttachedObjectReference{
		ObjectID:           fk.value,
		ObjectType:         &targetType,
		AttachedObjectID:   attachedID,
		AttachedObjectType: &attachedType,
		Relationship:       &relationship,
	}).Error; err != nil {
		return fmt.Errorf(
			"failed to create attached object reference for %s.%s: %w",
			attachedType, fk.fieldName, err,
		)
	}
	return nil
}

// processRelationshipTaggedFieldsAfterCreate inserts attached object
// references for each relationship-tagged foreign key with a non-nil value.
func processRelationshipTaggedFieldsAfterCreate(tx *gorm.DB, obj interface{}) error {
	fks, err := findRelationshipForeignKeys(obj)
	if err != nil {
		return err
	}
	if len(fks) == 0 {
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
	for _, fk := range fks {
		if fk.value == nil {
			continue
		}
		if err := insertAttachedObjectReference(tx, fk, objID, objType); err != nil {
			return err
		}
	}
	return nil
}

// processRelationshipTaggedFieldsBeforeUpdate rejects updates that would
// mutate a non-nil relationship-tagged FK on the row, and rejects updates to
// any row that is owned by another object via an incoming `owns` reference.
func processRelationshipTaggedFieldsBeforeUpdate(tx *gorm.DB, obj interface{}) error {
	oldFKs, err := findRelationshipForeignKeys(obj)
	if err != nil {
		return err
	}
	for _, fk := range oldFKs {
		if fk.value == nil {
			continue
		}
		if !tx.Statement.Changed(fk.fieldName) {
			continue
		}
		return util.NewBadRequestError(fmt.Sprintf(
			"%s is immutable; tear down and recreate the holder to undo this relationship",
			fk.fieldName,
		))
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

// processRelationshipTaggedFieldsAfterUpdate inserts attached object
// references for each relationship-tagged foreign key that just transitioned
// from nil to a value. Other transitions are blocked by BeforeUpdate.
func processRelationshipTaggedFieldsAfterUpdate(tx *gorm.DB, obj interface{}) error {
	fks, err := findRelationshipForeignKeys(obj)
	if err != nil {
		return err
	}
	if len(fks) == 0 {
		return nil
	}
	objType := util.ObjectTypeName(obj)
	objID := util.ObjectID(obj)
	if objID == nil {
		return nil
	}
	for _, fk := range fks {
		if fk.value == nil {
			continue
		}
		if !tx.Statement.Changed(fk.fieldName) {
			continue
		}
		if err := insertAttachedObjectReference(tx, fk, objID, objType); err != nil {
			return err
		}
	}
	return nil
}

// processRelationshipTaggedFieldsBeforeDelete blocks deletion when any
// blocking attached object reference targets this row, then cascade-deletes
// outgoing references where this row is the attached object.
func processRelationshipTaggedFieldsBeforeDelete(tx *gorm.DB, obj interface{}) error {
	objType := util.ObjectTypeName(obj)
	objID := util.ObjectID(obj)
	if objID == nil {
		return nil
	}

	blockingRefs, err := FindBlockingAttachedObjectReferences(tx, objType, objID)
	if err != nil {
		return err
	}
	if len(blockingRefs) > 0 {
		return util.NewConflictError(
			FormatBlockingAttachedObjectReferencesError(blockingRefs).Error(),
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
