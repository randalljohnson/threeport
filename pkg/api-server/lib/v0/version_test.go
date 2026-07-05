package v0

import (
	"reflect"
	"testing"
)

// resetObjectVersions swaps in a clean map and returns a restore function so
// each test starts from a known-empty state without leaking into siblings.
func resetObjectVersions(t *testing.T) func() {
	t.Helper()
	prev := ObjectVersions
	ObjectVersions = make(map[string]ApiObjectVersions)
	return func() { ObjectVersions = prev }
}

// TestAddObjectVersion_NewObject asserts a first-time object is recorded with
// a single-element Versions slice under its own key.
func TestAddObjectVersion_NewObject(t *testing.T) {
	// isolate the package-level map
	defer resetObjectVersions(t)()

	// action: register a brand-new object/version pair
	AddObjectVersion(VersionObject{Object: "WorkloadInstance", Version: "v0"})

	// assert: map contains exactly one entry keyed by the object name
	if len(ObjectVersions) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(ObjectVersions))
	}
	got, ok := ObjectVersions["WorkloadInstance"]
	if !ok {
		t.Fatalf("expected key %q in ObjectVersions", "WorkloadInstance")
	}
	// assert: recorded API name and Versions slice match the inputs
	if got.API != "WorkloadInstance" {
		t.Errorf("API = %q, want %q", got.API, "WorkloadInstance")
	}
	if !reflect.DeepEqual(got.Versions, []string{"v0"}) {
		t.Errorf("Versions = %v, want %v", got.Versions, []string{"v0"})
	}
}

// TestAddObjectVersion_DuplicateVersionNoop asserts re-adding an identical
// object/version pair leaves the Versions slice unchanged.
func TestAddObjectVersion_DuplicateVersionNoop(t *testing.T) {
	defer resetObjectVersions(t)()

	// setup: seed one entry
	AddObjectVersion(VersionObject{Object: "WorkloadInstance", Version: "v0"})

	// action: add the same pair a second time
	AddObjectVersion(VersionObject{Object: "WorkloadInstance", Version: "v0"})

	// assert: no duplicate version appended
	got := ObjectVersions["WorkloadInstance"].Versions
	if !reflect.DeepEqual(got, []string{"v0"}) {
		t.Errorf("Versions = %v, want %v", got, []string{"v0"})
	}
}

// TestAddObjectVersion_AppendsNewVersion asserts a new version for an existing
// object is appended to the Versions slice.
func TestAddObjectVersion_AppendsNewVersion(t *testing.T) {
	defer resetObjectVersions(t)()

	// setup: seed with v0
	AddObjectVersion(VersionObject{Object: "WorkloadInstance", Version: "v0"})

	// action: add a second version for the same object
	AddObjectVersion(VersionObject{Object: "WorkloadInstance", Version: "v1"})

	// assert: both versions present in insertion order
	got := ObjectVersions["WorkloadInstance"].Versions
	want := []string{"v0", "v1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Versions = %v, want %v", got, want)
	}
}

// TestAddObjectVersion_MultipleObjectsCoexist asserts distinct objects are
// stored under distinct keys without interfering with each other.
func TestAddObjectVersion_MultipleObjectsCoexist(t *testing.T) {
	defer resetObjectVersions(t)()

	// action: register two different objects
	AddObjectVersion(VersionObject{Object: "WorkloadInstance", Version: "v0"})
	AddObjectVersion(VersionObject{Object: "WorkloadDefinition", Version: "v1"})

	// assert: both keys present with their own versions
	if len(ObjectVersions) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(ObjectVersions))
	}
	if got := ObjectVersions["WorkloadInstance"].Versions; !reflect.DeepEqual(got, []string{"v0"}) {
		t.Errorf("WorkloadInstance Versions = %v, want [v0]", got)
	}
	if got := ObjectVersions["WorkloadDefinition"].Versions; !reflect.DeepEqual(got, []string{"v1"}) {
		t.Errorf("WorkloadDefinition Versions = %v, want [v1]", got)
	}
}

// TestAddObjectVersion_TableDriven walks a sequence of adds and checks the
// end-state Versions slice per object across happy, duplicate, and multi-object
// interleavings.
func TestAddObjectVersion_TableDriven(t *testing.T) {
	cases := []struct {
		name    string
		inputs  []VersionObject
		wantMap map[string][]string
	}{
		{
			name: "accepts empty object and empty version strings",
			inputs: []VersionObject{
				{Object: "", Version: ""},
			},
			wantMap: map[string][]string{
				"": {""},
			},
		},
		{
			name: "rejects duplicate across interleaved inserts",
			inputs: []VersionObject{
				{Object: "A", Version: "v0"},
				{Object: "B", Version: "v0"},
				{Object: "A", Version: "v0"},
				{Object: "A", Version: "v1"},
				{Object: "B", Version: "v0"},
			},
			wantMap: map[string][]string{
				"A": {"v0", "v1"},
				"B": {"v0"},
			},
		},
		{
			name: "accepts three-version chain on a single object",
			inputs: []VersionObject{
				{Object: "X", Version: "v0"},
				{Object: "X", Version: "v1"},
				{Object: "X", Version: "v2"},
			},
			wantMap: map[string][]string{
				"X": {"v0", "v1", "v2"},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// isolate per subtest so cases don't leak into each other
			defer resetObjectVersions(t)()

			// action: replay the sequence of adds
			for _, in := range tc.inputs {
				AddObjectVersion(in)
			}

			// assert: map has exactly the expected keys and versions
			if len(ObjectVersions) != len(tc.wantMap) {
				t.Fatalf("map size = %d, want %d (got %v)", len(ObjectVersions), len(tc.wantMap), ObjectVersions)
			}
			for k, wantVersions := range tc.wantMap {
				entry, ok := ObjectVersions[k]
				if !ok {
					t.Errorf("missing key %q", k)
					continue
				}
				if entry.API != k {
					t.Errorf("entry[%q].API = %q, want %q", k, entry.API, k)
				}
				if !reflect.DeepEqual(entry.Versions, wantVersions) {
					t.Errorf("entry[%q].Versions = %v, want %v", k, entry.Versions, wantVersions)
				}
			}
		})
	}
}
