package cmd

import (
	"strings"
	"testing"

	config_v0 "github.com/threeport/threeport/pkg/config/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestOutputGetv0ObservabilityStacksCmd_RendersHeaderAndRows covers the happy
// path where each stack in the slice contributes a row containing name,
// runtime instance name, metrics/logging flags and age under the header.
func TestOutputGetv0ObservabilityStacksCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two stacks with fully populated fields including runtime instance
	stacks := []config_v0.ObservabilityStackConfig{
		{ObservabilityStack: config_v0.ObservabilityStackValues{
			Name: util.Ptr("prod-stack"),
			KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
				Name: util.Ptr("prod-runtime"),
			},
			MetricsEnabled: util.Ptr(true),
			LoggingEnabled: util.Ptr(false),
			Age:            util.Ptr("2d"),
		}},
		{ObservabilityStack: config_v0.ObservabilityStackValues{
			Name: util.Ptr("dev-stack"),
			KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
				Name: util.Ptr("dev-runtime"),
			},
			MetricsEnabled: util.Ptr(false),
			LoggingEnabled: util.Ptr(true),
			Age:            util.Ptr("5h"),
		}},
	}

	// act: run the output function with stdout captured
	out, err := captureStdout(t, func() error {
		return outputGetv0ObservabilityStacksCmd(&stacks)
	})

	// assert: nil error and header columns render
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{"NAME", "OBSERVABILITY STACK DEFINITION", "OBSERVABILITY STACK INSTANCE", "KUBERNETES RUNTIME INSTANCE", "METRICS ENABLED", "LOGGING ENABLED", "AGE"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected header to contain %q, got %q", want, out)
		}
	}
	// assert per-row values render for both stacks including runtime and flags
	for _, want := range []string{"prod-stack", "prod-runtime", "2d", "dev-stack", "dev-runtime", "5h", "true", "false"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	// assert iteration order is preserved: prod row before dev row
	if strings.Index(out, "prod-stack") > strings.Index(out, "dev-stack") {
		t.Errorf("expected prod-stack row before dev-stack row, got %q", out)
	}
}

// TestOutputGetv0ObservabilityStacksCmd_NilRuntimeAndAge covers the branches
// that guard nil KubernetesRuntimeInstance, nil KubernetesRuntimeInstance.Name
// and nil Age: each collapses to the empty string rather than dereferencing.
func TestOutputGetv0ObservabilityStacksCmd_NilRuntimeAndAge(t *testing.T) {
	// arrange three stacks exercising each nil branch
	stacks := []config_v0.ObservabilityStackConfig{
		// nil KubernetesRuntimeInstance and nil Age
		{ObservabilityStack: config_v0.ObservabilityStackValues{
			Name:           util.Ptr("no-runtime"),
			MetricsEnabled: util.Ptr(true),
			LoggingEnabled: util.Ptr(true),
		}},
		// non-nil KubernetesRuntimeInstance but nil inner Name
		{ObservabilityStack: config_v0.ObservabilityStackValues{
			Name:                      util.Ptr("nil-inner"),
			KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{Name: nil},
			MetricsEnabled:            util.Ptr(false),
			LoggingEnabled:            util.Ptr(false),
			Age:                       util.Ptr("1h"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ObservabilityStacksCmd(&stacks)
	})

	// assert: no error and no panic, and each row's name renders
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{"no-runtime", "nil-inner", "1h"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
}

// TestOutputGetv0ObservabilityStacksCmd_EmptySlice covers the boundary
// where the slice is empty: only the header line should render.
func TestOutputGetv0ObservabilityStacksCmd_EmptySlice(t *testing.T) {
	// arrange: empty slice so the range body never executes
	stacks := []config_v0.ObservabilityStackConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ObservabilityStacksCmd(&stacks)
	})

	// assert: nil error, exactly one line (the header), and header content present
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "NAME") {
		t.Errorf("expected header row, got %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one line for empty input, got %d: %q", len(lines), out)
	}
}

// TestOutputGetv0ObservabilityStackDefinitionsCmd_RendersHeaderAndRows covers
// the happy path where each definition contributes a NAME + AGE row under
// the two-column header.
func TestOutputGetv0ObservabilityStackDefinitionsCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two definitions with distinct names and ages
	definitions := []config_v0.ObservabilityStackDefinitionConfig{
		{ObservabilityStackDefinition: config_v0.ObservabilityStackDefinitionValues{
			Name: util.Ptr("def-a"),
			Age:  util.Ptr("3d"),
		}},
		{ObservabilityStackDefinition: config_v0.ObservabilityStackDefinitionValues{
			Name: util.Ptr("def-b"),
			Age:  util.Ptr("7d"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ObservabilityStackDefinitionsCmd(&definitions)
	})

	// assert: nil error, both header columns and per-row values render
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{"NAME", "AGE", "def-a", "3d", "def-b", "7d"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	// assert iteration order is preserved
	if strings.Index(out, "def-a") > strings.Index(out, "def-b") {
		t.Errorf("expected def-a row before def-b row, got %q", out)
	}
}

// TestOutputGetv0ObservabilityStackDefinitionsCmd_NilAge covers the branch
// where Age is nil: the value defaults to empty string without dereferencing.
func TestOutputGetv0ObservabilityStackDefinitionsCmd_NilAge(t *testing.T) {
	// arrange: one definition with nil Age
	definitions := []config_v0.ObservabilityStackDefinitionConfig{
		{ObservabilityStackDefinition: config_v0.ObservabilityStackDefinitionValues{
			Name: util.Ptr("nil-age"),
			Age:  nil,
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ObservabilityStackDefinitionsCmd(&definitions)
	})

	// assert: no error and the name still renders
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "nil-age") {
		t.Errorf("expected output to contain %q, got %q", "nil-age", out)
	}
}

// TestOutputGetv0ObservabilityStackDefinitionsCmd_EmptySlice covers the
// boundary where the input slice is empty: only the header line renders.
func TestOutputGetv0ObservabilityStackDefinitionsCmd_EmptySlice(t *testing.T) {
	// arrange
	definitions := []config_v0.ObservabilityStackDefinitionConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ObservabilityStackDefinitionsCmd(&definitions)
	})

	// assert: nil error and only the header line
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "AGE") {
		t.Errorf("expected header with NAME and AGE, got %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one line for empty input, got %d: %q", len(lines), out)
	}
}

