package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestActuatorProfileCmdsMetadata asserts each profile subcommand carries the expected
// Use, non-empty Short and Long, a wired Run hook, and SilenceUsage true so cobra does
// not print usage on error.
func TestActuatorProfileCmdsMetadata(t *testing.T) {
	// each case pins one command's Use string and asserts the shared metadata contract
	cases := []struct {
		name string
		cmd  *cobra.Command
		use  string
	}{
		{name: "get profiles", cmd: GetProfilesCmd, use: "profiles"},
		{name: "create profile", cmd: CreateProfileCmd, use: "profile"},
		{name: "replace profile", cmd: ReplaceProfileCmd, use: "profile"},
		{name: "delete profile", cmd: DeleteProfileCmd, use: "profile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// verify Use string matches the CLI invocation
			if tc.cmd.Use != tc.use {
				t.Errorf("Use = %q, want %q", tc.cmd.Use, tc.use)
			}
			// verify Short is populated so cobra help output has a description
			if tc.cmd.Short == "" {
				t.Errorf("Short is empty, want non-empty description")
			}
			// verify Long is populated for the extended help view
			if tc.cmd.Long == "" {
				t.Errorf("Long is empty, want non-empty description")
			}
			// verify Run hook is wired so cobra dispatch reaches the handler
			if tc.cmd.Run == nil {
				t.Errorf("Run = nil, want a function")
			}
			// verify PreRun is wired so config initialization runs before the handler
			if tc.cmd.PreRun == nil {
				t.Errorf("PreRun = nil, want CommandPreRunFunc")
			}
			// verify SilenceUsage so a runtime error does not dump usage after the error
			if !tc.cmd.SilenceUsage {
				t.Errorf("SilenceUsage = false, want true")
			}
		})
	}
}

// TestActuatorTierCmdsMetadata mirrors the profile metadata check for the tier commands.
func TestActuatorTierCmdsMetadata(t *testing.T) {
	// each case pins one command's Use string and asserts the shared metadata contract
	cases := []struct {
		name string
		cmd  *cobra.Command
		use  string
	}{
		{name: "get tiers", cmd: GetTiersCmd, use: "tiers"},
		{name: "create tier", cmd: CreateTierCmd, use: "tier"},
		{name: "replace tier", cmd: ReplaceTierCmd, use: "tier"},
		{name: "delete tier", cmd: DeleteTierCmd, use: "tier"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// verify Use string matches the CLI invocation
			if tc.cmd.Use != tc.use {
				t.Errorf("Use = %q, want %q", tc.cmd.Use, tc.use)
			}
			// verify Short is populated so cobra help output has a description
			if tc.cmd.Short == "" {
				t.Errorf("Short is empty, want non-empty description")
			}
			// verify Long is populated for the extended help view
			if tc.cmd.Long == "" {
				t.Errorf("Long is empty, want non-empty description")
			}
			// verify Run hook is wired so cobra dispatch reaches the handler
			if tc.cmd.Run == nil {
				t.Errorf("Run = nil, want a function")
			}
			// verify PreRun is wired so config initialization runs before the handler
			if tc.cmd.PreRun == nil {
				t.Errorf("PreRun = nil, want CommandPreRunFunc")
			}
			// verify SilenceUsage so a runtime error does not dump usage after the error
			if !tc.cmd.SilenceUsage {
				t.Errorf("SilenceUsage = false, want true")
			}
		})
	}
}

// TestActuatorProfileCmdsRegistration asserts each profile subcommand is wired under the
// matching parent verb command by its init().
func TestActuatorProfileCmdsRegistration(t *testing.T) {
	// each case names the expected parent and the command that must appear underneath
	cases := []struct {
		name   string
		parent *cobra.Command
		child  *cobra.Command
	}{
		{name: "get profiles under get", parent: GetCmd, child: GetProfilesCmd},
		{name: "create profile under create", parent: CreateCmd, child: CreateProfileCmd},
		{name: "replace profile under replace", parent: ReplaceCmd, child: ReplaceProfileCmd},
		{name: "delete profile under delete", parent: DeleteCmd, child: DeleteProfileCmd},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// verify the init() registration so `tptctl <verb> profile[s]` resolves
			if !hasSubcommand(tc.parent, tc.child) {
				t.Errorf("%s not registered under %s", tc.child.Use, tc.parent.Use)
			}
		})
	}
}

// TestActuatorTierCmdsRegistration asserts each tier subcommand is wired under the
// matching parent verb command by its init().
func TestActuatorTierCmdsRegistration(t *testing.T) {
	// each case names the expected parent and the command that must appear underneath
	cases := []struct {
		name   string
		parent *cobra.Command
		child  *cobra.Command
	}{
		{name: "get tiers under get", parent: GetCmd, child: GetTiersCmd},
		{name: "create tier under create", parent: CreateCmd, child: CreateTierCmd},
		{name: "replace tier under replace", parent: ReplaceCmd, child: ReplaceTierCmd},
		{name: "delete tier under delete", parent: DeleteCmd, child: DeleteTierCmd},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// verify the init() registration so `tptctl <verb> tier[s]` resolves
			if !hasSubcommand(tc.parent, tc.child) {
				t.Errorf("%s not registered under %s", tc.child.Use, tc.parent.Use)
			}
		})
	}
}

