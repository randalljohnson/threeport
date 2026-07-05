package notif

import (
	"reflect"
	"testing"
)

// TestGetKubernetesRuntimeDefinitionSubjectsReturnsAllLifecycleSubjects asserts
// the helper returns the create, update, and delete subject constants for
// kubernetes runtime definitions in a stable order.
func TestGetKubernetesRuntimeDefinitionSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetKubernetesRuntimeDefinitionSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		KubernetesRuntimeDefinitionCreateSubject,
		KubernetesRuntimeDefinitionUpdateSubject,
		KubernetesRuntimeDefinitionDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetKubernetesRuntimeInstanceSubjectsReturnsAllLifecycleSubjects asserts
// the helper returns the create, update, and delete subject constants for
// kubernetes runtime instances in a stable order.
func TestGetKubernetesRuntimeInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetKubernetesRuntimeInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		KubernetesRuntimeInstanceCreateSubject,
		KubernetesRuntimeInstanceUpdateSubject,
		KubernetesRuntimeInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetKubernetesRuntimeDefinitionSubjectsMatchesWildcard asserts each
// per-lifecycle subject is covered by the wildcard subject pattern.
func TestGetKubernetesRuntimeDefinitionSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := KubernetesRuntimeDefinitionSubject[:len(KubernetesRuntimeDefinitionSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetKubernetesRuntimeDefinitionSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetKubernetesRuntimeInstanceSubjectsMatchesWildcard asserts each
// per-lifecycle subject is covered by the wildcard subject pattern.
func TestGetKubernetesRuntimeInstanceSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := KubernetesRuntimeInstanceSubject[:len(KubernetesRuntimeInstanceSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetKubernetesRuntimeInstanceSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetKubernetesRuntimeSubjectsAggregatesAllObjectSubjects asserts the
// kubernetes-runtime-wide subject helper includes every lifecycle subject from
// each contributing object type.
func TestGetKubernetesRuntimeSubjectsAggregatesAllObjectSubjects(t *testing.T) {
	// invoke the aggregator under test
	got := GetKubernetesRuntimeSubjects()

	// verify each contributing helper's subjects are present in the aggregated
	// result
	wantGroups := [][]string{
		GetKubernetesRuntimeDefinitionSubjects(),
		GetKubernetesRuntimeInstanceSubjects(),
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

// TestGetKubernetesRuntimeSubjectsLengthMatchesContributors asserts the
// aggregator returns exactly the sum of its contributing helpers with no
// extras.
func TestGetKubernetesRuntimeSubjectsLengthMatchesContributors(t *testing.T) {
	// invoke the aggregator and each contributing helper
	all := GetKubernetesRuntimeSubjects()
	sum := len(GetKubernetesRuntimeDefinitionSubjects()) +
		len(GetKubernetesRuntimeInstanceSubjects())

	// verify the aggregated length equals the sum of the per-object helpers
	if len(all) != sum {
		t.Fatalf("aggregated length %d does not match contributors sum %d", len(all), sum)
	}
}

// TestKubernetesRuntimeStreamNameConstant asserts the kubernetes runtime
// stream name constant is set to the expected value used by nats subscribers.
func TestKubernetesRuntimeStreamNameConstant(t *testing.T) {
	// verify the stream name constant holds the expected literal
	if KubernetesRuntimeStreamName != "kubernetesRuntimeStream" {
		t.Errorf("KubernetesRuntimeStreamName = %q, want %q", KubernetesRuntimeStreamName, "kubernetesRuntimeStream")
	}
}
