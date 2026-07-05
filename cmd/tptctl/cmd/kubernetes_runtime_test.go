package cmd

import (
	"testing"

	cobra "github.com/spf13/cobra"
)

// TestKubernetesRuntimeCommandsCoverExpectedMetadata asserts every exported
// cobra command variable in kubernetes_runtime.go carries the metadata fields
// (Use, Short, Long, Example, PreRun, Run, SilenceUsage) callers rely on for
// tptctl help output and dispatch.
func TestKubernetesRuntimeCommandsCoverExpectedMetadata(t *testing.T) {
	// each entry pairs a command with its expected Use token and Short line;
	// the fixture list is the surface kubernetes_runtime.go publishes.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		use   string
		short string
	}{
		{"GetKubernetesRuntimesCmd", GetKubernetesRuntimesCmd, "kubernetes-runtimes", "Get kubernetes runtimes from the system"},
		{"CreateKubernetesRuntimeCmd", CreateKubernetesRuntimeCmd, "kubernetes-runtime", "Create a new kubernetes runtime"},
		{"DeleteKubernetesRuntimeCmd", DeleteKubernetesRuntimeCmd, "kubernetes-runtime", "Delete an existing kubernetes runtime"},
		{"GetKubernetesRuntimeDefinitionsCmd", GetKubernetesRuntimeDefinitionsCmd, "kubernetes-runtime-definitions", "Get kubernetes runtime definitions from the system"},
		{"CreateKubernetesRuntimeDefinitionCmd", CreateKubernetesRuntimeDefinitionCmd, "kubernetes-runtime-definition", "Create a new kubernetes runtime definition"},
		{"ReplaceKubernetesRuntimeDefinitionCmd", ReplaceKubernetesRuntimeDefinitionCmd, "kubernetes-runtime-definition", "Replace an existing kubernetes runtime definition"},
		{"DeleteKubernetesRuntimeDefinitionCmd", DeleteKubernetesRuntimeDefinitionCmd, "kubernetes-runtime-definition", "Delete an existing kubernetes runtime definition"},
		{"GetKubernetesRuntimeInstancesCmd", GetKubernetesRuntimeInstancesCmd, "kubernetes-runtime-instances", "Get kubernetes runtime instances from the system"},
		{"CreateKubernetesRuntimeInstanceCmd", CreateKubernetesRuntimeInstanceCmd, "kubernetes-runtime-instance", "Create a new kubernetes runtime instance"},
		{"ReplaceKubernetesRuntimeInstanceCmd", ReplaceKubernetesRuntimeInstanceCmd, "kubernetes-runtime-instance", "Replace an existing kubernetes runtime instance"},
		{"DeleteKubernetesRuntimeInstanceCmd", DeleteKubernetesRuntimeInstanceCmd, "kubernetes-runtime-instance", "Delete an existing kubernetes runtime instance"},
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

// TestKubernetesRuntimeGetCommandsExposeSingularAlias asserts every get-style
// command exposes the singular alias so users can type the singular form
// (e.g. `kubernetes-runtime`) in place of the plural.
func TestKubernetesRuntimeGetCommandsExposeSingularAlias(t *testing.T) {
	// each entry maps a plural-form get command to its expected singular alias.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		alias string
	}{
		{"GetKubernetesRuntimesCmd", GetKubernetesRuntimesCmd, "kubernetes-runtime"},
		{"GetKubernetesRuntimeDefinitionsCmd", GetKubernetesRuntimeDefinitionsCmd, "kubernetes-runtime-definition"},
		{"GetKubernetesRuntimeInstancesCmd", GetKubernetesRuntimeInstancesCmd, "kubernetes-runtime-instance"},
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

// TestKubernetesRuntimeNonGetCommandsHaveNoAliases asserts that create,
// replace, and delete commands do not carry aliases; only the get commands
// expose the singular form.
func TestKubernetesRuntimeNonGetCommandsHaveNoAliases(t *testing.T) {
	// commands under Create/Replace/Delete are singular already; no aliases apply.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"CreateKubernetesRuntimeCmd", CreateKubernetesRuntimeCmd},
		{"DeleteKubernetesRuntimeCmd", DeleteKubernetesRuntimeCmd},
		{"CreateKubernetesRuntimeDefinitionCmd", CreateKubernetesRuntimeDefinitionCmd},
		{"ReplaceKubernetesRuntimeDefinitionCmd", ReplaceKubernetesRuntimeDefinitionCmd},
		{"DeleteKubernetesRuntimeDefinitionCmd", DeleteKubernetesRuntimeDefinitionCmd},
		{"CreateKubernetesRuntimeInstanceCmd", CreateKubernetesRuntimeInstanceCmd},
		{"ReplaceKubernetesRuntimeInstanceCmd", ReplaceKubernetesRuntimeInstanceCmd},
		{"DeleteKubernetesRuntimeInstanceCmd", DeleteKubernetesRuntimeInstanceCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// aliases slice must be empty on non-get commands.
			if len(tt.cmd.Aliases) != 0 {
				t.Errorf("Aliases = %v, want empty", tt.cmd.Aliases)
			}
		})
	}
}

