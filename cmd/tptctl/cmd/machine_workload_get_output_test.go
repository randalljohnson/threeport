package cmd

import (
	"strings"
	"testing"

	config_v0 "github.com/threeport/threeport/pkg/config/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestOutputGetv0MachineWorkloadsCmd_RendersHeaderAndRows covers the happy
// path: each MachineWorkloadConfig contributes a row with name repeated in
// the three name columns, plus runtime, status, and age columns.
func TestOutputGetv0MachineWorkloadsCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two workloads with fully populated optional pointer fields
	workloads := []config_v0.MachineWorkloadConfig{
		{MachineWorkload: config_v0.MachineWorkloadValues{
			Name:                   util.Ptr("web"),
			MachineRuntimeInstance: util.Ptr("runtime-a"),
			Status:                 util.Ptr("Healthy"),
			Age:                    util.Ptr("2d"),
		}},
		{MachineWorkload: config_v0.MachineWorkloadValues{
			Name:                   util.Ptr("api"),
			MachineRuntimeInstance: util.Ptr("runtime-b"),
			Status:                 util.Ptr("Reconciling"),
			Age:                    util.Ptr("3h"),
		}},
	}

	// act: capture stdout so we can inspect rendered tabular output
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineWorkloadsCmd(&workloads)
	})

	// assert: nil error and header columns present
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{
		"NAME", "MACHINE WORKLOAD DEFINITION", "MACHINE WORKLOAD INSTANCE",
		"MACHINE RUNTIME INSTANCE", "STATUS", "AGE",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected header column %q in output, got %q", want, out)
		}
	}
	// per-row values render
	for _, want := range []string{"web", "runtime-a", "Healthy", "2d", "api", "runtime-b", "Reconciling", "3h"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	// preserves input order: web precedes api
	if strings.Index(out, "web") > strings.Index(out, "api") {
		t.Errorf("expected web row before api row, got %q", out)
	}
}

// TestOutputGetv0MachineWorkloadsCmd_NilOptionalPointersFallback covers the
// branch where MachineRuntimeInstance, Status, and Age are nil: the columns
// should render as empty strings, not panic.
func TestOutputGetv0MachineWorkloadsCmd_NilOptionalPointersFallback(t *testing.T) {
	// arrange: only Name is set; the three optional pointer fields stay nil
	workloads := []config_v0.MachineWorkloadConfig{
		{MachineWorkload: config_v0.MachineWorkloadValues{
			Name: util.Ptr("bare"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineWorkloadsCmd(&workloads)
	})

	// assert: nil error, name renders, and no stray "<nil>" markers leaked in
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "bare") {
		t.Errorf("expected name 'bare' in output, got %q", out)
	}
	if strings.Contains(out, "<nil>") {
		t.Errorf("expected no <nil> markers for empty optional fields, got %q", out)
	}
}

// TestOutputGetv0MachineWorkloadsCmd_EmptySlice covers the boundary where
// the caller hands in an empty slice: only the header line prints.
func TestOutputGetv0MachineWorkloadsCmd_EmptySlice(t *testing.T) {
	// arrange
	workloads := []config_v0.MachineWorkloadConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineWorkloadsCmd(&workloads)
	})

	// assert: header renders, single line only
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

// TestOutputGetv0MachineWorkloadDefinitionsCmd_RendersHeaderAndRows covers
// the happy path for definitions: NAME + AGE columns, one row per definition.
func TestOutputGetv0MachineWorkloadDefinitionsCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two definitions with distinct ages to observe per-row rendering
	defs := []config_v0.MachineWorkloadDefinitionConfig{
		{MachineWorkloadDefinition: config_v0.MachineWorkloadDefinitionValues{
			Name: util.Ptr("web-def"),
			Age:  util.Ptr("7d"),
		}},
		{MachineWorkloadDefinition: config_v0.MachineWorkloadDefinitionValues{
			Name: util.Ptr("api-def"),
			Age:  util.Ptr("1h"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineWorkloadDefinitionsCmd(&defs)
	})

	// assert: nil error, header, and per-row values present
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{"NAME", "AGE", "web-def", "7d", "api-def", "1h"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	// preserves input order
	if strings.Index(out, "web-def") > strings.Index(out, "api-def") {
		t.Errorf("expected web-def row before api-def row, got %q", out)
	}
}

// TestOutputGetv0MachineWorkloadDefinitionsCmd_NilAgeFallback covers the
// branch where Age is nil: the column renders empty, not "<nil>".
func TestOutputGetv0MachineWorkloadDefinitionsCmd_NilAgeFallback(t *testing.T) {
	// arrange: Age left nil, only Name populated
	defs := []config_v0.MachineWorkloadDefinitionConfig{
		{MachineWorkloadDefinition: config_v0.MachineWorkloadDefinitionValues{
			Name: util.Ptr("no-age"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineWorkloadDefinitionsCmd(&defs)
	})

	// assert: nil error, name renders, no <nil> markers leaked
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "no-age") {
		t.Errorf("expected 'no-age' in output, got %q", out)
	}
	if strings.Contains(out, "<nil>") {
		t.Errorf("expected no <nil> for nil age, got %q", out)
	}
}

// TestOutputGetv0MachineWorkloadDefinitionsCmd_EmptySlice covers the empty
// boundary case: only the header renders.
func TestOutputGetv0MachineWorkloadDefinitionsCmd_EmptySlice(t *testing.T) {
	// arrange
	defs := []config_v0.MachineWorkloadDefinitionConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineWorkloadDefinitionsCmd(&defs)
	})

	// assert: header only, single line
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "AGE") {
		t.Errorf("expected header row, got %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one line for empty input, got %d: %q", len(lines), out)
	}
}

