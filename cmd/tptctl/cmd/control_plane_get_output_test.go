package cmd

import (
	"strings"
	"testing"

	config_v0 "github.com/threeport/threeport/pkg/config/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestOutputGetv0ControlPlanesCmd_RendersHeaderAndRows covers the happy path
// where each ControlPlaneConfig contributes a fully populated row under the
// seven-column tabwriter header.
func TestOutputGetv0ControlPlanesCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two control planes with distinct values so column population is
	// observable, plus the nested KubernetesRuntimeInstance name and Age
	controlPlanes := []config_v0.ControlPlaneConfig{
		{ControlPlane: config_v0.ControlPlaneValues{
			Name:        util.Ptr("cp-alpha"),
			AuthEnabled: util.Ptr(true),
			Genesis:     util.Ptr(false),
			KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
				Name: util.Ptr("cluster-alpha"),
			},
			Age: util.Ptr("2d"),
		}},
		{ControlPlane: config_v0.ControlPlaneValues{
			Name:        util.Ptr("cp-beta"),
			AuthEnabled: util.Ptr(false),
			Genesis:     util.Ptr(true),
			KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
				Name: util.Ptr("cluster-beta"),
			},
			Age: util.Ptr("7d"),
		}},
	}

	// act: invoke the command with stdout redirected so we can inspect output
	out, err := captureStdout(t, func() error {
		return outputGetv0ControlPlanesCmd(&controlPlanes)
	})

	// assert: nil error, header labels present, and each row's values rendered
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{
		"NAME", "CONTROL PLANE DEFINITION", "CONTROL PLANE INSTANCE",
		"AUTH ENABLED", "GENESIS CONTROL PLANE", "KUBERNETES RUNTIME INSTANCE", "AGE",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected header token %q, got %q", want, out)
		}
	}
	// per-row values: names, cluster names, and ages
	for _, want := range []string{
		"cp-alpha", "cluster-alpha", "2d",
		"cp-beta", "cluster-beta", "7d",
		"true", "false",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	// cp-alpha row precedes cp-beta row: preserves input iteration order
	if strings.Index(out, "cp-alpha") > strings.Index(out, "cp-beta") {
		t.Errorf("expected cp-alpha row before cp-beta row, got %q", out)
	}
}

// TestOutputGetv0ControlPlanesCmd_NilOptionalFields covers the branch where
// KubernetesRuntimeInstance and Age are nil: the code substitutes empty strings
// rather than dereferencing.
func TestOutputGetv0ControlPlanesCmd_NilOptionalFields(t *testing.T) {
	// arrange a control plane with the optional pointer fields left nil
	controlPlanes := []config_v0.ControlPlaneConfig{
		{ControlPlane: config_v0.ControlPlaneValues{
			Name:                      util.Ptr("cp-minimal"),
			AuthEnabled:               util.Ptr(true),
			Genesis:                   util.Ptr(false),
			KubernetesRuntimeInstance: nil,
			Age:                       nil,
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ControlPlanesCmd(&controlPlanes)
	})

	// assert: renders without panic and includes the name and auth flag
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "cp-minimal") {
		t.Errorf("expected output to contain cp-minimal, got %q", out)
	}
}

// TestOutputGetv0ControlPlanesCmd_NilInnerRuntimeName covers the sub-branch
// where the KubernetesRuntimeInstance is non-nil but its Name is nil: the
// guard's short-circuit is the only thing between the code and a nil deref.
func TestOutputGetv0ControlPlanesCmd_NilInnerRuntimeName(t *testing.T) {
	// arrange: a runtime pointer without a name pointer
	controlPlanes := []config_v0.ControlPlaneConfig{
		{ControlPlane: config_v0.ControlPlaneValues{
			Name:                      util.Ptr("cp-noname"),
			AuthEnabled:               util.Ptr(false),
			Genesis:                   util.Ptr(false),
			KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{Name: nil},
			Age:                       util.Ptr("1h"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ControlPlanesCmd(&controlPlanes)
	})

	// assert: happy return and no panic; the row still carries name and age
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "cp-noname") || !strings.Contains(out, "1h") {
		t.Errorf("expected output to contain cp-noname and 1h, got %q", out)
	}
}

// TestOutputGetv0ControlPlanesCmd_EmptySlice covers the boundary where the
// caller hands in an empty slice: only the header should render.
func TestOutputGetv0ControlPlanesCmd_EmptySlice(t *testing.T) {
	// arrange: an empty slice so the range body never executes
	controlPlanes := []config_v0.ControlPlaneConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ControlPlanesCmd(&controlPlanes)
	})

	// assert: only the single header line remains
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

