package notif

import (
	"reflect"
	"testing"
)

// TestGetHelmWorkloadDefinitionSubjectsReturnsAllLifecycleSubjects asserts the
// helper returns the create, update, and delete subject constants for helm
// workload definitions in a stable order.
func TestGetHelmWorkloadDefinitionSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetHelmWorkloadDefinitionSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		HelmWorkloadDefinitionCreateSubject,
		HelmWorkloadDefinitionUpdateSubject,
		HelmWorkloadDefinitionDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetHelmWorkloadDefinitionSubjectsMatchesWildcard asserts each per-lifecycle
// subject shares the wildcard subject's prefix.
func TestGetHelmWorkloadDefinitionSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := HelmWorkloadDefinitionSubject[:len(HelmWorkloadDefinitionSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetHelmWorkloadDefinitionSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetHelmWorkloadInstanceSubjectsReturnsAllLifecycleSubjects asserts the
// helper returns the create, update, and delete subject constants for helm
// workload instances in a stable order.
func TestGetHelmWorkloadInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetHelmWorkloadInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		HelmWorkloadInstanceCreateSubject,
		HelmWorkloadInstanceUpdateSubject,
		HelmWorkloadInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetHelmWorkloadInstanceSubjectsMatchesWildcard asserts each per-lifecycle
// subject shares the wildcard subject's prefix.
func TestGetHelmWorkloadInstanceSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := HelmWorkloadInstanceSubject[:len(HelmWorkloadInstanceSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetHelmWorkloadInstanceSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetHelmWorkloadSubjectsAggregatesDefinitionAndInstanceSubjects asserts
// the helm-workload-wide subject helper includes every definition and instance
// lifecycle subject.
func TestGetHelmWorkloadSubjectsAggregatesDefinitionAndInstanceSubjects(t *testing.T) {
	// invoke the aggregator under test
	got := GetHelmWorkloadSubjects()

	// verify each definition lifecycle subject is present in the aggregated result
	for _, want := range GetHelmWorkloadDefinitionSubjects() {
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
	for _, want := range GetHelmWorkloadInstanceSubjects() {
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

// TestGetHelmWorkloadSubjectsLengthMatchesConstituents asserts the aggregator
// returns exactly the definition plus instance subjects with no extras.
func TestGetHelmWorkloadSubjectsLengthMatchesConstituents(t *testing.T) {
	// invoke each helper under test
	all := GetHelmWorkloadSubjects()
	defs := GetHelmWorkloadDefinitionSubjects()
	insts := GetHelmWorkloadInstanceSubjects()

	// verify the aggregated length equals the sum of the constituent helpers
	if len(all) != len(defs)+len(insts) {
		t.Fatalf(
			"aggregated length %d does not match sum of constituents %d",
			len(all),
			len(defs)+len(insts),
		)
	}
}

// TestHelmWorkloadStreamNameConstant asserts the helm workload stream name
// constant holds the value nats subscribers expect.
func TestHelmWorkloadStreamNameConstant(t *testing.T) {
	// verify the stream name constant holds the expected literal
	if HelmWorkloadStreamName != "helmWorkloadStream" {
		t.Errorf(
			"HelmWorkloadStreamName = %q, want %q",
			HelmWorkloadStreamName,
			"helmWorkloadStream",
		)
	}
}
