package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestGetSecretsCmdMetadata asserts get-secrets command metadata (Use, alias, silence, PreRun).
func TestGetSecretsCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if GetSecretsCmd.Use != "secrets" {
		t.Errorf("GetSecretsCmd.Use = %q, want %q", GetSecretsCmd.Use, "secrets")
	}
	// verify singular alias registered
	found := false
	for _, a := range GetSecretsCmd.Aliases {
		if a == "secret" {
			found = true
		}
	}
	if !found {
		t.Errorf("GetSecretsCmd.Aliases = %v, want to include %q", GetSecretsCmd.Aliases, "secret")
	}
	// verify usage is silenced on error
	if !GetSecretsCmd.SilenceUsage {
		t.Errorf("GetSecretsCmd.SilenceUsage = false, want true")
	}
	// verify PreRun hook wired
	if GetSecretsCmd.PreRun == nil {
		t.Errorf("GetSecretsCmd.PreRun = nil, want a function")
	}
}

// TestGetSecretsCmdFlags asserts get-secrets registered flags.
func TestGetSecretsCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, GetSecretsCmd, []string{"name", "config", "version", "output", "control-plane-name"})
}

// TestGetSecretsCmdRegistered asserts GetSecretsCmd is a subcommand of GetCmd.
func TestGetSecretsCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(GetCmd, GetSecretsCmd) {
		t.Errorf("GetSecretsCmd not registered under GetCmd")
	}
}

// TestCreateSecretCmdMetadata asserts create-secret command metadata.
func TestCreateSecretCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if CreateSecretCmd.Use != "secret" {
		t.Errorf("CreateSecretCmd.Use = %q, want %q", CreateSecretCmd.Use, "secret")
	}
	// verify usage is silenced on error
	if !CreateSecretCmd.SilenceUsage {
		t.Errorf("CreateSecretCmd.SilenceUsage = false, want true")
	}
	// verify PreRun hook wired
	if CreateSecretCmd.PreRun == nil {
		t.Errorf("CreateSecretCmd.PreRun = nil, want a function")
	}
}

// TestCreateSecretCmdFlags asserts create-secret registered flags.
func TestCreateSecretCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, CreateSecretCmd, []string{"config", "stdin", "version", "control-plane-name"})
}

// TestCreateSecretCmdRegistered asserts CreateSecretCmd is a subcommand of CreateCmd.
func TestCreateSecretCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(CreateCmd, CreateSecretCmd) {
		t.Errorf("CreateSecretCmd not registered under CreateCmd")
	}
}

// TestDeleteSecretCmdMetadata asserts delete-secret command metadata.
func TestDeleteSecretCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if DeleteSecretCmd.Use != "secret" {
		t.Errorf("DeleteSecretCmd.Use = %q, want %q", DeleteSecretCmd.Use, "secret")
	}
	// verify usage is silenced on error
	if !DeleteSecretCmd.SilenceUsage {
		t.Errorf("DeleteSecretCmd.SilenceUsage = false, want true")
	}
	// verify PreRun hook wired
	if DeleteSecretCmd.PreRun == nil {
		t.Errorf("DeleteSecretCmd.PreRun = nil, want a function")
	}
}

// TestDeleteSecretCmdFlags asserts delete-secret registered flags.
func TestDeleteSecretCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, DeleteSecretCmd, []string{"config", "version", "control-plane-name"})
}

// TestDeleteSecretCmdRegistered asserts DeleteSecretCmd is a subcommand of DeleteCmd.
func TestDeleteSecretCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(DeleteCmd, DeleteSecretCmd) {
		t.Errorf("DeleteSecretCmd not registered under DeleteCmd")
	}
}

// TestGetSecretDefinitionsCmdMetadata asserts get-secret-definitions command metadata.
func TestGetSecretDefinitionsCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if GetSecretDefinitionsCmd.Use != "secret-definitions" {
		t.Errorf("GetSecretDefinitionsCmd.Use = %q, want %q", GetSecretDefinitionsCmd.Use, "secret-definitions")
	}
	// verify singular alias registered
	found := false
	for _, a := range GetSecretDefinitionsCmd.Aliases {
		if a == "secret-definition" {
			found = true
		}
	}
	if !found {
		t.Errorf("GetSecretDefinitionsCmd.Aliases = %v, want to include %q", GetSecretDefinitionsCmd.Aliases, "secret-definition")
	}
	// verify usage is silenced on error
	if !GetSecretDefinitionsCmd.SilenceUsage {
		t.Errorf("GetSecretDefinitionsCmd.SilenceUsage = false, want true")
	}
}

