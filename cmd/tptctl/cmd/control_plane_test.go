package cmd

import (
	"testing"

	cobra "github.com/spf13/cobra"
)

// TestControlPlaneCommandsCoverExpectedMetadata asserts that every exported
// control-plane cobra command variable in control_plane.go carries the metadata
// fields (Use, Short, Long, Example, PreRun, Run, SilenceUsage) callers rely on
// for tptctl help output and dispatch.
func TestControlPlaneCommandsCoverExpectedMetadata(t *testing.T) {
	// each table entry pairs a command with the expected Use token and Short
	// description; the fixture list is the surface control_plane.go publishes.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		use   string
		short string
	}{
		{"GetControlPlanesCmd", GetControlPlanesCmd, "control-planes", "Get control planes from the system"},
		{"CreateControlPlaneCmd", CreateControlPlaneCmd, "control-plane", "Create a new control plane"},
		{"DeleteControlPlaneCmd", DeleteControlPlaneCmd, "control-plane", "Delete an existing control plane"},
		{"GetControlPlaneDefinitionsCmd", GetControlPlaneDefinitionsCmd, "control-plane-definitions", "Get control plane definitions from the system"},
		{"CreateControlPlaneDefinitionCmd", CreateControlPlaneDefinitionCmd, "control-plane-definition", "Create a new control plane definition"},
		{"ReplaceControlPlaneDefinitionCmd", ReplaceControlPlaneDefinitionCmd, "control-plane-definition", "Replace an existing control plane definition"},
		{"DeleteControlPlaneDefinitionCmd", DeleteControlPlaneDefinitionCmd, "control-plane-definition", "Delete an existing control plane definition"},
		{"GetControlPlaneInstancesCmd", GetControlPlaneInstancesCmd, "control-plane-instances", "Get control plane instances from the system"},
		{"CreateControlPlaneInstanceCmd", CreateControlPlaneInstanceCmd, "control-plane-instance", "Create a new control plane instance"},
		{"ReplaceControlPlaneInstanceCmd", ReplaceControlPlaneInstanceCmd, "control-plane-instance", "Replace an existing control plane instance"},
		{"DeleteControlPlaneInstanceCmd", DeleteControlPlaneInstanceCmd, "control-plane-instance", "Delete an existing control plane instance"},
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

// TestControlPlaneGetCommandsExposeSingularAlias asserts that get-style commands
// accept the singular alias so users can type `control-plane` in place of
// `control-planes`.
func TestControlPlaneGetCommandsExposeSingularAlias(t *testing.T) {
	// each entry maps a plural-form get command to its expected singular alias.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		alias string
	}{
		{"GetControlPlanesCmd", GetControlPlanesCmd, "control-plane"},
		{"GetControlPlaneDefinitionsCmd", GetControlPlaneDefinitionsCmd, "control-plane-definition"},
		{"GetControlPlaneInstancesCmd", GetControlPlaneInstancesCmd, "control-plane-instance"},
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

// TestControlPlaneCommandsRegisterExpectedFlags asserts each control-plane
// command declares the flag surface consumers rely on: --config, --version,
// --control-plane-name on every command, plus --name / --output / --stdin
// where they appear in control_plane.go's init blocks.
func TestControlPlaneCommandsRegisterExpectedFlags(t *testing.T) {
	// each entry names a command and the flags that must be present on it.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		flags []string
	}{
		{
			name:  "GetControlPlanesCmd",
			cmd:   GetControlPlanesCmd,
			flags: []string{"name", "config", "version", "output", "control-plane-name"},
		},
		{
			name:  "CreateControlPlaneCmd",
			cmd:   CreateControlPlaneCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "DeleteControlPlaneCmd",
			cmd:   DeleteControlPlaneCmd,
			flags: []string{"config", "control-plane-name", "version"},
		},
		{
			name:  "GetControlPlaneDefinitionsCmd",
			cmd:   GetControlPlaneDefinitionsCmd,
			flags: []string{"name", "config", "version", "output", "control-plane-name"},
		},
		{
			name:  "CreateControlPlaneDefinitionCmd",
			cmd:   CreateControlPlaneDefinitionCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceControlPlaneDefinitionCmd",
			cmd:   ReplaceControlPlaneDefinitionCmd,
			flags: []string{"config", "stdin", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteControlPlaneDefinitionCmd",
			cmd:   DeleteControlPlaneDefinitionCmd,
			flags: []string{"config", "name", "control-plane-name", "version"},
		},
		{
			name:  "GetControlPlaneInstancesCmd",
			cmd:   GetControlPlaneInstancesCmd,
			flags: []string{"name", "config", "version", "output", "control-plane-name"},
		},
		{
			name:  "CreateControlPlaneInstanceCmd",
			cmd:   CreateControlPlaneInstanceCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceControlPlaneInstanceCmd",
			cmd:   ReplaceControlPlaneInstanceCmd,
			flags: []string{"config", "stdin", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteControlPlaneInstanceCmd",
			cmd:   DeleteControlPlaneInstanceCmd,
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

// TestControlPlaneFlagDefaults asserts the default values for --version and
// --output so tptctl behaves consistently when the user omits them.
func TestControlPlaneFlagDefaults(t *testing.T) {
	// each entry declares an expected default for a specific (command, flag).
	tests := []struct {
		name        string
		cmd         *cobra.Command
		flag        string
		wantDefault string
	}{
		{"GetControlPlanesCmd version default", GetControlPlanesCmd, "version", "v0"},
		{"GetControlPlanesCmd output default", GetControlPlanesCmd, "output", "tabular"},
		{"CreateControlPlaneCmd version default", CreateControlPlaneCmd, "version", "v0"},
		{"DeleteControlPlaneCmd version default", DeleteControlPlaneCmd, "version", "v0"},
		{"GetControlPlaneDefinitionsCmd version default", GetControlPlaneDefinitionsCmd, "version", "v0"},
		{"GetControlPlaneDefinitionsCmd output default", GetControlPlaneDefinitionsCmd, "output", "tabular"},
		{"CreateControlPlaneDefinitionCmd version default", CreateControlPlaneDefinitionCmd, "version", "v0"},
		{"ReplaceControlPlaneDefinitionCmd version default", ReplaceControlPlaneDefinitionCmd, "version", "v0"},
		{"DeleteControlPlaneDefinitionCmd version default", DeleteControlPlaneDefinitionCmd, "version", "v0"},
		{"GetControlPlaneInstancesCmd version default", GetControlPlaneInstancesCmd, "version", "v0"},
		{"GetControlPlaneInstancesCmd output default", GetControlPlaneInstancesCmd, "output", "tabular"},
		{"CreateControlPlaneInstanceCmd version default", CreateControlPlaneInstanceCmd, "version", "v0"},
		{"ReplaceControlPlaneInstanceCmd version default", ReplaceControlPlaneInstanceCmd, "version", "v0"},
		{"DeleteControlPlaneInstanceCmd version default", DeleteControlPlaneInstanceCmd, "version", "v0"},
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

// TestControlPlaneReplaceCommandsRequireName asserts every replace command marks
// --name as required, matching MarkFlagRequired("name") in control_plane.go.
func TestControlPlaneReplaceCommandsRequireName(t *testing.T) {
	// every replace command in control_plane.go marks --name as required.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"ReplaceControlPlaneDefinitionCmd", ReplaceControlPlaneDefinitionCmd},
		{"ReplaceControlPlaneInstanceCmd", ReplaceControlPlaneInstanceCmd},
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

// TestControlPlaneCommandsAttachedToParents asserts each control-plane command
// is registered with the appropriate parent verb command (Get / Create /
// Replace / Delete).
func TestControlPlaneCommandsAttachedToParents(t *testing.T) {
	// each entry maps a control-plane command to the parent verb command it must live under.
	tests := []struct {
		name   string
		cmd    *cobra.Command
		parent *cobra.Command
	}{
		{"GetControlPlanesCmd", GetControlPlanesCmd, GetCmd},
		{"CreateControlPlaneCmd", CreateControlPlaneCmd, CreateCmd},
		{"DeleteControlPlaneCmd", DeleteControlPlaneCmd, DeleteCmd},
		{"GetControlPlaneDefinitionsCmd", GetControlPlaneDefinitionsCmd, GetCmd},
		{"CreateControlPlaneDefinitionCmd", CreateControlPlaneDefinitionCmd, CreateCmd},
		{"ReplaceControlPlaneDefinitionCmd", ReplaceControlPlaneDefinitionCmd, ReplaceCmd},
		{"DeleteControlPlaneDefinitionCmd", DeleteControlPlaneDefinitionCmd, DeleteCmd},
		{"GetControlPlaneInstancesCmd", GetControlPlaneInstancesCmd, GetCmd},
		{"CreateControlPlaneInstanceCmd", CreateControlPlaneInstanceCmd, CreateCmd},
		{"ReplaceControlPlaneInstanceCmd", ReplaceControlPlaneInstanceCmd, ReplaceCmd},
		{"DeleteControlPlaneInstanceCmd", DeleteControlPlaneInstanceCmd, DeleteCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// walk the parent's Commands() slice looking for the control-plane command.
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

// TestControlPlaneFlagShorthandsMatch asserts that the shorthand flags
// control_plane.go declares map to the letters users see in the command help.
func TestControlPlaneFlagShorthandsMatch(t *testing.T) {
	// each entry pins a (command, flag) to its expected one-letter shorthand.
	tests := []struct {
		name      string
		cmd       *cobra.Command
		flag      string
		shorthand string
	}{
		{"GetControlPlanesCmd name -n", GetControlPlanesCmd, "name", "n"},
		{"GetControlPlanesCmd config -c", GetControlPlanesCmd, "config", "c"},
		{"GetControlPlanesCmd version -v", GetControlPlanesCmd, "version", "v"},
		{"GetControlPlanesCmd output -o", GetControlPlanesCmd, "output", "o"},
		{"GetControlPlanesCmd control-plane-name -i", GetControlPlanesCmd, "control-plane-name", "i"},
		{"CreateControlPlaneCmd config -c", CreateControlPlaneCmd, "config", "c"},
		{"DeleteControlPlaneDefinitionCmd name -n", DeleteControlPlaneDefinitionCmd, "name", "n"},
		{"ReplaceControlPlaneInstanceCmd name -n", ReplaceControlPlaneInstanceCmd, "name", "n"},
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
