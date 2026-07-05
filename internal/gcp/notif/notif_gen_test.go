package notif

import (
	"reflect"
	"testing"
)

// TestGetGcpGkeKubernetesRuntimeInstanceSubjectsReturnsAllLifecycleSubjects
// asserts the helper returns the create, update, and delete subject
// constants for gcp gke kubernetes runtime instances in a stable order.
func TestGetGcpGkeKubernetesRuntimeInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetGcpGkeKubernetesRuntimeInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		GcpGkeKubernetesRuntimeInstanceCreateSubject,
		GcpGkeKubernetesRuntimeInstanceUpdateSubject,
		GcpGkeKubernetesRuntimeInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetGcpGkeKubernetesRuntimeInstanceSubjectsMatchesWildcard asserts each
// per-lifecycle subject is covered by the wildcard subject pattern.
func TestGetGcpGkeKubernetesRuntimeInstanceSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := GcpGkeKubernetesRuntimeInstanceSubject[:len(GcpGkeKubernetesRuntimeInstanceSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetGcpGkeKubernetesRuntimeInstanceSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetGcpSubjectsAggregatesGkeSubjects asserts the gcp-wide subject helper
// includes every gke lifecycle subject.
func TestGetGcpSubjectsAggregatesGkeSubjects(t *testing.T) {
	// invoke the aggregator under test
	got := GetGcpSubjects()

	// verify each gke lifecycle subject is present in the aggregated result
	for _, want := range GetGcpGkeKubernetesRuntimeInstanceSubjects() {
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

// TestGetGcpSubjectsLengthMatchesGkeSubjects asserts the aggregator returns
// exactly the gke subjects while no other gcp object type contributes.
func TestGetGcpSubjectsLengthMatchesGkeSubjects(t *testing.T) {
	// invoke both helpers under test
	all := GetGcpSubjects()
	gke := GetGcpGkeKubernetesRuntimeInstanceSubjects()

	// verify the aggregated length equals the gke helper's length
	if len(all) != len(gke) {
		t.Fatalf("aggregated length %d does not match gke length %d", len(all), len(gke))
	}
}

// TestGcpStreamNameConstant asserts the gcp stream name constant is set to
// the expected value used by nats subscribers.
func TestGcpStreamNameConstant(t *testing.T) {
	// verify the stream name constant holds the expected literal
	if GcpStreamName != "gcpStream" {
		t.Errorf("GcpStreamName = %q, want %q", GcpStreamName, "gcpStream")
	}
}
