package cmd

import (
	"testing"
)

// TestRootCmdMetadata asserts that RootCmd exposes the expected cobra metadata
// (Use, Short, Long populated) so `threeport-sdk --help` renders a stable
// invocation banner and the command routes under the documented name.
func TestRootCmdMetadata(t *testing.T) {
	// verify the base command name so `threeport-sdk ...` continues to resolve
	if RootCmd.Use != "threeport-sdk" {
		t.Errorf("expected Use=threeport-sdk, got %q", RootCmd.Use)
	}

	// verify short and long descriptions are populated for the help index
	if RootCmd.Short == "" {
		t.Error("expected non-empty Short description")
	}
	if RootCmd.Long == "" {
		t.Error("expected non-empty Long description")
	}
}

// TestRootCmdToggleFlag asserts that init() registered the -t/--toggle flag on
// RootCmd so the persistent flag surface stays stable for cobra help.
func TestRootCmdToggleFlag(t *testing.T) {
	// resolve the flag by long name to inspect registration and shorthand
	toggle := RootCmd.Flags().Lookup("toggle")
	if toggle == nil {
		t.Fatal("expected --toggle flag to be registered on RootCmd")
	}

	// verify the -t shorthand so short-form invocations keep working
	if toggle.Shorthand != "t" {
		t.Errorf("expected --toggle shorthand to be t, got %q", toggle.Shorthand)
	}

	// verify the default is off so the flag is opt-in
	if toggle.DefValue != "false" {
		t.Errorf("expected --toggle default to be false, got %q", toggle.DefValue)
	}
}

// TestSubcommandsRegistered asserts that init() attached the create, gen, and
// version subcommands to RootCmd so `threeport-sdk create|gen|version` all
// resolve at the CLI layer.
func TestSubcommandsRegistered(t *testing.T) {
	// map the wanted commands by Use string so the check is order-insensitive
	want := map[string]bool{
		"create":  false,
		"gen":     false,
		"version": false,
	}

	// walk RootCmd's children and flip the flag for each match
	for _, c := range RootCmd.Commands() {
		if _, ok := want[c.Use]; ok {
			want[c.Use] = true
		}
	}

	// any subcommand still false means it was not attached in init()
	for use, seen := range want {
		if !seen {
			t.Errorf("expected subcommand %q to be registered on RootCmd", use)
		}
	}
}

// TestCreateCmdMetadata asserts createCmd exposes the expected cobra metadata
// so `threeport-sdk create --help` output stays stable.
func TestCreateCmdMetadata(t *testing.T) {
	// verify the subcommand name used for CLI routing
	if createCmd.Use != "create" {
		t.Errorf("expected Use=create, got %q", createCmd.Use)
	}

	// verify short and long descriptions are populated for the help index
	if createCmd.Short == "" {
		t.Error("expected non-empty Short description")
	}
	if createCmd.Long == "" {
		t.Error("expected non-empty Long description")
	}

	// verify the RunE hook is wired so cobra can dispatch on invocation
	if createCmd.RunE == nil {
		t.Error("expected RunE to be set on createCmd")
	}
}

// TestCreateCmdConfigFlag asserts init() registered the --config/-c flag on
// createCmd with the documented default and shorthand so the RunE body sees
// the value cobra parses.
func TestCreateCmdConfigFlag(t *testing.T) {
	// resolve the flag by long name to inspect its registration
	cfg := createCmd.Flags().Lookup("config")
	if cfg == nil {
		t.Fatal("expected --config flag to be registered on createCmd")
	}

	// verify the -c shorthand so short-form invocations keep working
	if cfg.Shorthand != "c" {
		t.Errorf("expected --config shorthand to be c, got %q", cfg.Shorthand)
	}

	// verify the default is empty so the required-flag check triggers when unset
	if cfg.DefValue != "" {
		t.Errorf("expected --config default to be empty, got %q", cfg.DefValue)
	}
}

// TestGenCmdMetadata asserts genCmd exposes the expected cobra metadata so
// `threeport-sdk gen --help` output stays stable.
func TestGenCmdMetadata(t *testing.T) {
	// verify the subcommand name used for CLI routing
	if genCmd.Use != "gen" {
		t.Errorf("expected Use=gen, got %q", genCmd.Use)
	}

	// verify short and long descriptions are populated for the help index
	if genCmd.Short == "" {
		t.Error("expected non-empty Short description")
	}
	if genCmd.Long == "" {
		t.Error("expected non-empty Long description")
	}

	// verify the RunE hook is wired so cobra can dispatch on invocation
	if genCmd.RunE == nil {
		t.Error("expected RunE to be set on genCmd")
	}
}

// TestGenCmdConfigFlag asserts init() registered the --config/-c flag on
// genCmd with the documented default and shorthand.
func TestGenCmdConfigFlag(t *testing.T) {
	// resolve the flag by long name to inspect its registration
	cfg := genCmd.Flags().Lookup("config")
	if cfg == nil {
		t.Fatal("expected --config flag to be registered on genCmd")
	}

	// verify the -c shorthand so short-form invocations keep working
	if cfg.Shorthand != "c" {
		t.Errorf("expected --config shorthand to be c, got %q", cfg.Shorthand)
	}

	// verify the default is empty so the required-flag check triggers when unset
	if cfg.DefValue != "" {
		t.Errorf("expected --config default to be empty, got %q", cfg.DefValue)
	}
}

// TestVersionCmdMetadata asserts versionCmd exposes the expected cobra
// metadata and has a Run hook so `threeport-sdk version` prints and exits.
func TestVersionCmdMetadata(t *testing.T) {
	// verify the subcommand name used for CLI routing
	if versionCmd.Use != "version" {
		t.Errorf("expected Use=version, got %q", versionCmd.Use)
	}

	// verify short and long descriptions are populated for the help index
	if versionCmd.Short == "" {
		t.Error("expected non-empty Short description")
	}
	if versionCmd.Long == "" {
		t.Error("expected non-empty Long description")
	}

	// verify the Run hook is wired so cobra can dispatch on invocation
	if versionCmd.Run == nil {
		t.Error("expected Run to be set on versionCmd")
	}
}
