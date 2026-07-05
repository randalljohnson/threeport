package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestGetModuleApisCmdMetadata asserts get module-apis command metadata (Use, alias, silence, PreRun).
func TestGetModuleApisCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if GetModuleApisCmd.Use != "module-apis" {
		t.Errorf("GetModuleApisCmd.Use = %q, want %q", GetModuleApisCmd.Use, "module-apis")
	}
	// verify singular alias registered
	found := false
	for _, a := range GetModuleApisCmd.Aliases {
		if a == "module-api" {
			found = true
		}
	}
	if !found {
		t.Errorf("GetModuleApisCmd.Aliases = %v, want to include %q", GetModuleApisCmd.Aliases, "module-api")
	}
	// verify usage is silenced on error
	if !GetModuleApisCmd.SilenceUsage {
		t.Errorf("GetModuleApisCmd.SilenceUsage = false, want true")
	}
	// verify PreRun hook wired
	if GetModuleApisCmd.PreRun == nil {
		t.Errorf("GetModuleApisCmd.PreRun = nil, want a function")
	}
	// verify short and long descriptions populated
	if GetModuleApisCmd.Short == "" {
		t.Errorf("GetModuleApisCmd.Short is empty")
	}
	if GetModuleApisCmd.Long == "" {
		t.Errorf("GetModuleApisCmd.Long is empty")
	}
}

// TestGetModuleApisCmdFlags asserts get module-apis registered flags.
func TestGetModuleApisCmdFlags(t *testing.T) {
	// verify all documented flags exist
	assertFlags(t, GetModuleApisCmd, []string{"name", "config", "version", "output", "control-plane-name"})
}

// TestGetModuleApisCmdRegistered asserts GetModuleApisCmd is a subcommand of GetCmd.
func TestGetModuleApisCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(GetCmd, GetModuleApisCmd) {
		t.Errorf("GetModuleApisCmd not registered under GetCmd")
	}
}

// TestGetModuleApiRoutesCmdMetadata asserts get module-api-routes command metadata.
func TestGetModuleApiRoutesCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if GetModuleApiRoutesCmd.Use != "module-api-routes" {
		t.Errorf("GetModuleApiRoutesCmd.Use = %q, want %q", GetModuleApiRoutesCmd.Use, "module-api-routes")
	}
	// verify singular alias registered
	found := false
	for _, a := range GetModuleApiRoutesCmd.Aliases {
		if a == "module-api-route" {
			found = true
		}
	}
	if !found {
		t.Errorf("GetModuleApiRoutesCmd.Aliases = %v, want to include %q", GetModuleApiRoutesCmd.Aliases, "module-api-route")
	}
	// verify usage is silenced on error
	if !GetModuleApiRoutesCmd.SilenceUsage {
		t.Errorf("GetModuleApiRoutesCmd.SilenceUsage = false, want true")
	}
	// verify PreRun hook wired
	if GetModuleApiRoutesCmd.PreRun == nil {
		t.Errorf("GetModuleApiRoutesCmd.PreRun = nil, want a function")
	}
}

// TestGetModuleApiRoutesCmdFlags asserts get module-api-routes registered flags.
func TestGetModuleApiRoutesCmdFlags(t *testing.T) {
	// verify all documented flags exist; note this cmd uses "path" instead of "name"
	assertFlags(t, GetModuleApiRoutesCmd, []string{"path", "config", "version", "output", "control-plane-name"})
}

// TestGetModuleApiRoutesCmdRegistered asserts GetModuleApiRoutesCmd is a subcommand of GetCmd.
func TestGetModuleApiRoutesCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(GetCmd, GetModuleApiRoutesCmd) {
		t.Errorf("GetModuleApiRoutesCmd not registered under GetCmd")
	}
}

// TestGetModuleControllersCmdMetadata asserts get module-controllers command metadata.
func TestGetModuleControllersCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if GetModuleControllersCmd.Use != "module-controllers" {
		t.Errorf("GetModuleControllersCmd.Use = %q, want %q", GetModuleControllersCmd.Use, "module-controllers")
	}
	// verify singular alias registered
	found := false
	for _, a := range GetModuleControllersCmd.Aliases {
		if a == "module-controller" {
			found = true
		}
	}
	if !found {
		t.Errorf("GetModuleControllersCmd.Aliases = %v, want to include %q", GetModuleControllersCmd.Aliases, "module-controller")
	}
	// verify usage is silenced on error
	if !GetModuleControllersCmd.SilenceUsage {
		t.Errorf("GetModuleControllersCmd.SilenceUsage = false, want true")
	}
	// verify PreRun hook wired
	if GetModuleControllersCmd.PreRun == nil {
		t.Errorf("GetModuleControllersCmd.PreRun = nil, want a function")
	}
}

// TestGetModuleControllersCmdFlags asserts get module-controllers registered flags.
func TestGetModuleControllersCmdFlags(t *testing.T) {
	// verify all documented flags exist
	assertFlags(t, GetModuleControllersCmd, []string{"name", "config", "version", "output", "control-plane-name"})
}

