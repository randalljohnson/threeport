package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestGetObservabilityStacksCmdMetadata asserts get observability-stacks metadata (Use, alias, silence, PreRun).
func TestGetObservabilityStacksCmdMetadata(t *testing.T) {
	// verify Use string matches the CLI invocation
	if GetObservabilityStacksCmd.Use != "observability-stacks" {
		t.Errorf("GetObservabilityStacksCmd.Use = %q, want %q", GetObservabilityStacksCmd.Use, "observability-stacks")
	}
	// verify singular alias is registered
	found := false
	for _, a := range GetObservabilityStacksCmd.Aliases {
		if a == "observability-stack" {
			found = true
		}
	}
	if !found {
		t.Errorf("GetObservabilityStacksCmd.Aliases = %v, want to include %q", GetObservabilityStacksCmd.Aliases, "observability-stack")
	}
	// verify usage is silenced on error
	if !GetObservabilityStacksCmd.SilenceUsage {
		t.Errorf("GetObservabilityStacksCmd.SilenceUsage = false, want true")
	}
	// verify PreRun hook is wired for control-plane setup
	if GetObservabilityStacksCmd.PreRun == nil {
		t.Errorf("GetObservabilityStacksCmd.PreRun = nil, want a function")
	}
}

// TestGetObservabilityStacksCmdFlags asserts get observability-stacks registers all documented flags.
func TestGetObservabilityStacksCmdFlags(t *testing.T) {
	// verify all documented flags exist on the command
	assertFlags(t, GetObservabilityStacksCmd, []string{"name", "config", "version", "output", "control-plane-name"})
}

// TestGetObservabilityStacksCmdRegistered asserts GetObservabilityStacksCmd is a subcommand of GetCmd.
func TestGetObservabilityStacksCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(GetCmd, GetObservabilityStacksCmd) {
		t.Errorf("GetObservabilityStacksCmd not registered under GetCmd")
	}
}

// TestCreateObservabilityStackCmdMetadata asserts create observability-stack metadata.
func TestCreateObservabilityStackCmdMetadata(t *testing.T) {
	// verify Use string matches the CLI invocation
	if CreateObservabilityStackCmd.Use != "observability-stack" {
		t.Errorf("CreateObservabilityStackCmd.Use = %q, want %q", CreateObservabilityStackCmd.Use, "observability-stack")
	}
	// verify usage is silenced on error
	if !CreateObservabilityStackCmd.SilenceUsage {
		t.Errorf("CreateObservabilityStackCmd.SilenceUsage = false, want true")
	}
	// verify PreRun hook is wired
	if CreateObservabilityStackCmd.PreRun == nil {
		t.Errorf("CreateObservabilityStackCmd.PreRun = nil, want a function")
	}
}

// TestCreateObservabilityStackCmdFlags asserts create observability-stack registers all documented flags.
func TestCreateObservabilityStackCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, CreateObservabilityStackCmd, []string{"config", "stdin", "version", "control-plane-name"})
}

// TestCreateObservabilityStackCmdRegistered asserts CreateObservabilityStackCmd is a subcommand of CreateCmd.
func TestCreateObservabilityStackCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(CreateCmd, CreateObservabilityStackCmd) {
		t.Errorf("CreateObservabilityStackCmd not registered under CreateCmd")
	}
}

// TestDeleteObservabilityStackCmdMetadata asserts delete observability-stack metadata.
func TestDeleteObservabilityStackCmdMetadata(t *testing.T) {
	// verify Use string matches the CLI invocation
	if DeleteObservabilityStackCmd.Use != "observability-stack" {
		t.Errorf("DeleteObservabilityStackCmd.Use = %q, want %q", DeleteObservabilityStackCmd.Use, "observability-stack")
	}
	// verify usage is silenced on error
	if !DeleteObservabilityStackCmd.SilenceUsage {
		t.Errorf("DeleteObservabilityStackCmd.SilenceUsage = false, want true")
	}
}