// TestGetSecretDefinitionsCmdFlags asserts get-secret-definitions registered flags.
func TestGetSecretDefinitionsCmdFlags(t *testing.T) {
	// verify all documented flags exist
	assertFlags(t, GetSecretDefinitionsCmd, []string{"name", "config", "version", "output", "control-plane-name"})
}

// TestGetSecretDefinitionsCmdRegistered asserts subcommand registration under GetCmd.
func TestGetSecretDefinitionsCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(GetCmd, GetSecretDefinitionsCmd) {
		t.Errorf("GetSecretDefinitionsCmd not registered under GetCmd")
	}
}

// TestCreateSecretDefinitionCmdMetadata asserts create-secret-definition command metadata.
func TestCreateSecretDefinitionCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if CreateSecretDefinitionCmd.Use != "secret-definition" {
		t.Errorf("CreateSecretDefinitionCmd.Use = %q, want %q", CreateSecretDefinitionCmd.Use, "secret-definition")
	}
	// verify usage is silenced on error
	if !CreateSecretDefinitionCmd.SilenceUsage {
		t.Errorf("CreateSecretDefinitionCmd.SilenceUsage = false, want true")
	}
}

// TestCreateSecretDefinitionCmdFlags asserts create-secret-definition registered flags.
func TestCreateSecretDefinitionCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, CreateSecretDefinitionCmd, []string{"config", "stdin", "version", "control-plane-name"})
}

// TestCreateSecretDefinitionCmdRegistered asserts subcommand registration under CreateCmd.
func TestCreateSecretDefinitionCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(CreateCmd, CreateSecretDefinitionCmd) {
		t.Errorf("CreateSecretDefinitionCmd not registered under CreateCmd")
	}
}

// TestReplaceSecretDefinitionCmdMetadata asserts replace-secret-definition command metadata.
func TestReplaceSecretDefinitionCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if ReplaceSecretDefinitionCmd.Use != "secret-definition" {
		t.Errorf("ReplaceSecretDefinitionCmd.Use = %q, want %q", ReplaceSecretDefinitionCmd.Use, "secret-definition")
	}
	// verify usage is silenced on error
	if !ReplaceSecretDefinitionCmd.SilenceUsage {
		t.Errorf("ReplaceSecretDefinitionCmd.SilenceUsage = false, want true")
	}
}

// TestReplaceSecretDefinitionCmdFlags asserts replace-secret-definition registered flags including required name.
func TestReplaceSecretDefinitionCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, ReplaceSecretDefinitionCmd, []string{"config", "stdin", "name", "version", "control-plane-name"})

	// verify name is marked required so cobra rejects invocations that omit it
	name := ReplaceSecretDefinitionCmd.Flags().Lookup("name")
	if name == nil {
		t.Fatalf("name flag missing")
	}
	req, ok := name.Annotations[cobra.BashCompOneRequiredFlag]
	if !ok || len(req) == 0 || req[0] != "true" {
		t.Errorf("name flag not marked required on ReplaceSecretDefinitionCmd")
	}
}

// TestReplaceSecretDefinitionCmdRegistered asserts subcommand registration under ReplaceCmd.
func TestReplaceSecretDefinitionCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(ReplaceCmd, ReplaceSecretDefinitionCmd) {
		t.Errorf("ReplaceSecretDefinitionCmd not registered under ReplaceCmd")
	}
}

// TestDeleteSecretDefinitionCmdMetadata asserts delete-secret-definition command metadata.
func TestDeleteSecretDefinitionCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if DeleteSecretDefinitionCmd.Use != "secret-definition" {
		t.Errorf("DeleteSecretDefinitionCmd.Use = %q, want %q", DeleteSecretDefinitionCmd.Use, "secret-definition")
	}
	// verify usage is silenced on error
	if !DeleteSecretDefinitionCmd.SilenceUsage {
		t.Errorf("DeleteSecretDefinitionCmd.SilenceUsage = false, want true")
	}
}

