package cmd

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestUpgradeCmdMetadata asserts UpgradeCmd's Use, Short, and Long strings match documented values.
func TestUpgradeCmdMetadata(t *testing.T) {
	// verify Use string matches the CLI invocation token
	if UpgradeCmd.Use != "upgrade" {
		t.Errorf("UpgradeCmd.Use = %q, want %q", UpgradeCmd.Use, "upgrade")
	}
	// verify Short description is set for cobra help output
	if UpgradeCmd.Short == "" {
		t.Errorf("UpgradeCmd.Short is empty, want non-empty description")
	}
	// verify Long description exists and mentions subcommand guidance
	if UpgradeCmd.Long == "" {
		t.Errorf("UpgradeCmd.Long is empty, want non-empty description")
	}
	if !strings.Contains(UpgradeCmd.Long, "subcommands") {
		t.Errorf("UpgradeCmd.Long = %q, want to mention subcommands", UpgradeCmd.Long)
	}
	// verify Run hook is wired so root cobra dispatch is honored
	if UpgradeCmd.Run == nil {
		t.Errorf("UpgradeCmd.Run = nil, want a function")
	}
}

// TestUpgradeCmdRegisteredOnRoot asserts UpgradeCmd is a subcommand of rootCmd via init().
func TestUpgradeCmdRegisteredOnRoot(t *testing.T) {
	// verify subcommand registration so the top-level `tptctl upgrade` resolves
	if !hasSubcommand(rootCmd, UpgradeCmd) {
		t.Errorf("UpgradeCmd not registered under rootCmd")
	}
}

// TestUpgradeCmdRunExitsNonZero exercises UpgradeCmd.Run's both branches via a subprocess
// so os.Exit does not terminate the test binary.
func TestUpgradeCmdRunExitsNonZero(t *testing.T) {
	// short-circuit into the direct-invocation path when the child env var is set
	if os.Getenv("TPTCTL_TEST_UPGRADE_RUN") != "" {
		args := []string{}
		if extra := os.Getenv("TPTCTL_TEST_UPGRADE_ARGS"); extra != "" {
			args = strings.Split(extra, ",")
		}
		UpgradeCmd.Run(UpgradeCmd, args)
		return
	}

	cases := []struct {
		name       string
		envArgs    string
		wantSubstr string
	}{
		{
			// no-arg branch prints the missing-subcommand hint and exits 1
			name:       "no args prints missing subcommand hint",
			envArgs:    "",
			wantSubstr: "missing subcommand",
		},
		{
			// arg branch prints the unknown-subcommand hint and exits 1
			name:       "unknown arg prints unknown subcommand hint",
			envArgs:    "bogus",
			wantSubstr: "unkown subcomand",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// re-exec the current test binary with a filter targeting only this test
			cmd := exec.Command(os.Args[0], "-test.run=TestUpgradeCmdRunExitsNonZero", "-test.v")
			cmd.Env = append(os.Environ(),
				"TPTCTL_TEST_UPGRADE_RUN=1",
				"TPTCTL_TEST_UPGRADE_ARGS="+tc.envArgs,
			)
			out, err := cmd.CombinedOutput()

			// assert the subprocess exited non-zero because Run calls os.Exit(1)
			exitErr, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("expected subprocess exit error, got %v (out=%s)", err, out)
			}
			if exitErr.ExitCode() != 1 {
				t.Errorf("subprocess exit code = %d, want 1 (out=%s)", exitErr.ExitCode(), out)
			}
			// assert the expected error hint reached the subprocess output
			if !strings.Contains(string(out), tc.wantSubstr) {
				t.Errorf("subprocess output missing %q; got:\n%s", tc.wantSubstr, out)
			}
		})
	}
}