// TestOutputGetv0ObservabilityStackInstancesCmd_RendersHeaderAndRows covers
// the happy path where each instance contributes a row with definition name,
// runtime instance name, flags, and age under the header.
func TestOutputGetv0ObservabilityStackInstancesCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two instances with fully populated fields
	instances := []config_v0.ObservabilityStackInstanceConfig{
		{ObservabilityStackInstance: config_v0.ObservabilityStackInstanceValues{
			Name: util.Ptr("inst-a"),
			ObservabilityStackDefinition: &config_v0.ObservabilityStackDefinitionValues{
				Name: util.Ptr("def-a"),
			},
			KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
				Name: util.Ptr("runtime-a"),
			},
			MetricsEnabled: util.Ptr(true),
			LoggingEnabled: util.Ptr(false),
			Age:            util.Ptr("4d"),
		}},
		{ObservabilityStackInstance: config_v0.ObservabilityStackInstanceValues{
			Name: util.Ptr("inst-b"),
			ObservabilityStackDefinition: &config_v0.ObservabilityStackDefinitionValues{
				Name: util.Ptr("def-b"),
			},
			KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
				Name: util.Ptr("runtime-b"),
			},
			MetricsEnabled: util.Ptr(false),
			LoggingEnabled: util.Ptr(true),
			Age:            util.Ptr("6h"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ObservabilityStackInstancesCmd(&instances)
	})

	// assert: nil error and every header column renders
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{"NAME", "OBSERVABILITY STACK DEFINITION", "KUBERNETES RUNTIME INSTANCE", "METRICS ENABLED", "LOGGING ENABLED", "AGE"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected header to contain %q, got %q", want, out)
		}
	}
	// assert per-row values render for both instances
	for _, want := range []string{"inst-a", "def-a", "runtime-a", "4d", "inst-b", "def-b", "runtime-b", "6h"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	// assert iteration order is preserved
	if strings.Index(out, "inst-a") > strings.Index(out, "inst-b") {
		t.Errorf("expected inst-a row before inst-b row, got %q", out)
	}
}

// TestOutputGetv0ObservabilityStackInstancesCmd_NilAssociationsAndAge covers
// the nil branches: nil ObservabilityStackDefinition, nil inner Name, nil
// KubernetesRuntimeInstance, nil inner Name, and nil Age all collapse to the
// empty string.
func TestOutputGetv0ObservabilityStackInstancesCmd_NilAssociationsAndAge(t *testing.T) {
	// arrange rows exercising each nil-guard branch
	instances := []config_v0.ObservabilityStackInstanceConfig{
		// nil ObservabilityStackDefinition and nil KubernetesRuntimeInstance and nil Age
		{ObservabilityStackInstance: config_v0.ObservabilityStackInstanceValues{
			Name:           util.Ptr("bare"),
			MetricsEnabled: util.Ptr(true),
			LoggingEnabled: util.Ptr(true),
		}},
		// non-nil associations with nil inner Name fields
		{ObservabilityStackInstance: config_v0.ObservabilityStackInstanceValues{
			Name:                         util.Ptr("nil-inner"),
			ObservabilityStackDefinition: &config_v0.ObservabilityStackDefinitionValues{Name: nil},
			KubernetesRuntimeInstance:    &config_v0.KubernetesRuntimeInstanceValues{Name: nil},
			MetricsEnabled:               util.Ptr(false),
			LoggingEnabled:               util.Ptr(false),
			Age:                          util.Ptr("30m"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ObservabilityStackInstancesCmd(&instances)
	})

	// assert: no error, no panic, both name rows render
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{"bare", "nil-inner", "30m"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
}

// TestOutputGetv0ObservabilityStackInstancesCmd_EmptySlice covers the boundary
// where the slice is empty: only the header line should render.
func TestOutputGetv0ObservabilityStackInstancesCmd_EmptySlice(t *testing.T) {
	// arrange: empty slice
	instances := []config_v0.ObservabilityStackInstanceConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ObservabilityStackInstancesCmd(&instances)
	})

	// assert: nil error, only the header line present
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "NAME") {
		t.Errorf("expected header row, got %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one line for empty input, got %d: %q", len(lines), out)
	}
}
