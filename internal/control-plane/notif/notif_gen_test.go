package notif

import (
	"reflect"
	"testing"
)

// TestGetControlPlaneDefinitionSubjectsReturnsAllLifecycleSubjects asserts the
// helper returns the create, update, and delete subject constants for control
// plane definitions in a stable order.
func TestGetControlPlaneDefinitionSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetControlPlaneDefinitionSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		ControlPlaneDefinitionCreateSubject,
		ControlPlaneDefinitionUpdateSubject,
		ControlPlaneDefinitionDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetControlPlaneDefinitionSubjectsMatchesWildcard asserts each per-lifecycle
// subject shares the wildcard subject's prefix.
func TestGetControlPlaneDefinitionSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := ControlPlaneDefinitionSubject[:len(ControlPlaneDefinitionSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetControlPlaneDefinitionSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetControlPlaneInstanceSubjectsReturnsAllLifecycleSubjects asserts the
// helper returns the create, update, and delete subject constants for control
// plane instances in a stable order.
func TestGetControlPlaneInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetControlPlaneInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		ControlPlaneInstanceCreateSubject,
		ControlPlaneInstanceUpdateSubject,
		ControlPlaneInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetControlPlaneInstanceSubjectsMatchesWildcard asserts each per-lifecycle
// subject shares the wildcard subject's prefix.
func TestGetControlPlaneInstanceSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := ControlPlaneInstanceSubject[:len(ControlPlaneInstanceSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetControlPlaneInstanceSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetControlPlaneSubjectsAggregatesDefinitionAndInstanceSubjects asserts the
// control-plane-wide subject helper includes every definition and instance
// lifecycle subject.
func TestGetControlPlaneSubjectsAggregatesDefinitionAndInstanceSubjects(t *testing.T) {
	// invoke the aggregator under test
	got := GetControlPlaneSubjects()

	// verify each definition lifecycle subject is present in the aggregated result
	for _, want := range GetControlPlaneDefinitionSubjects() {
		found := false
		for _, s := range got {
			if s == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("aggregated subjects missing definition subject %q", want)
		}
	}

	// verify each instance lifecycle subject is present in the aggregated result
	for _, want := range GetControlPlaneInstanceSubjects() {
		found := false
		for _, s := range got {
			if s == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("aggregated subjects missing instance subject %q", want)
		}
	}
}

// TestGetControlPlaneSubjectsLengthMatchesConstituents asserts the aggregator
// returns exactly the definition plus instance subjects with no extras.
func TestGetControlPlaneSubjectsLengthMatchesConstituents(t *testing.T) {
	// invoke each helper under test
	all := GetControlPlaneSubjects()
	defs := GetControlPlaneDefinitionSubjects()
	insts := GetControlPlaneInstanceSubjects()

	// verify the aggregated length equals the sum of the constituent helpers
	if len(all) != len(defs)+len(insts) {
		t.Fatalf(
			"aggregated length %d does not match sum of constituents %d",
			len(all),
			len(defs)+len(insts),
		)
	}
}

// TestControlPlaneStreamNameConstant asserts the control plane stream name
// constant holds the value nats subscribers expect.
func TestControlPlaneStreamNameConstant(t *testing.T) {
	// verify the stream name constant holds the expected literal
	if ControlPlaneStreamName != "controlPlaneStream" {
		t.Errorf(
			"ControlPlaneStreamName = %q, want %q",
			ControlPlaneStreamName,
			"controlPlaneStream",
		)
	}
}