// TestDeleteSecretDefinitionCmdFlags asserts delete-secret-definition registered flags.
func TestDeleteSecretDefinitionCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, DeleteSecretDefinitionCmd, []string{"config", "name", "version", "control-plane-name"})
}

// TestDeleteSecretDefinitionCmdRegistered asserts subcommand registration under DeleteCmd.
func TestDeleteSecretDefinitionCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(DeleteCmd, DeleteSecretDefinitionCmd) {
		t.Errorf("DeleteSecretDefinitionCmd not registered under DeleteCmd")
	}
}

// TestGetSecretInstancesCmdMetadata asserts get-secret-instances command metadata.
func TestGetSecretInstancesCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if GetSecretInstancesCmd.Use != "secret-instances" {
		t.Errorf("GetSecretInstancesCmd.Use = %q, want %q", GetSecretInstancesCmd.Use, "secret-instances")
	}
	// verify singular alias registered
	found := false
	for _, a := range GetSecretInstancesCmd.Aliases {
		if a == "secret-instance" {
			found = true
		}
	}
	if !found {
		t.Errorf("GetSecretInstancesCmd.Aliases = %v, want to include %q", GetSecretInstancesCmd.Aliases, "secret-instance")
	}
	// verify usage is silenced on error
	if !GetSecretInstancesCmd.SilenceUsage {
		t.Errorf("GetSecretInstancesCmd.SilenceUsage = false, want true")
	}
}

// TestGetSecretInstancesCmdFlags asserts get-secret-instances registered flags.
func TestGetSecretInstancesCmdFlags(t *testing.T) {
	// verify all documented flags exist
	assertFlags(t, GetSecretInstancesCmd, []string{"name", "config", "version", "output", "control-plane-name"})
}

// TestGetSecretInstancesCmdRegistered asserts subcommand registration under GetCmd.
func TestGetSecretInstancesCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(GetCmd, GetSecretInstancesCmd) {
		t.Errorf("GetSecretInstancesCmd not registered under GetCmd")
	}
}

// TestCreateSecretInstanceCmdMetadata asserts create-secret-instance command metadata.
func TestCreateSecretInstanceCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if CreateSecretInstanceCmd.Use != "secret-instance" {
		t.Errorf("CreateSecretInstanceCmd.Use = %q, want %q", CreateSecretInstanceCmd.Use, "secret-instance")
	}
	// verify usage is silenced on error
	if !CreateSecretInstanceCmd.SilenceUsage {
		t.Errorf("CreateSecretInstanceCmd.SilenceUsage = false, want true")
	}
}

// TestCreateSecretInstanceCmdFlags asserts create-secret-instance registered flags.
func TestCreateSecretInstanceCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, CreateSecretInstanceCmd, []string{"config", "stdin", "version", "control-plane-name"})
}

// TestCreateSecretInstanceCmdRegistered asserts subcommand registration under CreateCmd.
func TestCreateSecretInstanceCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(CreateCmd, CreateSecretInstanceCmd) {
		t.Errorf("CreateSecretInstanceCmd not registered under CreateCmd")
	}
}

// TestReplaceSecretInstanceCmdMetadata asserts replace-secret-instance command metadata.
func TestReplaceSecretInstanceCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if ReplaceSecretInstanceCmd.Use != "secret-instance" {
		t.Errorf("ReplaceSecretInstanceCmd.Use = %q, want %q", ReplaceSecretInstanceCmd.Use, "secret-instance")
	}
	// verify usage is silenced on error
	if !ReplaceSecretInstanceCmd.SilenceUsage {
		t.Errorf("ReplaceSecretInstanceCmd.SilenceUsage = false, want true")
	}
}

// TestReplaceSecretInstanceCmdFlags asserts replace-secret-instance registered flags including required name.
func TestReplaceSecretInstanceCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, ReplaceSecretInstanceCmd, []string{"config", "stdin", "name", "version", "control-plane-name"})

	// verify name is marked required so cobra rejects invocations that omit it
	name := ReplaceSecretInstanceCmd.Flags().Lookup("name")
	if name == nil {
		t.Fatalf("name flag missing")
	}
	req, ok := name.Annotations[cobra.BashCompOneRequiredFlag]
	if !ok || len(req) == 0 || req[0] != "true" {
		t.Errorf("name flag not marked required on ReplaceSecretInstanceCmd")
	}
}

