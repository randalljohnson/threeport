package notif

import (
	"reflect"
	"testing"
)

// TestGetMachineWorkloadInstanceSubjectsReturnsAllLifecycleSubjects asserts
// the helper returns the create, update, and delete subject constants for
// machine workload instances in a stable order.
func TestGetMachineWorkloadInstanceSubjectsReturnsAllLifecycleSubjects(t *testing.T) {
	// invoke the helper under test
	got := GetMachineWorkloadInstanceSubjects()

	// verify the returned slice matches the three lifecycle subject constants
	// in create, update, delete order
	want := []string{
		MachineWorkloadInstanceCreateSubject,
		MachineWorkloadInstanceUpdateSubject,
		MachineWorkloadInstanceDeleteSubject,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects mismatch: got %v, want %v", got, want)
	}
}

// TestGetMachineWorkloadInstanceSubjectsMatchesWildcard asserts each
// per-lifecycle subject shares the wildcard subject's prefix.
func TestGetMachineWorkloadInstanceSubjectsMatchesWildcard(t *testing.T) {
	// derive the wildcard prefix from the wildcard subject constant
	prefix := MachineWorkloadInstanceSubject[:len(MachineWorkloadInstanceSubject)-1]

	// verify every returned subject shares the wildcard prefix
	for _, s := range GetMachineWorkloadInstanceSubjects() {
		if len(s) < len(prefix) || s[:len(prefix)] != prefix {
			t.Errorf("subject %q does not match wildcard prefix %q", s, prefix)
		}
	}
}

// TestGetMachineWorkloadSubjectsAggregatesInstanceSubjects asserts the
// machine-workload-wide subject helper includes every instance lifecycle
// subject.
func TestGetMachineWorkloadSubjectsAggregatesInstanceSubjects(t *testing.T) {
	// invoke the aggregator under test
	got := GetMachineWorkloadSubjects()

	// verify each instance lifecycle subject is present in the aggregated result
	for _, want := range GetMachineWorkloadInstanceSubjects() {
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

// TestGetMachineWorkloadSubjectsLengthMatchesConstituents asserts the
// aggregator returns exactly the instance subjects with no extras.
func TestGetMachineWorkloadSubjectsLengthMatchesConstituents(t *testing.T) {
	// invoke each helper under test
	all := GetMachineWorkloadSubjects()
	insts := GetMachineWorkloadInstanceSubjects()

	// verify the aggregated length equals the constituent instance-subject count
	if len(all) != len(insts) {
		t.Fatalf(
			"aggregated length %d does not match constituent count %d",
			len(all),
			len(insts),
		)
	}
}

// TestMachineWorkloadStreamNameConstant asserts the machine workload stream
// name constant holds the value nats subscribers expect.
func TestMachineWorkloadStreamNameConstant(t *testing.T) {
	// verify the stream name constant holds the expected literal
	if MachineWorkloadStreamName != "machineWorkloadStream" {
		t.Errorf(
			"MachineWorkloadStreamName = %q, want %q",
			MachineWorkloadStreamName,
			"machineWorkloadStream",
		)
	}
}
