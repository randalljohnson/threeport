package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture builds a *Generator with the given per-object data populated on a
// single synthetic ApiObjectGroup. structTags, structEmbeds, and fieldTypes
// describe the model objects under test; embedTypes describes any base types
// they embed (Common, Definition, Instance, Reconciliation) that the
// validator needs to flatten in. Only the fields ValidateTags reads are
// populated.
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

// TestValidateTags_QueryTagRejected rejects any field carrying an
// explicit query tag. URL parameter keys derive from the lowercased Go
// field name, so an override is redundant and a silent rename hazard.
// Example:
//
//	type Foo struct {
//	    Name *string `json:",omitempty" query:"name" validate:"required"`
//	}
//
// Expected: error citing the query tag.
func TestValidateTags_QueryTagRejected(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"Name": tag("json", ",omitempty", "validate", "required", "query", "name")},
		},
		nil, nil, nil,
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query tag is not allowed")
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

// TestValidateTags_RelationshipNonUintField rejects a relationship tag on a
// field whose Go type isn't *uint (relationship-tagged fields are emitted as
// foreign-key columns).
// Example:
//
//	type Foo struct {
//	    BarName *string `json:",omitempty" validate:"required" relationship:"requires"`
//	}
//
// Expected: error citing the wrong field type.
func TestValidateTags_RelationshipNonUintField(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"BarName": tag("json", ",omitempty", "validate", "required", "relationship", "requires")},
		},
		nil,
		map[string]map[string]string{
			"Foo": {"BarName": "*string"},
		},
		nil,
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "*uint")
}

// TestValidateTags_RelationshipUnknownTypeModifier rejects a relationship
// `type:` modifier pointing to a type not in the registered API set.
// Example:
//
//	type Foo struct {
//	    BarID *uint `json:",omitempty" validate:"required" relationship:"requires;type:Ghost"`
//	}
//
// (Ghost is not declared anywhere in the fixture.)
// Expected: error citing "unknown API type".
func TestValidateTags_RelationshipUnknownTypeModifier(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"BarID": tag("json", ",omitempty", "validate", "required", "relationship", "requires;type:Ghost")},
		},
		nil,
		map[string]map[string]string{
			"Foo": {"BarID": "*uint"},
		},
		nil,
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown API type")
}

// TestValidateTags_RelationshipKnownTypeModifier accepts a relationship
// `type:` modifier when the named type is in the registered API set. The
// AOR codegen depends on this happy path: a typo in `type:` should fail
// validation (covered by the test above), but a valid `type:` must pass
// so downstream emitters can read it as the source of truth.
// Example:
//
//	type Foo struct {
//	    SomeID *uint `json:",omitempty" validate:"required" relationship:"requires;type:Bar"`
//	}
//	type Bar struct { ... }
//
// (Bar IS declared in the fixture so it's in knownTypes.)
// Expected: no error.
func TestValidateTags_RelationshipKnownTypeModifier(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"SomeID": tag("json", ",omitempty", "validate", "required", "relationship", "requires;type:Bar")},
			"Bar": {"Name": tag("json", ",omitempty", "validate", "required")},
		},
		nil,
		map[string]map[string]string{
			"Foo": {"SomeID": "*uint"},
		},
		nil,
	)
	assert.NoError(t, g.ValidateTags())
}

// TestValidateTags_RelationshipUnknownModifierKey rejects a relationship
// modifier whose key isn't the supported "type:".
// Example:
//
//	type Foo struct {
//	    BarID *uint `json:",omitempty" validate:"required" relationship:"requires;bogus:Foo"`
//	}
//
// Expected: error citing the unknown modifier key.
func TestValidateTags_RelationshipUnknownModifierKey(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"BarID": tag("json", ",omitempty", "validate", "required", "relationship", "requires;bogus:Foo")},
		},
		nil,
		map[string]map[string]string{
			"Foo": {"BarID": "*uint"},
		},
		nil,
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown relationship modifier key")
}

// TestValidateTags_RelationshipMalformedModifier rejects a modifier entry
// that doesn't follow the key:value form.
// Example:
//
//	type Foo struct {
//	    BarID *uint `json:",omitempty" validate:"required" relationship:"requires;noColon"`
//	}
//
// Expected: error citing the malformed modifier.
func TestValidateTags_RelationshipMalformedModifier(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"BarID": tag("json", ",omitempty", "validate", "required", "relationship", "requires;noColon")},
		},
		nil,
		map[string]map[string]string{
			"Foo": {"BarID": "*uint"},
		},
		nil,
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed relationship modifier")
}

