package cmd

import (
	"strings"
	"testing"

	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// componentNames returns the names of components in order, joined by commas
// so a test failure surfaces both selection and order in one message.
func componentNames(comps []*v0.ControlPlaneComponent) string {
	names := make([]string, 0, len(comps))
	for _, c := range comps {
		names = append(names, c.Name)
	}
	return strings.Join(names, ",")
}

// TestGetComponentList_ReturnsAllWhenNoNamesGiven asserts the default branch:
// an empty componentNames argument returns every component in allComponents,
// in the original order, so callers that omit the filter get the full set.
func TestGetComponentList_ReturnsAllWhenNoNamesGiven(t *testing.T) {
	// set up three components representing the full available set
	all := []*v0.ControlPlaneComponent{
		{Name: "rest-api"},
		{Name: "agent"},
		{Name: "workload-controller"},
	}

	// invoke with an empty filter, exercising the default branch
	got, err := GetComponentList("", all)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// assert every component is returned in the original order
	if want := "rest-api,agent,workload-controller"; componentNames(got) != want {
		t.Fatalf("componentNames = %q, want %q", componentNames(got), want)
	}
}

// TestGetComponentList_SelectsRequestedByCommaList asserts that a comma-separated
// list of names returns only the matching components, in the order requested.
func TestGetComponentList_SelectsRequestedByCommaList(t *testing.T) {
	// set up available components; the requested list picks a subset out of order
	all := []*v0.ControlPlaneComponent{
		{Name: "rest-api"},
		{Name: "agent"},
		{Name: "workload-controller"},
	}

	// invoke with two names, reversed relative to allComponents
	got, err := GetComponentList("workload-controller,rest-api", all)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// assert only the requested components are returned, in the requested order
	if want := "workload-controller,rest-api"; componentNames(got) != want {
		t.Fatalf("componentNames = %q, want %q", componentNames(got), want)
	}
}

// TestGetComponentList_RejectsUnknownName asserts the not-found error path:
// a name that does not match any available component returns an error naming it.
func TestGetComponentList_RejectsUnknownName(t *testing.T) {
	// set up an available set that does not include the requested name
	all := []*v0.ControlPlaneComponent{
		{Name: "rest-api"},
	}

	// request a name that does not exist in the available set
	_, err := GetComponentList("does-not-exist", all)
	if err == nil {
		t.Fatalf("expected error for unknown component, got nil")
	}

	// assert the error message names the unknown component so operators can fix the flag
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("error %q does not mention the unknown component name", err.Error())
	}
}

// TestGetComponentList_RejectsDuplicateAvailable asserts the duplicate-in-available guard:
// when the available list has two entries with the same name, requesting that name
// returns an error rather than silently duplicating the component.
func TestGetComponentList_RejectsDuplicateAvailable(t *testing.T) {
	// set up an available set with a duplicated name
	all := []*v0.ControlPlaneComponent{
		{Name: "rest-api"},
		{Name: "rest-api"},
	}

	// request the duplicated name, which should trip the found-twice guard
	_, err := GetComponentList("rest-api", all)
	if err == nil {
		t.Fatalf("expected error for duplicate component, got nil")
	}

	// assert the error mentions the duplicated component so the caller can fix its input
	if !strings.Contains(err.Error(), "rest-api") {
		t.Fatalf("error %q does not mention the duplicated component name", err.Error())
	}
}

// TestGetComponentList_EmptyAvailableReturnsEmpty asserts the empty-input case:
// an empty allComponents slice with an empty filter returns an empty result and no error.
func TestGetComponentList_EmptyAvailableReturnsEmpty(t *testing.T) {
	// invoke with both inputs empty, the trivial default-branch case
	got, err := GetComponentList("", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// assert an empty slice is returned so the caller iterates zero components
	if len(got) != 0 {
		t.Fatalf("expected empty result, got %d components", len(got))
	}
}
