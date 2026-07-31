package v0

import (
	"testing"
)

// TestDefaultControlPlaneTierForProvider asserts that only the local
// provider defaults to a droppable tier, so an install that reaches
// real infrastructure is never disposable by default.
func TestDefaultControlPlaneTierForProvider(t *testing.T) {
	tests := []struct {
		infraProvider string
		wantTier      string
	}{
		{infraProvider: "kind", wantTier: ControlPlaneTierDev},
		{infraProvider: "eks", wantTier: ControlPlaneTierProd},
		{infraProvider: "oke", wantTier: ControlPlaneTierProd},
		{infraProvider: "gke", wantTier: ControlPlaneTierProd},
		{infraProvider: "", wantTier: ControlPlaneTierProd},
	}

	for _, test := range tests {
		t.Run(test.infraProvider, func(t *testing.T) {
			if got := DefaultControlPlaneTierForProvider(test.infraProvider); got != test.wantTier {
				t.Errorf("provider %q: expected tier %q, got %q", test.infraProvider, test.wantTier, got)
			}
		})
	}
}