// TestOutputGetv0ControlPlaneDefinitionsCmd_RendersHeaderAndRows covers the
// happy path for the definitions output: three-column header plus a row per
// definition.
func TestOutputGetv0ControlPlaneDefinitionsCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two definitions with distinct AuthEnabled and Age values
	defs := []config_v0.ControlPlaneDefinitionConfig{
		{ControlPlaneDefinition: config_v0.ControlPlaneDefinitionValues{
			Name:        util.Ptr("def-alpha"),
			AuthEnabled: util.Ptr(true),
			Age:         util.Ptr("3d"),
		}},
		{ControlPlaneDefinition: config_v0.ControlPlaneDefinitionValues{
			Name:        util.Ptr("def-beta"),
			AuthEnabled: util.Ptr(false),
			Age:         util.Ptr("14d"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ControlPlaneDefinitionsCmd(&defs)
	})

	// assert: nil error, header labels, and per-row values present in order
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{"NAME", "AUTH ENABLED", "AGE", "def-alpha", "3d", "def-beta", "14d", "true", "false"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	// def-alpha row precedes def-beta row: preserves iteration order
	if strings.Index(out, "def-alpha") > strings.Index(out, "def-beta") {
		t.Errorf("expected def-alpha row before def-beta row, got %q", out)
	}
}

// TestOutputGetv0ControlPlaneDefinitionsCmd_NilAge covers the branch where the
// Age pointer is nil: the empty-string substitution keeps the row printable.
func TestOutputGetv0ControlPlaneDefinitionsCmd_NilAge(t *testing.T) {
	// arrange: a definition without an Age pointer
	defs := []config_v0.ControlPlaneDefinitionConfig{
		{ControlPlaneDefinition: config_v0.ControlPlaneDefinitionValues{
			Name:        util.Ptr("def-no-age"),
			AuthEnabled: util.Ptr(true),
			Age:         nil,
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ControlPlaneDefinitionsCmd(&defs)
	})

	// assert: no panic; row renders with the name and true flag
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "def-no-age") {
		t.Errorf("expected output to contain def-no-age, got %q", out)
	}
}

// TestOutputGetv0ControlPlaneDefinitionsCmd_EmptySlice covers the boundary
// where the caller hands in an empty slice: only the header should print.
func TestOutputGetv0ControlPlaneDefinitionsCmd_EmptySlice(t *testing.T) {
	// arrange
	defs := []config_v0.ControlPlaneDefinitionConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ControlPlaneDefinitionsCmd(&defs)
	})

	// assert: single header line only
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "AUTH ENABLED") {
		t.Errorf("expected header with AUTH ENABLED, got %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one line for empty input, got %d: %q", len(lines), out)
	}
}

