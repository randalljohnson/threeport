package helmworkload

import (
	"strings"
	"testing"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestMergeHelmValuesGo_MergesBaseAndOverrideWithOverrideWinning asserts that
// two YAML documents are merged into a single map[string]interface{} with the
// override document's values taking precedence on shared keys, and that
// override-only keys are preserved.
func TestMergeHelmValuesGo_MergesBaseAndOverrideWithOverrideWinning(t *testing.T) {
	// base document carries a shared and a base-only key
	base := "shared: base\nbaseOnly: keep\n"
	// override document overrides the shared key and adds an override-only key
	override := "shared: override\noverrideOnly: added\n"

	// invoke the merge under test
	merged, err := MergeHelmValuesGo(base, override)
	if err != nil {
		t.Fatalf("MergeHelmValuesGo returned unexpected error: %v", err)
	}

	// override wins on shared key
	if got, want := merged["shared"], "override"; got != want {
		t.Errorf("shared key = %v, want %v", got, want)
	}
	// base-only key survives the merge
	if got, want := merged["baseOnly"], "keep"; got != want {
		t.Errorf("baseOnly key = %v, want %v", got, want)
	}
	// override-only key is present in output
	if got, want := merged["overrideOnly"], "added"; got != want {
		t.Errorf("overrideOnly key = %v, want %v", got, want)
	}
}

// TestMergeHelmValuesGo_ReturnsErrorOnInvalidYaml asserts that an unparseable
// input surfaces a helm-side merge error rather than a partial or empty result.
func TestMergeHelmValuesGo_ReturnsErrorOnInvalidYaml(t *testing.T) {
	// base is not valid YAML: unmatched key
	invalid := "key: [unterminated"

	// invoke and expect an error from the underlying helm merge
	_, err := MergeHelmValuesGo(invalid, "other: value\n")
	if err == nil {
		t.Fatalf("expected error on invalid YAML, got nil")
	}
	// error should be wrapped through the merge failure path
	if !strings.Contains(err.Error(), "failed to merge helm values") {
		t.Errorf("expected merge failure wrap, got: %v", err)
	}
}

// TestMergeHelmValuesString_ReturnsOverrideWhenBaseEmpty asserts the short
// circuit path where an empty base document returns the override unchanged
// without invoking helm's merge machinery.
func TestMergeHelmValuesString_ReturnsOverrideWhenBaseEmpty(t *testing.T) {
	// empty base and non-empty override triggers the short-circuit branch
	got, err := MergeHelmValuesString("", "foo: bar\n")
	if err != nil {
		t.Fatalf("MergeHelmValuesString returned unexpected error: %v", err)
	}
	if want := "foo: bar\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestMergeHelmValuesString_ReturnsBaseWhenOverrideEmpty asserts the symmetric
// short circuit where an empty override returns the base unchanged.
func TestMergeHelmValuesString_ReturnsBaseWhenOverrideEmpty(t *testing.T) {
	// non-empty base and empty override triggers the symmetric short circuit
	got, err := MergeHelmValuesString("foo: bar\n", "")
	if err != nil {
		t.Fatalf("MergeHelmValuesString returned unexpected error: %v", err)
	}
	if want := "foo: bar\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestMergeHelmValuesString_MergesWhenBothPresent asserts a real merge path
// where both inputs are non-empty; the returned string parses back to a map
// with the override winning on shared keys.
func TestMergeHelmValuesString_MergesWhenBothPresent(t *testing.T) {
	// both non-empty triggers a real merge through MergeHelmValuesGo
	got, err := MergeHelmValuesString("a: 1\nb: 2\n", "b: 3\nc: 4\n")
	if err != nil {
		t.Fatalf("MergeHelmValuesString returned unexpected error: %v", err)
	}

	// round-trip through the unmarshal helper to inspect merged keys
	values, err := UnmarshalHelmValues(got)
	if err != nil {
		t.Fatalf("failed to unmarshal merged output: %v", err)
	}
	// base-only key survives
	if v, ok := values["a"]; !ok || toFloat(v) != 1 {
		t.Errorf("expected a=1, got %v (ok=%v)", v, ok)
	}
	// override wins on shared key
	if v, ok := values["b"]; !ok || toFloat(v) != 3 {
		t.Errorf("expected b=3, got %v (ok=%v)", v, ok)
	}
	// override-only key present
	if v, ok := values["c"]; !ok || toFloat(v) != 4 {
		t.Errorf("expected c=4, got %v (ok=%v)", v, ok)
	}
}

// TestMergeHelmValuesPtrs_HandlesNilAndValuePointers asserts the pointer
// wrapper collapses nil to empty string, forwards to the string merger, and
// returns the merged document.
func TestMergeHelmValuesPtrs_HandlesNilAndValuePointers(t *testing.T) {
	// nil base with concrete override returns override text
	base := util.Ptr("a: 1\n")
	override := util.Ptr("a: 2\n")

	got, err := MergeHelmValuesPtrs(base, override)
	if err != nil {
		t.Fatalf("MergeHelmValuesPtrs returned unexpected error: %v", err)
	}

	// override wins on the shared key
	values, err := UnmarshalHelmValues(got)
	if err != nil {
		t.Fatalf("failed to unmarshal merged output: %v", err)
	}
	if v, ok := values["a"]; !ok || toFloat(v) != 2 {
		t.Errorf("expected a=2, got %v (ok=%v)", v, ok)
	}

	// both nil pointers collapse to empty strings, returning empty output
	got, err = MergeHelmValuesPtrs(nil, nil)
	if err != nil {
		t.Fatalf("MergeHelmValuesPtrs with both nil returned unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string on both-nil, got %q", got)
	}
}

// TestUnmarshalHelmValues_ReturnsMapForValidYaml asserts a well-formed YAML
// document unmarshals into the expected map, exercising the happy path.
func TestUnmarshalHelmValues_ReturnsMapForValidYaml(t *testing.T) {
	// valid YAML document unmarshals into a keyed map
	values, err := UnmarshalHelmValues("foo: bar\nnested:\n  x: 1\n")
	if err != nil {
		t.Fatalf("UnmarshalHelmValues returned unexpected error: %v", err)
	}
	if got, want := values["foo"], "bar"; got != want {
		t.Errorf("foo = %v, want %v", got, want)
	}
	// nested map is preserved as a map[string]interface{}
	nested, ok := values["nested"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested to be a map, got %T", values["nested"])
	}
	if got := toFloat(nested["x"]); got != 1 {
		t.Errorf("nested.x = %v, want 1", got)
	}
}

// TestUnmarshalHelmValues_ReturnsErrorForInvalidYaml asserts malformed input
// surfaces as an unmarshal error, not silently as an empty map.
func TestUnmarshalHelmValues_ReturnsErrorForInvalidYaml(t *testing.T) {
	// malformed YAML surfaces an error from the unmarshal path
	_, err := UnmarshalHelmValues("key: [unterminated")
	if err == nil {
		t.Fatalf("expected error on invalid YAML, got nil")
	}
	if !strings.Contains(err.Error(), "failed to unmarshal helm values") {
		t.Errorf("expected unmarshal failure wrap, got: %v", err)
	}
}

// toFloat normalizes numeric values that YAML may decode as either int or
// float64, so assertions can compare against a single numeric type.
func toFloat(v interface{}) float64 {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case float64:
		return x
	}
	return -1
}
