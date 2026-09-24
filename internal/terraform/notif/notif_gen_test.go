package notif

import (
	"reflect"
	"testing"
)

// TestGetTerraformDefinitionSubjectsReturnsAllLifecycleSubjects asserts the
// helper returns the create, update, and delete subject constants for
// terraform definitions in a stable order.
func TestGetTerraformDefinitionSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetTerraformDefinitionSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		TerraformDefinitionCreateSubject,
		TerraformDefinitionUpdateSubject,
		TerraformDefinitionDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetTerraformInstanceSubjectsReturnsAllLifecycleSubjects asserts the
// helper returns the create, update, and delete subject constants for
// terraform instances in a stable order.
func TestGetTerraformInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetTerraformInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		TerraformInstanceCreateSubject,
		TerraformInstanceUpdateSubject,
		TerraformInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetTerraformDefinitionSubjectsMatchesWildcard asserts each per-lifecycle
// subject is covered by the wildcard subject pattern.
func TestGetTerraformDefinitionSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := TerraformDefinitionSubject[:len(TerraformDefinitionSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetTerraformDefinitionSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetTerraformInstanceSubjectsMatchesWildcard asserts each per-lifecycle
// subject is covered by the wildcard subject pattern.
func TestGetTerraformInstanceSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := TerraformInstanceSubject[:len(TerraformInstanceSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetTerraformInstanceSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetTerraformSubjectsAggregatesAllObjectSubjects asserts the terraform-wide
// subject helper includes every lifecycle subject from each contributing
// object type.
func TestGetTerraformSubjectsAggregatesAllObjectSubjects(t *testing.T) {
	// invoke the aggregator under test
	got := GetTerraformSubjects()

	// verify each contributing helper's subjects are present in the aggregated
	// result
	wantGroups := [][]string{
		GetTerraformDefinitionSubjects(),
		GetTerraformInstanceSubjects(),
	}
	for _, group := range wantGroups {
		for _, want := range group {
			found := false
			for _, s := range got {
				if s == want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("aggregated subjects missing %q", want)
			}
		}
	}
}

// TestGetTerraformSubjectsLengthMatchesContributors asserts the aggregator
// returns exactly the sum of its contributing helpers with no extras.
func TestGetTerraformSubjectsLengthMatchesContributors(t *testing.T) {
	// invoke the aggregator and each contributing helper
	all := GetTerraformSubjects()
	sum := len(GetTerraformDefinitionSubjects()) +
		len(GetTerraformInstanceSubjects())

	// verify the aggregated length equals the sum of the per-object helpers
	if len(all) != sum {
		t.Fatalf("aggregated length %d does not match contributors sum %d", len(all), sum)
	}
}

// TestTerraformStreamNameConstant asserts the terraform stream name constant
// is set to the expected value used by nats subscribers.
func TestTerraformStreamNameConstant(t *testing.T) {
	// verify the stream name constant holds the expected literal
	if TerraformStreamName != "terraformStream" {
		t.Errorf("TerraformStreamName = %q, want %q", TerraformStreamName, "terraformStream")
	}
}
