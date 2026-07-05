package cmd

import (
	"testing"

	cobra "github.com/spf13/cobra"
)

// TestHelmWorkloadCommandsCoverExpectedMetadata asserts that every exported
// cobra command variable in helm_workload.go carries the metadata fields (Use,
// Short, Long, Example, PreRun, Run, SilenceUsage) callers rely on for tptctl
// help output and dispatch.
func TestHelmWorkloadCommandsCoverExpectedMetadata(t *testing.T) {
	// each entry pairs a command with the expected Use token and Short description;
	// the fixture list is the surface helm_workload.go publishes.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		use   string
		short string
	}{
		{"GetHelmWorkloadsCmd", GetHelmWorkloadsCmd, "helm-workloads", "Get helm workloads from the system"},
		{"CreateHelmWorkloadCmd", CreateHelmWorkloadCmd, "helm-workload", "Create a new helm workload"},
		{"DeleteHelmWorkloadCmd", DeleteHelmWorkloadCmd, "helm-workload", "Delete an existing helm workload"},
		{"GetHelmWorkloadDefinitionsCmd", GetHelmWorkloadDefinitionsCmd, "helm-workload-definitions", "Get helm workload definitions from the system"},
		{"CreateHelmWorkloadDefinitionCmd", CreateHelmWorkloadDefinitionCmd, "helm-workload-definition", "Create a new helm workload definition"},
		{"ReplaceHelmWorkloadDefinitionCmd", ReplaceHelmWorkloadDefinitionCmd, "helm-workload-definition", "Replace an existing helm workload definition"},
		{"DeleteHelmWorkloadDefinitionCmd", DeleteHelmWorkloadDefinitionCmd, "helm-workload-definition", "Delete an existing helm workload definition"},
		{"GetHelmWorkloadInstancesCmd", GetHelmWorkloadInstancesCmd, "helm-workload-instances", "Get helm workload instances from the system"},
		{"CreateHelmWorkloadInstanceCmd", CreateHelmWorkloadInstanceCmd, "helm-workload-instance", "Create a new helm workload instance"},
		{"ReplaceHelmWorkloadInstanceCmd", ReplaceHelmWorkloadInstanceCmd, "helm-workload-instance", "Replace an existing helm workload instance"},
		{"DeleteHelmWorkloadInstanceCmd", DeleteHelmWorkloadInstanceCmd, "helm-workload-instance", "Delete an existing helm workload instance"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// command variable exists.
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

// TestHelmWorkloadGetCommandsExposeSingularAlias asserts every get-style
// command in helm_workload.go accepts the singular alias so users can type
// the singular form in place of the plural.
func TestHelmWorkloadGetCommandsExposeSingularAlias(t *testing.T) {
	// each entry maps a plural-form get command to its expected singular alias.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		alias string
	}{
		{"GetHelmWorkloadsCmd", GetHelmWorkloadsCmd, "helm-workload"},
		{"GetHelmWorkloadDefinitionsCmd", GetHelmWorkloadDefinitionsCmd, "helm-workload-definition"},
		{"GetHelmWorkloadInstancesCmd", GetHelmWorkloadInstancesCmd, "helm-workload-instance"},
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

// TestHelmWorkloadCommandsRegisterExpectedFlags asserts each helm workload
// command declares the flag surface consumers rely on: --config, --version,
// and --control-plane-name on every command, plus --name / --output / --stdin
// where they appear in helm_workload.go's init blocks.
func TestHelmWorkloadCommandsRegisterExpectedFlags(t *testing.T) {
	// each entry names a command and the flags that must be present on it.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		flags []string
	}{
		{"GetHelmWorkloadsCmd", GetHelmWorkloadsCmd, []string{"name", "config", "version", "output", "control-plane-name"}},
		{"CreateHelmWorkloadCmd", CreateHelmWorkloadCmd, []string{"config", "stdin", "control-plane-name", "version"}},
		{"DeleteHelmWorkloadCmd", DeleteHelmWorkloadCmd, []string{"config", "control-plane-name", "version"}},
		{"GetHelmWorkloadDefinitionsCmd", GetHelmWorkloadDefinitionsCmd, []string{"name", "config", "version", "output", "control-plane-name"}},
		{"CreateHelmWorkloadDefinitionCmd", CreateHelmWorkloadDefinitionCmd, []string{"config", "stdin", "control-plane-name", "version"}},
		{"ReplaceHelmWorkloadDefinitionCmd", ReplaceHelmWorkloadDefinitionCmd, []string{"config", "stdin", "name", "control-plane-name", "version"}},
		{"DeleteHelmWorkloadDefinitionCmd", DeleteHelmWorkloadDefinitionCmd, []string{"config", "name", "control-plane-name", "version"}},
		{"GetHelmWorkloadInstancesCmd", GetHelmWorkloadInstancesCmd, []string{"name", "config", "version", "output", "control-plane-name"}},
		{"CreateHelmWorkloadInstanceCmd", CreateHelmWorkloadInstanceCmd, []string{"config", "stdin", "control-plane-name", "version"}},
		{"ReplaceHelmWorkloadInstanceCmd", ReplaceHelmWorkloadInstanceCmd, []string{"config", "stdin", "name", "control-plane-name", "version"}},
		{"DeleteHelmWorkloadInstanceCmd", DeleteHelmWorkloadInstanceCmd, []string{"config", "name", "control-plane-name", "version"}},
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

// TestHelmWorkloadFlagDefaults asserts the default values for --version and
// --output so tptctl behaves consistently when the user omits them.
func TestHelmWorkloadFlagDefaults(t *testing.T) {
	// each entry declares an expected default for a specific (command, flag).
	tests := []struct {
		name        string
		cmd         *cobra.Command
		flag        string
		wantDefault string
	}{
		{"GetHelmWorkloadsCmd version default", GetHelmWorkloadsCmd, "version", "v0"},
		{"GetHelmWorkloadsCmd output default", GetHelmWorkloadsCmd, "output", "tabular"},
		{"CreateHelmWorkloadCmd version default", CreateHelmWorkloadCmd, "version", "v0"},
		{"DeleteHelmWorkloadCmd version default", DeleteHelmWorkloadCmd, "version", "v0"},
		{"GetHelmWorkloadDefinitionsCmd version default", GetHelmWorkloadDefinitionsCmd, "version", "v0"},
		{"GetHelmWorkloadDefinitionsCmd output default", GetHelmWorkloadDefinitionsCmd, "output", "tabular"},
		{"CreateHelmWorkloadDefinitionCmd version default", CreateHelmWorkloadDefinitionCmd, "version", "v0"},
		{"ReplaceHelmWorkloadDefinitionCmd version default", ReplaceHelmWorkloadDefinitionCmd, "version", "v0"},
		{"DeleteHelmWorkloadDefinitionCmd version default", DeleteHelmWorkloadDefinitionCmd, "version", "v0"},
		{"GetHelmWorkloadInstancesCmd version default", GetHelmWorkloadInstancesCmd, "version", "v0"},
		{"GetHelmWorkloadInstancesCmd output default", GetHelmWorkloadInstancesCmd, "output", "tabular"},
		{"CreateHelmWorkloadInstanceCmd version default", CreateHelmWorkloadInstanceCmd, "version", "v0"},
		{"ReplaceHelmWorkloadInstanceCmd version default", ReplaceHelmWorkloadInstanceCmd, "version", "v0"},
		{"DeleteHelmWorkloadInstanceCmd version default", DeleteHelmWorkloadInstanceCmd, "version", "v0"},
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

// TestHelmWorkloadReplaceCommandsRequireName asserts every replace command
// marks --name as required, matching MarkFlagRequired("name") in
// helm_workload.go.
func TestHelmWorkloadReplaceCommandsRequireName(t *testing.T) {
	// every replace command in helm_workload.go marks --name as required.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"ReplaceHelmWorkloadDefinitionCmd", ReplaceHelmWorkloadDefinitionCmd},
		{"ReplaceHelmWorkloadInstanceCmd", ReplaceHelmWorkloadInstanceCmd},
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

// TestHelmWorkloadCommandsAttachedToParents asserts each helm workload command
// is registered with the appropriate parent verb command
// (Get / Create / Replace / Delete).
func TestHelmWorkloadCommandsAttachedToParents(t *testing.T) {
	// each entry maps a helm workload command to the parent verb command it must
	// live under.
	tests := []struct {
		name   string
		cmd    *cobra.Command
		parent *cobra.Command
	}{
		{"GetHelmWorkloadsCmd", GetHelmWorkloadsCmd, GetCmd},
		{"CreateHelmWorkloadCmd", CreateHelmWorkloadCmd, CreateCmd},
		{"DeleteHelmWorkloadCmd", DeleteHelmWorkloadCmd, DeleteCmd},
		{"GetHelmWorkloadDefinitionsCmd", GetHelmWorkloadDefinitionsCmd, GetCmd},
		{"CreateHelmWorkloadDefinitionCmd", CreateHelmWorkloadDefinitionCmd, CreateCmd},
		{"ReplaceHelmWorkloadDefinitionCmd", ReplaceHelmWorkloadDefinitionCmd, ReplaceCmd},
		{"DeleteHelmWorkloadDefinitionCmd", DeleteHelmWorkloadDefinitionCmd, DeleteCmd},
		{"GetHelmWorkloadInstancesCmd", GetHelmWorkloadInstancesCmd, GetCmd},
		{"CreateHelmWorkloadInstanceCmd", CreateHelmWorkloadInstanceCmd, CreateCmd},
		{"ReplaceHelmWorkloadInstanceCmd", ReplaceHelmWorkloadInstanceCmd, ReplaceCmd},
		{"DeleteHelmWorkloadInstanceCmd", DeleteHelmWorkloadInstanceCmd, DeleteCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// walk the parent's Commands() slice looking for the helm workload command.
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

// TestHelmWorkloadFlagShorthandsMatch asserts that the shorthand flags
// helm_workload.go declares map to the letters users see in the command help.
func TestHelmWorkloadFlagShorthandsMatch(t *testing.T) {
	// each entry pins a (command, flag) to its expected one-letter shorthand.
	tests := []struct {
		name      string
		cmd       *cobra.Command
		flag      string
		shorthand string
	}{
		{"GetHelmWorkloadsCmd name -n", GetHelmWorkloadsCmd, "name", "n"},
		{"GetHelmWorkloadsCmd config -c", GetHelmWorkloadsCmd, "config", "c"},
		{"GetHelmWorkloadsCmd version -v", GetHelmWorkloadsCmd, "version", "v"},
		{"GetHelmWorkloadsCmd output -o", GetHelmWorkloadsCmd, "output", "o"},
		{"GetHelmWorkloadsCmd control-plane-name -i", GetHelmWorkloadsCmd, "control-plane-name", "i"},
		{"CreateHelmWorkloadCmd config -c", CreateHelmWorkloadCmd, "config", "c"},
		{"DeleteHelmWorkloadCmd config -c", DeleteHelmWorkloadCmd, "config", "c"},
		{"GetHelmWorkloadDefinitionsCmd name -n", GetHelmWorkloadDefinitionsCmd, "name", "n"},
		{"ReplaceHelmWorkloadDefinitionCmd name -n", ReplaceHelmWorkloadDefinitionCmd, "name", "n"},
		{"DeleteHelmWorkloadDefinitionCmd name -n", DeleteHelmWorkloadDefinitionCmd, "name", "n"},
		{"GetHelmWorkloadInstancesCmd name -n", GetHelmWorkloadInstancesCmd, "name", "n"},
		{"ReplaceHelmWorkloadInstanceCmd name -n", ReplaceHelmWorkloadInstanceCmd, "name", "n"},
		{"DeleteHelmWorkloadInstanceCmd name -n", DeleteHelmWorkloadInstanceCmd, "name", "n"},
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
