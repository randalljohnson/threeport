package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture builds a *Generator with the given per-object data populated on a
// single synthetic ApiObjectGroup. structTags, structEmbeds, and fieldTypes
// describe the model objects under test; embedTypes describes any base types
// they embed (e.g. Common, Instance) that the validator needs to flatten in.
// Only the fields ValidateTags reads are populated.
func fixture(
	structTags map[string]map[string]map[string]string,
	structEmbeds map[string][]string,
	fieldTypes map[string]map[string]string,
	embedTypes map[string]map[string]map[string]string,
) *Generator {
	apiObjects := make([]*ApiObject, 0, len(structTags))
	for name := range structTags {
		apiObjects = append(apiObjects, &ApiObject{TypeName: name})
	}
	return &Generator{
		ApiObjectGroups: []ApiObjectGroup{{
			ApiObjects:   apiObjects,
			StructTags:   structTags,
			StructEmbeds: structEmbeds,
			FieldTypes:   fieldTypes,
		}},
		EmbedTypes: embedTypes,
	}
}

// tag is a terse helper for fixture tag maps. Pass alternating key/value
// pairs: tag("validate", "required", "json", ",omitempty").
func tag(kv ...string) map[string]string {
	m := make(map[string]string, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

// TestValidateTags_Valid covers a fully compliant struct.
// Example:
//
//	type Foo struct {
//	    Name *string `json:",omitempty" validate:"required"`
//	}
//
// Expected: no error.
func TestValidateTags_Valid(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"Name": tag("json", ",omitempty", "validate", "required")},
		},
		nil, nil, nil,
	)
	assert.NoError(t, g.ValidateTags())
}

// TestValidateTags_ValidateRequiredMissingOmitempty enforces the omitempty
// pairing rule.
// Example:
//
//	type Foo struct {
//	    Name *string `validate:"required"`
//	}
//
// Expected: error citing "Foo.Name" requires `json:",omitempty"`.
func TestValidateTags_ValidateRequiredMissingOmitempty(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"Name": tag("validate", "required")},
		},
		nil, nil, nil,
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Foo.Name")
	assert.Contains(t, err.Error(), "omitempty")
}

// TestValidateTags_BogusValidateValue rejects unknown validate-tag values.
// Example:
//
//	type Foo struct {
//	    Name *string `json:",omitempty" validate:"mandatory"`
//	}
//
// Expected: error citing the invalid value "mandatory".
func TestValidateTags_BogusValidateValue(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"Name": tag("json", ",omitempty", "validate", "mandatory")},
		},
		nil, nil, nil,
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mandatory")
}

// TestValidateTags_BogusEncryptValue rejects encrypt-tag values other than
// "true".
// Example:
//
//	type Foo struct {
//	    Token *string `json:",omitempty" validate:"required" encrypt:"false"`
//	}
//
// Expected: error citing encrypt:"false".
func TestValidateTags_BogusEncryptValue(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"Token": tag("json", ",omitempty", "validate", "required", "encrypt", "false")},
		},
		nil, nil, nil,
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encrypt")
}

// TestValidateTags_QueryTagInvalidPattern rejects query-tag values that
// aren't lowercase alphanumerics.
// Example:
//
//	type Foo struct {
//	    Name *string `json:",omitempty" query:"NotAllowed" validate:"required"`
//	}
//
// Expected: error citing the invalid query value.
func TestValidateTags_QueryTagInvalidPattern(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"Name": tag("json", ",omitempty", "validate", "required", "query", "NotAllowed")},
		},
		nil, nil, nil,
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NotAllowed")
}

// TestValidateTags_PersistTagInvalidValue rejects any persist-tag value
// other than "false".
// Example:
//
//	type Foo struct {
//	    X *string `json:",omitempty" persist:"true" validate:"optional"`
//	}
//
// Expected: error citing persist:"true".
func TestValidateTags_PersistTagInvalidValue(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"X": tag("json", ",omitempty", "validate", "optional", "persist", "true")},
		},
		nil, nil, nil,
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist")
}

// TestValidateTags_TwoSiblingsSameQueryTag catches two directly-declared
// fields that share an explicit query-tag value.
// Example:
//
//	type Foo struct {
//	    A *string `json:",omitempty" query:"x" validate:"optional"`
//	    B *string `json:",omitempty" query:"x" validate:"optional"`
//	}
//
// Expected: collision on key "x".
func TestValidateTags_TwoSiblingsSameQueryTag(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {
				"A": tag("json", ",omitempty", "validate", "optional", "query", "x"),
				"B": tag("json", ",omitempty", "validate", "optional", "query", "x"),
			},
		},
		nil, nil, nil,
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `effective query key "x" collides`)
	assert.Contains(t, err.Error(), "Foo.A and Foo.B")
}

