package cmd

import (
	"testing"

	cobra "github.com/spf13/cobra"
)

// TestGcpCommandsCoverExpectedMetadata asserts that every exported gcp cobra
// command variable in gcp.go carries the metadata fields (Use, Short, PreRun,
// Run, SilenceUsage) callers rely on for tptctl help output and dispatch.
func TestGcpCommandsCoverExpectedMetadata(t *testing.T) {
	// each table entry pairs a command with the expected Use token and Short
	// description; the fixture list mirrors the surface gcp.go publishes.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		use   string
		short string
	}{
		{"GetGcpProvidersCmd", GetGcpProvidersCmd, "gcp-providers", "Get gcp providers from the system"},
		{"CreateGcpProviderCmd", CreateGcpProviderCmd, "gcp-provider", "Create a new gcp provider"},
		{"ReplaceGcpProviderCmd", ReplaceGcpProviderCmd, "gcp-provider", "Replace an existing gcp provider"},
		{"DeleteGcpProviderCmd", DeleteGcpProviderCmd, "gcp-provider", "Delete an existing gcp provider"},
		{"GetGcpGkeKubernetesRuntimesCmd", GetGcpGkeKubernetesRuntimesCmd, "gcp-gke-kubernetes-runtimes", "Get gcp gke kubernetes runtimes from the system"},
		{"CreateGcpGkeKubernetesRuntimeCmd", CreateGcpGkeKubernetesRuntimeCmd, "gcp-gke-kubernetes-runtime", "Create a new gcp gke kubernetes runtime"},
		{"DeleteGcpGkeKubernetesRuntimeCmd", DeleteGcpGkeKubernetesRuntimeCmd, "gcp-gke-kubernetes-runtime", "Delete an existing gcp gke kubernetes runtime"},
		{"GetGcpGkeKubernetesRuntimeDefinitionsCmd", GetGcpGkeKubernetesRuntimeDefinitionsCmd, "gcp-gke-kubernetes-runtime-definitions", "Get gcp gke kubernetes runtime definitions from the system"},
		{"CreateGcpGkeKubernetesRuntimeDefinitionCmd", CreateGcpGkeKubernetesRuntimeDefinitionCmd, "gcp-gke-kubernetes-runtime-definition", "Create a new gcp gke kubernetes runtime definition"},
		{"ReplaceGcpGkeKubernetesRuntimeDefinitionCmd", ReplaceGcpGkeKubernetesRuntimeDefinitionCmd, "gcp-gke-kubernetes-runtime-definition", "Replace an existing gcp gke kubernetes runtime definition"},
		{"DeleteGcpGkeKubernetesRuntimeDefinitionCmd", DeleteGcpGkeKubernetesRuntimeDefinitionCmd, "gcp-gke-kubernetes-runtime-definition", "Delete an existing gcp gke kubernetes runtime definition"},
		{"GetGcpGkeKubernetesRuntimeInstancesCmd", GetGcpGkeKubernetesRuntimeInstancesCmd, "gcp-gke-kubernetes-runtime-instances", "Get gcp gke kubernetes runtime instances from the system"},
		{"CreateGcpGkeKubernetesRuntimeInstanceCmd", CreateGcpGkeKubernetesRuntimeInstanceCmd, "gcp-gke-kubernetes-runtime-instance", "Create a new gcp gke kubernetes runtime instance"},
		{"ReplaceGcpGkeKubernetesRuntimeInstanceCmd", ReplaceGcpGkeKubernetesRuntimeInstanceCmd, "gcp-gke-kubernetes-runtime-instance", "Replace an existing gcp gke kubernetes runtime instance"},
		{"DeleteGcpGkeKubernetesRuntimeInstanceCmd", DeleteGcpGkeKubernetesRuntimeInstanceCmd, "gcp-gke-kubernetes-runtime-instance", "Delete an existing gcp gke kubernetes runtime instance"},
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

// TestGcpGetCommandsExposeSingularAlias asserts that get-style commands accept
// the singular alias so users can type `gcp-provider` in place of `gcp-providers`.
func TestGcpGetCommandsExposeSingularAlias(t *testing.T) {
	// each entry maps a plural-form get command to its expected singular alias.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		alias string
	}{
		{"GetGcpProvidersCmd", GetGcpProvidersCmd, "gcp-provider"},
		{"GetGcpGkeKubernetesRuntimesCmd", GetGcpGkeKubernetesRuntimesCmd, "gcp-gke-kubernetes-runtime"},
		{"GetGcpGkeKubernetesRuntimeDefinitionsCmd", GetGcpGkeKubernetesRuntimeDefinitionsCmd, "gcp-gke-kubernetes-runtime-definition"},
		{"GetGcpGkeKubernetesRuntimeInstancesCmd", GetGcpGkeKubernetesRuntimeInstancesCmd, "gcp-gke-kubernetes-runtime-instance"},
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

// TestGcpCommandsRegisterExpectedFlags asserts each gcp command declares the
// flag surface consumers rely on: --config, --version, --control-plane-name
// on every command, plus --name / --output where they appear in gcp.go's
// init blocks.
func TestGcpCommandsRegisterExpectedFlags(t *testing.T) {
	// each entry names a command and the flags that must be present on it.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		flags []string
	}{
		{
			name:  "GetGcpProvidersCmd",
			cmd:   GetGcpProvidersCmd,
			flags: []string{"name", "config", "version", "output", "control-plane-name"},
		},
		{
			name:  "CreateGcpProviderCmd",
			cmd:   CreateGcpProviderCmd,
			flags: []string{"config", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceGcpProviderCmd",
			cmd:   ReplaceGcpProviderCmd,
			flags: []string{"config", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteGcpProviderCmd",
			cmd:   DeleteGcpProviderCmd,
			flags: []string{"config", "name", "control-plane-name", "version"},
		},
		{
			name:  "GetGcpGkeKubernetesRuntimesCmd",
			cmd:   GetGcpGkeKubernetesRuntimesCmd,
			flags: []string{"name", "config", "version", "output", "control-plane-name"},
		},
		{
			name:  "CreateGcpGkeKubernetesRuntimeCmd",
			cmd:   CreateGcpGkeKubernetesRuntimeCmd,
			flags: []string{"config", "control-plane-name", "version"},
		},
		{
			name:  "DeleteGcpGkeKubernetesRuntimeCmd",
			cmd:   DeleteGcpGkeKubernetesRuntimeCmd,
			flags: []string{"config", "control-plane-name", "version"},
		},
		{
			name:  "GetGcpGkeKubernetesRuntimeDefinitionsCmd",
			cmd:   GetGcpGkeKubernetesRuntimeDefinitionsCmd,
			flags: []string{"name", "config", "version", "output", "control-plane-name"},
		},
		{
			name:  "CreateGcpGkeKubernetesRuntimeDefinitionCmd",
			cmd:   CreateGcpGkeKubernetesRuntimeDefinitionCmd,
			flags: []string{"config", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceGcpGkeKubernetesRuntimeDefinitionCmd",
			cmd:   ReplaceGcpGkeKubernetesRuntimeDefinitionCmd,
			flags: []string{"config", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteGcpGkeKubernetesRuntimeDefinitionCmd",
			cmd:   DeleteGcpGkeKubernetesRuntimeDefinitionCmd,
			flags: []string{"config", "name", "control-plane-name", "version"},
		},
		{
			name:  "GetGcpGkeKubernetesRuntimeInstancesCmd",
			cmd:   GetGcpGkeKubernetesRuntimeInstancesCmd,
			flags: []string{"name", "config", "version", "output", "control-plane-name"},
		},
		{
			name:  "CreateGcpGkeKubernetesRuntimeInstanceCmd",
			cmd:   CreateGcpGkeKubernetesRuntimeInstanceCmd,
			flags: []string{"config", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceGcpGkeKubernetesRuntimeInstanceCmd",
			cmd:   ReplaceGcpGkeKubernetesRuntimeInstanceCmd,
			flags: []string{"config", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteGcpGkeKubernetesRuntimeInstanceCmd",
			cmd:   DeleteGcpGkeKubernetesRuntimeInstanceCmd,
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

// TestGcpFlagDefaults asserts the default values for --version and --output
// so tptctl behaves consistently when the user omits them.
func TestGcpFlagDefaults(t *testing.T) {
	// each entry declares an expected default for a specific (command, flag).
	tests := []struct {
		name        string
		cmd         *cobra.Command
		flag        string
		wantDefault string
	}{
		{"GetGcpProvidersCmd version default", GetGcpProvidersCmd, "version", "v0"},
		{"GetGcpProvidersCmd output default", GetGcpProvidersCmd, "output", "tabular"},
		{"CreateGcpProviderCmd version default", CreateGcpProviderCmd, "version", "v0"},
		{"ReplaceGcpProviderCmd version default", ReplaceGcpProviderCmd, "version", "v0"},
		{"DeleteGcpProviderCmd version default", DeleteGcpProviderCmd, "version", "v0"},
		{"GetGcpGkeKubernetesRuntimesCmd output default", GetGcpGkeKubernetesRuntimesCmd, "output", "tabular"},
		{"GetGcpGkeKubernetesRuntimeDefinitionsCmd output default", GetGcpGkeKubernetesRuntimeDefinitionsCmd, "output", "tabular"},
		{"GetGcpGkeKubernetesRuntimeInstancesCmd output default", GetGcpGkeKubernetesRuntimeInstancesCmd, "output", "tabular"},
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

// TestGcpCreateAndReplaceCommandsRequireConfig asserts every create and replace
// command marks --config as required, matching MarkFlagRequired("config") in
// gcp.go.
func TestGcpCreateAndReplaceCommandsRequireConfig(t *testing.T) {
	// every create / replace command in gcp.go marks --config as required.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"CreateGcpProviderCmd", CreateGcpProviderCmd},
		{"ReplaceGcpProviderCmd", ReplaceGcpProviderCmd},
		{"CreateGcpGkeKubernetesRuntimeCmd", CreateGcpGkeKubernetesRuntimeCmd},
		{"CreateGcpGkeKubernetesRuntimeDefinitionCmd", CreateGcpGkeKubernetesRuntimeDefinitionCmd},
		{"ReplaceGcpGkeKubernetesRuntimeDefinitionCmd", ReplaceGcpGkeKubernetesRuntimeDefinitionCmd},
		{"CreateGcpGkeKubernetesRuntimeInstanceCmd", CreateGcpGkeKubernetesRuntimeInstanceCmd},
		{"ReplaceGcpGkeKubernetesRuntimeInstanceCmd", ReplaceGcpGkeKubernetesRuntimeInstanceCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// cobra records required flags in an annotation on the flag itself.
			f := tt.cmd.Flags().Lookup("config")
			if f == nil {
				t.Fatalf("flag \"config\" not registered")
			}
			required, ok := f.Annotations[cobra.BashCompOneRequiredFlag]
			if !ok || len(required) == 0 || required[0] != "true" {
				t.Errorf("--config is not marked required (annotations = %v)", f.Annotations)
			}
		})
	}
}

// TestGcpReplaceCommandsRequireName asserts every replace command marks --name
// as required, matching MarkFlagRequired("name") in gcp.go.
func TestGcpReplaceCommandsRequireName(t *testing.T) {
	// every replace command in gcp.go marks --name as required.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"ReplaceGcpProviderCmd", ReplaceGcpProviderCmd},
		{"ReplaceGcpGkeKubernetesRuntimeDefinitionCmd", ReplaceGcpGkeKubernetesRuntimeDefinitionCmd},
		{"ReplaceGcpGkeKubernetesRuntimeInstanceCmd", ReplaceGcpGkeKubernetesRuntimeInstanceCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// look up --name and verify the required-flag annotation is present.
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

// TestGcpCommandsAttachedToParents asserts each gcp command is registered with
// the appropriate parent verb command (Get / Create / Replace / Delete).
func TestGcpCommandsAttachedToParents(t *testing.T) {
	// each entry maps a gcp command to the parent verb command it must live under.
	tests := []struct {
		name   string
		cmd    *cobra.Command
		parent *cobra.Command
	}{
		{"GetGcpProvidersCmd", GetGcpProvidersCmd, GetCmd},
		{"CreateGcpProviderCmd", CreateGcpProviderCmd, CreateCmd},
		{"ReplaceGcpProviderCmd", ReplaceGcpProviderCmd, ReplaceCmd},
		{"DeleteGcpProviderCmd", DeleteGcpProviderCmd, DeleteCmd},
		{"GetGcpGkeKubernetesRuntimesCmd", GetGcpGkeKubernetesRuntimesCmd, GetCmd},
		{"CreateGcpGkeKubernetesRuntimeCmd", CreateGcpGkeKubernetesRuntimeCmd, CreateCmd},
		{"DeleteGcpGkeKubernetesRuntimeCmd", DeleteGcpGkeKubernetesRuntimeCmd, DeleteCmd},
		{"GetGcpGkeKubernetesRuntimeDefinitionsCmd", GetGcpGkeKubernetesRuntimeDefinitionsCmd, GetCmd},
		{"CreateGcpGkeKubernetesRuntimeDefinitionCmd", CreateGcpGkeKubernetesRuntimeDefinitionCmd, CreateCmd},
		{"ReplaceGcpGkeKubernetesRuntimeDefinitionCmd", ReplaceGcpGkeKubernetesRuntimeDefinitionCmd, ReplaceCmd},
		{"DeleteGcpGkeKubernetesRuntimeDefinitionCmd", DeleteGcpGkeKubernetesRuntimeDefinitionCmd, DeleteCmd},
		{"GetGcpGkeKubernetesRuntimeInstancesCmd", GetGcpGkeKubernetesRuntimeInstancesCmd, GetCmd},
		{"CreateGcpGkeKubernetesRuntimeInstanceCmd", CreateGcpGkeKubernetesRuntimeInstanceCmd, CreateCmd},
		{"ReplaceGcpGkeKubernetesRuntimeInstanceCmd", ReplaceGcpGkeKubernetesRuntimeInstanceCmd, ReplaceCmd},
		{"DeleteGcpGkeKubernetesRuntimeInstanceCmd", DeleteGcpGkeKubernetesRuntimeInstanceCmd, DeleteCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// walk the parent's Commands() slice looking for the gcp command.
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

// TestGcpFlagShorthandsMatch asserts that the shorthand flags gcp.go declares
// map to the letters users see in the command help.
func TestGcpFlagShorthandsMatch(t *testing.T) {
	// each entry pins a (command, flag) to its expected one-letter shorthand.
	tests := []struct {
		name      string
		cmd       *cobra.Command
		flag      string
		shorthand string
	}{
		{"GetGcpProvidersCmd name -n", GetGcpProvidersCmd, "name", "n"},
		{"GetGcpProvidersCmd config -c", GetGcpProvidersCmd, "config", "c"},
		{"GetGcpProvidersCmd version -v", GetGcpProvidersCmd, "version", "v"},
		{"GetGcpProvidersCmd output -o", GetGcpProvidersCmd, "output", "o"},
		{"GetGcpProvidersCmd control-plane-name -i", GetGcpProvidersCmd, "control-plane-name", "i"},
		{"CreateGcpProviderCmd config -c", CreateGcpProviderCmd, "config", "c"},
		{"DeleteGcpProviderCmd name -n", DeleteGcpProviderCmd, "name", "n"},
		{"ReplaceGcpProviderCmd name -n", ReplaceGcpProviderCmd, "name", "n"},
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
