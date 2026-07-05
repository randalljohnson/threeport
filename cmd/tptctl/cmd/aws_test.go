package cmd

import (
	"testing"

	cobra "github.com/spf13/cobra"
)

// TestAwsCommandsCoverExpectedMetadata asserts that every exported aws cobra
// command variable in aws.go carries the metadata fields (Use, Short, PreRun,
// Run, SilenceUsage) callers rely on for tptctl help output and dispatch.
func TestAwsCommandsCoverExpectedMetadata(t *testing.T) {
	// each table entry pairs a command with the expected Use token and Short
	// description; the fixture list is the surface aws.go publishes.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		use   string
		short string
	}{
		{"GetAwsProvidersCmd", GetAwsProvidersCmd, "aws-providers", "Get aws providers from the system"},
		{"CreateAwsProviderCmd", CreateAwsProviderCmd, "aws-provider", "Create a new aws provider"},
		{"ReplaceAwsProviderCmd", ReplaceAwsProviderCmd, "aws-provider", "Replace an existing aws provider"},
		{"DeleteAwsProviderCmd", DeleteAwsProviderCmd, "aws-provider", "Delete an existing aws provider"},
		{"GetAwsEksKubernetesRuntimesCmd", GetAwsEksKubernetesRuntimesCmd, "aws-eks-kubernetes-runtimes", "Get aws eks kubernetes runtimes from the system"},
		{"CreateAwsEksKubernetesRuntimeCmd", CreateAwsEksKubernetesRuntimeCmd, "aws-eks-kubernetes-runtime", "Create a new aws eks kubernetes runtime"},
		{"DeleteAwsEksKubernetesRuntimeCmd", DeleteAwsEksKubernetesRuntimeCmd, "aws-eks-kubernetes-runtime", "Delete an existing aws eks kubernetes runtime"},
		{"GetAwsEksKubernetesRuntimeDefinitionsCmd", GetAwsEksKubernetesRuntimeDefinitionsCmd, "aws-eks-kubernetes-runtime-definitions", "Get aws eks kubernetes runtime definitions from the system"},
		{"CreateAwsEksKubernetesRuntimeDefinitionCmd", CreateAwsEksKubernetesRuntimeDefinitionCmd, "aws-eks-kubernetes-runtime-definition", "Create a new aws eks kubernetes runtime definition"},
		{"ReplaceAwsEksKubernetesRuntimeDefinitionCmd", ReplaceAwsEksKubernetesRuntimeDefinitionCmd, "aws-eks-kubernetes-runtime-definition", "Replace an existing aws eks kubernetes runtime definition"},
		{"DeleteAwsEksKubernetesRuntimeDefinitionCmd", DeleteAwsEksKubernetesRuntimeDefinitionCmd, "aws-eks-kubernetes-runtime-definition", "Delete an existing aws eks kubernetes runtime definition"},
		{"GetAwsEksKubernetesRuntimeInstancesCmd", GetAwsEksKubernetesRuntimeInstancesCmd, "aws-eks-kubernetes-runtime-instances", "Get aws eks kubernetes runtime instances from the system"},
		{"CreateAwsEksKubernetesRuntimeInstanceCmd", CreateAwsEksKubernetesRuntimeInstanceCmd, "aws-eks-kubernetes-runtime-instance", "Create a new aws eks kubernetes runtime instance"},
		{"ReplaceAwsEksKubernetesRuntimeInstanceCmd", ReplaceAwsEksKubernetesRuntimeInstanceCmd, "aws-eks-kubernetes-runtime-instance", "Replace an existing aws eks kubernetes runtime instance"},
		{"DeleteAwsEksKubernetesRuntimeInstanceCmd", DeleteAwsEksKubernetesRuntimeInstanceCmd, "aws-eks-kubernetes-runtime-instance", "Delete an existing aws eks kubernetes runtime instance"},
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

// TestAwsGetCommandsExposeSingularAlias asserts that get-style commands accept
// the singular alias so users can type `aws-provider` in place of `aws-providers`.
func TestAwsGetCommandsExposeSingularAlias(t *testing.T) {
	// each entry maps a plural-form get command to its expected singular alias.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		alias string
	}{
		{"GetAwsProvidersCmd", GetAwsProvidersCmd, "aws-provider"},
		{"GetAwsEksKubernetesRuntimesCmd", GetAwsEksKubernetesRuntimesCmd, "aws-eks-kubernetes-runtime"},
		{"GetAwsEksKubernetesRuntimeDefinitionsCmd", GetAwsEksKubernetesRuntimeDefinitionsCmd, "aws-eks-kubernetes-runtime-definition"},
		{"GetAwsEksKubernetesRuntimeInstancesCmd", GetAwsEksKubernetesRuntimeInstancesCmd, "aws-eks-kubernetes-runtime-instance"},
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

// TestAwsCommandsRegisterExpectedFlags asserts each aws command declares the
// flag surface consumers rely on: --config, --version, --control-plane-name
// on every command, plus --name / --output / --decrypt-secrets / --stdin
// where they appear in aws.go's init blocks.
func TestAwsCommandsRegisterExpectedFlags(t *testing.T) {
	// each entry names a command and the flags that must be present on it.
	tests := []struct {
		name  string
		cmd   *cobra.Command
		flags []string
	}{
		{
			name:  "GetAwsProvidersCmd",
			cmd:   GetAwsProvidersCmd,
			flags: []string{"name", "config", "version", "output", "decrypt-secrets", "control-plane-name"},
		},
		{
			name:  "CreateAwsProviderCmd",
			cmd:   CreateAwsProviderCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceAwsProviderCmd",
			cmd:   ReplaceAwsProviderCmd,
			flags: []string{"config", "stdin", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteAwsProviderCmd",
			cmd:   DeleteAwsProviderCmd,
			flags: []string{"config", "name", "control-plane-name", "version"},
		},
		{
			name:  "GetAwsEksKubernetesRuntimesCmd",
			cmd:   GetAwsEksKubernetesRuntimesCmd,
			flags: []string{"name", "config", "version", "output", "control-plane-name"},
		},
		{
			name:  "CreateAwsEksKubernetesRuntimeCmd",
			cmd:   CreateAwsEksKubernetesRuntimeCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "DeleteAwsEksKubernetesRuntimeCmd",
			cmd:   DeleteAwsEksKubernetesRuntimeCmd,
			flags: []string{"config", "control-plane-name", "version"},
		},
		{
			name:  "GetAwsEksKubernetesRuntimeDefinitionsCmd",
			cmd:   GetAwsEksKubernetesRuntimeDefinitionsCmd,
			flags: []string{"name", "config", "version", "output", "control-plane-name"},
		},
		{
			name:  "CreateAwsEksKubernetesRuntimeDefinitionCmd",
			cmd:   CreateAwsEksKubernetesRuntimeDefinitionCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceAwsEksKubernetesRuntimeDefinitionCmd",
			cmd:   ReplaceAwsEksKubernetesRuntimeDefinitionCmd,
			flags: []string{"config", "stdin", "name", "control-plane-name", "version"},
		},
		{
			name:  "DeleteAwsEksKubernetesRuntimeDefinitionCmd",
			cmd:   DeleteAwsEksKubernetesRuntimeDefinitionCmd,
			flags: []string{"config", "name", "control-plane-name", "version"},
		},
		{
			name:  "GetAwsEksKubernetesRuntimeInstancesCmd",
			cmd:   GetAwsEksKubernetesRuntimeInstancesCmd,
			flags: []string{"name", "config", "version", "output", "control-plane-name"},
		},
		{
			name:  "CreateAwsEksKubernetesRuntimeInstanceCmd",
			cmd:   CreateAwsEksKubernetesRuntimeInstanceCmd,
			flags: []string{"config", "stdin", "control-plane-name", "version"},
		},
		{
			name:  "ReplaceAwsEksKubernetesRuntimeInstanceCmd",
			cmd:   ReplaceAwsEksKubernetesRuntimeInstanceCmd,
			flags: []string{"config", "stdin", "name", "control-plane-name", "version"},
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

// TestAwsFlagDefaults asserts the default values for --version and --output
// so tptctl behaves consistently when the user omits them.
func TestAwsFlagDefaults(t *testing.T) {
	// each entry declares an expected default for a specific (command, flag).
	tests := []struct {
		name        string
		cmd         *cobra.Command
		flag        string
		wantDefault string
	}{
		{"GetAwsProvidersCmd version default", GetAwsProvidersCmd, "version", "v0"},
		{"GetAwsProvidersCmd output default", GetAwsProvidersCmd, "output", "tabular"},
		{"CreateAwsProviderCmd version default", CreateAwsProviderCmd, "version", "v0"},
		{"ReplaceAwsProviderCmd version default", ReplaceAwsProviderCmd, "version", "v0"},
		{"DeleteAwsProviderCmd version default", DeleteAwsProviderCmd, "version", "v0"},
		{"GetAwsEksKubernetesRuntimesCmd output default", GetAwsEksKubernetesRuntimesCmd, "output", "tabular"},
		{"GetAwsEksKubernetesRuntimeDefinitionsCmd output default", GetAwsEksKubernetesRuntimeDefinitionsCmd, "output", "tabular"},
		{"GetAwsEksKubernetesRuntimeInstancesCmd output default", GetAwsEksKubernetesRuntimeInstancesCmd, "output", "tabular"},
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

// TestAwsReplaceCommandsRequireName asserts every replace command marks --name
// as required, matching MarkFlagRequired("name") in aws.go.
func TestAwsReplaceCommandsRequireName(t *testing.T) {
	// every replace command in aws.go marks --name as required.
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"ReplaceAwsProviderCmd", ReplaceAwsProviderCmd},
		{"ReplaceAwsEksKubernetesRuntimeDefinitionCmd", ReplaceAwsEksKubernetesRuntimeDefinitionCmd},
		{"ReplaceAwsEksKubernetesRuntimeInstanceCmd", ReplaceAwsEksKubernetesRuntimeInstanceCmd},
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

// TestAwsCommandsAttachedToParents asserts each aws command is registered with
// the appropriate parent verb command (Get / Create / Replace / Delete).
func TestAwsCommandsAttachedToParents(t *testing.T) {
	// each entry maps an aws command to the parent verb command it must live under.
	tests := []struct {
		name   string
		cmd    *cobra.Command
		parent *cobra.Command
	}{
		{"GetAwsProvidersCmd", GetAwsProvidersCmd, GetCmd},
		{"CreateAwsProviderCmd", CreateAwsProviderCmd, CreateCmd},
		{"ReplaceAwsProviderCmd", ReplaceAwsProviderCmd, ReplaceCmd},
		{"DeleteAwsProviderCmd", DeleteAwsProviderCmd, DeleteCmd},
		{"GetAwsEksKubernetesRuntimesCmd", GetAwsEksKubernetesRuntimesCmd, GetCmd},
		{"CreateAwsEksKubernetesRuntimeCmd", CreateAwsEksKubernetesRuntimeCmd, CreateCmd},
		{"DeleteAwsEksKubernetesRuntimeCmd", DeleteAwsEksKubernetesRuntimeCmd, DeleteCmd},
		{"GetAwsEksKubernetesRuntimeDefinitionsCmd", GetAwsEksKubernetesRuntimeDefinitionsCmd, GetCmd},
		{"CreateAwsEksKubernetesRuntimeDefinitionCmd", CreateAwsEksKubernetesRuntimeDefinitionCmd, CreateCmd},
		{"ReplaceAwsEksKubernetesRuntimeDefinitionCmd", ReplaceAwsEksKubernetesRuntimeDefinitionCmd, ReplaceCmd},
		{"DeleteAwsEksKubernetesRuntimeDefinitionCmd", DeleteAwsEksKubernetesRuntimeDefinitionCmd, DeleteCmd},
		{"GetAwsEksKubernetesRuntimeInstancesCmd", GetAwsEksKubernetesRuntimeInstancesCmd, GetCmd},
		{"CreateAwsEksKubernetesRuntimeInstanceCmd", CreateAwsEksKubernetesRuntimeInstanceCmd, CreateCmd},
		{"ReplaceAwsEksKubernetesRuntimeInstanceCmd", ReplaceAwsEksKubernetesRuntimeInstanceCmd, ReplaceCmd},
		{"DeleteAwsEksKubernetesRuntimeInstanceCmd", DeleteAwsEksKubernetesRuntimeInstanceCmd, DeleteCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// walk the parent's Commands() slice looking for the aws command.
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

// TestAwsFlagShorthandsMatch asserts that the shorthand flags aws.go declares
// map to the letters users see in the command help.
func TestAwsFlagShorthandsMatch(t *testing.T) {
	// each entry pins a (command, flag) to its expected one-letter shorthand.
	tests := []struct {
		name      string
		cmd       *cobra.Command
		flag      string
		shorthand string
	}{
		{"GetAwsProvidersCmd name -n", GetAwsProvidersCmd, "name", "n"},
		{"GetAwsProvidersCmd config -c", GetAwsProvidersCmd, "config", "c"},
		{"GetAwsProvidersCmd version -v", GetAwsProvidersCmd, "version", "v"},
		{"GetAwsProvidersCmd output -o", GetAwsProvidersCmd, "output", "o"},
		{"GetAwsProvidersCmd decrypt-secrets -d", GetAwsProvidersCmd, "decrypt-secrets", "d"},
		{"GetAwsProvidersCmd control-plane-name -i", GetAwsProvidersCmd, "control-plane-name", "i"},
		{"CreateAwsProviderCmd config -c", CreateAwsProviderCmd, "config", "c"},
		{"DeleteAwsProviderCmd name -n", DeleteAwsProviderCmd, "name", "n"},
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