// TestValidateTags_ModuleModeAllowsUnknownType confirms module mode skips
// the "unknown API type" check for relationship type modifiers. Modules
// reference types declared in the imported threeport core package, which
// the Go compiler will verify at build time, so the codegen-side check is
// intentionally relaxed.
// Example: same as TestValidateTags_RelationshipUnknownTypeModifier above,
// but with g.Module = true.
//
// Expected: no error.
func TestValidateTags_ModuleModeAllowsUnknownType(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {"BarID": tag("json", ",omitempty", "validate", "required", "relationship", "requires;type:Ghost")},
		},
		nil,
		map[string]map[string]string{
			"Foo": {"BarID": "*uint"},
		},
		nil,
	)
	g.Module = true
	assert.NoError(t, g.ValidateTags())
}

// TestValidateTags_StableErrorOrdering verifies the validator's output is
// deterministic across runs. Map iteration over StructTags and EmbedTypes
// is randomized by Go; the trailing sort.Strings(problems) normalizes the
// final message. A regression that removes the sort would surface here.
// Example: two unrelated validate-tag errors. Running ValidateTags multiple
// times must produce the same error string.
func TestValidateTags_StableErrorOrdering(t *testing.T) {
	build := func() *Generator {
		return fixture(
			map[string]map[string]map[string]string{
				"Alpha": {"X": tag("json", ",omitempty", "validate", "wrong1")},
				"Beta":  {"Y": tag("json", ",omitempty", "validate", "wrong2")},
			},
			nil, nil, nil,
		)
	}
	first := build().ValidateTags().Error()
	for i := 0; i < 25; i++ {
		assert.Equal(t, first, build().ValidateTags().Error(),
			"problem-message ordering must be stable across runs")
	}
}

// TestValidateTags_RejectsNonStandardEmbed enforces the api-type embed
// whitelist. Models may only embed Common, Definition, Instance, or
// Reconciliation; anything else surfaces an error so model authors and
// readers can rely on a flat, one-level embed model.
// Example:
//
//	type Random struct { ... }
//	type Foo struct {
//	    Random // not in {Common, Definition, Instance, Reconciliation}
//	}
//
// Expected: error citing the disallowed embed.
func TestValidateTags_RejectsNonStandardEmbed(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {},
		},
		map[string][]string{"Foo": {"Random"}},
		nil,
		map[string]map[string]map[string]string{
			"Random": {"X": tag("json", ",omitempty", "validate", "optional")},
		},
	)
	err := g.ValidateTags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Foo embeds Random")
	assert.Contains(t, err.Error(), "Common, Definition, Instance, or Reconciliation")
}

// TestHasFieldWithTagValue_Match returns true when any field on the
// object carries the requested tag value. Mirrors the persist:"false"
// case that drives the reconciler's skip-getLatest behavior.
func TestHasFieldWithTagValue_Match(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {
				"Name": tag("json", ",omitempty", "validate", "required"),
				"Data": tag("json", ",omitempty", "validate", "required", "persist", "false"),
			},
		},
		nil, nil, nil,
	)
	assert.True(t, g.ApiObjectGroups[0].HasFieldWithTagValue("Foo", "persist", "false"))
}

// TestHasFieldWithTagValue_FieldNameAgnostic confirms the search is
// field-name-agnostic: persist:"false" on a non-Data field is still
// found. This is the behavioral difference from CheckStructTagMap,
// which required the caller to name the field.
func TestHasFieldWithTagValue_FieldNameAgnostic(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {
				"Payload": tag("json", ",omitempty", "validate", "required", "persist", "false"),
			},
		},
		nil, nil, nil,
	)
	assert.True(t, g.ApiObjectGroups[0].HasFieldWithTagValue("Foo", "persist", "false"))
}

// TestHasFieldWithTagValue_NoMatch covers the negative cases: missing
// object, missing tag, and present tag with a different value all
// return false.
func TestHasFieldWithTagValue_NoMatch(t *testing.T) {
	g := fixture(
		map[string]map[string]map[string]string{
			"Foo": {
				"Name": tag("json", ",omitempty", "validate", "required"),
				"Data": tag("json", ",omitempty", "validate", "required", "persist", "true"),
			},
		},
		nil, nil, nil,
	)
	group := g.ApiObjectGroups[0]
	assert.False(t, group.HasFieldWithTagValue("Bar", "persist", "false"), "missing object")
	assert.False(t, group.HasFieldWithTagValue("Foo", "encrypt", "true"), "tag key absent")
	assert.False(t, group.HasFieldWithTagValue("Foo", "persist", "false"), "tag present with wrong value")
}

// indexOf reports the position of name in names, or -1 when absent.
func indexOf(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
}

// sortFixture builds a *Generator carrying only the relationship-dependency
// graph the migration sort reads. Keys are referencing types; values are the
// types their foreign keys reference.
func sortFixture(dependencies map[string][]string) *Generator {
	return &Generator{RelationshipDependencies: dependencies}
}

