package cmd

import (
	"testing"

	cobra "github.com/spf13/cobra"
)

// TestMachineWorkloadCommandsCoverExpectedMetadata asserts every exported
// machine workload cobra command variable in machine_workload.go carries the
// metadata fields (Use, Short, PreRun, Run, SilenceUsage) callers rely on for
// tptctl help output and dispatch.
func TestMachineWorkloadCommandsCoverExpectedMetadata(t *testing.T) {
	// each table entry pairs a command with the expected Use token and Short
	// description; the fixture list is the surface machine_workload.go publishes.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		use   string
		short string
	}{
		{"GetMachineWorkloadsCmd", GetMachineWorkloadsCmd, "machine-workloads", "Get machine workloads from the system"},
		{"CreateMachineWorkloadCmd", CreateMachineWorkloadCmd, "machine-workload", "Create a new machine workload"},
		{"DeleteMachineWorkloadCmd", DeleteMachineWorkloadCmd, "machine-workload", "Delete an existing machine workload"},
		{"GetMachineWorkloadDefinitionsCmd", GetMachineWorkloadDefinitionsCmd, "machine-workload-definitions", "Get machine workload definitions from the system"},
		{"CreateMachineWorkloadDefinitionCmd", CreateMachineWorkloadDefinitionCmd, "machine-workload-definition", "Create a new machine workload definition"},
		{"ReplaceMachineWorkloadDefinitionCmd", ReplaceMachineWorkloadDefinitionCmd, "machine-workload-definition", "Replace an existing machine workload definition"},
		{"DeleteMachineWorkloadDefinitionCmd", DeleteMachineWorkloadDefinitionCmd, "machine-workload-definition", "Delete an existing machine workload definition"},
		{"GetMachineWorkloadInstancesCmd", GetMachineWorkloadInstancesCmd, "machine-workload-instances", "Get machine workload instances from the system"},
		{"CreateMachineWorkloadInstanceCmd", CreateMachineWorkloadInstanceCmd, "machine-workload-instance", "Create a new machine workload instance"},
		{"ReplaceMachineWorkloadInstanceCmd", ReplaceMachineWorkloadInstanceCmd, "machine-workload-instance", "Replace an existing machine workload instance"},
		{"DeleteMachineWorkloadInstanceCmd", DeleteMachineWorkloadInstanceCmd, "machine-workload-instance", "Delete an existing machine workload instance"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// command exists and is non-nil.
			if tt.cmd == nil {
				t.Fatalf("command %s is nil", tt.name)
			}
			// Use verb matches expected token.
			if tt.cmd.Use != tt.use {
				t.Errorf("Use = %q, want %q", tt.cmd.Use, tt.use)
			}
			// Short description matches.
			if tt.cmd.Short != tt.short {
				t.Errorf("Short = %q, want %q", tt.cmd.Short, tt.short)
			}
			// Long is populated so `--help` produces the full description.
			if tt.cmd.Long == "" {
				t.Errorf("Long is empty")
			}
			// Example text is populated so users see a runnable sample.
			if tt.cmd.Example == "" {
				t.Errorf("Example is empty")
			}
			// SilenceUsage is true so failures do not dump usage on top of the error.
			if !tt.cmd.SilenceUsage {
				t.Errorf("SilenceUsage = false, want true")
			}
			// PreRun and Run must be wired so cobra can dispatch.
			if tt.cmd.PreRun == nil {
				t.Errorf("PreRun is nil")
			}
			if tt.cmd.Run == nil {
				t.Errorf("Run is nil")
			}
		})
	}
}

// TestMachineWorkloadGetCommandsExposeSingularAlias asserts that get-style
// commands accept the singular alias so users can type `machine-workload` in
// place of `machine-workloads`.
func TestMachineWorkloadGetCommandsExposeSingularAlias(t *testing.T) {
	// each entry maps a plural-form get command to its expected singular alias.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		alias string
	}{
		{"GetMachineWorkloadsCmd", GetMachineWorkloadsCmd, "machine-workload"},
		{"GetMachineWorkloadDefinitionsCmd", GetMachineWorkloadDefinitionsCmd, "machine-workload-definition"},
		{"GetMachineWorkloadInstancesCmd", GetMachineWorkloadInstancesCmd, "machine-workload-instance"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// walk the Aliases slice to confirm the singular alias is present.
			found := false
			for _, a := range tt.cmd.Aliases {
				if a == tt.alias {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Aliases = %v, want to contain %q", tt.cmd.Aliases, tt.alias)
			}
		})
	}
}

