package v0

import (
	"strings"
	"testing"
)

// TestValidateControlPlaneName asserts that a name the config knows is
// accepted, and that every name it does not know is refused with a message
// showing what the config holds. Callers validate before they act, so the
// refusal has to be complete here: a name that gets past this is not rejected
// until something downstream tries to resolve it.
func TestValidateControlPlaneName(t *testing.T) {
	populated := &ThreeportConfig{
		ControlPlanes: []ControlPlane{
			{Name: "dev-0"},
			{Name: "sxalable-dev"},
		},
		CurrentControlPlane: "dev-0",
	}

	tests := []struct {
		name        string
		config      *ThreeportConfig
		controlPlan string
		wantErr     bool
		wantInErr   []string
	}{
		{
			name:        "a known control plane is accepted",
			config:      populated,
			controlPlan: "sxalable-dev",
		},
		{
			// the cluster hosting a control plane carries a different
			// name, and supplying it is the mistake this catches
			name:        "the cluster name is refused and the control planes are listed",
			config:      populated,
			controlPlan: "sxalable-dev-threeport",
			wantErr:     true,
			wantInErr:   []string{"sxalable-dev-threeport", "dev-0", "sxalable-dev"},
		},
		{
			name:        "an empty name is refused",
			config:      populated,
			controlPlan: "",
			wantErr:     true,
			wantInErr:   []string{"dev-0", "sxalable-dev"},
		},
		{
			// listing the available names is no help when there are
			// none, so an empty config says that instead
			name:        "an empty config says it holds nothing",
			config:      &ThreeportConfig{},
			controlPlan: "dev-0",
			wantErr:     true,
			wantInErr:   []string{"dev-0", "no control planes"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.ValidateControlPlaneName(test.controlPlan)

			if !test.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			if err == nil {
				t.Fatal("expected an error, got none")
			}
			for _, want := range test.wantInErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("expected error to contain %q, got %q", want, err.Error())
				}
			}
		})
	}
}
