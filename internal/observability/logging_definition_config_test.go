package observability

import (
	"testing"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestGetLoggingDefinitionOperations covers that the operations builder
// appends the loki and promtail operations in order and wires Create and
// Delete handlers for each.
func TestGetLoggingDefinitionOperations(t *testing.T) {
	// build a config with a representative logging definition name so the
	// operations builder has a valid receiver to close over
	cfg := &LoggingDefinitionConfig{
		loggingDefinition: &v0.LoggingDefinition{
			Definition: v0.Definition{
				Name: util.Ptr("logs"),
			},
		},
	}

	// exercise the operations builder
	ops := cfg.getLoggingDefinitionOperations()

	// assert both operations are appended in the expected order
	wantNames := []string{"loki", "promtail"}
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
