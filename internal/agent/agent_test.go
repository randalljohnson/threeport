package agent

import (
	"strings"
	"testing"
)

// TestThreeportWorkloadName_Kubernetes covers the kubernetes workload branch:
// asserts the returned name is the "kubernetes-workload-instance-<id>" form and
// no error is returned for a recognized type.
func TestThreeportWorkloadName_Kubernetes(t *testing.T) {
	// invoke with a kubernetes workload type and a representative instance ID
	got, err := ThreeportWorkloadName(42, KubernetesWorkloadInstanceType)
	// asserts no error is returned for a recognized workload type
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// asserts the name uses the kubernetes-workload-instance prefix and the ID
	want := "kubernetes-workload-instance-42"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestThreeportWorkloadName_Helm covers the helm workload branch: asserts the
// returned name is the "helm-workload-instance-<id>" form and no error is
// returned for a recognized type.
func TestThreeportWorkloadName_Helm(t *testing.T) {
	// invoke with a helm workload type and a representative instance ID
	got, err := ThreeportWorkloadName(7, HelmWorkloadInstanceType)
	// asserts no error is returned for a recognized workload type
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// asserts the name uses the helm-workload-instance prefix and the ID
	want := "helm-workload-instance-7"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestThreeportWorkloadName_UnrecognizedType rejects an unknown workload type:
// asserts an empty name and an error naming both recognized types.
func TestThreeportWorkloadName_UnrecognizedType(t *testing.T) {
	// invoke with an unrecognized workload type
	got, err := ThreeportWorkloadName(1, "SomeOtherType")
	// asserts an error is returned for an unrecognized workload type
	if err == nil {
		t.Fatal("expected error for unrecognized workload type, got nil")
	}
	// asserts the returned name is empty on error
	if got != "" {
		t.Errorf("expected empty name on error, got %q", got)
	}
	// asserts the error message names both recognized types so the caller can act
	msg := err.Error()
	if !strings.Contains(msg, KubernetesWorkloadInstanceType) {
		t.Errorf("error message missing kubernetes type: %s", msg)
	}
	if !strings.Contains(msg, HelmWorkloadInstanceType) {
		t.Errorf("error message missing helm type: %s", msg)
	}
}

// TestThreeportWorkloadName_ZeroID covers the boundary where the instance ID is
// zero: asserts the numeric suffix is still emitted verbatim.
func TestThreeportWorkloadName_ZeroID(t *testing.T) {
	// invoke with instance ID zero to check numeric formatting at the boundary
	got, err := ThreeportWorkloadName(0, KubernetesWorkloadInstanceType)
	// asserts no error at the zero boundary
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// asserts the zero ID is formatted into the name suffix
	want := "kubernetes-workload-instance-0"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestThreeportWorkloadFinalizerConstant pins the finalizer key so an
// accidental rename shows up as a test failure and not a silent controller
// change.
func TestThreeportWorkloadFinalizerConstant(t *testing.T) {
	// asserts the finalizer key stays the documented control-plane key
	want := "control-plane.threeport.io/threeport-workload-finalizer"
	if ThreeportWorkloadFinalizer != want {
		t.Errorf("ThreeportWorkloadFinalizer = %q, want %q", ThreeportWorkloadFinalizer, want)
	}
}
