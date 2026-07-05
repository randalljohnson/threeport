package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// hasSubcommand reports whether parent registered child in its command tree.
func hasSubcommand(parent, child *cobra.Command) bool {
	for _, c := range parent.Commands() {
		if c == child {
			return true
		}
	}
	return false
}

// assertFlags reports flag names missing from cmd's flag set.
func assertFlags(t *testing.T, cmd *cobra.Command, want []string) {
	t.Helper()
	for _, name := range want {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("expected flag %q on %q, not found", name, cmd.Use)
		}
	}
}

// TestGetProfilesCmdMetadata asserts get-profiles command metadata (Use, alias, silence, PreRun).
func TestGetProfilesCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if GetProfilesCmd.Use != "profiles" {
		t.Errorf("GetProfilesCmd.Use = %q, want %q", GetProfilesCmd.Use, "profiles")
	}
	// verify singular alias registered
	found := false
	for _, a := range GetProfilesCmd.Aliases {
		if a == "profile" {
			found = true
		}
	}
	if !found {
		t.Errorf("GetProfilesCmd.Aliases = %v, want to include %q", GetProfilesCmd.Aliases, "profile")
	}
	// verify usage is silenced on error
	if !GetProfilesCmd.SilenceUsage {
		t.Errorf("GetProfilesCmd.SilenceUsage = false, want true")
	}
	// verify PreRun hook wired
	if GetProfilesCmd.PreRun == nil {
		t.Errorf("GetProfilesCmd.PreRun = nil, want a function")
	}
}

// TestGetProfilesCmdFlags asserts get-profiles registered flags.
func TestGetProfilesCmdFlags(t *testing.T) {
	// verify all documented flags exist
	assertFlags(t, GetProfilesCmd, []string{"name", "config", "version", "output", "control-plane-name"})
}

// TestGetProfilesCmdRegistered asserts GetProfilesCmd is a subcommand of GetCmd.
func TestGetProfilesCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(GetCmd, GetProfilesCmd) {
		t.Errorf("GetProfilesCmd not registered under GetCmd")
	}
}

// TestCreateProfileCmdMetadata asserts create-profile command metadata.
func TestCreateProfileCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if CreateProfileCmd.Use != "profile" {
		t.Errorf("CreateProfileCmd.Use = %q, want %q", CreateProfileCmd.Use, "profile")
	}
	// verify usage is silenced on error
	if !CreateProfileCmd.SilenceUsage {
		t.Errorf("CreateProfileCmd.SilenceUsage = false, want true")
	}
	// verify PreRun hook wired
	if CreateProfileCmd.PreRun == nil {
		t.Errorf("CreateProfileCmd.PreRun = nil, want a function")
	}
}

// TestCreateProfileCmdFlags asserts create-profile registered flags.
func TestCreateProfileCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, CreateProfileCmd, []string{"config", "stdin", "version", "control-plane-name"})
}

// TestCreateProfileCmdRegistered asserts CreateProfileCmd is a subcommand of CreateCmd.
func TestCreateProfileCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(CreateCmd, CreateProfileCmd) {
		t.Errorf("CreateProfileCmd not registered under CreateCmd")
	}
}

// TestReplaceProfileCmdMetadata asserts replace-profile command metadata.
func TestReplaceProfileCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if ReplaceProfileCmd.Use != "profile" {
		t.Errorf("ReplaceProfileCmd.Use = %q, want %q", ReplaceProfileCmd.Use, "profile")
	}
	// verify usage is silenced on error
	if !ReplaceProfileCmd.SilenceUsage {
		t.Errorf("ReplaceProfileCmd.SilenceUsage = false, want true")
	}
}

// TestReplaceProfileCmdFlags asserts replace-profile registered flags including required name.
func TestReplaceProfileCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, ReplaceProfileCmd, []string{"config", "stdin", "name", "version", "control-plane-name"})

	// verify name is marked required so cobra rejects invocations that omit it
	name := ReplaceProfileCmd.Flags().Lookup("name")
	if name == nil {
		t.Fatalf("name flag missing")
	}
	req, ok := name.Annotations[cobra.BashCompOneRequiredFlag]
	if !ok || len(req) == 0 || req[0] != "true" {
		t.Errorf("name flag not marked required on ReplaceProfileCmd")
	}
}

// TestReplaceProfileCmdRegistered asserts ReplaceProfileCmd is a subcommand of ReplaceCmd.
func TestReplaceProfileCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(ReplaceCmd, ReplaceProfileCmd) {
		t.Errorf("ReplaceProfileCmd not registered under ReplaceCmd")
	}
}

// TestDeleteProfileCmdMetadata asserts delete-profile command metadata.
func TestDeleteProfileCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if DeleteProfileCmd.Use != "profile" {
		t.Errorf("DeleteProfileCmd.Use = %q, want %q", DeleteProfileCmd.Use, "profile")
	}
	// verify usage is silenced on error
	if !DeleteProfileCmd.SilenceUsage {
		t.Errorf("DeleteProfileCmd.SilenceUsage = false, want true")
	}
}

// TestDeleteProfileCmdFlags asserts delete-profile registered flags.
func TestDeleteProfileCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, DeleteProfileCmd, []string{"config", "name", "version", "control-plane-name"})
}

// TestDeleteProfileCmdRegistered asserts DeleteProfileCmd is a subcommand of DeleteCmd.
func TestDeleteProfileCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(DeleteCmd, DeleteProfileCmd) {
		t.Errorf("DeleteProfileCmd not registered under DeleteCmd")
	}
}