// TestSortDatabaseInitNamesByDependency_ReferencedBeforeReferencing covers
// the core ordering guarantee: a type whose foreign key references another
// type in the list is emitted after the type it references.
//
// The fixture supplies the dependency graph directly, as a map from each type
// to the types it references. Deriving that graph from model source is a
// separate step this test does not reach.
//
// Expected: Parent precedes Child even though it sorts later alphabetically.
func TestSortDatabaseInitNamesByDependency_ReferencedBeforeReferencing(t *testing.T) {
	// Child holds the foreign key, so it depends on Parent
	g := sortFixture(map[string][]string{
		"Child": {"Parent"},
	})

	// sort the list passed in declaration order, referencing type first
	sorted := g.SortDatabaseInitNamesByDependency([]string{
		"Child",
		"Parent",
	})

	// the referenced table must land ahead of the table that references it
	parentIdx := indexOf(sorted, "Parent")
	childIdx := indexOf(sorted, "Child")
	require.NotEqual(t, -1, parentIdx)
	require.NotEqual(t, -1, childIdx)
	assert.Less(t, parentIdx, childIdx, "referenced table must precede referencing table")
}

// TestSortDatabaseInitNamesByDependency_DropsDuplicateNames covers a repeated
// entry in the input: it is emitted once and no other name is lost.
//
// Before the input was deduplicated, the emit loop ran until it had emitted as
// many names as it was handed. A repeat left it one short, the cycle fallback
// found nothing remaining, and the result came back missing a table that the
// generated AutoMigrate call would then never create.
func TestSortDatabaseInitNamesByDependency_DropsDuplicateNames(t *testing.T) {
	// B is listed twice and A depends on it
	g := sortFixture(map[string][]string{
		"A": {"B"},
	})

	sorted := g.SortDatabaseInitNamesByDependency([]string{"A", "B", "B"})

	// both distinct tables survive, in dependency order, with no repeat
	assert.Equal(t, []string{"B", "A"}, sorted)
}

// TestSortDatabaseInitNamesByDependency_TransitiveChain covers a multi-hop
// chain: A references B and B references C, so the emitted order must be
// C, B, A regardless of input order.
func TestSortDatabaseInitNamesByDependency_TransitiveChain(t *testing.T) {
	// A depends on B, B depends on C, forming a three-link chain
	g := sortFixture(map[string][]string{
		"A": {"B"},
		"B": {"C"},
	})

	// pass the chain in reverse dependency order
	sorted := g.SortDatabaseInitNamesByDependency([]string{"A", "B", "C"})

	// the leaf table comes first, the root table last
	assert.Equal(t, []string{"C", "B", "A"}, sorted)
}

// TestSortDatabaseInitNamesByDependency_IgnoresExternalReference covers a
// foreign key that points at a type not in the migration list: the edge is
// dropped so external references do not perturb the ordering.
//
// The fixture supplies the dependency graph directly, so Local depends on
// ExternalThing and ExternalThing never appears in the list being sorted.
//
// Expected: with only Local and Other in the list, alphabetical order holds.
func TestSortDatabaseInitNamesByDependency_IgnoresExternalReference(t *testing.T) {
	// Local references a type that is not part of the migration list
	g := sortFixture(map[string][]string{
		"Local": {"ExternalThing"},
	})

	// the referenced ExternalThing is absent from the list passed in
	sorted := g.SortDatabaseInitNamesByDependency([]string{"Other", "Local"})

	// with no in-list edge, the deterministic alphabetical tie-break applies
	assert.Equal(t, []string{"Local", "Other"}, sorted)
}

// TestSortDatabaseInitNamesByDependency_Deterministic covers tie-breaking:
// independent types come out in alphabetical order regardless of input order
// so the generated migration is stable across runs.
func TestSortDatabaseInitNamesByDependency_Deterministic(t *testing.T) {
	// three types with no relationship edges between them
	g := sortFixture(map[string][]string{})

	// pass the names in non-alphabetical order
	sorted := g.SortDatabaseInitNamesByDependency([]string{"Charlie", "Bravo", "Alpha"})

	// independent types come out alphabetically sorted
	assert.Equal(t, []string{"Alpha", "Bravo", "Charlie"}, sorted)
}

// TestSortDatabaseInitNamesByDependency_CycleFallback covers a true
// dependency cycle: rather than failing, the remaining cyclic names are
// appended in alphabetical order so generation still produces deterministic
// output.
func TestSortDatabaseInitNamesByDependency_CycleFallback(t *testing.T) {
	// two types reference each other, forming a cycle the sort cannot break
	g := sortFixture(map[string][]string{
		"Yin":  {"Yang"},
		"Yang": {"Yin"},
	})

	// neither type can be emitted first, triggering the cycle fallback
	sorted := g.SortDatabaseInitNamesByDependency([]string{"Yin", "Yang"})

	// the fallback emits the cyclic names in deterministic alphabetical order
	assert.Equal(t, []string{"Yang", "Yin"}, sorted)
}