// TestMachineWorkloadCommandsRegisterExpectedFlags asserts each machine
// workload command declares the flag surface consumers rely on: --config,
// --version, --control-plane-name on every command, plus --name / --output /
// --decrypt-secrets / --stdin where they appear in machine_workload.go's init
// blocks.
func TestMachineWorkloadCommandsRegisterExpectedFlags(t *testing.T) {
	// each entry names a command and the flags that must be present on it.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		flags []string
	}{
		{
			name:  "GetMachineWorkloadsCmd",
			cmd:   GetMachineWorkloadsCmd,
			flags: []string{"name", "config", "version", "output", "decrypt-secrets", "control-plane-name"},
		},
		{
			name:  "CreateMachineWorkloadCmd",
			cmd:   CreateMachineWorkloadCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "DeleteMachineWorkloadCmd",
			cmd:   DeleteMachineWorkloadCmd,
			flags: []string{"config", "control-plane-name", "version"},
		},
		{
			name:  "GetMachineWorkloadDefinitionsCmd",
			cmd:   GetMachineWorkloadDefinitionsCmd,
			flags: []string{"name", "config", "version", "output", "decrypt-secrets", "control-plane-name"},
		},
		{
			name:  "CreateMachineWorkloadDefinitionCmd",
			cmd:   CreateMachineWorkloadDefinitionCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceMachineWorkloadDefinitionCmd",
			cmd:   ReplaceMachineWorkloadDefinitionCmd,
			flags: []string{"config", "stdin", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteMachineWorkloadDefinitionCmd",
			cmd:   DeleteMachineWorkloadDefinitionCmd,
			flags: []string{"config", "name", "control-plane-name", "version"},
		},
		{
			name:  "GetMachineWorkloadInstancesCmd",
			cmd:   GetMachineWorkloadInstancesCmd,
			flags: []string{"name", "config", "version", "output", "decrypt-secrets", "control-plane-name"},
		},
		{
			name:  "CreateMachineWorkloadInstanceCmd",
			cmd:   CreateMachineWorkloadInstanceCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceMachineWorkloadInstanceCmd",
			cmd:   ReplaceMachineWorkloadInstanceCmd,
			flags: []string{"config", "stdin", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteMachineWorkloadInstanceCmd",
			cmd:   DeleteMachineWorkloadInstanceCmd,
			flags: []string{"config", "name", "control-plane-name", "version"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// each expected flag must be discoverable on the command.
			for _, f := range tt.flags {
				if tt.cmd.Flags().Lookup(f) == nil {
					t.Errorf("flag %q is not registered", f)
				}
			}
		})
	}
}

// TestMachineWorkloadFlagDefaults asserts the default values for --version and
// --output so tptctl behaves consistently when the user omits them.
func TestMachineWorkloadFlagDefaults(t *testing.T) {
	// each entry declares an expected default for a specific (command, flag).
	tests := []struct {
		name        string
		cmd         *cobra.Command
		flag        string
		wantDefault string
	}{
		{"GetMachineWorkloadsCmd version default", GetMachineWorkloadsCmd, "version", "v0"},
		{"GetMachineWorkloadsCmd output default", GetMachineWorkloadsCmd, "output", "tabular"},
		{"CreateMachineWorkloadCmd version default", CreateMachineWorkloadCmd, "version", "v0"},
		{"DeleteMachineWorkloadCmd version default", DeleteMachineWorkloadCmd, "version", "v0"},
		{"GetMachineWorkloadDefinitionsCmd version default", GetMachineWorkloadDefinitionsCmd, "version", "v0"},
		{"GetMachineWorkloadDefinitionsCmd output default", GetMachineWorkloadDefinitionsCmd, "output", "tabular"},
		{"CreateMachineWorkloadDefinitionCmd version default", CreateMachineWorkloadDefinitionCmd, "version", "v0"},
		{"ReplaceMachineWorkloadDefinitionCmd version default", ReplaceMachineWorkloadDefinitionCmd, "version", "v0"},
		{"DeleteMachineWorkloadDefinitionCmd version default", DeleteMachineWorkloadDefinitionCmd, "version", "v0"},
		{"GetMachineWorkloadInstancesCmd version default", GetMachineWorkloadInstancesCmd, "version", "v0"},
		{"GetMachineWorkloadInstancesCmd output default", GetMachineWorkloadInstancesCmd, "output", "tabular"},
		{"CreateMachineWorkloadInstanceCmd version default", CreateMachineWorkloadInstanceCmd, "version", "v0"},
		{"ReplaceMachineWorkloadInstanceCmd version default", ReplaceMachineWorkloadInstanceCmd, "version", "v0"},
		{"DeleteMachineWorkloadInstanceCmd version default", DeleteMachineWorkloadInstanceCmd, "version", "v0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// look up the flag, then compare DefValue against expectation.
			f := tt.cmd.Flags().Lookup(tt.flag)
			if f == nil {
				t.Fatalf("flag %q not registered", tt.flag)
			}
			if f.DefValue != tt.wantDefault {
				t.Errorf("flag %q DefValue = %q, want %q", tt.flag, f.DefValue, tt.wantDefault)
			}
		})
	}
}