// TestDeleteObservabilityStackCmdFlags asserts delete observability-stack registers all documented flags.
func TestDeleteObservabilityStackCmdFlags(t *testing.T) {
	// verify documented flags exist; the aggregate delete does not accept --name
	assertFlags(t, DeleteObservabilityStackCmd, []string{"config", "version", "control-plane-name"})
}

// TestDeleteObservabilityStackCmdRegistered asserts DeleteObservabilityStackCmd is a subcommand of DeleteCmd.
func TestDeleteObservabilityStackCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(DeleteCmd, DeleteObservabilityStackCmd) {
		t.Errorf("DeleteObservabilityStackCmd not registered under DeleteCmd")
	}
}

// TestGetObservabilityStackDefinitionsCmdMetadata asserts get observability-stack-definitions metadata.
func TestGetObservabilityStackDefinitionsCmdMetadata(t *testing.T) {
	// verify Use string matches the CLI invocation
	if GetObservabilityStackDefinitionsCmd.Use != "observability-stack-definitions" {
		t.Errorf("GetObservabilityStackDefinitionsCmd.Use = %q, want %q", GetObservabilityStackDefinitionsCmd.Use, "observability-stack-definitions")
	}
	// verify singular alias is registered
	found := false
	for _, a := range GetObservabilityStackDefinitionsCmd.Aliases {
		if a == "observability-stack-definition" {
			found = true
		}
	}
	if !found {
		t.Errorf("GetObservabilityStackDefinitionsCmd.Aliases = %v, want to include %q", GetObservabilityStackDefinitionsCmd.Aliases, "observability-stack-definition")
	}
	// verify usage is silenced on error
	if !GetObservabilityStackDefinitionsCmd.SilenceUsage {
		t.Errorf("GetObservabilityStackDefinitionsCmd.SilenceUsage = false, want true")
	}
}

// TestGetObservabilityStackDefinitionsCmdFlags asserts get observability-stack-definitions registers all documented flags.
func TestGetObservabilityStackDefinitionsCmdFlags(t *testing.T) {
	// verify all documented flags exist
	assertFlags(t, GetObservabilityStackDefinitionsCmd, []string{"name", "config", "version", "output", "control-plane-name"})
}

// TestGetObservabilityStackDefinitionsCmdRegistered asserts GetObservabilityStackDefinitionsCmd is a subcommand of GetCmd.
func TestGetObservabilityStackDefinitionsCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(GetCmd, GetObservabilityStackDefinitionsCmd) {
		t.Errorf("GetObservabilityStackDefinitionsCmd not registered under GetCmd")
	}
}

// TestCreateObservabilityStackDefinitionCmdMetadata asserts create observability-stack-definition metadata.
func TestCreateObservabilityStackDefinitionCmdMetadata(t *testing.T) {
	// verify Use string matches the CLI invocation
	if CreateObservabilityStackDefinitionCmd.Use != "observability-stack-definition" {
		t.Errorf("CreateObservabilityStackDefinitionCmd.Use = %q, want %q", CreateObservabilityStackDefinitionCmd.Use, "observability-stack-definition")
	}
	// verify usage is silenced on error
	if !CreateObservabilityStackDefinitionCmd.SilenceUsage {
		t.Errorf("CreateObservabilityStackDefinitionCmd.SilenceUsage = false, want true")
	}
}

// TestCreateObservabilityStackDefinitionCmdFlags asserts create observability-stack-definition registers all documented flags.
func TestCreateObservabilityStackDefinitionCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, CreateObservabilityStackDefinitionCmd, []string{"config", "stdin", "version", "control-plane-name"})
}

// TestCreateObservabilityStackDefinitionCmdRegistered asserts CreateObservabilityStackDefinitionCmd is a subcommand of CreateCmd.
func TestCreateObservabilityStackDefinitionCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(CreateCmd, CreateObservabilityStackDefinitionCmd) {
		t.Errorf("CreateObservabilityStackDefinitionCmd not registered under CreateCmd")
	}
}

