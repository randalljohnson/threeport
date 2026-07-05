package cmd

import (
	"testing"

	cobra "github.com/spf13/cobra"
)

// TestKubernetesWorkloadCommandsCoverExpectedMetadata asserts that every
// exported cobra command variable in kubernetes_workload.go carries the
// metadata fields (Use, Short, Long, Example, PreRun, Run, SilenceUsage)
// callers rely on for tptctl help output and dispatch.
func TestKubernetesWorkloadCommandsCoverExpectedMetadata(t *testing.T) {
	// each entry pairs a command with the expected Use token and Short description;
	// the fixture list is the surface kubernetes_workload.go publishes.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		use   string
		short string
	}{
		{"GetKubernetesWorkloadsCmd", GetKubernetesWorkloadsCmd, "workloads", "Get workloads from the system"},
		{"CreateKubernetesWorkloadCmd", CreateKubernetesWorkloadCmd, "workload", "Create a new workload"},
		{"DeleteKubernetesWorkloadCmd", DeleteKubernetesWorkloadCmd, "workload", "Delete an existing workload"},
		{"GetKubernetesWorkloadDefinitionsCmd", GetKubernetesWorkloadDefinitionsCmd, "workload-definitions", "Get workload definitions from the system"},
		{"CreateKubernetesWorkloadDefinitionCmd", CreateKubernetesWorkloadDefinitionCmd, "workload-definition", "Create a new kubernetes workload definition"},
		{"ReplaceKubernetesWorkloadDefinitionCmd", ReplaceKubernetesWorkloadDefinitionCmd, "workload-definition", "Replace an existing kubernetes workload definition"},
		{"DeleteKubernetesWorkloadDefinitionCmd", DeleteKubernetesWorkloadDefinitionCmd, "workload-definition", "Delete an existing kubernetes workload definition"},
		{"GetKubernetesWorkloadInstancesCmd", GetKubernetesWorkloadInstancesCmd, "workload-instances", "Get workload instances from the system"},
		{"CreateKubernetesWorkloadInstanceCmd", CreateKubernetesWorkloadInstanceCmd, "workload-instance", "Create a new kubernetes workload instance"},
		{"ReplaceKubernetesWorkloadInstanceCmd", ReplaceKubernetesWorkloadInstanceCmd, "workload-instance", "Replace an existing kubernetes workload instance"},
		{"DeleteKubernetesWorkloadInstanceCmd", DeleteKubernetesWorkloadInstanceCmd, "workload-instance", "Delete an existing kubernetes workload instance"},
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

// TestKubernetesWorkloadGetCommandsExposeSingularAlias asserts every get-style
// command in kubernetes_workload.go accepts the singular alias so users can
// type `workload` in place of `workloads`.
func TestKubernetesWorkloadGetCommandsExposeSingularAlias(t *testing.T) {
	// each entry maps a plural-form get command to its expected singular alias.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		alias string
	}{
		{"GetKubernetesWorkloadsCmd", GetKubernetesWorkloadsCmd, "workload"},
		{"GetKubernetesWorkloadDefinitionsCmd", GetKubernetesWorkloadDefinitionsCmd, "workload-definition"},
		{"GetKubernetesWorkloadInstancesCmd", GetKubernetesWorkloadInstancesCmd, "workload-instance"},
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

// TestKubernetesWorkloadCommandsRegisterExpectedFlags asserts each command
// declares the flag surface consumers rely on: --config, --version, and
// --control-plane-name on every command, plus --name / --output / --stdin
// where they appear in kubernetes_workload.go's init blocks.
func TestKubernetesWorkloadCommandsRegisterExpectedFlags(t *testing.T) {
	// each entry names a command and the flags that must be present on it.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		flags []string
	}{
		{"GetKubernetesWorkloadsCmd", GetKubernetesWorkloadsCmd, []string{"name", "config", "version", "output", "control-plane-name"}},
		{"CreateKubernetesWorkloadCmd", CreateKubernetesWorkloadCmd, []string{"config", "stdin", "control-plane-name", "version"}},
		// DeleteKubernetesWorkloadCmd does not register a --name flag in kubernetes_workload.go.
		{"DeleteKubernetesWorkloadCmd", DeleteKubernetesWorkloadCmd, []string{"config", "control-plane-name", "version"}},
		{"GetKubernetesWorkloadDefinitionsCmd", GetKubernetesWorkloadDefinitionsCmd, []string{"name", "config", "version", "output", "control-plane-name"}},
		{"CreateKubernetesWorkloadDefinitionCmd", CreateKubernetesWorkloadDefinitionCmd, []string{"config", "stdin", "control-plane-name", "version"}},
		{"ReplaceKubernetesWorkloadDefinitionCmd", ReplaceKubernetesWorkloadDefinitionCmd, []string{"config", "stdin", "name", "control-plane-name", "version"}},
		{"DeleteKubernetesWorkloadDefinitionCmd", DeleteKubernetesWorkloadDefinitionCmd, []string{"config", "name", "control-plane-name", "version"}},
		{"GetKubernetesWorkloadInstancesCmd", GetKubernetesWorkloadInstancesCmd, []string{"name", "config", "version", "output", "control-plane-name"}},
		{"CreateKubernetesWorkloadInstanceCmd", CreateKubernetesWorkloadInstanceCmd, []string{"config", "stdin", "control-plane-name", "version"}},
		{"ReplaceKubernetesWorkloadInstanceCmd", ReplaceKubernetesWorkloadInstanceCmd, []string{"config", "stdin", "name", "control-plane-name", "version"}},
		{"DeleteKubernetesWorkloadInstanceCmd", DeleteKubernetesWorkloadInstanceCmd, []string{"config", "name", "control-plane-name", "version"}},
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

// TestKubernetesWorkloadFlagDefaults asserts the default values for --version
// and --output so tptctl behaves consistently when the user omits them.
func TestKubernetesWorkloadFlagDefaults(t *testing.T) {
	// each entry declares an expected default for a specific (command, flag).
	tests := []struct {
		name        string
		cmd         *cobra.Command
		flag        string
		wantDefault string
	}{
		{"GetKubernetesWorkloadsCmd version default", GetKubernetesWorkloadsCmd, "version", "v0"},
		{"GetKubernetesWorkloadsCmd output default", GetKubernetesWorkloadsCmd, "output", "tabular"},
		{"CreateKubernetesWorkloadCmd version default", CreateKubernetesWorkloadCmd, "version", "v0"},
		{"DeleteKubernetesWorkloadCmd version default", DeleteKubernetesWorkloadCmd, "version", "v0"},
		{"GetKubernetesWorkloadDefinitionsCmd version default", GetKubernetesWorkloadDefinitionsCmd, "version", "v0"},
		{"GetKubernetesWorkloadDefinitionsCmd output default", GetKubernetesWorkloadDefinitionsCmd, "output", "tabular"},
		{"ReplaceKubernetesWorkloadDefinitionCmd version default", ReplaceKubernetesWorkloadDefinitionCmd, "version", "v0"},
		{"GetKubernetesWorkloadInstancesCmd output default", GetKubernetesWorkloadInstancesCmd, "output", "tabular"},
		{"ReplaceKubernetesWorkloadInstanceCmd version default", ReplaceKubernetesWorkloadInstanceCmd, "version", "v0"},
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

// TestKubernetesWorkloadReplaceCommandsRequireName asserts every replace
// command marks --name as required, matching MarkFlagRequired("name") in
// kubernetes_workload.go.
func TestKubernetesWorkloadReplaceCommandsRequireName(t *testing.T) {
	// every replace command in kubernetes_workload.go marks --name as required.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"ReplaceKubernetesWorkloadDefinitionCmd", ReplaceKubernetesWorkloadDefinitionCmd},
		{"ReplaceKubernetesWorkloadInstanceCmd", ReplaceKubernetesWorkloadInstanceCmd},
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

// TestKubernetesWorkloadCommandsAttachedToParents asserts each command is
// registered with the appropriate parent verb command (Get / Create / Replace
// / Delete).
func TestKubernetesWorkloadCommandsAttachedToParents(t *testing.T) {
	// each entry maps a command to the parent verb command it must live under.
	tests := []struct {
		name   string
		cmd    *cobra.Command
		parent *cobra.Command
	}{
		{"GetKubernetesWorkloadsCmd", GetKubernetesWorkloadsCmd, GetCmd},
		{"CreateKubernetesWorkloadCmd", CreateKubernetesWorkloadCmd, CreateCmd},
		{"DeleteKubernetesWorkloadCmd", DeleteKubernetesWorkloadCmd, DeleteCmd},
		{"GetKubernetesWorkloadDefinitionsCmd", GetKubernetesWorkloadDefinitionsCmd, GetCmd},
		{"CreateKubernetesWorkloadDefinitionCmd", CreateKubernetesWorkloadDefinitionCmd, CreateCmd},
		{"ReplaceKubernetesWorkloadDefinitionCmd", ReplaceKubernetesWorkloadDefinitionCmd, ReplaceCmd},
		{"DeleteKubernetesWorkloadDefinitionCmd", DeleteKubernetesWorkloadDefinitionCmd, DeleteCmd},
		{"GetKubernetesWorkloadInstancesCmd", GetKubernetesWorkloadInstancesCmd, GetCmd},
		{"CreateKubernetesWorkloadInstanceCmd", CreateKubernetesWorkloadInstanceCmd, CreateCmd},
		{"ReplaceKubernetesWorkloadInstanceCmd", ReplaceKubernetesWorkloadInstanceCmd, ReplaceCmd},
		{"DeleteKubernetesWorkloadInstanceCmd", DeleteKubernetesWorkloadInstanceCmd, DeleteCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// walk the parent's Commands() slice looking for the command.
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

// TestKubernetesWorkloadFlagShorthandsMatch asserts that the shorthand flags
// kubernetes_workload.go declares map to the letters users see in the command
// help.
func TestKubernetesWorkloadFlagShorthandsMatch(t *testing.T) {
	// each entry pins a (command, flag) to its expected one-letter shorthand.
	tests := []struct {
		name      string
		cmd       *cobra.Command
		flag      string
		shorthand string
	}{
		{"GetKubernetesWorkloadsCmd name -n", GetKubernetesWorkloadsCmd, "name", "n"},
		{"GetKubernetesWorkloadsCmd config -c", GetKubernetesWorkloadsCmd, "config", "c"},
		{"GetKubernetesWorkloadsCmd version -v", GetKubernetesWorkloadsCmd, "version", "v"},
		{"GetKubernetesWorkloadsCmd output -o", GetKubernetesWorkloadsCmd, "output", "o"},
		{"GetKubernetesWorkloadsCmd control-plane-name -i", GetKubernetesWorkloadsCmd, "control-plane-name", "i"},
		{"CreateKubernetesWorkloadCmd config -c", CreateKubernetesWorkloadCmd, "config", "c"},
		{"DeleteKubernetesWorkloadCmd config -c", DeleteKubernetesWorkloadCmd, "config", "c"},
		{"ReplaceKubernetesWorkloadDefinitionCmd name -n", ReplaceKubernetesWorkloadDefinitionCmd, "name", "n"},
		{"ReplaceKubernetesWorkloadInstanceCmd name -n", ReplaceKubernetesWorkloadInstanceCmd, "name", "n"},
		{"DeleteKubernetesWorkloadDefinitionCmd name -n", DeleteKubernetesWorkloadDefinitionCmd, "name", "n"},
		{"DeleteKubernetesWorkloadInstanceCmd name -n", DeleteKubernetesWorkloadInstanceCmd, "name", "n"},
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

// TestKubernetesWorkloadStdinFlagIsBool asserts commands that read config from
// stdin register --stdin as a boolean flag defaulting to false.
func TestKubernetesWorkloadStdinFlagIsBool(t *testing.T) {
	// each entry names a command that must expose --stdin as a bool defaulting to false.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"CreateKubernetesWorkloadCmd", CreateKubernetesWorkloadCmd},
		{"CreateKubernetesWorkloadDefinitionCmd", CreateKubernetesWorkloadDefinitionCmd},
		{"ReplaceKubernetesWorkloadDefinitionCmd", ReplaceKubernetesWorkloadDefinitionCmd},
		{"CreateKubernetesWorkloadInstanceCmd", CreateKubernetesWorkloadInstanceCmd},
		{"ReplaceKubernetesWorkloadInstanceCmd", ReplaceKubernetesWorkloadInstanceCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// look up --stdin and confirm the flag is a bool defaulting to false.
			f := tt.cmd.Flags().Lookup("stdin")
			if f == nil {
				t.Fatalf("flag \"stdin\" not registered")
			}
			if f.Value.Type() != "bool" {
				t.Errorf("flag \"stdin\" type = %q, want %q", f.Value.Type(), "bool")
			}
			if f.DefValue != "false" {
				t.Errorf("flag \"stdin\" DefValue = %q, want %q", f.DefValue, "false")
			}
		})
	}
}
