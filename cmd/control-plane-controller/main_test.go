package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// showHelpAndExit() writes usage to stdout and terminates the process with
// the supplied exit code, so exercise it in a re-invoked subprocess and
// observe the child's stdout and exit status.

// TestShowHelpAndExitZero asserts a zero exit-code call prints the usage
// banner and terminates the process cleanly.
func TestShowHelpAndExitZero(t *testing.T) {
	// re-entry: the child process branch calls showHelpAndExit and never returns.
	if os.Getenv("TEST_SHOW_HELP_EXIT_CODE") == "0" {
		showHelpAndExit(0)
		return
	}

	// spawn the subprocess, gated by the env var above, and capture output.
	cmd := exec.Command(os.Args[0], "-test.run=^TestShowHelpAndExitZero$")
	cmd.Env = append(os.Environ(), "TEST_SHOW_HELP_EXIT_CODE=0")
	out, err := cmd.CombinedOutput()

	// assert clean exit: os.Exit(0) surfaces as a nil error from Run/CombinedOutput.
	if err != nil {
		t.Fatalf("expected clean exit from showHelpAndExit(0), got error: %v\noutput: %s", err, string(out))
	}

	// assert the usage banner reached stdout so operators actually see help text.
	if !strings.Contains(string(out), "Usage: threeport-control-plane-controller") {
		t.Fatalf("expected usage banner in output, got: %s", string(out))
	}

	// assert the options header rendered so PrintDefaults ran.
	if !strings.Contains(string(out), "options:") {
		t.Fatalf("expected options header in output, got: %s", string(out))
	}
}

// TestShowHelpAndExitNonZero asserts a non-zero exit-code call still prints
// the usage banner and propagates the requested exit code to the caller.
func TestShowHelpAndExitNonZero(t *testing.T) {
	// re-entry branch: caller's exit code must round-trip through os.Exit.
	if os.Getenv("TEST_SHOW_HELP_EXIT_CODE") == "2" {
		showHelpAndExit(2)
		return
	}

	// spawn the subprocess and capture output plus its exit status.
	cmd := exec.Command(os.Args[0], "-test.run=^TestShowHelpAndExitNonZero$")
	cmd.Env = append(os.Environ(), "TEST_SHOW_HELP_EXIT_CODE=2")
	out, err := cmd.CombinedOutput()

	// assert the child exited with an *exec.ExitError carrying code 2.
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError from showHelpAndExit(2), got %T: %v\noutput: %s", err, err, string(out))
	}
	if got := exitErr.ExitCode(); got != 2 {
		t.Fatalf("expected exit code 2, got %d\noutput: %s", got, string(out))
	}

	// assert the usage banner still printed on the non-zero path.
	if !strings.Contains(string(out), "Usage: threeport-control-plane-controller") {
		t.Fatalf("expected usage banner in output, got: %s", string(out))
	}
}
