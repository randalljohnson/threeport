package cmd

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestDeleteCmdMetadata asserts DeleteCmd's Use, Short, and Long strings match documented values.
func TestDeleteCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if DeleteCmd.Use != "delete" {
		t.Errorf("DeleteCmd.Use = %q, want %q", DeleteCmd.Use, "delete")
	}
	// verify Short description matches the documented help string
	if DeleteCmd.Short != "Delete Threeport objects" {
		t.Errorf("DeleteCmd.Short = %q, want %q", DeleteCmd.Short, "Delete Threeport objects")
	}
	// verify Long description exists and mentions subcommand guidance
	if DeleteCmd.Long == "" {
		t.Errorf("DeleteCmd.Long is empty, want non-empty description")
	}
	if !strings.Contains(DeleteCmd.Long, "subcommands") {
		t.Errorf("DeleteCmd.Long = %q, want to mention subcommands", DeleteCmd.Long)
	}
	// verify Run hook is wired so root cobra dispatch reaches it
	if DeleteCmd.Run == nil {
		t.Errorf("DeleteCmd.Run = nil, want a function")
	}
}

// TestDeleteCmdRegisteredOnRoot asserts DeleteCmd is a subcommand of rootCmd via init().
func TestDeleteCmdRegisteredOnRoot(t *testing.T) {
	// verify subcommand registration by init() so the top-level `tptctl delete` resolves
	if !hasSubcommand(rootCmd, DeleteCmd) {
		t.Errorf("DeleteCmd not registered under rootCmd")
	}
}

// TestDeleteCmdRunExitsNonZero exercises DeleteCmd.Run's both branches via a subprocess
// so os.Exit does not terminate the test binary.
func TestDeleteCmdRunExitsNonZero(t *testing.T) {
	// short-circuit the subprocess path when the env var is set: invoke Run directly
	if os.Getenv("TPTCTL_TEST_DELETE_RUN") != "" {
		args := []string{}
		if extra := os.Getenv("TPTCTL_TEST_DELETE_ARGS"); extra != "" {
			args = strings.Split(extra, ",")
		}
		DeleteCmd.Run(DeleteCmd, args)
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
			// re-exec the current test binary with a filter that targets only this test
			cmd := exec.Command(os.Args[0], "-test.run=TestDeleteCmdRunExitsNonZero", "-test.v")
			cmd.Env = append(os.Environ(),
				"TPTCTL_TEST_DELETE_RUN=1",
				"TPTCTL_TEST_DELETE_ARGS="+tc.envArgs,
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
