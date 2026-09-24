package cmd

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestDescribeCmdMetadata asserts DescribeCmd's Use, Short, and Long strings match documented values.
func TestDescribeCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if DescribeCmd.Use != "describe" {
		t.Errorf("DescribeCmd.Use = %q, want %q", DescribeCmd.Use, "describe")
	}
	// verify Short description matches the documented help string
	if DescribeCmd.Short != "Describe a Threeport object" {
		t.Errorf("DescribeCmd.Short = %q, want %q", DescribeCmd.Short, "Describe a Threeport object")
	}
	// verify Long description is present and mentions subcommand guidance
	if DescribeCmd.Long == "" {
		t.Errorf("DescribeCmd.Long is empty, want non-empty description")
	}
	if !strings.Contains(DescribeCmd.Long, "subcommands") {
		t.Errorf("DescribeCmd.Long = %q, want to mention subcommands", DescribeCmd.Long)
	}
	// verify Run hook is wired so root cobra dispatch reaches it
	if DescribeCmd.Run == nil {
		t.Errorf("DescribeCmd.Run = nil, want a function")
	}
}

// TestDescribeCmdRegisteredOnRoot asserts DescribeCmd is a subcommand of rootCmd via init().
func TestDescribeCmdRegisteredOnRoot(t *testing.T) {
	// verify subcommand registration by init() so the top-level `tptctl describe` resolves
	if !hasSubcommand(rootCmd, DescribeCmd) {
		t.Errorf("DescribeCmd not registered under rootCmd")
	}
}

// TestDescribeCmdRunExitsNonZero exercises DescribeCmd.Run's both branches via a subprocess
// so os.Exit does not terminate the test binary.
func TestDescribeCmdRunExitsNonZero(t *testing.T) {
	// short-circuit the subprocess path when the env var is set: invoke Run directly
	if os.Getenv("TPTCTL_TEST_DESCRIBE_RUN") != "" {
		args := []string{}
		if extra := os.Getenv("TPTCTL_TEST_DESCRIBE_ARGS"); extra != "" {
			args = strings.Split(extra, ",")
		}
		DescribeCmd.Run(DescribeCmd, args)
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
			cmd := exec.Command(os.Args[0], "-test.run=TestDescribeCmdRunExitsNonZero", "-test.v")
			cmd.Env = append(os.Environ(),
				"TPTCTL_TEST_DESCRIBE_RUN=1",
				"TPTCTL_TEST_DESCRIBE_ARGS="+tc.envArgs,
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
