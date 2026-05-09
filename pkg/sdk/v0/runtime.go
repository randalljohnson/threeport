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

// EncryptedField describes an encrypt-tagged field on an API type. The SDK
// generates an EncryptedFields method per type that returns one entry per
// field, so runtime hooks read the list directly instead of walking struct
// tags via reflection.
type EncryptedField struct {
	Name  FieldName
	Value interface{} // *string or []string of KEY=VALUE
}

// EncryptedFieldProvider is implemented by every API type with at least one
// encrypt-tagged field.
type EncryptedFieldProvider interface {
	EncryptedFields() []EncryptedField
}

// ForeignKey describes a *uint ID field on an API type tagged
// `relationship:"owns"` or `relationship:"requires"`. The SDK generates a
// ForeignKeys method per type that returns one entry per field, so runtime
// hooks read the list directly instead of walking struct tags via reflection.
type ForeignKey struct {
	FieldName    FieldName
	ObjectType   string // e.g. "WorkloadInstance"
	Relationship string // RelationshipOwns or RelationshipRequires
	ObjectID     *uint
}

// ForeignKeyProvider is implemented by every API type with at least one
// relationship-tagged foreign key.
type ForeignKeyProvider interface {
	ForeignKeys() []ForeignKey
}