// TestOutputGetv0MachineWorkloadInstancesCmd_RendersHeaderAndRows covers the
// happy path for instances: five-column header plus rows with all optional
// name pointers populated on the nested Values structs.
func TestOutputGetv0MachineWorkloadInstancesCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two instances with fully populated nested definition and runtime refs
	insts := []config_v0.MachineWorkloadInstanceConfig{
		{MachineWorkloadInstance: config_v0.MachineWorkloadInstanceValues{
			Name: util.Ptr("web-inst"),
			MachineWorkloadDefinition: &config_v0.MachineWorkloadDefinitionValues{
				Name: util.Ptr("web-def"),
			},
			MachineRuntimeInstance: &config_v0.MachineRuntimeInstanceValues{
				Name: util.Ptr("runtime-a"),
			},
			Status: util.Ptr("Healthy"),
			Age:    util.Ptr("2d"),
		}},
		{MachineWorkloadInstance: config_v0.MachineWorkloadInstanceValues{
			Name: util.Ptr("api-inst"),
			MachineWorkloadDefinition: &config_v0.MachineWorkloadDefinitionValues{
				Name: util.Ptr("api-def"),
			},
			MachineRuntimeInstance: &config_v0.MachineRuntimeInstanceValues{
				Name: util.Ptr("runtime-b"),
			},
			Status: util.Ptr("Reconciling"),
			Age:    util.Ptr("3h"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineWorkloadInstancesCmd(&insts)
	})

	// assert: nil error, five-column header, and per-row values present
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{
		"NAME", "MACHINE WORKLOAD DEFINITION", "MACHINE RUNTIME INSTANCE", "STATUS", "AGE",
		"web-inst", "web-def", "runtime-a", "Healthy", "2d",
		"api-inst", "api-def", "runtime-b", "Reconciling", "3h",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	// preserves input order
	if strings.Index(out, "web-inst") > strings.Index(out, "api-inst") {
		t.Errorf("expected web-inst row before api-inst row, got %q", out)
	}
}

// TestOutputGetv0MachineWorkloadInstancesCmd_NilNestedRefs covers the two
// pointer-guard branches: MachineWorkloadDefinition and MachineRuntimeInstance
// nil at the top level should fall back to empty column strings.
func TestOutputGetv0MachineWorkloadInstancesCmd_NilNestedRefs(t *testing.T) {
	// arrange: both nested reference pointers left nil so the guard
	// short-circuits to an empty name for those columns
	insts := []config_v0.MachineWorkloadInstanceConfig{
		{MachineWorkloadInstance: config_v0.MachineWorkloadInstanceValues{
			Name: util.Ptr("orphan"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineWorkloadInstancesCmd(&insts)
	})

	// assert: nil error, name renders, no <nil> markers leaked
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "orphan") {
		t.Errorf("expected 'orphan' in output, got %q", out)
	}
	if strings.Contains(out, "<nil>") {
		t.Errorf("expected no <nil> for nil nested refs, got %q", out)
	}
}

// TestOutputGetv0MachineWorkloadInstancesCmd_NestedPresentButInnerNameNil
// covers the subtler branch where the nested Values struct is non-nil but
// its Name pointer is nil: the second half of the guard should still fall
// back to an empty column.
func TestOutputGetv0MachineWorkloadInstancesCmd_NestedPresentButInnerNameNil(t *testing.T) {
	// arrange: nested pointers non-nil but Name inside them is nil
	insts := []config_v0.MachineWorkloadInstanceConfig{
		{MachineWorkloadInstance: config_v0.MachineWorkloadInstanceValues{
			Name:                      util.Ptr("halfset"),
			MachineWorkloadDefinition: &config_v0.MachineWorkloadDefinitionValues{},
			MachineRuntimeInstance:    &config_v0.MachineRuntimeInstanceValues{},
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineWorkloadInstancesCmd(&insts)
	})

	// assert: nil error, own name renders, no panic and no <nil> leaked
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "halfset") {
		t.Errorf("expected 'halfset' in output, got %q", out)
	}
	if strings.Contains(out, "<nil>") {
		t.Errorf("expected no <nil> for nested nil-name pointers, got %q", out)
	}
}

// TestOutputGetv0MachineWorkloadInstancesCmd_EmptySlice covers the empty
// boundary: only the header renders.
func TestOutputGetv0MachineWorkloadInstancesCmd_EmptySlice(t *testing.T) {
	// arrange
	insts := []config_v0.MachineWorkloadInstanceConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0MachineWorkloadInstancesCmd(&insts)
	})

	// assert: single header line, no rows
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "STATUS") {
		t.Errorf("expected header row with STATUS, got %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one line for empty input, got %d: %q", len(lines), out)
	}
}
