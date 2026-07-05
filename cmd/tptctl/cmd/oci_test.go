package cmd

import (
	"testing"

	cobra "github.com/spf13/cobra"
)

// TestOciCommandsCoverExpectedMetadata asserts that every exported oci cobra
// command variable in oci.go carries the metadata fields (Use, Short, PreRun,
// Run, SilenceUsage) callers rely on for tptctl help output and dispatch.
func TestOciCommandsCoverExpectedMetadata(t *testing.T) {
	// each table entry pairs a command with the expected Use token and Short
	// description; the fixture list is the surface oci.go publishes.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		use   string
		short string
	}{
		{"GetOciProvidersCmd", GetOciProvidersCmd, "oci-providers", "Get oci providers from the system"},
		{"CreateOciProviderCmd", CreateOciProviderCmd, "oci-provider", "Create a new oci provider"},
		{"ReplaceOciProviderCmd", ReplaceOciProviderCmd, "oci-provider", "Replace an existing oci provider"},
		{"DeleteOciProviderCmd", DeleteOciProviderCmd, "oci-provider", "Delete an existing oci provider"},
		{"GetOciOkeKubernetesRuntimesCmd", GetOciOkeKubernetesRuntimesCmd, "oci-oke-kubernetes-runtimes", "Get oci oke kubernetes runtimes from the system"},
		{"CreateOciOkeKubernetesRuntimeCmd", CreateOciOkeKubernetesRuntimeCmd, "oci-oke-kubernetes-runtime", "Create a new oci oke kubernetes runtime"},
		{"DeleteOciOkeKubernetesRuntimeCmd", DeleteOciOkeKubernetesRuntimeCmd, "oci-oke-kubernetes-runtime", "Delete an existing oci oke kubernetes runtime"},
		{"GetOciOkeKubernetesRuntimeDefinitionsCmd", GetOciOkeKubernetesRuntimeDefinitionsCmd, "oci-oke-kubernetes-runtime-definitions", "Get oci oke kubernetes runtime definitions from the system"},
		{"CreateOciOkeKubernetesRuntimeDefinitionCmd", CreateOciOkeKubernetesRuntimeDefinitionCmd, "oci-oke-kubernetes-runtime-definition", "Create a new oci oke kubernetes runtime definition"},
		{"ReplaceOciOkeKubernetesRuntimeDefinitionCmd", ReplaceOciOkeKubernetesRuntimeDefinitionCmd, "oci-oke-kubernetes-runtime-definition", "Replace an existing oci oke kubernetes runtime definition"},
		{"DeleteOciOkeKubernetesRuntimeDefinitionCmd", DeleteOciOkeKubernetesRuntimeDefinitionCmd, "oci-oke-kubernetes-runtime-definition", "Delete an existing oci oke kubernetes runtime definition"},
		{"GetOciOkeKubernetesRuntimeInstancesCmd", GetOciOkeKubernetesRuntimeInstancesCmd, "oci-oke-kubernetes-runtime-instances", "Get oci oke kubernetes runtime instances from the system"},
		{"CreateOciOkeKubernetesRuntimeInstanceCmd", CreateOciOkeKubernetesRuntimeInstanceCmd, "oci-oke-kubernetes-runtime-instance", "Create a new oci oke kubernetes runtime instance"},
		{"ReplaceOciOkeKubernetesRuntimeInstanceCmd", ReplaceOciOkeKubernetesRuntimeInstanceCmd, "oci-oke-kubernetes-runtime-instance", "Replace an existing oci oke kubernetes runtime instance"},
		{"DeleteOciOkeKubernetesRuntimeInstanceCmd", DeleteOciOkeKubernetesRuntimeInstanceCmd, "oci-oke-kubernetes-runtime-instance", "Delete an existing oci oke kubernetes runtime instance"},
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

// TestOciGetCommandsExposeSingularAlias asserts that get-style commands accept
// the singular alias so users can type `oci-provider` in place of `oci-providers`.
func TestOciGetCommandsExposeSingularAlias(t *testing.T) {
	// each entry maps a plural-form get command to its expected singular alias.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		alias string
	}{
		{"GetOciProvidersCmd", GetOciProvidersCmd, "oci-provider"},
		{"GetOciOkeKubernetesRuntimesCmd", GetOciOkeKubernetesRuntimesCmd, "oci-oke-kubernetes-runtime"},
		{"GetOciOkeKubernetesRuntimeDefinitionsCmd", GetOciOkeKubernetesRuntimeDefinitionsCmd, "oci-oke-kubernetes-runtime-definition"},
		{"GetOciOkeKubernetesRuntimeInstancesCmd", GetOciOkeKubernetesRuntimeInstancesCmd, "oci-oke-kubernetes-runtime-instance"},
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

// TestOciCommandsRegisterExpectedFlags asserts each oci command declares the
// flag surface consumers rely on: --config, --version, --control-plane-name
// on every command, plus --name / --output / --decrypt-secrets / --stdin
// where they appear in oci.go's init blocks.
func TestOciCommandsRegisterExpectedFlags(t *testing.T) {
	// each entry names a command and the flags that must be present on it.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		flags []string
	}{
		{
			name:  "GetOciProvidersCmd",
			cmd:   GetOciProvidersCmd,
			flags: []string{"name", "config", "version", "output", "decrypt-secrets", "control-plane-name"},
		},
		{
			name:  "CreateOciProviderCmd",
			cmd:   CreateOciProviderCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceOciProviderCmd",
			cmd:   ReplaceOciProviderCmd,
			flags: []string{"config", "stdin", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteOciProviderCmd",
			cmd:   DeleteOciProviderCmd,
			flags: []string{"config", "name", "control-plane-name", "version"},
		},
		{
			name:  "GetOciOkeKubernetesRuntimesCmd",
			cmd:   GetOciOkeKubernetesRuntimesCmd,
			flags: []string{"name", "config", "version", "output", "control-plane-name"},
		},
		{
			name:  "CreateOciOkeKubernetesRuntimeCmd",
			cmd:   CreateOciOkeKubernetesRuntimeCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "DeleteOciOkeKubernetesRuntimeCmd",
			cmd:   DeleteOciOkeKubernetesRuntimeCmd,
			flags: []string{"config", "control-plane-name", "version"},
		},
		{
			name:  "GetOciOkeKubernetesRuntimeDefinitionsCmd",
			cmd:   GetOciOkeKubernetesRuntimeDefinitionsCmd,
			flags: []string{"name", "config", "version", "output", "control-plane-name"},
		},
		{
			name:  "CreateOciOkeKubernetesRuntimeDefinitionCmd",
			cmd:   CreateOciOkeKubernetesRuntimeDefinitionCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceOciOkeKubernetesRuntimeDefinitionCmd",
			cmd:   ReplaceOciOkeKubernetesRuntimeDefinitionCmd,
			flags: []string{"config", "stdin", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteOciOkeKubernetesRuntimeDefinitionCmd",
			cmd:   DeleteOciOkeKubernetesRuntimeDefinitionCmd,
			flags: []string{"config", "name", "control-plane-name", "version"},
		},
		{
			name:  "GetOciOkeKubernetesRuntimeInstancesCmd",
			cmd:   GetOciOkeKubernetesRuntimeInstancesCmd,
			flags: []string{"name", "config", "version", "output", "control-plane-name"},
		},
		{
			name:  "CreateOciOkeKubernetesRuntimeInstanceCmd",
			cmd:   CreateOciOkeKubernetesRuntimeInstanceCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceOciOkeKubernetesRuntimeInstanceCmd",
			cmd:   ReplaceOciOkeKubernetesRuntimeInstanceCmd,
			flags: []string{"config", "stdin", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteOciOkeKubernetesRuntimeInstanceCmd",
			cmd:   DeleteOciOkeKubernetesRuntimeInstanceCmd,
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

// TestOciFlagDefaults asserts the default values for --version and --output
// so tptctl behaves consistently when the user omits them.
func TestOciFlagDefaults(t *testing.T) {
	// each entry declares an expected default for a specific (command, flag).
	tests := []struct {
		name        string
		cmd         *cobra.Command
		flag        string
		wantDefault string
	}{
		{"GetOciProvidersCmd version default", GetOciProvidersCmd, "version", "v0"},
		{"GetOciProvidersCmd output default", GetOciProvidersCmd, "output", "tabular"},
		{"GetOciProvidersCmd decrypt-secrets default", GetOciProvidersCmd, "decrypt-secrets", "false"},
		{"CreateOciProviderCmd version default", CreateOciProviderCmd, "version", "v0"},
		{"ReplaceOciProviderCmd version default", ReplaceOciProviderCmd, "version", "v0"},
		{"DeleteOciProviderCmd version default", DeleteOciProviderCmd, "version", "v0"},
		{"GetOciOkeKubernetesRuntimesCmd output default", GetOciOkeKubernetesRuntimesCmd, "output", "tabular"},
		{"GetOciOkeKubernetesRuntimeDefinitionsCmd output default", GetOciOkeKubernetesRuntimeDefinitionsCmd, "output", "tabular"},
		{"GetOciOkeKubernetesRuntimeInstancesCmd output default", GetOciOkeKubernetesRuntimeInstancesCmd, "output", "tabular"},
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

// TestOciReplaceCommandsRequireName asserts every replace command marks --name
// as required, matching MarkFlagRequired("name") in oci.go.
func TestOciReplaceCommandsRequireName(t *testing.T) {
	// every replace command in oci.go marks --name as required.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"ReplaceOciProviderCmd", ReplaceOciProviderCmd},
		{"ReplaceOciOkeKubernetesRuntimeDefinitionCmd", ReplaceOciOkeKubernetesRuntimeDefinitionCmd},
		{"ReplaceOciOkeKubernetesRuntimeInstanceCmd", ReplaceOciOkeKubernetesRuntimeInstanceCmd},
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

// TestOciCommandsAttachedToParents asserts each oci command is registered with
// the appropriate parent verb command (Get / Create / Replace / Delete).
func TestOciCommandsAttachedToParents(t *testing.T) {
	// each entry maps an oci command to the parent verb command it must live under.
	tests := []struct {
		name   string
		cmd    *cobra.Command
		parent *cobra.Command
	}{
		{"GetOciProvidersCmd", GetOciProvidersCmd, GetCmd},
		{"CreateOciProviderCmd", CreateOciProviderCmd, CreateCmd},
		{"ReplaceOciProviderCmd", ReplaceOciProviderCmd, ReplaceCmd},
		{"DeleteOciProviderCmd", DeleteOciProviderCmd, DeleteCmd},
		{"GetOciOkeKubernetesRuntimesCmd", GetOciOkeKubernetesRuntimesCmd, GetCmd},
		{"CreateOciOkeKubernetesRuntimeCmd", CreateOciOkeKubernetesRuntimeCmd, CreateCmd},
		{"DeleteOciOkeKubernetesRuntimeCmd", DeleteOciOkeKubernetesRuntimeCmd, DeleteCmd},
		{"GetOciOkeKubernetesRuntimeDefinitionsCmd", GetOciOkeKubernetesRuntimeDefinitionsCmd, GetCmd},
		{"CreateOciOkeKubernetesRuntimeDefinitionCmd", CreateOciOkeKubernetesRuntimeDefinitionCmd, CreateCmd},
		{"ReplaceOciOkeKubernetesRuntimeDefinitionCmd", ReplaceOciOkeKubernetesRuntimeDefinitionCmd, ReplaceCmd},
		{"DeleteOciOkeKubernetesRuntimeDefinitionCmd", DeleteOciOkeKubernetesRuntimeDefinitionCmd, DeleteCmd},
		{"GetOciOkeKubernetesRuntimeInstancesCmd", GetOciOkeKubernetesRuntimeInstancesCmd, GetCmd},
		{"CreateOciOkeKubernetesRuntimeInstanceCmd", CreateOciOkeKubernetesRuntimeInstanceCmd, CreateCmd},
		{"ReplaceOciOkeKubernetesRuntimeInstanceCmd", ReplaceOciOkeKubernetesRuntimeInstanceCmd, ReplaceCmd},
		{"DeleteOciOkeKubernetesRuntimeInstanceCmd", DeleteOciOkeKubernetesRuntimeInstanceCmd, DeleteCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// walk the parent's Commands() slice looking for the oci command.
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

// TestOciFlagShorthandsMatch asserts that the shorthand flags oci.go declares
// map to the letters users see in the command help.
func TestOciFlagShorthandsMatch(t *testing.T) {
	// each entry pins a (command, flag) to its expected one-letter shorthand.
	tests := []struct {
		name      string
		cmd       *cobra.Command
		flag      string
		shorthand string
	}{
		{"GetOciProvidersCmd name -n", GetOciProvidersCmd, "name", "n"},
		{"GetOciProvidersCmd config -c", GetOciProvidersCmd, "config", "c"},
		{"GetOciProvidersCmd version -v", GetOciProvidersCmd, "version", "v"},
		{"GetOciProvidersCmd output -o", GetOciProvidersCmd, "output", "o"},
		{"GetOciProvidersCmd decrypt-secrets -d", GetOciProvidersCmd, "decrypt-secrets", "d"},
		{"GetOciProvidersCmd control-plane-name -i", GetOciProvidersCmd, "control-plane-name", "i"},
		{"CreateOciProviderCmd config -c", CreateOciProviderCmd, "config", "c"},
		{"DeleteOciProviderCmd name -n", DeleteOciProviderCmd, "name", "n"},
		{"ReplaceOciProviderCmd name -n", ReplaceOciProviderCmd, "name", "n"},
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

// TestOciNonGetCommandsHaveNoAliases asserts that create/replace/delete oci
// commands publish no aliases: only get-verb commands accept the singular form.
func TestOciNonGetCommandsHaveNoAliases(t *testing.T) {
	// each entry lists a non-get command that should have zero aliases.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"CreateOciProviderCmd", CreateOciProviderCmd},
		{"ReplaceOciProviderCmd", ReplaceOciProviderCmd},
		{"DeleteOciProviderCmd", DeleteOciProviderCmd},
		{"CreateOciOkeKubernetesRuntimeCmd", CreateOciOkeKubernetesRuntimeCmd},
		{"DeleteOciOkeKubernetesRuntimeCmd", DeleteOciOkeKubernetesRuntimeCmd},
		{"CreateOciOkeKubernetesRuntimeDefinitionCmd", CreateOciOkeKubernetesRuntimeDefinitionCmd},
		{"ReplaceOciOkeKubernetesRuntimeDefinitionCmd", ReplaceOciOkeKubernetesRuntimeDefinitionCmd},
		{"DeleteOciOkeKubernetesRuntimeDefinitionCmd", DeleteOciOkeKubernetesRuntimeDefinitionCmd},
		{"CreateOciOkeKubernetesRuntimeInstanceCmd", CreateOciOkeKubernetesRuntimeInstanceCmd},
		{"ReplaceOciOkeKubernetesRuntimeInstanceCmd", ReplaceOciOkeKubernetesRuntimeInstanceCmd},
		{"DeleteOciOkeKubernetesRuntimeInstanceCmd", DeleteOciOkeKubernetesRuntimeInstanceCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// non-get commands are not expected to declare aliases.
			if len(tt.cmd.Aliases) != 0 {
				t.Errorf("Aliases = %v, want empty", tt.cmd.Aliases)
			}
		})
	}
}
