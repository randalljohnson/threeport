package observability

import (
	"testing"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestGetObservabilityStackDefinitionOperations covers that the operations
// builder appends the observability dashboard, logging, and metrics
// operations in order and wires Create and Delete handlers for each.
func TestGetObservabilityStackDefinitionOperations(t *testing.T) {
	// build a config with a representative stack definition name so the
	// operations builder has a valid receiver to close over
	cfg := &ObservabilityStackDefinitionConfig{
		observabilityStackDefinition: &v0.ObservabilityStackDefinition{
			Definition: v0.Definition{
				Name: util.Ptr("stack"),
			},
		},
	}

	// exercise the operations builder
	ops := cfg.getObservabilityStackDefinitionOperations()

	// assert all three operations are appended in the expected order
	wantNames := []string{"observability dashboard", "logging", "metrics"}
	if got, want := len(ops.Operations), len(wantNames); got != want {
		t.Fatalf("operations length = %d, want %d", got, want)
	}
	for i, want := range wantNames {
		// assert this operation carries the expected name
		if got := ops.Operations[i].Name; got != want {
			t.Errorf("Operations[%d].Name = %q, want %q", i, got, want)
		}
		// assert Create is wired for this operation
		if ops.Operations[i].Create == nil {
			t.Errorf("Operations[%d].Create is nil", i)
		}
		// assert Delete is wired for this operation
		if ops.Operations[i].Delete == nil {
			t.Errorf("Operations[%d].Delete is nil", i)
		}
	}
}
