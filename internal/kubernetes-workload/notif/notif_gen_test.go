package notif

import (
	"reflect"
	"testing"
)

// TestGetKubernetesWorkloadDefinitionSubjectsReturnsAllLifecycleSubjects asserts
// the helper returns the create, update, and delete subject constants for
// kubernetes workload definitions in a stable order.
func TestGetKubernetesWorkloadDefinitionSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetKubernetesWorkloadDefinitionSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		KubernetesWorkloadDefinitionCreateSubject,
		KubernetesWorkloadDefinitionUpdateSubject,
		KubernetesWorkloadDefinitionDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetKubernetesWorkloadDefinitionSubjectsMatchesWildcard asserts each
// per-lifecycle subject shares the wildcard subject's prefix.
func TestGetKubernetesWorkloadDefinitionSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := KubernetesWorkloadDefinitionSubject[:len(KubernetesWorkloadDefinitionSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetKubernetesWorkloadDefinitionSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetKubernetesWorkloadInstanceSubjectsReturnsAllLifecycleSubjects asserts
// the helper returns the create, update, and delete subject constants for
// kubernetes workload instances in a stable order.
func TestGetKubernetesWorkloadInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetKubernetesWorkloadInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		KubernetesWorkloadInstanceCreateSubject,
		KubernetesWorkloadInstanceUpdateSubject,
		KubernetesWorkloadInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetKubernetesWorkloadInstanceSubjectsMatchesWildcard asserts each
// per-lifecycle subject shares the wildcard subject's prefix.
func TestGetKubernetesWorkloadInstanceSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := KubernetesWorkloadInstanceSubject[:len(KubernetesWorkloadInstanceSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetKubernetesWorkloadInstanceSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetKubernetesWorkloadSubjectsAggregatesDefinitionAndInstanceSubjects
// asserts the kubernetes-workload-wide subject helper includes every definition
// and instance lifecycle subject.
func TestGetKubernetesWorkloadSubjectsAggregatesDefinitionAndInstanceSubjects(t *testing.T) {
	// invoke the aggregator under test
	got := GetKubernetesWorkloadSubjects()

	// verify each definition lifecycle subject is present in the aggregated result
	for _, want := range GetKubernetesWorkloadDefinitionSubjects() {
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
	for _, want := range GetKubernetesWorkloadInstanceSubjects() {
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

// TestGetKubernetesWorkloadSubjectsLengthMatchesConstituents asserts the
// aggregator returns exactly the definition plus instance subjects with no
// extras.
func TestGetKubernetesWorkloadSubjectsLengthMatchesConstituents(t *testing.T) {
	// invoke each helper under test
	all := GetKubernetesWorkloadSubjects()
	defs := GetKubernetesWorkloadDefinitionSubjects()
	insts := GetKubernetesWorkloadInstanceSubjects()

	// verify the aggregated length equals the sum of the constituent helpers
	if len(all) != len(defs)+len(insts) {
		t.Fatalf(
			"aggregated length %d does not match sum of constituents %d",
			len(all),
			len(defs)+len(insts),
		)
	}
}

// TestKubernetesWorkloadStreamNameConstant asserts the kubernetes workload
// stream name constant holds the value nats subscribers expect.
func TestKubernetesWorkloadStreamNameConstant(t *testing.T) {
	// verify the stream name constant holds the expected literal
	if KubernetesWorkloadStreamName != "kubernetesWorkloadStream" {
		t.Errorf(
			"KubernetesWorkloadStreamName = %q, want %q",
			KubernetesWorkloadStreamName,
			"kubernetesWorkloadStream",
		)
	}
}