// TestValidateTags_OverrideCollidesWithLiteralFieldName catches an override
// that clashes with another field's default lowercased key.
// Example:
//
//	type Foo struct {
//	    Name   *string `json:",omitempty" validate:"required"`
//	    Legacy *string `json:",omitempty" query:"name" validate:"optional"`
//	}
//
// Expected: collision on key "name".
func TestValidateTags_OverrideCollidesWithLiteralFieldName(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {
				"Name":   tag("json", ",omitempty", "validate", "required"),
				"Legacy": tag("json", ",omitempty", "validate", "optional", "query", "name"),
			},
		},
		nil, nil, nil,
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `effective query key "name" collides`)
}

// TestValidateTags_TwoFieldsLowercaseToSameKey catches two exported field
// names that lowercase to the same key.
// Example:
//
//	type Foo struct {
//	    URL *string `json:",omitempty" validate:"optional"`
//	    Url *string `json:",omitempty" validate:"optional"`
//	}
//
// Expected: collision on key "url".
func TestValidateTags_TwoFieldsLowercaseToSameKey(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {
				"URL": tag("json", ",omitempty", "validate", "optional"),
				"Url": tag("json", ",omitempty", "validate", "optional"),
			},
		},
		nil, nil, nil,
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `effective query key "url" collides`)
}

// TestValidateTags_OuterShadowsEmbedAllowed treats outer-name == embed-name
// as the deliberate Go shadowing pattern and passes silently.
// Example:
//
//	type Common struct {
//	    ID *uint `json:",omitempty"`
//	}
//	type Foo struct {
//	    Common
//	    ID *uint `json:",omitempty" validate:"optional"`  // shadows Common.ID
//	}
//
// Expected: no error (the outer wins at both bind time and source access).
func TestValidateTags_OuterShadowsEmbedAllowed(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"ID": tag("json", ",omitempty", "validate", "optional")},
		},
		map[string][]string{"Foo": {"Common"}},
		nil,
		map[string]map[string]map[string]string{
			"Common": {"ID": tag("json", ",omitempty")},
		},
	)
	assert.NoError(t, g.ValidateTags())
}

// TestValidateTags_OverrideCollidesWithEmbedDefaultKey catches a directly-
// declared field whose tag override matches an embedded field's default key
// without being a Go shadow of it.
// Example:
//
//	type Common struct {
//	    ID *uint `json:",omitempty"`
//	}
//	type Foo struct {
//	    Common
//	    MyField *uint `json:",omitempty" query:"id" validate:"optional"`
//	}
//
// Expected: collision on key "id".
func TestValidateTags_OverrideCollidesWithEmbedDefaultKey(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"MyField": tag("json", ",omitempty", "validate", "optional", "query", "id")},
		},
		map[string][]string{"Foo": {"Common"}},
		nil,
		map[string]map[string]map[string]string{
			"Common": {"ID": tag("json", ",omitempty")},
		},
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `effective query key "id" collides`)
}

// TestValidateTags_TwoEmbeddedFieldsCollide catches two embedded fields
// whose effective keys collide.
// Example:
//
//	type A struct {
//	    X *string `json:",omitempty" validate:"required"`
//	}
//	type B struct {
//	    X *string `json:",omitempty" validate:"required"`
//	}
//	type Foo struct {
//	    A
//	    B
//	}
//
// Expected: collision on key "x".
func TestValidateTags_TwoEmbeddedFieldsCollide(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {},
		},
		map[string][]string{"Foo": {"A", "B"}},
		nil,
		map[string]map[string]map[string]string{
			"A": {"X": tag("json", ",omitempty", "validate", "required")},
			"B": {"X": tag("json", ",omitempty", "validate", "required")},
		},
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `effective query key "x" collides`)
}

// TestValidateTags_BogusRelationshipKind rejects relationship-tag values
// whose kind isn't a known relationship type.
// Example:
//
//	type Foo struct {
//	    BarID *uint `json:",omitempty" validate:"required" relationship:"unknown"`
//	}
//
// Expected: error citing the invalid kind.
func TestValidateTags_BogusRelationshipKind(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"BarID": tag("json", ",omitempty", "validate", "required", "relationship", "unknown")},
		},
		nil,
		map[string]map[string]string{
			"Foo": {"BarID": "*uint"},
		},
		nil,
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
}
