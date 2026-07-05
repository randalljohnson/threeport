package notif

import (
	"reflect"
	"testing"
)

// TestGetAwsEksKubernetesRuntimeInstanceSubjectsReturnsAllLifecycleSubjects
// asserts the helper returns the create, update, and delete subject
// constants for aws eks kubernetes runtime instances in a stable order.
func TestGetAwsEksKubernetesRuntimeInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetAwsEksKubernetesRuntimeInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		AwsEksKubernetesRuntimeInstanceCreateSubject,
		AwsEksKubernetesRuntimeInstanceUpdateSubject,
		AwsEksKubernetesRuntimeInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetAwsEksKubernetesRuntimeInstanceSubjectsMatchesWildcard asserts each
// per-lifecycle subject is covered by the wildcard subject pattern.
func TestGetAwsEksKubernetesRuntimeInstanceSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := AwsEksKubernetesRuntimeInstanceSubject[:len(AwsEksKubernetesRuntimeInstanceSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetAwsEksKubernetesRuntimeInstanceSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetAwsSubjectsAggregatesEksSubjects asserts the aws-wide subject helper
// includes every eks lifecycle subject.
func TestGetAwsSubjectsAggregatesEksSubjects(t *testing.T) {
	// invoke the aggregator under test
	got := GetAwsSubjects()

	// verify each eks lifecycle subject is present in the aggregated result
	for _, want := range GetAwsEksKubernetesRuntimeInstanceSubjects() {
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

// TestGetAwsSubjectsLengthMatchesEksSubjects asserts the aggregator returns
// exactly the eks subjects while no other aws object type contributes.
func TestGetAwsSubjectsLengthMatchesEksSubjects(t *testing.T) {
	// invoke both helpers under test
	all := GetAwsSubjects()
	eks := GetAwsEksKubernetesRuntimeInstanceSubjects()

	// verify the aggregated length equals the eks helper's length
	if len(all) != len(eks) {
		t.Fatalf("aggregated length %d does not match eks length %d", len(all), len(eks))
	}
}

// TestAwsStreamNameConstant asserts the aws stream name constant is set to
// the expected value used by nats subscribers.
func TestAwsStreamNameConstant(t *testing.T) {
	// verify the stream name constant holds the expected literal
	if AwsStreamName != "awsStream" {
		t.Errorf("AwsStreamName = %q, want %q", AwsStreamName, "awsStream")
	}
}
