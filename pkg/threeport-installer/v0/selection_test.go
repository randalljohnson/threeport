package v0

import (
	"strings"
	"testing"

	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// testControllers returns a small representative list of controllers
// covering single-word, underscored, and dashed group names.
func testControllers() []*v0.ControlPlaneComponent {
	return []*v0.ControlPlaneComponent{
		{Name: "kubernetes-workload-controller"},
		{Name: "gateway-controller"},
		{Name: "kubernetes-runtime-controller"},
		{Name: "aws-controller"},
	}
}

// TestSelectControllersByGroup exercises the pure filter helper
// across the cases callers depend on: empty input passthrough,
// single and multi group selection, unknown group rejection, and
// a known group missing from the controller list.
func TestSelectControllersByGroup(t *testing.T) {
	allControllers := testControllers()

	tests := []struct {
		name        string
		groupNames  []string
		controllers []*v0.ControlPlaneComponent
		wantNames   []string
		wantErrSub  string
	}{
		{
			name:        "empty group names returns all controllers unchanged",
			groupNames:  nil,
			controllers: allControllers,
			wantNames: []string{
				"kubernetes-workload-controller",
				"gateway-controller",
				"kubernetes-runtime-controller",
				"aws-controller",
			},
		},
		{
			name:        "single group selects matching controller",
			groupNames:  []string{"gateway"},
			controllers: allControllers,
			wantNames:   []string{"gateway-controller"},
		},
		{
			name:        "underscored group maps to dashed controller name",
			groupNames:  []string{"kubernetes_workload"},
			controllers: allControllers,
			wantNames:   []string{"kubernetes-workload-controller"},
		},
		{
			name:        "multiple groups select matching controllers in order",
			groupNames:  []string{"kubernetes_workload", "gateway"},
			controllers: allControllers,
			wantNames: []string{
				"kubernetes-workload-controller",
				"gateway-controller",
			},
		},
		{
			name:        "unknown group returns error listing valid choices",
			groupNames:  []string{"bogus_group"},
			controllers: allControllers,
			wantErrSub:  "unknown api object group",
		},
		{
			name:       "group with no controller in trimmed list returns error",
			groupNames: []string{"aws"},
			controllers: []*v0.ControlPlaneComponent{
				{Name: "gateway-controller"},
				{Name: "kubernetes-workload-controller"},
			},
			wantErrSub: "unknown api object group \"aws\"",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SelectControllersByGroup(tc.groupNames, tc.controllers)
			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrSub)
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.wantNames) {
				t.Fatalf("got %d controllers, want %d", len(got), len(tc.wantNames))
			}
			for i, controller := range got {
				if controller.Name != tc.wantNames[i] {
					t.Errorf("position %d: got %q, want %q", i, controller.Name, tc.wantNames[i])
				}
			}
		})
	}
}

// TestSelectControllersByGroupErrorListsValidNames confirms the error
// path enumerates the inferred group-name set so users can correct
// a typo without spelunking the source.
func TestSelectControllersByGroupErrorListsValidNames(t *testing.T) {
	_, err := SelectControllersByGroup([]string{"bogus"}, testControllers())
	if err == nil {
		t.Fatal("expected error for unknown group, got nil")
	}
	for _, expected := range []string{"aws", "gateway", "kubernetes_runtime", "kubernetes_workload"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error %q missing expected group %q", err.Error(), expected)
		}
	}
}
