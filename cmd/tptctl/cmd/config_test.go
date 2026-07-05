package cmd

import (
	"strings"
	"testing"
)

// TestConfigCmdMetadata asserts that ConfigCmd exposes the expected cobra
// metadata (Use, Short, Long) so help output and command routing stay stable.
func TestConfigCmdMetadata(t *testing.T) {
	// verify command name used for CLI routing
	if ConfigCmd.Use != "config" {
		t.Errorf("expected Use=config, got %q", ConfigCmd.Use)
	}

	// verify short help is populated for the help index
	if ConfigCmd.Short == "" {
		t.Error("expected non-empty Short description")
	}
	if !strings.Contains(ConfigCmd.Short, "Threeport") {
		t.Errorf("expected Short to mention Threeport, got %q", ConfigCmd.Short)
	}

	// verify long help mentions the config path so users can find it
	if ConfigCmd.Long == "" {
		t.Error("expected non-empty Long description")
	}
	if !strings.Contains(ConfigCmd.Long, "~/.threeport/config.yaml") {
		t.Errorf("expected Long to mention default config path, got %q", ConfigCmd.Long)
	}
}

// TestConfigCmdRegisteredWithRoot asserts that init() attached ConfigCmd to
// rootCmd so `tptctl config` resolves at the CLI layer.
func TestConfigCmdRegisteredWithRoot(t *testing.T) {
	// look up the child by name from rootCmd's command tree
	var found bool
	for _, c := range rootCmd.Commands() {
		if c == ConfigCmd {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected ConfigCmd to be registered as a subcommand of rootCmd")
	}
}

// TestConfigCmdRunIsNil asserts that ConfigCmd itself has no Run or RunE, so
// invoking `tptctl config` without a subcommand prints help instead of acting.
func TestConfigCmdRunIsNil(t *testing.T) {
	// no direct action: config is a parent-only command
	if ConfigCmd.Run != nil {
		t.Error("expected ConfigCmd.Run to be nil")
	}
	if ConfigCmd.RunE != nil {
		t.Error("expected ConfigCmd.RunE to be nil")
	}
}