// TestActuatorProfileCmdsFlags asserts each profile subcommand exposes its documented flags
// so shell-level invocations don't silently drop options.
func TestActuatorProfileCmdsFlags(t *testing.T) {
	// get profiles: read-only listing plus a name filter and output format
	assertFlags(t, GetProfilesCmd, []string{"name", "config", "version", "output", "control-plane-name"})
	// create profile: config file or stdin input, no name flag
	assertFlags(t, CreateProfileCmd, []string{"config", "stdin", "control-plane-name", "version"})
	// replace profile: name is required to target the existing object
	assertFlags(t, ReplaceProfileCmd, []string{"config", "stdin", "name", "control-plane-name", "version"})
	// delete profile: config file or name selects the target
	assertFlags(t, DeleteProfileCmd, []string{"config", "name", "control-plane-name", "version"})
}

// TestActuatorTierCmdsFlags asserts each tier subcommand exposes its documented flags
// so shell-level invocations don't silently drop options.
func TestActuatorTierCmdsFlags(t *testing.T) {
	// get tiers: read-only listing plus a name filter and output format
	assertFlags(t, GetTiersCmd, []string{"name", "config", "version", "output", "control-plane-name"})
	// create tier: config file or stdin input, no name flag
	assertFlags(t, CreateTierCmd, []string{"config", "stdin", "control-plane-name", "version"})
	// replace tier: name is required to target the existing object
	assertFlags(t, ReplaceTierCmd, []string{"config", "stdin", "name", "control-plane-name", "version"})
	// delete tier: config file or name selects the target
	assertFlags(t, DeleteTierCmd, []string{"config", "name", "control-plane-name", "version"})
}

// TestActuatorReplaceRequiresName asserts the replace commands mark the --name flag
// required, so cobra rejects a missing --name before the handler runs.
func TestActuatorReplaceRequiresName(t *testing.T) {
	// each replace command must annotate --name as required
	cases := []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "replace profile", cmd: ReplaceProfileCmd},
		{name: "replace tier", cmd: ReplaceTierCmd},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// look up the flag and inspect the cobra required annotation
			flag := tc.cmd.Flags().Lookup("name")
			if flag == nil {
				t.Fatalf("%s missing --name flag", tc.cmd.Use)
			}
			// cobra tags a required flag with BashCompOneRequiredFlag=1
			annotations := flag.Annotations[cobra.BashCompOneRequiredFlag]
			found := false
			for _, a := range annotations {
				if a == "true" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s --name flag is not marked required (annotations: %v)", tc.cmd.Use, annotations)
			}
		})
	}
}

// TestActuatorVersionFlagDefaultsToV0 asserts every actuator command defaults --version to v0.
// The Run handler switches on this value; a drifted default silently changes the target API.
func TestActuatorVersionFlagDefaultsToV0(t *testing.T) {
	// each command's version flag drives the switch in its Run handler
	cmds := []*cobra.Command{
		GetProfilesCmd, CreateProfileCmd, ReplaceProfileCmd, DeleteProfileCmd,
		GetTiersCmd, CreateTierCmd, ReplaceTierCmd, DeleteTierCmd,
	}
	for _, c := range cmds {
		t.Run(c.Use, func(t *testing.T) {
			// verify the flag exists and its default is "v0"
			flag := c.Flags().Lookup("version")
			if flag == nil {
				t.Fatalf("%s missing --version flag", c.Use)
			}
			if flag.DefValue != "v0" {
				t.Errorf("%s --version default = %q, want %q", c.Use, flag.DefValue, "v0")
			}
		})
	}
}

// TestActuatorGetOutputFlagDefaultsToTabular asserts the get commands default --output
// to tabular so an unqualified `tptctl get profiles` prints a table, not JSON.
func TestActuatorGetOutputFlagDefaultsToTabular(t *testing.T) {
	// only the get commands expose --output; the create/replace/delete pairs do not
	cases := []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "get profiles", cmd: GetProfilesCmd},
		{name: "get tiers", cmd: GetTiersCmd},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// verify the flag exists and defaults to tabular
			flag := tc.cmd.Flags().Lookup("output")
			if flag == nil {
				t.Fatalf("%s missing --output flag", tc.cmd.Use)
			}
			if flag.DefValue != "tabular" {
				t.Errorf("%s --output default = %q, want %q", tc.cmd.Use, flag.DefValue, "tabular")
			}
		})
	}
}

// TestActuatorGetCmdAliases asserts the plural get commands accept the singular alias
// so `tptctl get profile` and `tptctl get tier` also resolve.
func TestActuatorGetCmdAliases(t *testing.T) {
	// each plural get command has a singular alias for user convenience
	cases := []struct {
		name  string
		cmd   *cobra.Command
		alias string
	}{
		{name: "profiles", cmd: GetProfilesCmd, alias: "profile"},
		{name: "tiers", cmd: GetTiersCmd, alias: "tier"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// scan the alias slice for the expected singular form
			found := false
			for _, a := range tc.cmd.Aliases {
				if a == tc.alias {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s aliases = %v, want to contain %q", tc.cmd.Use, tc.cmd.Aliases, tc.alias)
			}
		})
	}
}

// TestActuatorExampleTextsReferenceCommand asserts each command's Example string mentions
// tptctl so cobra help output is self-explanatory and drift in the boilerplate is caught.
func TestActuatorExampleTextsReferenceCommand(t *testing.T) {
	// every actuator command carries a documented Example block
	cmds := []*cobra.Command{
		GetProfilesCmd, CreateProfileCmd, ReplaceProfileCmd, DeleteProfileCmd,
		GetTiersCmd, CreateTierCmd, ReplaceTierCmd, DeleteTierCmd,
	}
	for _, c := range cmds {
		t.Run(c.Use, func(t *testing.T) {
			// Example must be present and reference the CLI binary
			if c.Example == "" {
				t.Errorf("%s Example is empty, want a documented example", c.Use)
			}
			if !strings.Contains(c.Example, "tptctl") {
				t.Errorf("%s Example = %q, want it to reference tptctl", c.Use, c.Example)
			}
		})
	}
}
