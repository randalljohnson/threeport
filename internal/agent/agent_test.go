package agent

import (
	"strings"
	"testing"
)

// TestThreeportWorkloadName_ReturnsFormattedNameByWorkloadType asserts the
// function produces the correct standardized name for each recognized workload
// type, and returns an error for unrecognized types.
func TestThreeportWorkloadName_ReturnsFormattedNameByWorkloadType(t *testing.T) {
	// each case pairs an input (id + type) with either an expected name or an
	// expected error, exercising the recognized types plus common error paths.
	tests := []struct {
		name         string
		workloadID   uint
		workloadType string
		want         string
		wantErr      bool
	}{
		{
			name:         "kubernetes workload instance builds kubernetes prefixed name",
			workloadID:   1,
			workloadType: KubernetesWorkloadInstanceType,
			want:         "kubernetes-workload-instance-1",
		},
		{
			name:         "helm workload instance builds helm prefixed name",
			workloadID:   42,
			workloadType: HelmWorkloadInstanceType,
			want:         "helm-workload-instance-42",
		},
		{
			name:         "zero id still produces a well-formed name for kubernetes",
			workloadID:   0,
			workloadType: KubernetesWorkloadInstanceType,
			want:         "kubernetes-workload-instance-0",
		},
		{
			name:         "zero id still produces a well-formed name for helm",
			workloadID:   0,
			workloadType: HelmWorkloadInstanceType,
			want:         "helm-workload-instance-0",
		},
		{
			name:         "large id renders without overflow",
			workloadID:   ^uint(0),
			workloadType: KubernetesWorkloadInstanceType,
		},
		{
			name:         "empty workload type is unrecognized",
			workloadID:   1,
			workloadType: "",
			wantErr:      true,
		},
		{
			name:         "unknown workload type is unrecognized",
			workloadID:   1,
			workloadType: "SomeOtherType",
			wantErr:      true,
		},
		{
			name:         "case sensitivity: lowercase variant is unrecognized",
			workloadID:   1,
			workloadType: "kubernetesworkloadinstance",
			wantErr:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// invoke the function under test
			got, err := ThreeportWorkloadName(tc.workloadID, tc.workloadType)

			// verify the error contract
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for workloadType %q, got nil", tc.workloadType)
				}
				// verify empty string on error path
				if got != "" {
					t.Errorf("expected empty name on error, got %q", got)
				}
				// verify the error message enumerates both recognized types
				msg := err.Error()
				if !strings.Contains(msg, KubernetesWorkloadInstanceType) ||
					!strings.Contains(msg, HelmWorkloadInstanceType) {
					t.Errorf("error message should list recognized types, got %q", msg)
				}
				return
			}

			// verify the happy path returns no error
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// verify the produced name matches the expected format
			if tc.want != "" && got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}

			// for the large-id case, only check the prefix since the id is
			// platform dependent
			if tc.want == "" {
				if !strings.HasPrefix(got, "kubernetes-workload-instance-") {
					t.Errorf("expected kubernetes prefix, got %q", got)
				}
			}
		})
	}
}

// TestConstants_HoldExpectedValues asserts the exported package constants
// remain at their documented values so downstream label and finalizer wiring
// stays stable.
func TestConstants_HoldExpectedValues(t *testing.T) {
	// each case pins one exported constant to its expected literal value
	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "threeport workload finalizer",
			got:  ThreeportWorkloadFinalizer,
			want: "control-plane.threeport.io/threeport-workload-finalizer",
		},
		{
			name: "kubernetes workload instance type",
			got:  KubernetesWorkloadInstanceType,
			want: "KubernetesWorkloadInstance",
		},
		{
			name: "helm workload instance type",
			got:  HelmWorkloadInstanceType,
			want: "HelmWorkloadInstance",
		},
		{
			name: "kubernetes workload instance label key",
			got:  KubernetesWorkloadInstanceLabelKey,
			want: "control-plane.threeport.io/kubernetes-workload-instance",
		},
		{
			name: "helm workload instance label key",
			got:  HelmWorkloadInstanceLabelKey,
			want: "control-plane.threeport.io/helm-workload-instance",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// verify the constant matches its documented value
			if tc.got != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}
