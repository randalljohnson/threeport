package notif

import (
	"reflect"
	"testing"
)

// TestGetOciOkeKubernetesRuntimeInstanceSubjectsReturnsAllLifecycleSubjects
// asserts the helper returns the create, update, and delete subject
// constants for oci oke kubernetes runtime instances in a stable order.
func TestGetOciOkeKubernetesRuntimeInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetOciOkeKubernetesRuntimeInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		OciOkeKubernetesRuntimeInstanceCreateSubject,
		OciOkeKubernetesRuntimeInstanceUpdateSubject,
		OciOkeKubernetesRuntimeInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetOciOkeKubernetesRuntimeInstanceSubjectsMatchesWildcard asserts each
// per-lifecycle subject is covered by the wildcard subject pattern.
func TestGetOciOkeKubernetesRuntimeInstanceSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := OciOkeKubernetesRuntimeInstanceSubject[:len(OciOkeKubernetesRuntimeInstanceSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetOciOkeKubernetesRuntimeInstanceSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetOciSubjectsAggregatesOkeSubjects asserts the oci-wide subject helper
// includes every oke lifecycle subject.
func TestGetOciSubjectsAggregatesOkeSubjects(t *testing.T) {
	// invoke the aggregator under test
	got := GetOciSubjects()

	// verify each oke lifecycle subject is present in the aggregated result
	for _, want := range GetOciOkeKubernetesRuntimeInstanceSubjects() {
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

// TestGetOciSubjectsLengthMatchesOkeSubjects asserts the aggregator returns
// exactly the oke subjects while no other oci object type contributes.
func TestGetOciSubjectsLengthMatchesOkeSubjects(t *testing.T) {
	// invoke both helpers under test
	all := GetOciSubjects()
	oke := GetOciOkeKubernetesRuntimeInstanceSubjects()

	// verify the aggregated length equals the oke helper's length
	if len(all) != len(oke) {
		t.Fatalf("aggregated length %d does not match oke length %d", len(all), len(oke))
	}
}

// TestOciStreamNameConstant asserts the oci stream name constant is set to
// the expected value used by nats subscribers.
func TestOciStreamNameConstant(t *testing.T) {
	// verify the stream name constant holds the expected literal
	if OciStreamName != "ociStream" {
		t.Errorf("OciStreamName = %q, want %q", OciStreamName, "ociStream")
	}
}