// TestReplaceObservabilityStackDefinitionCmdMetadata asserts replace observability-stack-definition metadata.
func TestReplaceObservabilityStackDefinitionCmdMetadata(t *testing.T) {
	// verify Use string matches the CLI invocation
	if ReplaceObservabilityStackDefinitionCmd.Use != "observability-stack-definition" {
		t.Errorf("ReplaceObservabilityStackDefinitionCmd.Use = %q, want %q", ReplaceObservabilityStackDefinitionCmd.Use, "observability-stack-definition")
	}
	// verify usage is silenced on error
	if !ReplaceObservabilityStackDefinitionCmd.SilenceUsage {
		t.Errorf("ReplaceObservabilityStackDefinitionCmd.SilenceUsage = false, want true")
	}
}

// TestReplaceObservabilityStackDefinitionCmdFlags asserts replace observability-stack-definition flags including required name.
func TestReplaceObservabilityStackDefinitionCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, ReplaceObservabilityStackDefinitionCmd, []string{"config", "stdin", "name", "version", "control-plane-name"})

	// verify name is marked required so cobra rejects invocations that omit it
	name := ReplaceObservabilityStackDefinitionCmd.Flags().Lookup("name")
	if name == nil {
		t.Fatalf("name flag missing")
	}
	req, ok := name.Annotations[cobra.BashCompOneRequiredFlag]
	if !ok || len(req) == 0 || req[0] != "true" {
		t.Errorf("name flag not marked required on ReplaceObservabilityStackDefinitionCmd")
	}
}

// TestReplaceObservabilityStackDefinitionCmdRegistered asserts ReplaceObservabilityStackDefinitionCmd is a subcommand of ReplaceCmd.
func TestReplaceObservabilityStackDefinitionCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(ReplaceCmd, ReplaceObservabilityStackDefinitionCmd) {
		t.Errorf("ReplaceObservabilityStackDefinitionCmd not registered under ReplaceCmd")
	}
}

// TestDeleteObservabilityStackDefinitionCmdMetadata asserts delete observability-stack-definition metadata.
func TestDeleteObservabilityStackDefinitionCmdMetadata(t *testing.T) {
	// verify Use string matches the CLI invocation
	if DeleteObservabilityStackDefinitionCmd.Use != "observability-stack-definition" {
		t.Errorf("DeleteObservabilityStackDefinitionCmd.Use = %q, want %q", DeleteObservabilityStackDefinitionCmd.Use, "observability-stack-definition")
	}
	// verify usage is silenced on error
	if !DeleteObservabilityStackDefinitionCmd.SilenceUsage {
		t.Errorf("DeleteObservabilityStackDefinitionCmd.SilenceUsage = false, want true")
	}
}

// TestDeleteObservabilityStackDefinitionCmdFlags asserts delete observability-stack-definition registers all documented flags.
func TestDeleteObservabilityStackDefinitionCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, DeleteObservabilityStackDefinitionCmd, []string{"config", "name", "version", "control-plane-name"})
}

// TestDeleteObservabilityStackDefinitionCmdRegistered asserts DeleteObservabilityStackDefinitionCmd is a subcommand of DeleteCmd.
func TestDeleteObservabilityStackDefinitionCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(DeleteCmd, DeleteObservabilityStackDefinitionCmd) {
		t.Errorf("DeleteObservabilityStackDefinitionCmd not registered under DeleteCmd")
	}
}

// TestGetObservabilityStackInstancesCmdMetadata asserts get observability-stack-instances metadata.
func TestGetObservabilityStackInstancesCmdMetadata(t *testing.T) {
	// verify Use string matches the CLI invocation
	if GetObservabilityStackInstancesCmd.Use != "observability-stack-instances" {
		t.Errorf("GetObservabilityStackInstancesCmd.Use = %q, want %q", GetObservabilityStackInstancesCmd.Use, "observability-stack-instances")
	}
	// verify singular alias is registered
	found := false
	for _, a := range GetObservabilityStackInstancesCmd.Aliases {
		if a == "observability-stack-instance" {
			found = true
		}
	}
	if !found {
		t.Errorf("GetObservabilityStackInstancesCmd.Aliases = %v, want to include %q", GetObservabilityStackInstancesCmd.Aliases, "observability-stack-instance")
	}
	// verify usage is silenced on error
	if !GetObservabilityStackInstancesCmd.SilenceUsage {
		t.Errorf("GetObservabilityStackInstancesCmd.SilenceUsage = false, want true")
	}
}

