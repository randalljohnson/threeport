package gen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// TestParseRelationshipDependencies_SkipsManyToMany covers a gorm many2many
// pair alongside an ordinary has-many. Both sides of a many2many declare a
// slice of the other, so counting those slices as has-many edges would make
// each side depend on the other and no migration order could satisfy both.
// The keys live in a join table, so the pair contributes no edge at all while
// the plain has-many still keys the child.
func TestParseRelationshipDependencies_SkipsManyToMany(t *testing.T) {
	// tick keeps the struct tags readable inside a Go string literal
	const tick = "`"
	source := "package v0\n\n" +
		"type Left struct {\n" +
		"\tRights []*Right " + tick + `gorm:"many2many:lefts_rights;"` + tick + "\n" +
		"}\n\n" +
		"type Right struct {\n" +
		"\tLefts []*Left " + tick + `gorm:"many2many:lefts_rights;"` + tick + "\n" +
		"}\n\n" +
		"type Parent struct {\n" +
		"\tChildren []*Child\n" +
		"}\n\n" +
		"type Child struct {\n" +
		"\tParentID *uint\n" +
		"}\n"

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "model.go"), []byte(source), 0o600))

	dependencies, err := parseRelationshipDependencies(dir)
	require.NoError(t, err)

	// the many2many pair leaves both sides free to migrate in either order
	assert.NotContains(t, dependencies, "Left")
	assert.NotContains(t, dependencies, "Right")

	// the has-many still puts the key on the child, so Child follows Parent
	assert.Equal(t, []string{"Parent"}, dependencies["Child"])
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

// TestSortDatabaseInitNamesByDependency_CycleFallback covers the defence that
// stays in the sort behind ValidateRelationshipCycles: handed a cyclic graph
// directly, the sort still returns every name, in alphabetical order, rather
// than dropping tables from the migration list.
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

// TestValidateRelationshipCycles_AcyclicGraph covers the passing case: a chain
// and a self-reference both migrate cleanly, so neither is reported. gorm
// creates a table before adding its constraints, which is why a type whose
// foreign key points at itself is not a cycle.
func TestValidateRelationshipCycles_AcyclicGraph(t *testing.T) {
	g := sortFixture(map[string][]string{
		"A":    {"B"},
		"B":    {"C"},
		"Self": {"Self"},
	})

	assert.NoError(t, g.ValidateRelationshipCycles())
}

// TestValidateRelationshipCycles_ReportsCycle covers the failing case: two
// types whose foreign keys point at each other have no valid table-creation
// order, so generation must stop and name both types.
func TestValidateRelationshipCycles_ReportsCycle(t *testing.T) {
	// Yin and Yang each carry a foreign key to the other
	g := sortFixture(map[string][]string{
		"Yin":  {"Yang"},
		"Yang": {"Yin"},
	})

	err := g.ValidateRelationshipCycles()

	// the message must name every type on the cycle so the author can break it
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Yin")
	assert.Contains(t, err.Error(), "Yang")
}

// TestValidateRelationshipCycles_StableCycleReport verifies the reported cycle
// is the same on every run. Map iteration over the dependency graph is
// randomized by Go, so the walk sorts both its roots and each type's edges; a
// regression that drops either sort surfaces here.
func TestValidateRelationshipCycles_StableCycleReport(t *testing.T) {
	build := func() *Generator {
		// two independent cycles, so an unsorted walk could report either
		return sortFixture(map[string][]string{
			"Yin":   {"Yang"},
			"Yang":  {"Yin"},
			"Alpha": {"Beta"},
			"Beta":  {"Alpha"},
		})
	}

	first := build().ValidateRelationshipCycles().Error()
	for i := 0; i < 25; i++ {
		assert.Equal(t, first, build().ValidateRelationshipCycles().Error(),
			"reported cycle must be stable across runs")
	}
}
