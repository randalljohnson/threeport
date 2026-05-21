package v0

import (
	"errors"
	"fmt"
	"reflect"
)

// ObjectID returns the *uint ID field of an API object, or nil if missing
// or unset.
func ObjectID(obj interface{}) *uint {
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

// ObjectTypeName returns the package-qualified type name of the dereferenced
// object, so a value and its pointer produce the same string.
func ObjectTypeName(obj interface{}) string {
	t := reflect.TypeOf(obj)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.String()
}

// GetPtrValue returns the string value of a pointer field.
func GetPtrValue(field reflect.Value) (string, error) {
	if !IsNonNilPtr(field) {
		return "", nil
	}
	createdVal, ok := field.Elem().Interface().(string)
	if !ok {
		return "", errors.New("field value is not a string")
	}
	return createdVal, nil
}

// IsNonNilPtr returns true if the field is a nil pointer.
func IsNonNilPtr(field reflect.Value) bool {
	if field.Kind() != reflect.Ptr {
		return false
	}
	return field.IsNil() == false
}

// GetObjectFieldValue takes a struct object and a field name as a string and
// returns the value for that field from the struct if it exists.  If the value
// of the field is a nil pointer, the string 'no value' will be returned.
func GetObjectFieldValue(
	object interface{},
	fieldName string,
) (reflect.Value, error) {
	objectVal := reflect.ValueOf(object)

	// dereference if object a non-nil pointer
	if objectVal.Kind() == reflect.Ptr && !objectVal.IsNil() {
		objectVal = objectVal.Elem()
	}

	if objectVal.Kind() == reflect.Struct {
		fieldVal := objectVal.FieldByName(fieldName)
		if !fieldVal.IsValid() {
			return reflect.Value{}, fmt.Errorf("field '%s' not found", fieldName)
		}

		if fieldVal.Kind() == reflect.Ptr {
			if fieldVal.IsNil() {
				return reflect.ValueOf("no value"), nil
			} else {
				return fieldVal.Elem(), nil
			}
		}

		return fieldVal, nil
	}

	return reflect.Value{}, errors.New("provided object not a struct")
}