// TestGetObservabilityStackInstancesCmdFlags asserts get observability-stack-instances registers all documented flags.
func TestGetObservabilityStackInstancesCmdFlags(t *testing.T) {
	// verify all documented flags exist
	assertFlags(t, GetObservabilityStackInstancesCmd, []string{"name", "config", "version", "output", "control-plane-name"})
}

// TestGetObservabilityStackInstancesCmdRegistered asserts GetObservabilityStackInstancesCmd is a subcommand of GetCmd.
func TestGetObservabilityStackInstancesCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(GetCmd, GetObservabilityStackInstancesCmd) {
		t.Errorf("GetObservabilityStackInstancesCmd not registered under GetCmd")
	}
}

// TestCreateObservabilityStackInstanceCmdMetadata asserts create observability-stack-instance metadata.
func TestCreateObservabilityStackInstanceCmdMetadata(t *testing.T) {
	// verify Use string matches the CLI invocation
	if CreateObservabilityStackInstanceCmd.Use != "observability-stack-instance" {
		t.Errorf("CreateObservabilityStackInstanceCmd.Use = %q, want %q", CreateObservabilityStackInstanceCmd.Use, "observability-stack-instance")
	}
	// verify usage is silenced on error
	if !CreateObservabilityStackInstanceCmd.SilenceUsage {
		t.Errorf("CreateObservabilityStackInstanceCmd.SilenceUsage = false, want true")
	}
}

// TestCreateObservabilityStackInstanceCmdFlags asserts create observability-stack-instance registers all documented flags.
func TestCreateObservabilityStackInstanceCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, CreateObservabilityStackInstanceCmd, []string{"config", "stdin", "version", "control-plane-name"})
}

// TestCreateObservabilityStackInstanceCmdRegistered asserts CreateObservabilityStackInstanceCmd is a subcommand of CreateCmd.
func TestCreateObservabilityStackInstanceCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(CreateCmd, CreateObservabilityStackInstanceCmd) {
		t.Errorf("CreateObservabilityStackInstanceCmd not registered under CreateCmd")
	}
}

// TestReplaceObservabilityStackInstanceCmdMetadata asserts replace observability-stack-instance metadata.
func TestReplaceObservabilityStackInstanceCmdMetadata(t *testing.T) {
	// verify Use string matches the CLI invocation
	if ReplaceObservabilityStackInstanceCmd.Use != "observability-stack-instance" {
		t.Errorf("ReplaceObservabilityStackInstanceCmd.Use = %q, want %q", ReplaceObservabilityStackInstanceCmd.Use, "observability-stack-instance")
	}
	// verify usage is silenced on error
	if !ReplaceObservabilityStackInstanceCmd.SilenceUsage {
		t.Errorf("ReplaceObservabilityStackInstanceCmd.SilenceUsage = false, want true")
	}
}

// TestReplaceObservabilityStackInstanceCmdFlags asserts replace observability-stack-instance flags including required name.
func TestReplaceObservabilityStackInstanceCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, ReplaceObservabilityStackInstanceCmd, []string{"config", "stdin", "name", "version", "control-plane-name"})

	// verify name is marked required so cobra rejects invocations that omit it
	name := ReplaceObservabilityStackInstanceCmd.Flags().Lookup("name")
	if name == nil {
		t.Fatalf("name flag missing")
	}
	req, ok := name.Annotations[cobra.BashCompOneRequiredFlag]
	if !ok || len(req) == 0 || req[0] != "true" {
		t.Errorf("name flag not marked required on ReplaceObservabilityStackInstanceCmd")
	}
}

