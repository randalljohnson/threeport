package cmd

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestCreateCmdMetadata asserts CreateCmd's Use, Short, and Long strings match documented values.
func TestCreateCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if CreateCmd.Use != "create" {
		t.Errorf("CreateCmd.Use = %q, want %q", CreateCmd.Use, "create")
	}
	// verify Short description is set for cobra help output
	if CreateCmd.Short != "Create Threeport objects" {
		t.Errorf("CreateCmd.Short = %q, want %q", CreateCmd.Short, "Create Threeport objects")
	}
	// verify Long description exists and mentions subcommand guidance
	if CreateCmd.Long == "" {
		t.Errorf("CreateCmd.Long is empty, want non-empty description")
	}
	if !strings.Contains(CreateCmd.Long, "subcommands") {
		t.Errorf("CreateCmd.Long = %q, want to mention subcommands", CreateCmd.Long)
	}
	// verify Run hook is wired so root cobra dispatch is honored
	if CreateCmd.Run == nil {
		t.Errorf("CreateCmd.Run = nil, want a function")
	}
}

// TestCreateCmdRegisteredOnRoot asserts CreateCmd is a subcommand of rootCmd via init().
func TestCreateCmdRegisteredOnRoot(t *testing.T) {
	// verify subcommand registration by init() so the top-level `tptctl create` resolves
	if !hasSubcommand(rootCmd, CreateCmd) {
		t.Errorf("CreateCmd not registered under rootCmd")
	}
}

// TestCreateCmdRunExitsNonZero exercises CreateCmd.Run's both branches via a subprocess
// so os.Exit does not terminate the test binary.
func TestCreateCmdRunExitsNonZero(t *testing.T) {
	// short-circuit the subprocess path when the env var is set: invoke Run directly
	if os.Getenv("TPTCTL_TEST_CREATE_RUN") != "" {
		args := []string{}
		if extra := os.Getenv("TPTCTL_TEST_CREATE_ARGS"); extra != "" {
			args = strings.Split(extra, ",")
		}
		CreateCmd.Run(CreateCmd, args)
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
			cmd := exec.Command(os.Args[0], "-test.run=TestCreateCmdRunExitsNonZero", "-test.v")
			cmd.Env = append(os.Environ(),
				"TPTCTL_TEST_CREATE_RUN=1",
				"TPTCTL_TEST_CREATE_ARGS="+tc.envArgs,
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
