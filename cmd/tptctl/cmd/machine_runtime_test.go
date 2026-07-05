package cmd

import (
	"testing"

	cobra "github.com/spf13/cobra"
)

// TestMachineRuntimeCommandsCoverExpectedMetadata asserts every exported machine
// runtime cobra command variable in machine_runtime.go carries the metadata
// fields (Use, Short, PreRun, Run, SilenceUsage) callers rely on for tptctl help
// output and dispatch.
func TestMachineRuntimeCommandsCoverExpectedMetadata(t *testing.T) {
	// each table entry pairs a command with the expected Use token and Short
	// description; the fixture list is the surface machine_runtime.go publishes.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		use   string
		short string
	}{
		{"GetMachineRuntimesCmd", GetMachineRuntimesCmd, "machine-runtimes", "Get machine runtimes from the system"},
		{"CreateMachineRuntimeCmd", CreateMachineRuntimeCmd, "machine-runtime", "Create a new machine runtime"},
		{"DeleteMachineRuntimeCmd", DeleteMachineRuntimeCmd, "machine-runtime", "Delete an existing machine runtime"},
		{"GetMachineRuntimeDefinitionsCmd", GetMachineRuntimeDefinitionsCmd, "machine-runtime-definitions", "Get machine runtime definitions from the system"},
		{"CreateMachineRuntimeDefinitionCmd", CreateMachineRuntimeDefinitionCmd, "machine-runtime-definition", "Create a new machine runtime definition"},
		{"ReplaceMachineRuntimeDefinitionCmd", ReplaceMachineRuntimeDefinitionCmd, "machine-runtime-definition", "Replace an existing machine runtime definition"},
		{"DeleteMachineRuntimeDefinitionCmd", DeleteMachineRuntimeDefinitionCmd, "machine-runtime-definition", "Delete an existing machine runtime definition"},
		{"GetMachineRuntimeInstancesCmd", GetMachineRuntimeInstancesCmd, "machine-runtime-instances", "Get machine runtime instances from the system"},
		{"CreateMachineRuntimeInstanceCmd", CreateMachineRuntimeInstanceCmd, "machine-runtime-instance", "Create a new machine runtime instance"},
		{"ReplaceMachineRuntimeInstanceCmd", ReplaceMachineRuntimeInstanceCmd, "machine-runtime-instance", "Replace an existing machine runtime instance"},
		{"DeleteMachineRuntimeInstanceCmd", DeleteMachineRuntimeInstanceCmd, "machine-runtime-instance", "Delete an existing machine runtime instance"},
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

// TestMachineRuntimeGetCommandsExposeSingularAlias asserts that get-style
// commands accept the singular alias so users can type `machine-runtime` in
// place of `machine-runtimes`.
func TestMachineRuntimeGetCommandsExposeSingularAlias(t *testing.T) {
	// each entry maps a plural-form get command to its expected singular alias.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		alias string
	}{
		{"GetMachineRuntimesCmd", GetMachineRuntimesCmd, "machine-runtime"},
		{"GetMachineRuntimeDefinitionsCmd", GetMachineRuntimeDefinitionsCmd, "machine-runtime-definition"},
		{"GetMachineRuntimeInstancesCmd", GetMachineRuntimeInstancesCmd, "machine-runtime-instance"},
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

// TestMachineRuntimeCommandsRegisterExpectedFlags asserts each machine runtime
// command declares the flag surface consumers rely on: --config, --version,
// --control-plane-name on every command, plus --name / --output /
// --decrypt-secrets / --stdin where they appear in machine_runtime.go's init
// blocks.
func TestMachineRuntimeCommandsRegisterExpectedFlags(t *testing.T) {
	// each entry names a command and the flags that must be present on it.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		flags []string
	}{
		{
			name:  "GetMachineRuntimesCmd",
			cmd:   GetMachineRuntimesCmd,
			flags: []string{"name", "config", "version", "output", "decrypt-secrets", "control-plane-name"},
		},
		{
			name:  "CreateMachineRuntimeCmd",
			cmd:   CreateMachineRuntimeCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "DeleteMachineRuntimeCmd",
			cmd:   DeleteMachineRuntimeCmd,
			flags: []string{"config", "control-plane-name", "version"},
		},
		{
			name:  "GetMachineRuntimeDefinitionsCmd",
			cmd:   GetMachineRuntimeDefinitionsCmd,
			flags: []string{"name", "config", "version", "output", "decrypt-secrets", "control-plane-name"},
		},
		{
			name:  "CreateMachineRuntimeDefinitionCmd",
			cmd:   CreateMachineRuntimeDefinitionCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceMachineRuntimeDefinitionCmd",
			cmd:   ReplaceMachineRuntimeDefinitionCmd,
			flags: []string{"config", "stdin", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteMachineRuntimeDefinitionCmd",
			cmd:   DeleteMachineRuntimeDefinitionCmd,
			flags: []string{"config", "name", "control-plane-name", "version"},
		},
		{
			name:  "GetMachineRuntimeInstancesCmd",
			cmd:   GetMachineRuntimeInstancesCmd,
			flags: []string{"name", "config", "version", "output", "decrypt-secrets", "control-plane-name"},
		},
		{
			name:  "CreateMachineRuntimeInstanceCmd",
			cmd:   CreateMachineRuntimeInstanceCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceMachineRuntimeInstanceCmd",
			cmd:   ReplaceMachineRuntimeInstanceCmd,
			flags: []string{"config", "stdin", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteMachineRuntimeInstanceCmd",
			cmd:   DeleteMachineRuntimeInstanceCmd,
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

// TestMachineRuntimeFlagDefaults asserts the default values for --version and
// --output so tptctl behaves consistently when the user omits them.
func TestMachineRuntimeFlagDefaults(t *testing.T) {
	// each entry declares an expected default for a specific (command, flag).
	tests := []struct {
		name        string
		cmd         *cobra.Command
		flag        string
		wantDefault string
	}{
		{"GetMachineRuntimesCmd version default", GetMachineRuntimesCmd, "version", "v0"},
		{"GetMachineRuntimesCmd output default", GetMachineRuntimesCmd, "output", "tabular"},
		{"CreateMachineRuntimeCmd version default", CreateMachineRuntimeCmd, "version", "v0"},
		{"DeleteMachineRuntimeCmd version default", DeleteMachineRuntimeCmd, "version", "v0"},
		{"GetMachineRuntimeDefinitionsCmd version default", GetMachineRuntimeDefinitionsCmd, "version", "v0"},
		{"GetMachineRuntimeDefinitionsCmd output default", GetMachineRuntimeDefinitionsCmd, "output", "tabular"},
		{"CreateMachineRuntimeDefinitionCmd version default", CreateMachineRuntimeDefinitionCmd, "version", "v0"},
		{"ReplaceMachineRuntimeDefinitionCmd version default", ReplaceMachineRuntimeDefinitionCmd, "version", "v0"},
		{"DeleteMachineRuntimeDefinitionCmd version default", DeleteMachineRuntimeDefinitionCmd, "version", "v0"},
		{"GetMachineRuntimeInstancesCmd version default", GetMachineRuntimeInstancesCmd, "version", "v0"},
		{"GetMachineRuntimeInstancesCmd output default", GetMachineRuntimeInstancesCmd, "output", "tabular"},
		{"CreateMachineRuntimeInstanceCmd version default", CreateMachineRuntimeInstanceCmd, "version", "v0"},
		{"ReplaceMachineRuntimeInstanceCmd version default", ReplaceMachineRuntimeInstanceCmd, "version", "v0"},
		{"DeleteMachineRuntimeInstanceCmd version default", DeleteMachineRuntimeInstanceCmd, "version", "v0"},
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

// TestMachineRuntimeReplaceCommandsRequireName asserts every replace command
// marks --name as required, matching MarkFlagRequired("name") in
// machine_runtime.go.
func TestMachineRuntimeReplaceCommandsRequireName(t *testing.T) {
	// every replace command in machine_runtime.go marks --name as required.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"ReplaceMachineRuntimeDefinitionCmd", ReplaceMachineRuntimeDefinitionCmd},
		{"ReplaceMachineRuntimeInstanceCmd", ReplaceMachineRuntimeInstanceCmd},
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

// TestMachineRuntimeCommandsAttachedToParents asserts each machine runtime
// command is registered with the appropriate parent verb command (Get / Create
// / Replace / Delete).
func TestMachineRuntimeCommandsAttachedToParents(t *testing.T) {
	// each entry maps a command to the parent verb command it must live under.
	tests := []struct {
		name   string
		cmd    *cobra.Command
		parent *cobra.Command
	}{
		{"GetMachineRuntimesCmd", GetMachineRuntimesCmd, GetCmd},
		{"CreateMachineRuntimeCmd", CreateMachineRuntimeCmd, CreateCmd},
		{"DeleteMachineRuntimeCmd", DeleteMachineRuntimeCmd, DeleteCmd},
		{"GetMachineRuntimeDefinitionsCmd", GetMachineRuntimeDefinitionsCmd, GetCmd},
		{"CreateMachineRuntimeDefinitionCmd", CreateMachineRuntimeDefinitionCmd, CreateCmd},
		{"ReplaceMachineRuntimeDefinitionCmd", ReplaceMachineRuntimeDefinitionCmd, ReplaceCmd},
		{"DeleteMachineRuntimeDefinitionCmd", DeleteMachineRuntimeDefinitionCmd, DeleteCmd},
		{"GetMachineRuntimeInstancesCmd", GetMachineRuntimeInstancesCmd, GetCmd},
		{"CreateMachineRuntimeInstanceCmd", CreateMachineRuntimeInstanceCmd, CreateCmd},
		{"ReplaceMachineRuntimeInstanceCmd", ReplaceMachineRuntimeInstanceCmd, ReplaceCmd},
		{"DeleteMachineRuntimeInstanceCmd", DeleteMachineRuntimeInstanceCmd, DeleteCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// walk the parent's Commands() slice looking for the machine runtime command.
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

// TestMachineRuntimeFlagShorthandsMatch asserts that the shorthand flags
// machine_runtime.go declares map to the letters users see in the command help.
func TestMachineRuntimeFlagShorthandsMatch(t *testing.T) {
	// each entry pins a (command, flag) to its expected one-letter shorthand.
	tests := []struct {
		name      string
		cmd       *cobra.Command
		flag      string
		shorthand string
	}{
		{"GetMachineRuntimesCmd name -n", GetMachineRuntimesCmd, "name", "n"},
		{"GetMachineRuntimesCmd config -c", GetMachineRuntimesCmd, "config", "c"},
		{"GetMachineRuntimesCmd version -v", GetMachineRuntimesCmd, "version", "v"},
		{"GetMachineRuntimesCmd output -o", GetMachineRuntimesCmd, "output", "o"},
		{"GetMachineRuntimesCmd decrypt-secrets -d", GetMachineRuntimesCmd, "decrypt-secrets", "d"},
		{"GetMachineRuntimesCmd control-plane-name -i", GetMachineRuntimesCmd, "control-plane-name", "i"},
		{"CreateMachineRuntimeCmd config -c", CreateMachineRuntimeCmd, "config", "c"},
		{"DeleteMachineRuntimeDefinitionCmd name -n", DeleteMachineRuntimeDefinitionCmd, "name", "n"},
		{"ReplaceMachineRuntimeInstanceCmd name -n", ReplaceMachineRuntimeInstanceCmd, "name", "n"},
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
