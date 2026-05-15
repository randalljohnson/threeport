package v0

import (
	"github.com/iancoleman/strcase"
)

// FieldName is a Go struct field name on an API type. The Column method
// derives the GORM column name via the default snake-case naming strategy.
type FieldName string

// Column returns the GORM column for this struct field name.
func (f FieldName) Column() string {
	return strcase.ToSnake(string(f))
}

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

// EncryptedField describes an encrypt-tagged field on an API type. The SDK
// generates an EncryptedFields method per type that returns one entry per
// field, so runtime hooks read the list without walking struct tags.
type EncryptedField struct {
	Name  FieldName
	Value interface{} // *string or []string of KEY=VALUE
}

// EncryptedFieldProvider is implemented by every API type with at least one
// encrypt-tagged field.
type EncryptedFieldProvider interface {
	EncryptedFields() []EncryptedField
}

// RelationshipTaggedForeignKey describes a *uint ID field on an API type
// tagged with a `relationship:` value. The SDK generates a
// RelationshipTaggedForeignKeys method per type that returns one entry
// per such field, so runtime hooks read the list directly instead of
// walking struct tags via reflection.
type RelationshipTaggedForeignKey struct {
	FieldName    FieldName
	ObjectType   string // e.g. "WorkloadInstance"
	Relationship Relationship
	ObjectID     *uint
}

// RelationshipTaggedForeignKeyProvider is implemented by every API type
// with at least one relationship-tagged foreign key.
type RelationshipTaggedForeignKeyProvider interface {
	RelationshipTaggedForeignKeys() []RelationshipTaggedForeignKey
}