// TestOutputGetv0ControlPlaneInstancesCmd_RendersHeaderAndRows covers the
// happy path for the instances output: all five columns populated per row.
func TestOutputGetv0ControlPlaneInstancesCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two instances with nested ControlPlaneDefinition and
	// KubernetesRuntimeInstance pointers populated
	insts := []config_v0.ControlPlaneInstanceConfig{
		{ControlPlaneInstance: config_v0.ControlPlaneInstanceValues{
			Name:    util.Ptr("inst-alpha"),
			Genesis: util.Ptr(true),
			ControlPlaneDefinition: &config_v0.ControlPlaneDefinitionValues{
				Name: util.Ptr("def-alpha"),
			},
			KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
				Name: util.Ptr("cluster-alpha"),
			},
			Age: util.Ptr("5m"),
		}},
		{ControlPlaneInstance: config_v0.ControlPlaneInstanceValues{
			Name:    util.Ptr("inst-beta"),
			Genesis: util.Ptr(false),
			ControlPlaneDefinition: &config_v0.ControlPlaneDefinitionValues{
				Name: util.Ptr("def-beta"),
			},
			KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{
				Name: util.Ptr("cluster-beta"),
			},
			Age: util.Ptr("1h"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ControlPlaneInstancesCmd(&insts)
	})

	// assert: nil error, header, and every populated cell present
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{
		"NAME", "GENESIS CONTROL PLANE", "CONTROL PLANE DEFINITION",
		"KUBERNETES RUNTIME INSTANCE", "AGE",
		"inst-alpha", "def-alpha", "cluster-alpha", "5m",
		"inst-beta", "def-beta", "cluster-beta", "1h",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	// inst-alpha row precedes inst-beta row: preserves iteration order
	if strings.Index(out, "inst-alpha") > strings.Index(out, "inst-beta") {
		t.Errorf("expected inst-alpha row before inst-beta row, got %q", out)
	}
}

// TestOutputGetv0ControlPlaneInstancesCmd_NilOptionalFields covers the branch
// where every optional pointer (definition, runtime instance, age) is nil.
func TestOutputGetv0ControlPlaneInstancesCmd_NilOptionalFields(t *testing.T) {
	// arrange an instance with only the required fields; every optional pointer nil
	insts := []config_v0.ControlPlaneInstanceConfig{
		{ControlPlaneInstance: config_v0.ControlPlaneInstanceValues{
			Name:                      util.Ptr("inst-minimal"),
			Genesis:                   util.Ptr(false),
			ControlPlaneDefinition:    nil,
			KubernetesRuntimeInstance: nil,
			Age:                       nil,
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ControlPlaneInstancesCmd(&insts)
	})

	// assert: renders without panic; carries the name
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "inst-minimal") {
		t.Errorf("expected output to contain inst-minimal, got %q", out)
	}
}

// TestOutputGetv0ControlPlaneInstancesCmd_NilInnerNames covers the sub-branch
// where the nested pointers exist but their Name pointers are nil: the guard's
// short-circuit is the only thing between the code and a nil deref.
func TestOutputGetv0ControlPlaneInstancesCmd_NilInnerNames(t *testing.T) {
	// arrange nested pointers with nil Names
	insts := []config_v0.ControlPlaneInstanceConfig{
		{ControlPlaneInstance: config_v0.ControlPlaneInstanceValues{
			Name:                      util.Ptr("inst-nonames"),
			Genesis:                   util.Ptr(true),
			ControlPlaneDefinition:    &config_v0.ControlPlaneDefinitionValues{Name: nil},
			KubernetesRuntimeInstance: &config_v0.KubernetesRuntimeInstanceValues{Name: nil},
			Age:                       util.Ptr("2h"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ControlPlaneInstancesCmd(&insts)
	})

	// assert: no panic; row still carries the instance name and age
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "inst-nonames") || !strings.Contains(out, "2h") {
		t.Errorf("expected output to contain inst-nonames and 2h, got %q", out)
	}
}

// TestOutputGetv0ControlPlaneInstancesCmd_EmptySlice covers the boundary where
// the caller hands in an empty slice: only the header should print.
func TestOutputGetv0ControlPlaneInstancesCmd_EmptySlice(t *testing.T) {
	// arrange
	insts := []config_v0.ControlPlaneInstanceConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ControlPlaneInstancesCmd(&insts)
	})

	// assert: single header line only
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "GENESIS CONTROL PLANE") {
		t.Errorf("expected header row with GENESIS CONTROL PLANE, got %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one line for empty input, got %d: %q", len(lines), out)
	}
}
