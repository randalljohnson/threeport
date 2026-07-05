package cmd

import (
	"testing"

	cobra "github.com/spf13/cobra"
)

// TestTerraformCommandsCoverExpectedMetadata asserts that every exported
// terraform cobra command variable in terraform.go carries the metadata fields
// (Use, Short, Long, Example, PreRun, Run, SilenceUsage) callers rely on for
// tptctl help output and dispatch.
func TestTerraformCommandsCoverExpectedMetadata(t *testing.T) {
	// each table entry pairs a command with the expected Use token and Short
	// description; the fixture list is the surface terraform.go publishes.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		use   string
		short string
	}{
		{"GetTerraformsCmd", GetTerraformsCmd, "terraforms", "Get terraforms from the system"},
		{"CreateTerraformCmd", CreateTerraformCmd, "terraform", "Create a new terraform"},
		{"DeleteTerraformCmd", DeleteTerraformCmd, "terraform", "Delete an existing terraform"},
		{"GetTerraformDefinitionsCmd", GetTerraformDefinitionsCmd, "terraform-definitions", "Get terraform definitions from the system"},
		{"CreateTerraformDefinitionCmd", CreateTerraformDefinitionCmd, "terraform-definition", "Create a new terraform definition"},
		{"ReplaceTerraformDefinitionCmd", ReplaceTerraformDefinitionCmd, "terraform-definition", "Replace an existing terraform definition"},
		{"DeleteTerraformDefinitionCmd", DeleteTerraformDefinitionCmd, "terraform-definition", "Delete an existing terraform definition"},
		{"GetTerraformInstancesCmd", GetTerraformInstancesCmd, "terraform-instances", "Get terraform instances from the system"},
		{"CreateTerraformInstanceCmd", CreateTerraformInstanceCmd, "terraform-instance", "Create a new terraform instance"},
		{"ReplaceTerraformInstanceCmd", ReplaceTerraformInstanceCmd, "terraform-instance", "Replace an existing terraform instance"},
		{"DeleteTerraformInstanceCmd", DeleteTerraformInstanceCmd, "terraform-instance", "Delete an existing terraform instance"},
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

// TestTerraformGetCommandsExposeSingularAlias asserts that get-style commands
// accept the singular alias so users can type `terraform` in place of `terraforms`.
func TestTerraformGetCommandsExposeSingularAlias(t *testing.T) {
	// each entry maps a plural-form get command to its expected singular alias.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		alias string
	}{
		{"GetTerraformsCmd", GetTerraformsCmd, "terraform"},
		{"GetTerraformDefinitionsCmd", GetTerraformDefinitionsCmd, "terraform-definition"},
		{"GetTerraformInstancesCmd", GetTerraformInstancesCmd, "terraform-instance"},
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

// TestTerraformCommandsRegisterExpectedFlags asserts each terraform command
// declares the flag surface consumers rely on: --config, --version, --control-plane-name
// on every command, plus --name / --output / --decrypt-secrets / --stdin
// where they appear in terraform.go's init blocks.
func TestTerraformCommandsRegisterExpectedFlags(t *testing.T) {
	// each entry names a command and the flags that must be present on it.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		flags []string
	}{
		{
			name:  "GetTerraformsCmd",
			cmd:   GetTerraformsCmd,
			flags: []string{"name", "config", "version", "output", "decrypt-secrets", "control-plane-name"},
		},
		{
			name:  "CreateTerraformCmd",
			cmd:   CreateTerraformCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "DeleteTerraformCmd",
			cmd:   DeleteTerraformCmd,
			flags: []string{"config", "control-plane-name", "version"},
		},
		{
			name:  "GetTerraformDefinitionsCmd",
			cmd:   GetTerraformDefinitionsCmd,
			flags: []string{"name", "config", "version", "output", "control-plane-name"},
		},
		{
			name:  "CreateTerraformDefinitionCmd",
			cmd:   CreateTerraformDefinitionCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceTerraformDefinitionCmd",
			cmd:   ReplaceTerraformDefinitionCmd,
			flags: []string{"config", "stdin", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteTerraformDefinitionCmd",
			cmd:   DeleteTerraformDefinitionCmd,
			flags: []string{"config", "name", "control-plane-name", "version"},
		},
		{
			name:  "GetTerraformInstancesCmd",
			cmd:   GetTerraformInstancesCmd,
			flags: []string{"name", "config", "version", "output", "decrypt-secrets", "control-plane-name"},
		},
		{
			name:  "CreateTerraformInstanceCmd",
			cmd:   CreateTerraformInstanceCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceTerraformInstanceCmd",
			cmd:   ReplaceTerraformInstanceCmd,
			flags: []string{"config", "stdin", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteTerraformInstanceCmd",
			cmd:   DeleteTerraformInstanceCmd,
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

// TestTerraformFlagDefaults asserts the default values for --version and --output
// so tptctl behaves consistently when the user omits them.
func TestTerraformFlagDefaults(t *testing.T) {
	// each entry declares an expected default for a specific (command, flag).
	tests := []struct {
		name        string
		cmd         *cobra.Command
		flag        string
		wantDefault string
	}{
		{"GetTerraformsCmd version default", GetTerraformsCmd, "version", "v0"},
		{"GetTerraformsCmd output default", GetTerraformsCmd, "output", "tabular"},
		{"CreateTerraformCmd version default", CreateTerraformCmd, "version", "v0"},
		{"DeleteTerraformCmd version default", DeleteTerraformCmd, "version", "v0"},
		{"GetTerraformDefinitionsCmd version default", GetTerraformDefinitionsCmd, "version", "v0"},
		{"GetTerraformDefinitionsCmd output default", GetTerraformDefinitionsCmd, "output", "tabular"},
		{"CreateTerraformDefinitionCmd version default", CreateTerraformDefinitionCmd, "version", "v0"},
		{"ReplaceTerraformDefinitionCmd version default", ReplaceTerraformDefinitionCmd, "version", "v0"},
		{"DeleteTerraformDefinitionCmd version default", DeleteTerraformDefinitionCmd, "version", "v0"},
		{"GetTerraformInstancesCmd version default", GetTerraformInstancesCmd, "version", "v0"},
		{"GetTerraformInstancesCmd output default", GetTerraformInstancesCmd, "output", "tabular"},
		{"CreateTerraformInstanceCmd version default", CreateTerraformInstanceCmd, "version", "v0"},
		{"ReplaceTerraformInstanceCmd version default", ReplaceTerraformInstanceCmd, "version", "v0"},
		{"DeleteTerraformInstanceCmd version default", DeleteTerraformInstanceCmd, "version", "v0"},
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

// TestTerraformReplaceCommandsRequireName asserts every replace command marks
// --name as required, matching MarkFlagRequired("name") in terraform.go.
func TestTerraformReplaceCommandsRequireName(t *testing.T) {
	// every replace command in terraform.go marks --name as required.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"ReplaceTerraformDefinitionCmd", ReplaceTerraformDefinitionCmd},
		{"ReplaceTerraformInstanceCmd", ReplaceTerraformInstanceCmd},
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

// TestTerraformCommandsAttachedToParents asserts each terraform command is
// registered with the appropriate parent verb command (Get / Create / Replace / Delete).
func TestTerraformCommandsAttachedToParents(t *testing.T) {
	// each entry maps a terraform command to the parent verb command it must live under.
	tests := []struct {
		name   string
		cmd    *cobra.Command
		parent *cobra.Command
	}{
		{"GetTerraformsCmd", GetTerraformsCmd, GetCmd},
		{"CreateTerraformCmd", CreateTerraformCmd, CreateCmd},
		{"DeleteTerraformCmd", DeleteTerraformCmd, DeleteCmd},
		{"GetTerraformDefinitionsCmd", GetTerraformDefinitionsCmd, GetCmd},
		{"CreateTerraformDefinitionCmd", CreateTerraformDefinitionCmd, CreateCmd},
		{"ReplaceTerraformDefinitionCmd", ReplaceTerraformDefinitionCmd, ReplaceCmd},
		{"DeleteTerraformDefinitionCmd", DeleteTerraformDefinitionCmd, DeleteCmd},
		{"GetTerraformInstancesCmd", GetTerraformInstancesCmd, GetCmd},
		{"CreateTerraformInstanceCmd", CreateTerraformInstanceCmd, CreateCmd},
		{"ReplaceTerraformInstanceCmd", ReplaceTerraformInstanceCmd, ReplaceCmd},
		{"DeleteTerraformInstanceCmd", DeleteTerraformInstanceCmd, DeleteCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// walk the parent's Commands() slice looking for the terraform command.
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

// TestTerraformFlagShorthandsMatch asserts that the shorthand flags terraform.go
// declares map to the letters users see in the command help.
func TestTerraformFlagShorthandsMatch(t *testing.T) {
	// each entry pins a (command, flag) to its expected one-letter shorthand.
	tests := []struct {
		name      string
		cmd       *cobra.Command
		flag      string
		shorthand string
	}{
		{"GetTerraformsCmd name -n", GetTerraformsCmd, "name", "n"},
		{"GetTerraformsCmd config -c", GetTerraformsCmd, "config", "c"},
		{"GetTerraformsCmd version -v", GetTerraformsCmd, "version", "v"},
		{"GetTerraformsCmd output -o", GetTerraformsCmd, "output", "o"},
		{"GetTerraformsCmd decrypt-secrets -d", GetTerraformsCmd, "decrypt-secrets", "d"},
		{"GetTerraformsCmd control-plane-name -i", GetTerraformsCmd, "control-plane-name", "i"},
		{"CreateTerraformCmd config -c", CreateTerraformCmd, "config", "c"},
		{"DeleteTerraformCmd config -c", DeleteTerraformCmd, "config", "c"},
		{"GetTerraformInstancesCmd decrypt-secrets -d", GetTerraformInstancesCmd, "decrypt-secrets", "d"},
		{"ReplaceTerraformDefinitionCmd name -n", ReplaceTerraformDefinitionCmd, "name", "n"},
		{"ReplaceTerraformInstanceCmd name -n", ReplaceTerraformInstanceCmd, "name", "n"},
		{"DeleteTerraformDefinitionCmd name -n", DeleteTerraformDefinitionCmd, "name", "n"},
		{"DeleteTerraformInstanceCmd name -n", DeleteTerraformInstanceCmd, "name", "n"},
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

// TestTerraformStdinFlagsDefaultFalse asserts that the boolean --stdin flag on
// create/replace commands defaults to false so tptctl reads from the config
// file path unless explicitly told to read stdin.
func TestTerraformStdinFlagsDefaultFalse(t *testing.T) {
	// each entry names a command that declares --stdin.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"CreateTerraformCmd", CreateTerraformCmd},
		{"CreateTerraformDefinitionCmd", CreateTerraformDefinitionCmd},
		{"ReplaceTerraformDefinitionCmd", ReplaceTerraformDefinitionCmd},
		{"CreateTerraformInstanceCmd", CreateTerraformInstanceCmd},
		{"ReplaceTerraformInstanceCmd", ReplaceTerraformInstanceCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// stdin flag must exist and default to "false".
			f := tt.cmd.Flags().Lookup("stdin")
			if f == nil {
				t.Fatalf("flag \"stdin\" not registered")
			}
			if f.DefValue != "false" {
				t.Errorf("flag \"stdin\" DefValue = %q, want %q", f.DefValue, "false")
			}
		})
	}
}
