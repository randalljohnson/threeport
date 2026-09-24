package notif

import (
	"reflect"
	"testing"
)

// TestGetGatewayDefinitionSubjectsReturnsAllLifecycleSubjects asserts the
// helper returns the create, update, and delete subject constants for gateway
// definitions in a stable order.
func TestGetGatewayDefinitionSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetGatewayDefinitionSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		GatewayDefinitionCreateSubject,
		GatewayDefinitionUpdateSubject,
		GatewayDefinitionDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetGatewayInstanceSubjectsReturnsAllLifecycleSubjects asserts the helper
// returns the create, update, and delete subject constants for gateway
// instances in a stable order.
func TestGetGatewayInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetGatewayInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		GatewayInstanceCreateSubject,
		GatewayInstanceUpdateSubject,
		GatewayInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetDomainNameInstanceSubjectsReturnsAllLifecycleSubjects asserts the
// helper returns the create, update, and delete subject constants for domain
// name instances in a stable order.
func TestGetDomainNameInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetDomainNameInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		DomainNameInstanceCreateSubject,
		DomainNameInstanceUpdateSubject,
		DomainNameInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetGatewayDefinitionSubjectsMatchesWildcard asserts each per-lifecycle
// subject is covered by the wildcard subject pattern.
func TestGetGatewayDefinitionSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := GatewayDefinitionSubject[:len(GatewayDefinitionSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetGatewayDefinitionSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetGatewayInstanceSubjectsMatchesWildcard asserts each per-lifecycle
// subject is covered by the wildcard subject pattern.
func TestGetGatewayInstanceSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := GatewayInstanceSubject[:len(GatewayInstanceSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetGatewayInstanceSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetDomainNameInstanceSubjectsMatchesWildcard asserts each per-lifecycle
// subject is covered by the wildcard subject pattern.
func TestGetDomainNameInstanceSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := DomainNameInstanceSubject[:len(DomainNameInstanceSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetDomainNameInstanceSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetGatewaySubjectsAggregatesAllObjectSubjects asserts the gateway-wide
// subject helper includes every lifecycle subject from each contributing
// object type.
func TestGetGatewaySubjectsAggregatesAllObjectSubjects(t *testing.T) {
	// invoke the aggregator under test
	got := GetGatewaySubjects()

	// verify each contributing helper's subjects are present in the aggregated
	// result
	wantGroups := [][]string{
		GetGatewayDefinitionSubjects(),
		GetGatewayInstanceSubjects(),
		GetDomainNameInstanceSubjects(),
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

// TestGetGatewaySubjectsLengthMatchesContributors asserts the aggregator
// returns exactly the sum of its contributing helpers with no extras.
func TestGetGatewaySubjectsLengthMatchesContributors(t *testing.T) {
	// invoke the aggregator and each contributing helper
	all := GetGatewaySubjects()
	sum := len(GetGatewayDefinitionSubjects()) +
		len(GetGatewayInstanceSubjects()) +
		len(GetDomainNameInstanceSubjects())

	// verify the aggregated length equals the sum of the per-object helpers
	if len(all) != sum {
		t.Fatalf("aggregated length %d does not match contributors sum %d", len(all), sum)
	}
}

// TestGatewayStreamNameConstant asserts the gateway stream name constant is
// set to the expected value used by nats subscribers.
func TestGatewayStreamNameConstant(t *testing.T) {
	// verify the stream name constant holds the expected literal
	if GatewayStreamName != "gatewayStream" {
		t.Errorf("GatewayStreamName = %q, want %q", GatewayStreamName, "gatewayStream")
	}
}
