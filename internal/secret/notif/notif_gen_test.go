package notif

import (
	"reflect"
	"testing"
)

// TestGetSecretDefinitionSubjectsReturnsAllLifecycleSubjects asserts the
// helper returns the create, update, and delete subject constants for secret
// definitions in a stable order.
func TestGetSecretDefinitionSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetSecretDefinitionSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		SecretDefinitionCreateSubject,
		SecretDefinitionUpdateSubject,
		SecretDefinitionDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetSecretDefinitionSubjectsMatchesWildcard asserts each per-lifecycle
// subject shares the wildcard subject's prefix.
func TestGetSecretDefinitionSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := SecretDefinitionSubject[:len(SecretDefinitionSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetSecretDefinitionSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetSecretInstanceSubjectsReturnsAllLifecycleSubjects asserts the
// helper returns the create, update, and delete subject constants for secret
// instances in a stable order.
func TestGetSecretInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetSecretInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		SecretInstanceCreateSubject,
		SecretInstanceUpdateSubject,
		SecretInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetSecretInstanceSubjectsMatchesWildcard asserts each per-lifecycle
// subject shares the wildcard subject's prefix.
func TestGetSecretInstanceSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := SecretInstanceSubject[:len(SecretInstanceSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetSecretInstanceSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetSecretSubjectsAggregatesDefinitionAndInstanceSubjects asserts the
// secret-wide subject helper includes every definition and instance
// lifecycle subject.
func TestGetSecretSubjectsAggregatesDefinitionAndInstanceSubjects(t *testing.T) {
	// invoke the aggregator under test
	got := GetSecretSubjects()

	// verify each definition lifecycle subject is present in the aggregated result
	for _, want := range GetSecretDefinitionSubjects() {
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
	for _, want := range GetSecretInstanceSubjects() {
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

// TestGetSecretSubjectsLengthMatchesConstituents asserts the aggregator
// returns exactly the definition plus instance subjects with no extras.
func TestGetSecretSubjectsLengthMatchesConstituents(t *testing.T) {
	// invoke each helper under test
	all := GetSecretSubjects()
	defs := GetSecretDefinitionSubjects()
	insts := GetSecretInstanceSubjects()

	// verify the aggregated length equals the sum of the constituent helpers
	if len(all) != len(defs)+len(insts) {
		t.Fatalf(
			"aggregated length %d does not match sum of constituents %d",
			len(all),
			len(defs)+len(insts),
		)
	}
}

// TestSecretStreamNameConstant asserts the secret stream name constant holds
// the value nats subscribers expect.
func TestSecretStreamNameConstant(t *testing.T) {
	// verify the stream name constant holds the expected literal
	if SecretStreamName != "secretStream" {
		t.Errorf(
			"SecretStreamName = %q, want %q",
			SecretStreamName,
			"secretStream",
		)
	}
}
