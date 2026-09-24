package cmd

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestGetCmdMetadata asserts GetCmd's Use, Short, and Long strings match documented values.
func TestGetCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if GetCmd.Use != "get" {
		t.Errorf("GetCmd.Use = %q, want %q", GetCmd.Use, "get")
	}
	// verify Short description is set for cobra help output
	if GetCmd.Short != "Get Threeport objects" {
		t.Errorf("GetCmd.Short = %q, want %q", GetCmd.Short, "Get Threeport objects")
	}
	// verify Long description exists and mentions subcommand guidance
	if GetCmd.Long == "" {
		t.Errorf("GetCmd.Long is empty, want non-empty description")
	}
	if !strings.Contains(GetCmd.Long, "subcommands") {
		t.Errorf("GetCmd.Long = %q, want to mention subcommands", GetCmd.Long)
	}
	// verify Run hook is wired so root cobra dispatch is honored
	if GetCmd.Run == nil {
		t.Errorf("GetCmd.Run = nil, want a function")
	}
}

// TestGetCmdRegisteredOnRoot asserts GetCmd is a subcommand of rootCmd via init().
func TestGetCmdRegisteredOnRoot(t *testing.T) {
	// verify subcommand registration by init() so the top-level `tptctl get` resolves
	if !hasSubcommand(rootCmd, GetCmd) {
		t.Errorf("GetCmd not registered under rootCmd")
	}
}

// TestGetCmdRunExitsNonZero exercises GetCmd.Run's both branches via a subprocess
// so os.Exit does not terminate the test binary.
func TestGetCmdRunExitsNonZero(t *testing.T) {
	// short-circuit the subprocess path when the env var is set: invoke Run directly
	if os.Getenv("TPTCTL_TEST_GET_RUN") != "" {
		args := []string{}
		if extra := os.Getenv("TPTCTL_TEST_GET_ARGS"); extra != "" {
			args = strings.Split(extra, ",")
		}
		GetCmd.Run(GetCmd, args)
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
			cmd := exec.Command(os.Args[0], "-test.run=TestGetCmdRunExitsNonZero", "-test.v")
			cmd.Env = append(os.Environ(),
				"TPTCTL_TEST_GET_RUN=1",
				"TPTCTL_TEST_GET_ARGS="+tc.envArgs,
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