// TestGetTiersCmdMetadata asserts get-tiers command metadata.
func TestGetTiersCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if GetTiersCmd.Use != "tiers" {
		t.Errorf("GetTiersCmd.Use = %q, want %q", GetTiersCmd.Use, "tiers")
	}
	// verify singular alias registered
	found := false
	for _, a := range GetTiersCmd.Aliases {
		if a == "tier" {
			found = true
		}
	}
	if !found {
		t.Errorf("GetTiersCmd.Aliases = %v, want to include %q", GetTiersCmd.Aliases, "tier")
	}
	// verify usage is silenced on error
	if !GetTiersCmd.SilenceUsage {
		t.Errorf("GetTiersCmd.SilenceUsage = false, want true")
	}
}

// TestGetTiersCmdFlags asserts get-tiers registered flags.
func TestGetTiersCmdFlags(t *testing.T) {
	// verify all documented flags exist
	assertFlags(t, GetTiersCmd, []string{"name", "config", "version", "output", "control-plane-name"})
}

// TestGetTiersCmdRegistered asserts GetTiersCmd is a subcommand of GetCmd.
func TestGetTiersCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(GetCmd, GetTiersCmd) {
		t.Errorf("GetTiersCmd not registered under GetCmd")
	}
}

// TestCreateTierCmdMetadata asserts create-tier command metadata.
func TestCreateTierCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if CreateTierCmd.Use != "tier" {
		t.Errorf("CreateTierCmd.Use = %q, want %q", CreateTierCmd.Use, "tier")
	}
	// verify usage is silenced on error
	if !CreateTierCmd.SilenceUsage {
		t.Errorf("CreateTierCmd.SilenceUsage = false, want true")
	}
}

// TestCreateTierCmdFlags asserts create-tier registered flags.
func TestCreateTierCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, CreateTierCmd, []string{"config", "stdin", "version", "control-plane-name"})
}

// TestCreateTierCmdRegistered asserts CreateTierCmd is a subcommand of CreateCmd.
func TestCreateTierCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(CreateCmd, CreateTierCmd) {
		t.Errorf("CreateTierCmd not registered under CreateCmd")
	}
}

// TestReplaceTierCmdMetadata asserts replace-tier command metadata.
func TestReplaceTierCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if ReplaceTierCmd.Use != "tier" {
		t.Errorf("ReplaceTierCmd.Use = %q, want %q", ReplaceTierCmd.Use, "tier")
	}
	// verify usage is silenced on error
	if !ReplaceTierCmd.SilenceUsage {
		t.Errorf("ReplaceTierCmd.SilenceUsage = false, want true")
	}
}

// TestReplaceTierCmdFlags asserts replace-tier registered flags including required name.
func TestReplaceTierCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, ReplaceTierCmd, []string{"config", "stdin", "name", "version", "control-plane-name"})

	// verify name is marked required so cobra rejects invocations that omit it
	name := ReplaceTierCmd.Flags().Lookup("name")
	if name == nil {
		t.Fatalf("name flag missing")
	}
	req, ok := name.Annotations[cobra.BashCompOneRequiredFlag]
	if !ok || len(req) == 0 || req[0] != "true" {
		t.Errorf("name flag not marked required on ReplaceTierCmd")
	}
}

// TestReplaceTierCmdRegistered asserts ReplaceTierCmd is a subcommand of ReplaceCmd.
func TestReplaceTierCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(ReplaceCmd, ReplaceTierCmd) {
		t.Errorf("ReplaceTierCmd not registered under ReplaceCmd")
	}
}

// TestDeleteTierCmdMetadata asserts delete-tier command metadata.
func TestDeleteTierCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if DeleteTierCmd.Use != "tier" {
		t.Errorf("DeleteTierCmd.Use = %q, want %q", DeleteTierCmd.Use, "tier")
	}
	// verify usage is silenced on error
	if !DeleteTierCmd.SilenceUsage {
		t.Errorf("DeleteTierCmd.SilenceUsage = false, want true")
	}
}

// TestDeleteTierCmdFlags asserts delete-tier registered flags.
func TestDeleteTierCmdFlags(t *testing.T) {
	// verify documented flags exist
	assertFlags(t, DeleteTierCmd, []string{"config", "name", "version", "control-plane-name"})
}

// TestDeleteTierCmdRegistered asserts DeleteTierCmd is a subcommand of DeleteCmd.
func TestDeleteTierCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(DeleteCmd, DeleteTierCmd) {
		t.Errorf("DeleteTierCmd not registered under DeleteCmd")
	}
}

// TestActuatorFlagDefaults asserts version and output flags default to sensible values.
func TestActuatorFlagDefaults(t *testing.T) {
	cases := []struct {
		name string
		cmd  *cobra.Command
		flag string
		want string
	}{
		{"GetProfiles version", GetProfilesCmd, "version", "v0"},
		{"GetProfiles output", GetProfilesCmd, "output", "tabular"},
		{"CreateProfile version", CreateProfileCmd, "version", "v0"},
		{"ReplaceProfile version", ReplaceProfileCmd, "version", "v0"},
		{"DeleteProfile version", DeleteProfileCmd, "version", "v0"},
		{"GetTiers version", GetTiersCmd, "version", "v0"},
		{"GetTiers output", GetTiersCmd, "output", "tabular"},
		{"CreateTier version", CreateTierCmd, "version", "v0"},
		{"ReplaceTier version", ReplaceTierCmd, "version", "v0"},
		{"DeleteTier version", DeleteTierCmd, "version", "v0"},
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