// TestMachineWorkloadReplaceCommandsRequireName asserts every replace command
// marks --name as required, matching MarkFlagRequired("name") in
// machine_workload.go.
func TestMachineWorkloadReplaceCommandsRequireName(t *testing.T) {
	// every replace command in machine_workload.go marks --name as required.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"ReplaceMachineWorkloadDefinitionCmd", ReplaceMachineWorkloadDefinitionCmd},
		{"ReplaceMachineWorkloadInstanceCmd", ReplaceMachineWorkloadInstanceCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// cobra records required flags in an annotation on the flag itself.
			f := tt.cmd.Flags().Lookup("name")
			if f == nil {
				t.Fatalf("flag \"name\" not registered")
			}
			required, ok := f.Annotations[cobra.BashCompOneRequiredFlag]
			if !ok || len(required) == 0 || required[0] != "true" {
				t.Errorf("--name is not marked required (annotations = %v)", f.Annotations)
			}
		})
	}
}

// TestMachineWorkloadCommandsAttachedToParents asserts each machine workload
// command is registered with the appropriate parent verb command (Get / Create
// / Replace / Delete).
func TestMachineWorkloadCommandsAttachedToParents(t *testing.T) {
	// each entry maps a command to the parent verb command it must live under.
	tests := []struct {
		name   string
		cmd    *cobra.Command
		parent *cobra.Command
	}{
		{"GetMachineWorkloadsCmd", GetMachineWorkloadsCmd, GetCmd},
		{"CreateMachineWorkloadCmd", CreateMachineWorkloadCmd, CreateCmd},
		{"DeleteMachineWorkloadCmd", DeleteMachineWorkloadCmd, DeleteCmd},
		{"GetMachineWorkloadDefinitionsCmd", GetMachineWorkloadDefinitionsCmd, GetCmd},
		{"CreateMachineWorkloadDefinitionCmd", CreateMachineWorkloadDefinitionCmd, CreateCmd},
		{"ReplaceMachineWorkloadDefinitionCmd", ReplaceMachineWorkloadDefinitionCmd, ReplaceCmd},
		{"DeleteMachineWorkloadDefinitionCmd", DeleteMachineWorkloadDefinitionCmd, DeleteCmd},
		{"GetMachineWorkloadInstancesCmd", GetMachineWorkloadInstancesCmd, GetCmd},
		{"CreateMachineWorkloadInstanceCmd", CreateMachineWorkloadInstanceCmd, CreateCmd},
		{"ReplaceMachineWorkloadInstanceCmd", ReplaceMachineWorkloadInstanceCmd, ReplaceCmd},
		{"DeleteMachineWorkloadInstanceCmd", DeleteMachineWorkloadInstanceCmd, DeleteCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// walk the parent's Commands() slice looking for the machine workload command.
			found := false
			for _, c := range tt.parent.Commands() {
				if c == tt.cmd {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s is not registered as a subcommand of %s", tt.name, tt.parent.Use)
			}
			// parent linkage on the child must also point back.
			if tt.cmd.Parent() != tt.parent {
				t.Errorf("Parent() = %v, want %v", tt.cmd.Parent(), tt.parent)
			}
		})
	}
}

// TestMachineWorkloadFlagShorthandsMatch asserts that the shorthand flags
// machine_workload.go declares map to the letters users see in the command help.
func TestMachineWorkloadFlagShorthandsMatch(t *testing.T) {
	// each entry pins a (command, flag) to its expected one-letter shorthand.
	tests := []struct {
		name      string
		cmd       *cobra.Command
		flag      string
		shorthand string
	}{
		{"GetMachineWorkloadsCmd name -n", GetMachineWorkloadsCmd, "name", "n"},
		{"GetMachineWorkloadsCmd config -c", GetMachineWorkloadsCmd, "config", "c"},
		{"GetMachineWorkloadsCmd version -v", GetMachineWorkloadsCmd, "version", "v"},
		{"GetMachineWorkloadsCmd output -o", GetMachineWorkloadsCmd, "output", "o"},
		{"GetMachineWorkloadsCmd decrypt-secrets -d", GetMachineWorkloadsCmd, "decrypt-secrets", "d"},
		{"GetMachineWorkloadsCmd control-plane-name -i", GetMachineWorkloadsCmd, "control-plane-name", "i"},
		{"CreateMachineWorkloadCmd config -c", CreateMachineWorkloadCmd, "config", "c"},
		{"DeleteMachineWorkloadDefinitionCmd name -n", DeleteMachineWorkloadDefinitionCmd, "name", "n"},
		{"ReplaceMachineWorkloadInstanceCmd name -n", ReplaceMachineWorkloadInstanceCmd, "name", "n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// pflag stores the one-letter alias on the Flag itself.
			f := tt.cmd.Flags().Lookup(tt.flag)
			if f == nil {
				t.Fatalf("flag %q not registered", tt.flag)
			}
			if f.Shorthand != tt.shorthand {
				t.Errorf("flag %q shorthand = %q, want %q", tt.flag, f.Shorthand, tt.shorthand)
			}
		})
	}
}