// TestReplaceObservabilityStackInstanceCmdRegistered asserts ReplaceObservabilityStackInstanceCmd is a subcommand of ReplaceCmd.
func TestReplaceObservabilityStackInstanceCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(ReplaceCmd, ReplaceObservabilityStackInstanceCmd) {
		t.Errorf("ReplaceObservabilityStackInstanceCmd not registered under ReplaceCmd")
	}
}

// TestDeleteObservabilityStackInstanceCmdMetadata asserts delete observability-stack-instance metadata.
func TestDeleteObservabilityStackInstanceCmdMetadata(t *testing.T) {
	// verify Use string matches the CLI invocation
	if DeleteObservabilityStackInstanceCmd.Use != "observability-stack-instance" {
		t.Errorf("DeleteObservabilityStackInstanceCmd.Use = %q, want %q", DeleteObservabilityStackInstanceCmd.Use, "observability-stack-instance")
	}
	// verify usage is silenced on error
	if !DeleteObservabilityStackInstanceCmd.SilenceUsage {
		t.Errorf("DeleteObservabilityStackInstanceCmd.SilenceUsage = false, want true")
	}
}

// TestDeleteObservabilityStackInstanceCmdFlags asserts delete observability-stack-instance registers all documented flags.
func TestDeleteObservabilityStackInstanceCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, DeleteObservabilityStackInstanceCmd, []string{"config", "name", "version", "control-plane-name"})
}

// TestDeleteObservabilityStackInstanceCmdRegistered asserts DeleteObservabilityStackInstanceCmd is a subcommand of DeleteCmd.
func TestDeleteObservabilityStackInstanceCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(DeleteCmd, DeleteObservabilityStackInstanceCmd) {
		t.Errorf("DeleteObservabilityStackInstanceCmd not registered under DeleteCmd")
	}
}

// TestObservabilityFlagDefaults asserts version and output flags default to sensible values.
func TestObservabilityFlagDefaults(t *testing.T) {
	cases := []struct {
		name string
		cmd  *cobra.Command
		flag string
		want string
	}{
		{"GetObservabilityStacks version", GetObservabilityStacksCmd, "version", "v0"},
		{"GetObservabilityStacks output", GetObservabilityStacksCmd, "output", "tabular"},
		{"CreateObservabilityStack version", CreateObservabilityStackCmd, "version", "v0"},
		{"DeleteObservabilityStack version", DeleteObservabilityStackCmd, "version", "v0"},
		{"GetObservabilityStackDefinitions version", GetObservabilityStackDefinitionsCmd, "version", "v0"},
		{"GetObservabilityStackDefinitions output", GetObservabilityStackDefinitionsCmd, "output", "tabular"},
		{"CreateObservabilityStackDefinition version", CreateObservabilityStackDefinitionCmd, "version", "v0"},
		{"ReplaceObservabilityStackDefinition version", ReplaceObservabilityStackDefinitionCmd, "version", "v0"},
		{"DeleteObservabilityStackDefinition version", DeleteObservabilityStackDefinitionCmd, "version", "v0"},
		{"GetObservabilityStackInstances version", GetObservabilityStackInstancesCmd, "version", "v0"},
		{"GetObservabilityStackInstances output", GetObservabilityStackInstancesCmd, "output", "tabular"},
		{"CreateObservabilityStackInstance version", CreateObservabilityStackInstanceCmd, "version", "v0"},
		{"ReplaceObservabilityStackInstance version", ReplaceObservabilityStackInstanceCmd, "version", "v0"},
		{"DeleteObservabilityStackInstance version", DeleteObservabilityStackInstanceCmd, "version", "v0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// verify the flag exists and its default value matches expectation
			f := tc.cmd.Flags().Lookup(tc.flag)
			if f == nil {
				t.Fatalf("flag %q missing on %q", tc.flag, tc.cmd.Use)
			}
			if f.DefValue != tc.want {
				t.Errorf("flag %q default = %q, want %q", tc.flag, f.DefValue, tc.want)
			}
		})
	}
}
