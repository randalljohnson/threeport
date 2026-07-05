package cmd

import (
	"strings"
	"testing"

	config_v0 "github.com/threeport/threeport/pkg/config/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestOutputGetv0MachineRuntimesCmd_RendersHeaderAndRows covers the happy path
// for the machine-runtimes tabular output: full six-column header and per-row
// content for every entry in the input slice.
func TestOutputGetv0MachineRuntimesCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two machine runtimes with all optional pointer fields populated
	runtimes := []config_v0.MachineRuntimeConfig{
		{MachineRuntime: config_v0.MachineRuntimeValues{
			Name:     util.Ptr("mr-alpha"),
			Hostname: util.Ptr("alpha.example"),
			Status:   util.Ptr("Reconciled"),
			Age:      util.Ptr("3d"),
		}},
		{MachineRuntime: config_v0.MachineRuntimeValues{
			Name:     util.Ptr("mr-beta"),
			Hostname: util.Ptr("beta.example"),
			Status:   util.Ptr("Pending"),
			Age:      util.Ptr("7h"),
		}},
	}

	// act: invoke with stdout redirected so we can inspect what tabwriter emitted
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineRuntimesCmd(&runtimes)
	})

	// assert: nil error and full header appears
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, col := range []string{"NAME", "MACHINE RUNTIME DEFINITION", "MACHINE RUNTIME INSTANCE", "HOSTNAME", "STATUS", "AGE"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected header column %q, got %q", col, out)
		}
	}
	// per-row values render, including the name reused across three columns
	for _, want := range []string{"mr-alpha", "alpha.example", "Reconciled", "3d", "mr-beta", "beta.example", "Pending", "7h"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	// input order is preserved: alpha row precedes beta row
	if strings.Index(out, "mr-alpha") > strings.Index(out, "mr-beta") {
		t.Errorf("expected mr-alpha row before mr-beta row, got %q", out)
	}
}

// TestOutputGetv0MachineRuntimesCmd_NilOptionalFields covers the branch where
// Hostname, Status, and Age are nil: the function must render empty strings
// for those columns rather than dereferencing a nil pointer.
func TestOutputGetv0MachineRuntimesCmd_NilOptionalFields(t *testing.T) {
	// arrange a runtime with only the required Name set; all optional pointers nil
	runtimes := []config_v0.MachineRuntimeConfig{
		{MachineRuntime: config_v0.MachineRuntimeValues{
			Name: util.Ptr("mr-minimal"),
		}},
	}

	// act: nil-guarded fields must not panic
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineRuntimesCmd(&runtimes)
	})

	// assert: nil error and Name still renders
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "mr-minimal") {
		t.Errorf("expected output to contain mr-minimal, got %q", out)
	}
}

// TestOutputGetv0MachineRuntimesCmd_EmptySlice covers the boundary where the
// caller passes an empty slice: only the header line should print.
func TestOutputGetv0MachineRuntimesCmd_EmptySlice(t *testing.T) {
	// arrange an empty slice so the range body never executes
	runtimes := []config_v0.MachineRuntimeConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineRuntimesCmd(&runtimes)
	})

	// assert: nil error and exactly one line of output (the header)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "NAME") {
		t.Errorf("expected header row, got %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one header line for empty input, got %d: %q", len(lines), out)
	}
}

// TestOutputGetv0MachineRuntimeDefinitionsCmd_RendersHeaderAndRows covers the
// happy path for the machine-runtime-definitions output: NAME + AGE header
// plus one row per definition.
func TestOutputGetv0MachineRuntimeDefinitionsCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two definitions with age populated
	definitions := []config_v0.MachineRuntimeDefinitionConfig{
		{MachineRuntimeDefinition: config_v0.MachineRuntimeDefinitionValues{
			Name: util.Ptr("def-one"),
			Age:  util.Ptr("2d"),
		}},
		{MachineRuntimeDefinition: config_v0.MachineRuntimeDefinitionValues{
			Name: util.Ptr("def-two"),
			Age:  util.Ptr("5d"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineRuntimeDefinitionsCmd(&definitions)
	})

	// assert: header and per-row values render, input order preserved
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{"NAME", "AGE", "def-one", "2d", "def-two", "5d"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	if strings.Index(out, "def-one") > strings.Index(out, "def-two") {
		t.Errorf("expected def-one before def-two, got %q", out)
	}
}

// TestOutputGetv0MachineRuntimeDefinitionsCmd_NilAge covers the branch where
// Age is nil: the row must still render with an empty age column instead of
// dereferencing the nil pointer.
func TestOutputGetv0MachineRuntimeDefinitionsCmd_NilAge(t *testing.T) {
	// arrange a definition with only Name set
	definitions := []config_v0.MachineRuntimeDefinitionConfig{
		{MachineRuntimeDefinition: config_v0.MachineRuntimeDefinitionValues{
			Name: util.Ptr("def-noage"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineRuntimeDefinitionsCmd(&definitions)
	})

	// assert: nil-guarded field renders without panic; Name still present
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "def-noage") {
		t.Errorf("expected output to contain def-noage, got %q", out)
	}
}

// TestOutputGetv0MachineRuntimeDefinitionsCmd_EmptySlice covers the boundary
// where the caller passes an empty slice: only the header line prints.
func TestOutputGetv0MachineRuntimeDefinitionsCmd_EmptySlice(t *testing.T) {
	// arrange
	definitions := []config_v0.MachineRuntimeDefinitionConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineRuntimeDefinitionsCmd(&definitions)
	})

	// assert: single header line
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one header line for empty input, got %d: %q", len(lines), out)
	}
}

