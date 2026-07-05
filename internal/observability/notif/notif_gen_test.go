package notif

import (
	"reflect"
	"testing"
)

// TestGetObservabilityStackDefinitionSubjectsReturnsAllLifecycleSubjects
// asserts the helper returns the create, update, and delete subject
// constants for observability stack definitions in a stable order.
func TestGetObservabilityStackDefinitionSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetObservabilityStackDefinitionSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		ObservabilityStackDefinitionCreateSubject,
		ObservabilityStackDefinitionUpdateSubject,
		ObservabilityStackDefinitionDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetObservabilityStackInstanceSubjectsReturnsAllLifecycleSubjects asserts
// the helper returns the create, update, and delete subject constants for
// observability stack instances in a stable order.
func TestGetObservabilityStackInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetObservabilityStackInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	want := []string{
		ObservabilityStackInstanceCreateSubject,
		ObservabilityStackInstanceUpdateSubject,
		ObservabilityStackInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetObservabilityDashboardDefinitionSubjectsReturnsAllLifecycleSubjects
// asserts the helper returns the create, update, and delete subject constants
// for observability dashboard definitions in a stable order.
func TestGetObservabilityDashboardDefinitionSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetObservabilityDashboardDefinitionSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	want := []string{
		ObservabilityDashboardDefinitionCreateSubject,
		ObservabilityDashboardDefinitionUpdateSubject,
		ObservabilityDashboardDefinitionDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetObservabilityDashboardInstanceSubjectsReturnsAllLifecycleSubjects
// asserts the helper returns the create, update, and delete subject constants
// for observability dashboard instances in a stable order.
func TestGetObservabilityDashboardInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetObservabilityDashboardInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	want := []string{
		ObservabilityDashboardInstanceCreateSubject,
		ObservabilityDashboardInstanceUpdateSubject,
		ObservabilityDashboardInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetMetricsDefinitionSubjectsReturnsAllLifecycleSubjects asserts the
// helper returns the create, update, and delete subject constants for metrics
// definitions in a stable order.
func TestGetMetricsDefinitionSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetMetricsDefinitionSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	want := []string{
		MetricsDefinitionCreateSubject,
		MetricsDefinitionUpdateSubject,
		MetricsDefinitionDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetMetricsInstanceSubjectsReturnsAllLifecycleSubjects asserts the helper
// returns the create, update, and delete subject constants for metrics
// instances in a stable order.
func TestGetMetricsInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetMetricsInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	want := []string{
		MetricsInstanceCreateSubject,
		MetricsInstanceUpdateSubject,
		MetricsInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetLoggingDefinitionSubjectsReturnsAllLifecycleSubjects asserts the
// helper returns the create, update, and delete subject constants for logging
// definitions in a stable order.
func TestGetLoggingDefinitionSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetLoggingDefinitionSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	want := []string{
		LoggingDefinitionCreateSubject,
		LoggingDefinitionUpdateSubject,
		LoggingDefinitionDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetLoggingInstanceSubjectsReturnsAllLifecycleSubjects asserts the helper
// returns the create, update, and delete subject constants for logging
// instances in a stable order.
func TestGetLoggingInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetLoggingInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	want := []string{
		LoggingInstanceCreateSubject,
		LoggingInstanceUpdateSubject,
		LoggingInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestPerObjectSubjectsMatchWildcardPrefix asserts every per-lifecycle subject
// returned by each object helper shares the prefix of its wildcard subject.
func TestPerObjectSubjectsMatchWildcardPrefix(t *testing.T) {
	// pair each wildcard subject with the helper that returns its lifecycle subjects
	cases := []struct {
		name     string
		wildcard string
		subjects []string
	}{
		{"observabilityStackDefinition", ObservabilityStackDefinitionSubject, GetObservabilityStackDefinitionSubjects()},
		{"observabilityStackInstance", ObservabilityStackInstanceSubject, GetObservabilityStackInstanceSubjects()},
		{"observabilityDashboardDefinition", ObservabilityDashboardDefinitionSubject, GetObservabilityDashboardDefinitionSubjects()},
		{"observabilityDashboardInstance", ObservabilityDashboardInstanceSubject, GetObservabilityDashboardInstanceSubjects()},
		{"metricsDefinition", MetricsDefinitionSubject, GetMetricsDefinitionSubjects()},
		{"metricsInstance", MetricsInstanceSubject, GetMetricsInstanceSubjects()},
		{"loggingDefinition", LoggingDefinitionSubject, GetLoggingDefinitionSubjects()},
		{"loggingInstance", LoggingInstanceSubject, GetLoggingInstanceSubjects()},
	}

	// verify each per-object subject shares the wildcard prefix
	for _, c := range cases {
		prefix := c.wildcard[:len(c.wildcard)-1]
		for _, s := range c.subjects {
			if len(s) < len(prefix) || s[:len(prefix)] != prefix {
				t.Errorf("%s: subject %q does not match wildcard prefix %q", c.name, s, prefix)
			}
		}
	}
}

// TestGetObservabilitySubjectsAggregatesAllObjectSubjects asserts the
// observability-wide helper returns every per-object lifecycle subject in the
// documented aggregation order.
func TestGetObservabilitySubjectsAggregatesAllObjectSubjects(t *testing.T) {
	// invoke the aggregator under test
	got := GetObservabilitySubjects()

	// construct the expected concatenation in aggregation order
	var want []string
	want = append(want, GetObservabilityStackDefinitionSubjects()...)
	want = append(want, GetObservabilityStackInstanceSubjects()...)
	want = append(want, GetObservabilityDashboardDefinitionSubjects()...)
	want = append(want, GetObservabilityDashboardInstanceSubjects()...)
	want = append(want, GetMetricsDefinitionSubjects()...)
	want = append(want, GetMetricsInstanceSubjects()...)
	want = append(want, GetLoggingDefinitionSubjects()...)
	want = append(want, GetLoggingInstanceSubjects()...)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aggregated subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetObservabilitySubjectsIncludesEveryPerObjectSubject asserts every
// subject returned by an individual object helper appears in the aggregator's
// output.
func TestGetObservabilitySubjectsIncludesEveryPerObjectSubject(t *testing.T) {
	// build a lookup of aggregator output
	aggregated := GetObservabilitySubjects()
	set := make(map[string]struct{}, len(aggregated))
	for _, s := range aggregated {
		set[s] = struct{}{}
	}

	// verify each per-object subject is present in the aggregator's output
	perObject := [][]string{
		GetObservabilityStackDefinitionSubjects(),
		GetObservabilityStackInstanceSubjects(),
		GetObservabilityDashboardDefinitionSubjects(),
		GetObservabilityDashboardInstanceSubjects(),
		GetMetricsDefinitionSubjects(),
		GetMetricsInstanceSubjects(),
		GetLoggingDefinitionSubjects(),
		GetLoggingInstanceSubjects(),
	}
	for _, subjects := range perObject {
		for _, s := range subjects {
			if _, ok := set[s]; !ok {
				t.Errorf("aggregated subjects missing %q", s)
			}
		}
	}
}

// TestObservabilityStreamNameConstant asserts the stream name constant holds
// the expected literal used by nats subscribers.
func TestObservabilityStreamNameConstant(t *testing.T) {
	// verify the stream name constant holds the expected literal
	if ObservabilityStreamName != "observabilityStream" {
		t.Errorf("ObservabilityStreamName = %q, want %q", ObservabilityStreamName, "observabilityStream")
	}
}