// TestReplaceSecretInstanceCmdRegistered asserts subcommand registration under ReplaceCmd.
func TestReplaceSecretInstanceCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(ReplaceCmd, ReplaceSecretInstanceCmd) {
		t.Errorf("ReplaceSecretInstanceCmd not registered under ReplaceCmd")
	}
}

// TestDeleteSecretInstanceCmdMetadata asserts delete-secret-instance command metadata.
func TestDeleteSecretInstanceCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if DeleteSecretInstanceCmd.Use != "secret-instance" {
		t.Errorf("DeleteSecretInstanceCmd.Use = %q, want %q", DeleteSecretInstanceCmd.Use, "secret-instance")
	}
	// verify usage is silenced on error
	if !DeleteSecretInstanceCmd.SilenceUsage {
		t.Errorf("DeleteSecretInstanceCmd.SilenceUsage = false, want true")
	}
}

// TestDeleteSecretInstanceCmdFlags asserts delete-secret-instance registered flags.
func TestDeleteSecretInstanceCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, DeleteSecretInstanceCmd, []string{"config", "name", "version", "control-plane-name"})
}

// TestDeleteSecretInstanceCmdRegistered asserts subcommand registration under DeleteCmd.
func TestDeleteSecretInstanceCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(DeleteCmd, DeleteSecretInstanceCmd) {
		t.Errorf("DeleteSecretInstanceCmd not registered under DeleteCmd")
	}
}

// TestSecretFlagDefaults asserts version and output flags default to sensible values across the secret command tree.
func TestSecretFlagDefaults(t *testing.T) {
	cases := []struct {
		name string
		cmd  *cobra.Command
		flag string
		want string
	}{
		{"GetSecrets version", GetSecretsCmd, "version", "v0"},
		{"GetSecrets output", GetSecretsCmd, "output", "tabular"},
		{"CreateSecret version", CreateSecretCmd, "version", "v0"},
		{"DeleteSecret version", DeleteSecretCmd, "version", "v0"},
		{"GetSecretDefinitions version", GetSecretDefinitionsCmd, "version", "v0"},
		{"GetSecretDefinitions output", GetSecretDefinitionsCmd, "output", "tabular"},
		{"CreateSecretDefinition version", CreateSecretDefinitionCmd, "version", "v0"},
		{"ReplaceSecretDefinition version", ReplaceSecretDefinitionCmd, "version", "v0"},
		{"DeleteSecretDefinition version", DeleteSecretDefinitionCmd, "version", "v0"},
		{"GetSecretInstances version", GetSecretInstancesCmd, "version", "v0"},
		{"GetSecretInstances output", GetSecretInstancesCmd, "output", "tabular"},
		{"CreateSecretInstance version", CreateSecretInstanceCmd, "version", "v0"},
		{"ReplaceSecretInstance version", ReplaceSecretInstanceCmd, "version", "v0"},
		{"DeleteSecretInstance version", DeleteSecretInstanceCmd, "version", "v0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// verify flag default matches expected value
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

// TestSecretStdinFlagDefaults asserts the stdin flag defaults false on every command that exposes it.
func TestSecretStdinFlagDefaults(t *testing.T) {
	cases := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"CreateSecret", CreateSecretCmd},
		{"CreateSecretDefinition", CreateSecretDefinitionCmd},
		{"ReplaceSecretDefinition", ReplaceSecretDefinitionCmd},
		{"CreateSecretInstance", CreateSecretInstanceCmd},
		{"ReplaceSecretInstance", ReplaceSecretInstanceCmd},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// verify stdin defaults to false so file config is the default source
			f := tc.cmd.Flags().Lookup("stdin")
			if f == nil {
				t.Fatalf("stdin flag missing on %q", tc.cmd.Use)
			}
			if f.DefValue != "false" {
				t.Errorf("stdin default = %q, want %q", f.DefValue, "false")
			}
		})
	}
}