// TestGetModuleControllersCmdRegistered asserts GetModuleControllersCmd is a subcommand of GetCmd.
func TestGetModuleControllersCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(GetCmd, GetModuleControllersCmd) {
		t.Errorf("GetModuleControllersCmd not registered under GetCmd")
	}
}

// TestGetModuleObjectsCmdMetadata asserts get module-objects command metadata.
func TestGetModuleObjectsCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if GetModuleObjectsCmd.Use != "module-objects" {
		t.Errorf("GetModuleObjectsCmd.Use = %q, want %q", GetModuleObjectsCmd.Use, "module-objects")
	}
	// verify singular alias registered
	found := false
	for _, a := range GetModuleObjectsCmd.Aliases {
		if a == "module-object" {
			found = true
		}
	}
	if !found {
		t.Errorf("GetModuleObjectsCmd.Aliases = %v, want to include %q", GetModuleObjectsCmd.Aliases, "module-object")
	}
	// verify usage is silenced on error
	if !GetModuleObjectsCmd.SilenceUsage {
		t.Errorf("GetModuleObjectsCmd.SilenceUsage = false, want true")
	}
	// verify PreRun hook wired
	if GetModuleObjectsCmd.PreRun == nil {
		t.Errorf("GetModuleObjectsCmd.PreRun = nil, want a function")
	}
}

// TestGetModuleObjectsCmdFlags asserts get module-objects registered flags.
func TestGetModuleObjectsCmdFlags(t *testing.T) {
	// verify all documented flags exist
	assertFlags(t, GetModuleObjectsCmd, []string{"name", "config", "version", "output", "control-plane-name"})
}

// TestGetModuleObjectsCmdRegistered asserts GetModuleObjectsCmd is a subcommand of GetCmd.
func TestGetModuleObjectsCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(GetCmd, GetModuleObjectsCmd) {
		t.Errorf("GetModuleObjectsCmd not registered under GetCmd")
	}
}

// TestModuleFlagDefaults asserts version and output flags default to sensible values across all module get commands.
func TestModuleFlagDefaults(t *testing.T) {
	cases := []struct {
		name string
		cmd  *cobra.Command
		flag string
		want string
	}{
		// each get-command shares the same version/output defaults
		{"GetModuleApis version", GetModuleApisCmd, "version", "v0"},
		{"GetModuleApis output", GetModuleApisCmd, "output", "tabular"},
		{"GetModuleApis name empty default", GetModuleApisCmd, "name", ""},
		{"GetModuleApis config empty default", GetModuleApisCmd, "config", ""},
		{"GetModuleApiRoutes version", GetModuleApiRoutesCmd, "version", "v0"},
		{"GetModuleApiRoutes output", GetModuleApiRoutesCmd, "output", "tabular"},
		{"GetModuleApiRoutes path empty default", GetModuleApiRoutesCmd, "path", ""},
		{"GetModuleControllers version", GetModuleControllersCmd, "version", "v0"},
		{"GetModuleControllers output", GetModuleControllersCmd, "output", "tabular"},
		{"GetModuleObjects version", GetModuleObjectsCmd, "version", "v0"},
		{"GetModuleObjects output", GetModuleObjectsCmd, "output", "tabular"},
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

// TestModuleFlagShorthands asserts shorthand keys wired on module get commands.
func TestModuleFlagShorthands(t *testing.T) {
	cases := []struct {
		name      string
		cmd       *cobra.Command
		flag      string
		shorthand string
	}{
		// short keys stated in the command's Flag registration
		{"GetModuleApis name -n", GetModuleApisCmd, "name", "n"},
		{"GetModuleApis config -c", GetModuleApisCmd, "config", "c"},
		{"GetModuleApis version -v", GetModuleApisCmd, "version", "v"},
		{"GetModuleApis output -o", GetModuleApisCmd, "output", "o"},
		{"GetModuleApis control-plane-name -i", GetModuleApisCmd, "control-plane-name", "i"},
		{"GetModuleApiRoutes path -p", GetModuleApiRoutesCmd, "path", "p"},
		{"GetModuleControllers name -n", GetModuleControllersCmd, "name", "n"},
		{"GetModuleObjects name -n", GetModuleObjectsCmd, "name", "n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// verify shorthand matches
			f := tc.cmd.Flags().Lookup(tc.flag)
			if f == nil {
				t.Fatalf("flag %q missing on %q", tc.flag, tc.cmd.Use)
			}
			if f.Shorthand != tc.shorthand {
				t.Errorf("flag %q shorthand = %q, want %q", tc.flag, f.Shorthand, tc.shorthand)
			}
		})
	}
}

// TestModuleCommandsHaveExamples asserts every module get command carries an Example string.
func TestModuleCommandsHaveExamples(t *testing.T) {
	// non-empty examples give users a copy-pasteable starting point
	cmds := []*cobra.Command{
		GetModuleApisCmd,
		GetModuleApiRoutesCmd,
		GetModuleControllersCmd,
		GetModuleObjectsCmd,
	}
	for _, c := range cmds {
		if c.Example == "" {
			t.Errorf("cmd %q has empty Example", c.Use)
		}
		if c.Run == nil {
			t.Errorf("cmd %q has nil Run", c.Use)
		}
	}
}
