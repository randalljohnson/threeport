package v0

import (
	"fmt"
	"reflect"
	"strings"

	"gorm.io/gorm"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// Relationship tag values.
const (
	RelationshipOwns     = "owns"
	RelationshipRequires = "requires"
)

// relationshipFK describes one *uint <Target>ID field on an API object that
// is tagged `relationship:"owns"` or `relationship:"requires"`.
type relationshipFK struct {
	fieldName    string
	targetType   string // e.g. "WorkloadInstance"
	relationship string // "owns" or "requires"
	value        *uint
}

// findRelationshipFKs walks the struct fields of obj and returns each *uint
// field tagged `relationship:`. Returns an error if a tag value is invalid
// or the field shape does not match the FK convention.
func findRelationshipFKs(obj interface{}) ([]relationshipFK, error) {
	objVal := reflect.ValueOf(obj)
	if objVal.Kind() == reflect.Ptr {
		objVal = objVal.Elem()
	}
	if objVal.Kind() != reflect.Struct {
		return nil, nil
	}
	objType := objVal.Type()

	var fks []relationshipFK
	for i := 0; i < objType.NumField(); i++ {
		field := objType.Field(i)
		rel := field.Tag.Get("relationship")
		if rel == "" {
			continue
		}
		if rel != RelationshipOwns && rel != RelationshipRequires {
			return nil, fmt.Errorf(
				"field %s has invalid relationship tag %q: expected %q or %q",
				field.Name, rel, RelationshipOwns, RelationshipRequires,
			)
		}
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
		var v *uint
		fieldVal := objVal.Field(i)
		if !fieldVal.IsNil() {
			v = fieldVal.Interface().(*uint)
		}
		fks = append(fks, relationshipFK{
			fieldName:    field.Name,
			targetType:   strings.TrimSuffix(field.Name, "ID"),
			relationship: rel,
			value:        v,
		})
	}
	return fks, nil
}

// objectID extracts the ID field of an API object via reflection.
func objectID(obj interface{}) *uint {
	objVal := reflect.ValueOf(obj)
	if objVal.Kind() == reflect.Ptr {
		objVal = objVal.Elem()
	}
	idField := objVal.FieldByName("ID")
	if !idField.IsValid() || idField.Kind() != reflect.Ptr || idField.IsNil() {
		return nil
	}
	return idField.Interface().(*uint)
}

// objectTypeName returns the package-qualified type name of the dereferenced
// API object (e.g. "v0.SecretInstance").
func objectTypeName(obj interface{}) string {
	t := reflect.TypeOf(obj)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.String()
}

// targetTypeName converts an FK target like "WorkloadInstance" into a
// package-qualified type name by reusing the receiver's package prefix.
func targetTypeName(receiverType, fkTarget string) string {
	if i := strings.LastIndex(receiverType, "."); i >= 0 {
		return receiverType[:i+1] + fkTarget
	}
	return fkTarget
}

// insertBlockingAOR creates a blocking attached object reference for the
// given relationship FK. Both `owns` and `requires` are immutable once set,
// so this only ever runs for fresh edges — no lookup-and-update is needed.
func insertBlockingAOR(tx *gorm.DB, fk relationshipFK, attachedID *uint, attachedType string) error {
	targetType := targetTypeName(attachedType, fk.targetType)
	blocking := true
	if err := tx.Create(&AttachedObjectReference{
		ObjectID:           fk.value,
		ObjectType:         &targetType,
		AttachedObjectID:   attachedID,
		AttachedObjectType: &attachedType,
		Blocking:           &blocking,
	}).Error; err != nil {
		return fmt.Errorf(
			"failed to create attached object reference for %s.%s: %w",
			attachedType, fk.fieldName, err,
		)
	}
	return nil
}

// processRelationshipTaggedFieldsAfterCreate inserts blocking AORs for each
// relationship-tagged FK with a non-nil value.
func processRelationshipTaggedFieldsAfterCreate(tx *gorm.DB, obj interface{}) error {
	fks, err := findRelationshipFKs(obj)
	if err != nil {
		return err
	}
	if len(fks) == 0 {
		return nil
	}
	objType := objectTypeName(obj)
	objID := objectID(obj)
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
		if err := insertBlockingAOR(tx, fk, objID, objType); err != nil {
			return err
		}
	}
	return nil
}

// processRelationshipTaggedFieldsBeforeUpdate rejects updates that change any
// non-nil relationship-tagged FK. Both `owns` and `requires` are immutable
// once set; the only allowed transition is the initial nil → non-nil first
// set (handled in AfterUpdate). To "undo" a relationship, the holder object
// must be deleted.
func processRelationshipTaggedFieldsBeforeUpdate(tx *gorm.DB, obj interface{}) error {
	oldFKs, err := findRelationshipFKs(obj)
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
	return nil
}

// processRelationshipTaggedFieldsAfterUpdate inserts blocking AORs for each
// relationship-tagged FK that just transitioned from nil to a value. Other
// transitions are blocked by BeforeUpdate.
func processRelationshipTaggedFieldsAfterUpdate(tx *gorm.DB, obj interface{}) error {
	fks, err := findRelationshipFKs(obj)
	if err != nil {
		return err
	}
	if len(fks) == 0 {
		return nil
	}
	objType := objectTypeName(obj)
	objID := objectID(obj)
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
		if err := insertBlockingAOR(tx, fk, objID, objType); err != nil {
			return err
		}
	}
	return nil
}

// processRelationshipTaggedFieldsBeforeDelete blocks deletion when any blocking AORs
// reference this object, then cascade-deletes outgoing AORs where this row
// is the attached object.
func processRelationshipTaggedFieldsBeforeDelete(tx *gorm.DB, obj interface{}) error {
	objType := objectTypeName(obj)
	objID := objectID(obj)
	if objID == nil {
		return nil
	}

	// block delete if any blocking AOR points at this row as its target
	blockingRefs, err := FindBlockingAttachedObjectReferences(tx, objType, objID)
	if err != nil {
		return err
	}
	if len(blockingRefs) > 0 {
		return util.NewConflictError(
			FormatBlockingAttachedObjectReferencesError(blockingRefs).Error(),
		)
	}

	// cascade-delete outgoing AORs (this row is the attached object)
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
