package v0

import (
	"fmt"
	"testing"

	"github.com/threeport/threeport/internal/version"
)

// TestAllControlPlaneComponents_ContainsControllersRestApiAndAgent covers that
// AllControlPlaneComponents() returns every controller, plus the REST API and
// agent components, in that order.
func TestAllControlPlaneComponents_ContainsControllersRestApiAndAgent(t *testing.T) {
	// invoke under test
	got := AllControlPlaneComponents()

	// asserts every controller plus rest api and agent are present
	wantLen := len(ThreeportControllerList) + 2
	if len(got) != wantLen {
		t.Fatalf("expected %d components, got %d", wantLen, len(got))
	}

	// asserts the controllers appear first in original order
	for i, c := range ThreeportControllerList {
		if got[i] != c {
			t.Errorf("component at index %d = %v, want %v", i, got[i], c)
		}
	}

	// asserts the rest api and agent are appended in that order at the tail
	restApiIdx := len(ThreeportControllerList)
	if got[restApiIdx] != ThreeportRestApi {
		t.Errorf("expected ThreeportRestApi at index %d, got %v", restApiIdx, got[restApiIdx])
	}
	agentIdx := restApiIdx + 1
	if got[agentIdx] != ThreeportAgent {
		t.Errorf("expected ThreeportAgent at index %d, got %v", agentIdx, got[agentIdx])
	}
}

// TestAllControlPlaneComponents_ComponentsHaveExpectedFields asserts every
// returned component has non-empty core identity fields so a caller can rely
// on them without nil-guarding.
func TestAllControlPlaneComponents_ComponentsHaveExpectedFields(t *testing.T) {
	// invoke under test
	got := AllControlPlaneComponents()

	// asserts each component has name, image, and namespace populated
	for i, c := range got {
		if c == nil {
			t.Errorf("component at index %d is nil", i)
			continue
		}
		if c.Name == "" {
			t.Errorf("component at index %d has empty Name", i)
		}
		if c.ImageName == "" {
			t.Errorf("component %q has empty ImageName", c.Name)
		}
		if c.ImageNamespace == "" {
			t.Errorf("component %q has empty ImageNamespace", c.Name)
		}
		if c.ImageTag == "" {
			t.Errorf("component %q has empty ImageTag", c.Name)
		}
	}
}

// TestAllControlPlaneComponents_UsesVersionForImageTag asserts every component
// picks up the compiled-in version as its image tag.
func TestAllControlPlaneComponents_UsesVersionForImageTag(t *testing.T) {
	// invoke under test
	got := AllControlPlaneComponents()

	// asserts every component's ImageTag matches version.GetVersion()
	want := version.GetVersion()
	for _, c := range got {
		if c.ImageTag != want {
			t.Errorf("component %q has ImageTag %q, want %q", c.Name, c.ImageTag, want)
		}
	}
}

// TestThreeportApiAltNames covers the DNS name expansion for the API server
// certificate across representative namespace inputs.
func TestThreeportApiAltNames(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		want      []string
	}{
		{
			name:      "standard namespace produces five alt names",
			namespace: "threeport-control-plane",
			want: []string{
				ThreeportAPIServiceResourceName,
				fmt.Sprintf("%s.threeport-control-plane", ThreeportAPIServiceResourceName),
				fmt.Sprintf("%s.threeport-control-plane.svc", ThreeportAPIServiceResourceName),
				fmt.Sprintf("%s.threeport-control-plane.svc.cluster", ThreeportAPIServiceResourceName),
				fmt.Sprintf("%s.threeport-control-plane.svc.cluster.local", ThreeportAPIServiceResourceName),
			},
		},
		{
			name:      "empty namespace still returns five entries with trailing dots",
			namespace: "",
			want: []string{
				ThreeportAPIServiceResourceName,
				fmt.Sprintf("%s.", ThreeportAPIServiceResourceName),
				fmt.Sprintf("%s..svc", ThreeportAPIServiceResourceName),
				fmt.Sprintf("%s..svc.cluster", ThreeportAPIServiceResourceName),
				fmt.Sprintf("%s..svc.cluster.local", ThreeportAPIServiceResourceName),
			},
		},
		{
			name:      "alternate namespace substitutes correctly across every suffix",
			namespace: "custom-ns",
			want: []string{
				ThreeportAPIServiceResourceName,
				fmt.Sprintf("%s.custom-ns", ThreeportAPIServiceResourceName),
				fmt.Sprintf("%s.custom-ns.svc", ThreeportAPIServiceResourceName),
				fmt.Sprintf("%s.custom-ns.svc.cluster", ThreeportAPIServiceResourceName),
				fmt.Sprintf("%s.custom-ns.svc.cluster.local", ThreeportAPIServiceResourceName),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// invoke under test
			got := ThreeportApiAltNames(tt.namespace)

			// asserts length matches the fixed expansion of five names
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d alt names, got %d: %v", len(tt.want), len(got), got)
			}

			// asserts each generated alt name matches the expected form
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("alt name at index %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestThreeportApiAltNames_FirstEntryIsBareServiceName asserts the bare
// service name always leads the returned slice regardless of namespace.
func TestThreeportApiAltNames_FirstEntryIsBareServiceName(t *testing.T) {
	// invoke under test with an arbitrary namespace
	got := ThreeportApiAltNames("anything")

	// asserts the leading entry is the bare service resource name
	if len(got) == 0 {
		t.Fatal("expected at least one alt name")
	}
	if got[0] != ThreeportAPIServiceResourceName {
		t.Errorf("first alt name = %q, want %q", got[0], ThreeportAPIServiceResourceName)
	}
}
