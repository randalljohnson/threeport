package notif

import (
	"reflect"
	"testing"
)

// TestGetMachineRuntimeInstanceSubjectsReturnsAllLifecycleSubjects asserts the
// helper returns the create, update, and delete subject constants for machine
// runtime instances in a stable order.
func TestGetMachineRuntimeInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetMachineRuntimeInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		MachineRuntimeInstanceCreateSubject,
		MachineRuntimeInstanceUpdateSubject,
		MachineRuntimeInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetMachineRuntimeInstanceSubjectsMatchesWildcard asserts each
// per-lifecycle subject shares the wildcard subject's prefix.
func TestGetMachineRuntimeInstanceSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix by stripping the trailing wildcard character
	prefix := MachineRuntimeInstanceSubject[:len(MachineRuntimeInstanceSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetMachineRuntimeInstanceSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetMachineRuntimeSubjectsAggregatesInstanceSubjects asserts the
// runtime-wide subject helper includes every instance lifecycle subject.
func TestGetMachineRuntimeSubjectsAggregatesInstanceSubjects(t *testing.T) {
	// invoke the aggregator under test
	got := GetMachineRuntimeSubjects()

	// verify each instance lifecycle subject is present in the aggregated result
	for _, want := range GetMachineRuntimeInstanceSubjects() {
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

// TestGetMachineRuntimeSubjectsLengthMatchesConstituents asserts the aggregator
// returns exactly the instance subjects with no extras.
func TestGetMachineRuntimeSubjectsLengthMatchesConstituents(t *testing.T) {
	// invoke each helper under test
	all := GetMachineRuntimeSubjects()
	insts := GetMachineRuntimeInstanceSubjects()

	// verify the aggregated length equals the sum of the constituent helpers
	if len(all) != len(insts) {
		t.Fatalf(
			"aggregated length %d does not match sum of constituents %d",
			len(all),
			len(insts),
		)
	}
}

// TestMachineRuntimeStreamNameConstant asserts the machine runtime stream name
// constant holds the value nats subscribers expect.
func TestMachineRuntimeStreamNameConstant(t *testing.T) {
	// verify the stream name constant holds the expected literal
	if MachineRuntimeStreamName != "machineRuntimeStream" {
		t.Errorf(
			"MachineRuntimeStreamName = %q, want %q",
			MachineRuntimeStreamName,
			"machineRuntimeStream",
		)
	}
}