// TestKubernetesRuntimeCommandsRegisterExpectedFlags asserts each command
// declares the flag surface consumers rely on: --config, --version, and
// --control-plane-name on every command, plus --name / --output / --stdin
// where they appear in kubernetes_runtime.go's init blocks.
func TestKubernetesRuntimeCommandsRegisterExpectedFlags(t *testing.T) {
	// each entry names a command and the flags that must be present on it;
	// note DeleteKubernetesRuntimeCmd deliberately omits --name since the
	// top-level delete requires a config file.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		flags []string
	}{
		{"GetKubernetesRuntimesCmd", GetKubernetesRuntimesCmd, []string{"name", "config", "version", "output", "control-plane-name"}},
		{"CreateKubernetesRuntimeCmd", CreateKubernetesRuntimeCmd, []string{"config", "stdin", "control-plane-name", "version"}},
		{"DeleteKubernetesRuntimeCmd", DeleteKubernetesRuntimeCmd, []string{"config", "control-plane-name", "version"}},
		{"GetKubernetesRuntimeDefinitionsCmd", GetKubernetesRuntimeDefinitionsCmd, []string{"name", "config", "version", "output", "control-plane-name"}},
		{"CreateKubernetesRuntimeDefinitionCmd", CreateKubernetesRuntimeDefinitionCmd, []string{"config", "stdin", "control-plane-name", "version"}},
		{"ReplaceKubernetesRuntimeDefinitionCmd", ReplaceKubernetesRuntimeDefinitionCmd, []string{"config", "stdin", "name", "control-plane-name", "version"}},
		{"DeleteKubernetesRuntimeDefinitionCmd", DeleteKubernetesRuntimeDefinitionCmd, []string{"config", "name", "control-plane-name", "version"}},
		{"GetKubernetesRuntimeInstancesCmd", GetKubernetesRuntimeInstancesCmd, []string{"name", "config", "version", "output", "control-plane-name"}},
		{"CreateKubernetesRuntimeInstanceCmd", CreateKubernetesRuntimeInstanceCmd, []string{"config", "stdin", "control-plane-name", "version"}},
		{"ReplaceKubernetesRuntimeInstanceCmd", ReplaceKubernetesRuntimeInstanceCmd, []string{"config", "stdin", "name", "control-plane-name", "version"}},
		{"DeleteKubernetesRuntimeInstanceCmd", DeleteKubernetesRuntimeInstanceCmd, []string{"config", "name", "control-plane-name", "version"}},
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

// TestKubernetesRuntimeDeleteRootHasNoNameFlag pins the intentional absence
// of --name on DeleteKubernetesRuntimeCmd: the unified delete requires a
// config file so callers cannot delete by name from the top-level command.
func TestKubernetesRuntimeDeleteRootHasNoNameFlag(t *testing.T) {
	// look up --name and confirm it is absent.
	if DeleteKubernetesRuntimeCmd.Flags().Lookup("name") != nil {
		t.Errorf("DeleteKubernetesRuntimeCmd unexpectedly registers --name flag")
	}
}

// TestKubernetesRuntimeFlagDefaults asserts the default values for --version
// and --output so tptctl behaves consistently when the user omits them.
func TestKubernetesRuntimeFlagDefaults(t *testing.T) {
	// each entry declares an expected default for a specific (command, flag).
	tests := []struct {
		name        string
		cmd         *cobra.Command
		flag        string
		wantDefault string
	}{
		{"GetKubernetesRuntimesCmd version default", GetKubernetesRuntimesCmd, "version", "v0"},
		{"GetKubernetesRuntimesCmd output default", GetKubernetesRuntimesCmd, "output", "tabular"},
		{"CreateKubernetesRuntimeCmd version default", CreateKubernetesRuntimeCmd, "version", "v0"},
		{"DeleteKubernetesRuntimeCmd version default", DeleteKubernetesRuntimeCmd, "version", "v0"},
		{"GetKubernetesRuntimeDefinitionsCmd version default", GetKubernetesRuntimeDefinitionsCmd, "version", "v0"},
		{"GetKubernetesRuntimeDefinitionsCmd output default", GetKubernetesRuntimeDefinitionsCmd, "output", "tabular"},
		{"CreateKubernetesRuntimeDefinitionCmd version default", CreateKubernetesRuntimeDefinitionCmd, "version", "v0"},
		{"ReplaceKubernetesRuntimeDefinitionCmd version default", ReplaceKubernetesRuntimeDefinitionCmd, "version", "v0"},
		{"DeleteKubernetesRuntimeDefinitionCmd version default", DeleteKubernetesRuntimeDefinitionCmd, "version", "v0"},
		{"GetKubernetesRuntimeInstancesCmd version default", GetKubernetesRuntimeInstancesCmd, "version", "v0"},
		{"GetKubernetesRuntimeInstancesCmd output default", GetKubernetesRuntimeInstancesCmd, "output", "tabular"},
		{"CreateKubernetesRuntimeInstanceCmd version default", CreateKubernetesRuntimeInstanceCmd, "version", "v0"},
		{"ReplaceKubernetesRuntimeInstanceCmd version default", ReplaceKubernetesRuntimeInstanceCmd, "version", "v0"},
		{"DeleteKubernetesRuntimeInstanceCmd version default", DeleteKubernetesRuntimeInstanceCmd, "version", "v0"},
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

// TestKubernetesRuntimeReplaceCommandsRequireName asserts every replace
// command marks --name as required, matching MarkFlagRequired("name") in
// kubernetes_runtime.go.
func TestKubernetesRuntimeReplaceCommandsRequireName(t *testing.T) {
	// every replace command in kubernetes_runtime.go marks --name as required.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"ReplaceKubernetesRuntimeDefinitionCmd", ReplaceKubernetesRuntimeDefinitionCmd},
		{"ReplaceKubernetesRuntimeInstanceCmd", ReplaceKubernetesRuntimeInstanceCmd},
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

// TestKubernetesRuntimeCommandsAttachedToParents asserts each kubernetes
// runtime command is registered under the appropriate parent verb command
// (Get / Create / Replace / Delete).
func TestKubernetesRuntimeCommandsAttachedToParents(t *testing.T) {
	// each entry maps a command to the parent verb command it must live under.
	tests := []struct {
		name   string
		cmd    *cobra.Command
		parent *cobra.Command
	}{
		{"GetKubernetesRuntimesCmd", GetKubernetesRuntimesCmd, GetCmd},
		{"CreateKubernetesRuntimeCmd", CreateKubernetesRuntimeCmd, CreateCmd},
		{"DeleteKubernetesRuntimeCmd", DeleteKubernetesRuntimeCmd, DeleteCmd},
		{"GetKubernetesRuntimeDefinitionsCmd", GetKubernetesRuntimeDefinitionsCmd, GetCmd},
		{"CreateKubernetesRuntimeDefinitionCmd", CreateKubernetesRuntimeDefinitionCmd, CreateCmd},
		{"ReplaceKubernetesRuntimeDefinitionCmd", ReplaceKubernetesRuntimeDefinitionCmd, ReplaceCmd},
		{"DeleteKubernetesRuntimeDefinitionCmd", DeleteKubernetesRuntimeDefinitionCmd, DeleteCmd},
		{"GetKubernetesRuntimeInstancesCmd", GetKubernetesRuntimeInstancesCmd, GetCmd},
		{"CreateKubernetesRuntimeInstanceCmd", CreateKubernetesRuntimeInstanceCmd, CreateCmd},
		{"ReplaceKubernetesRuntimeInstanceCmd", ReplaceKubernetesRuntimeInstanceCmd, ReplaceCmd},
		{"DeleteKubernetesRuntimeInstanceCmd", DeleteKubernetesRuntimeInstanceCmd, DeleteCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// walk the parent's Commands() slice looking for the target.
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

// TestKubernetesRuntimeFlagShorthandsMatch asserts that the shorthand flags
// kubernetes_runtime.go declares map to the letters users see in the command
// help output.
func TestKubernetesRuntimeFlagShorthandsMatch(t *testing.T) {
	// each entry pins a (command, flag) to its expected one-letter shorthand.
	tests := []struct {
		name      string
		cmd       *cobra.Command
		flag      string
		shorthand string
	}{
		{"GetKubernetesRuntimesCmd name -n", GetKubernetesRuntimesCmd, "name", "n"},
		{"GetKubernetesRuntimesCmd config -c", GetKubernetesRuntimesCmd, "config", "c"},
		{"GetKubernetesRuntimesCmd version -v", GetKubernetesRuntimesCmd, "version", "v"},
		{"GetKubernetesRuntimesCmd output -o", GetKubernetesRuntimesCmd, "output", "o"},
		{"GetKubernetesRuntimesCmd control-plane-name -i", GetKubernetesRuntimesCmd, "control-plane-name", "i"},
		{"CreateKubernetesRuntimeCmd config -c", CreateKubernetesRuntimeCmd, "config", "c"},
		{"DeleteKubernetesRuntimeCmd config -c", DeleteKubernetesRuntimeCmd, "config", "c"},
		{"ReplaceKubernetesRuntimeDefinitionCmd name -n", ReplaceKubernetesRuntimeDefinitionCmd, "name", "n"},
		{"ReplaceKubernetesRuntimeInstanceCmd name -n", ReplaceKubernetesRuntimeInstanceCmd, "name", "n"},
		{"DeleteKubernetesRuntimeDefinitionCmd name -n", DeleteKubernetesRuntimeDefinitionCmd, "name", "n"},
		{"DeleteKubernetesRuntimeInstanceCmd name -n", DeleteKubernetesRuntimeInstanceCmd, "name", "n"},
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

// TestKubernetesRuntimeStdinFlagDefaultsFalse asserts the --stdin flag is a
// bool flag that defaults false on every command that registers it.
func TestKubernetesRuntimeStdinFlagDefaultsFalse(t *testing.T) {
	// each entry names a command that must register --stdin defaulting to false.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"CreateKubernetesRuntimeCmd", CreateKubernetesRuntimeCmd},
		{"CreateKubernetesRuntimeDefinitionCmd", CreateKubernetesRuntimeDefinitionCmd},
		{"ReplaceKubernetesRuntimeDefinitionCmd", ReplaceKubernetesRuntimeDefinitionCmd},
		{"CreateKubernetesRuntimeInstanceCmd", CreateKubernetesRuntimeInstanceCmd,},
		{"ReplaceKubernetesRuntimeInstanceCmd", ReplaceKubernetesRuntimeInstanceCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// the flag must exist and default to "false".
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