// TestOutputGetv0MachineRuntimeInstancesCmd_RendersHeaderAndRows covers the
// happy path for the machine-runtime-instances output including the linked
// MachineRuntimeDefinition name column.
func TestOutputGetv0MachineRuntimeInstancesCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two instances, each linked to a named definition with all
	// optional pointer fields populated
	instances := []config_v0.MachineRuntimeInstanceConfig{
		{MachineRuntimeInstance: config_v0.MachineRuntimeInstanceValues{
			Name:     util.Ptr("inst-one"),
			Hostname: util.Ptr("host-one.example"),
			Status:   util.Ptr("Running"),
			Age:      util.Ptr("1h"),
			MachineRuntimeDefinition: &config_v0.MachineRuntimeDefinitionValues{
				Name: util.Ptr("linked-def-a"),
			},
		}},
		{MachineRuntimeInstance: config_v0.MachineRuntimeInstanceValues{
			Name:     util.Ptr("inst-two"),
			Hostname: util.Ptr("host-two.example"),
			Status:   util.Ptr("Stopped"),
			Age:      util.Ptr("2h"),
			MachineRuntimeDefinition: &config_v0.MachineRuntimeDefinitionValues{
				Name: util.Ptr("linked-def-b"),
			},
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineRuntimeInstancesCmd(&instances)
	})

	// assert: full five-column header appears
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, col := range []string{"NAME", "MACHINE RUNTIME DEFINITION", "HOSTNAME", "STATUS", "AGE"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected header column %q, got %q", col, out)
		}
	}
	// per-row values render, including the linked definition names
	for _, want := range []string{"inst-one", "linked-def-a", "host-one.example", "Running", "1h", "inst-two", "linked-def-b", "host-two.example", "Stopped", "2h"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	// input order preserved
	if strings.Index(out, "inst-one") > strings.Index(out, "inst-two") {
		t.Errorf("expected inst-one row before inst-two row, got %q", out)
	}
}

// TestOutputGetv0MachineRuntimeInstancesCmd_NilDefinitionReference covers the
// branch where MachineRuntimeDefinition is nil: the guarded lookup must skip
// the deref and leave the DEFINITION column blank.
func TestOutputGetv0MachineRuntimeInstancesCmd_NilDefinitionReference(t *testing.T) {
	// arrange an instance whose MachineRuntimeDefinition pointer is nil
	instances := []config_v0.MachineRuntimeInstanceConfig{
		{MachineRuntimeInstance: config_v0.MachineRuntimeInstanceValues{
			Name:                     util.Ptr("inst-nodef"),
			Hostname:                 util.Ptr("host.example"),
			Status:                   util.Ptr("Running"),
			Age:                      util.Ptr("1h"),
			MachineRuntimeDefinition: nil,
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineRuntimeInstancesCmd(&instances)
	})

	// assert: nil-guard prevents panic and Name still renders
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "inst-nodef") {
		t.Errorf("expected output to contain inst-nodef, got %q", out)
	}
}

// TestOutputGetv0MachineRuntimeInstancesCmd_DefinitionWithNilName covers the
// second half of the guard: MachineRuntimeDefinition is non-nil but its Name
// pointer is nil. The nested check must skip the deref.
func TestOutputGetv0MachineRuntimeInstancesCmd_DefinitionWithNilName(t *testing.T) {
	// arrange an instance whose linked definition exists but has a nil Name
	instances := []config_v0.MachineRuntimeInstanceConfig{
		{MachineRuntimeInstance: config_v0.MachineRuntimeInstanceValues{
			Name: util.Ptr("inst-defnilname"),
			MachineRuntimeDefinition: &config_v0.MachineRuntimeDefinitionValues{
				Name: nil,
			},
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineRuntimeInstancesCmd(&instances)
	})

	// assert: nested nil-guard prevents panic
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "inst-defnilname") {
		t.Errorf("expected output to contain inst-defnilname, got %q", out)
	}
}

// TestOutputGetv0MachineRuntimeInstancesCmd_NilOptionalFields covers the
// branches where Hostname, Status, and Age are all nil.
func TestOutputGetv0MachineRuntimeInstancesCmd_NilOptionalFields(t *testing.T) {
	// arrange an instance with only Name set
	instances := []config_v0.MachineRuntimeInstanceConfig{
		{MachineRuntimeInstance: config_v0.MachineRuntimeInstanceValues{
			Name: util.Ptr("inst-minimal"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineRuntimeInstancesCmd(&instances)
	})

	// assert: no panic, Name renders
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "inst-minimal") {
		t.Errorf("expected output to contain inst-minimal, got %q", out)
	}
}

// TestOutputGetv0MachineRuntimeInstancesCmd_EmptySlice covers the boundary
// where the caller passes an empty slice: only the header line prints.
func TestOutputGetv0MachineRuntimeInstancesCmd_EmptySlice(t *testing.T) {
	// arrange
	instances := []config_v0.MachineRuntimeInstanceConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineRuntimeInstancesCmd(&instances)
	})

	// assert: single header line only
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one header line for empty input, got %d: %q", len(lines), out)
	}
}
