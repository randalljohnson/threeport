package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// helperEnvVar signals to the test binary that it should invoke
// showHelpAndExit rather than run the parent test logic.
const helperEnvVar = "TERRAFORM_CTRL_HELP_EXIT_CODE"

// helperMainVar signals to the test binary that it should invoke main()
// with the -help flag so the top of main runs and exits fast via the
// help branch. That path exercises flag registration, flag parsing, and
// the encryption key lookup before terminating.
const helperMainVar = "TERRAFORM_CTRL_MAIN_HELP"

// TestMain intercepts the process before the testing framework starts
// so a re-exec of this binary under helperEnvVar exercises
// showHelpAndExit directly. showHelpAndExit terminates the process
// via os.Exit, which cannot be observed from an in-process call.
func TestMain(m *testing.M) {
	// if the helper env var is set, act as the helper: call the target
	// function with the caller-supplied exit code and let os.Exit end the process.
	if raw, ok := os.LookupEnv(helperEnvVar); ok {
		code, err := strconv.Atoi(raw)
		if err != nil {
			os.Exit(2)
		}
		showHelpAndExit(code)
		// unreachable; showHelpAndExit calls os.Exit.
		return
	}
	// if the main-help helper env var is set, invoke main() with -help so
	// it exits at the help branch before touching NATS or the API server.
	if _, ok := os.LookupEnv(helperMainVar); ok {
		os.Args = []string{"terraform-controller", "-help=true"}
		main()
		// unreachable; main calls showHelpAndExit which calls os.Exit.
		return
	}
	// normal path: run the package tests.
	os.Exit(m.Run())
}

// runHelper re-execs the test binary in helper mode with the given exit code
// so the test can observe showHelpAndExit's stdout and process exit status.
func runHelper(t *testing.T, code int) (stdout string, exitErr *exec.ExitError, err error) {
	t.Helper()
	// invoke self with a no-match test filter so only TestMain's helper branch runs.
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), helperEnvVar+"="+strconv.Itoa(code))
	out, runErr := cmd.Output()
	if runErr != nil {
		ee, ok := runErr.(*exec.ExitError)
		if !ok {
			return string(out), nil, runErr
		}
		return string(out), ee, nil
	}
	return string(out), nil, nil
}

// TestShowHelpAndExitExitCodeZero verifies showHelpAndExit(0) terminates
// the process with a zero exit status, matching the --help flow.
func TestShowHelpAndExitExitCodeZero(t *testing.T) {
	// re-exec the binary so os.Exit(0) is observable as a normal termination.
	_, exitErr, err := runHelper(t, 0)
	if err != nil {
		t.Fatalf("failed to run helper: %v", err)
	}
	// exit 0 surfaces as a nil *ExitError from cmd.Output.
	if exitErr != nil {
		t.Fatalf("expected exit code 0, got %d", exitErr.ExitCode())
	}
}

// TestShowHelpAndExitExitCodeNonZero verifies showHelpAndExit forwards
// its argument to os.Exit so callers can signal an error.
func TestShowHelpAndExitExitCodeNonZero(t *testing.T) {
	// re-exec asking for a specific non-zero code and confirm it round-trips.
	const want = 3
	_, exitErr, err := runHelper(t, want)
	if err != nil {
		t.Fatalf("failed to run helper: %v", err)
	}
	// non-zero exit surfaces as a non-nil ExitError with matching code.
	if exitErr == nil {
		t.Fatalf("expected non-zero exit, got success")
	}
	if got := exitErr.ExitCode(); got != want {
		t.Fatalf("exit code = %d, want %d", got, want)
	}
}

// TestShowHelpAndExitPrintsUsage verifies the help output leads with the
// expected banner so operators can grep for the command name.
func TestShowHelpAndExitPrintsUsage(t *testing.T) {
	// helper writes to stdout before exiting; capture and inspect it.
	stdout, _, err := runHelper(t, 0)
	if err != nil {
		t.Fatalf("failed to run helper: %v", err)
	}
	// verify the leading banner names the binary.
	wantPrefix := "Usage: threeport-terraform-controller [options]"
	if !strings.HasPrefix(stdout, wantPrefix) {
		t.Fatalf("stdout prefix = %q, want prefix %q", stdout, wantPrefix)
	}
	// verify the options header is emitted so flag.PrintDefaults is invoked.
	if !strings.Contains(stdout, "options:") {
		t.Fatalf("stdout missing options header: %q", stdout)
	}
}

// TestShowHelpAndExitEndsWithNewline verifies the banner ends with a newline
// so terminal output is not left mid-line before flag.PrintDefaults runs.
func TestShowHelpAndExitEndsWithNewline(t *testing.T) {
	// the banner is emitted by fmt.Printf plus fmt.Println, both of which
	// yield trailing newlines; the combined output must terminate with one.
	stdout, _, err := runHelper(t, 0)
	if err != nil {
		t.Fatalf("failed to run helper: %v", err)
	}
	if !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("stdout missing trailing newline: %q", stdout)
	}
}

// TestMainHelpFlagExitsCleanly verifies main() reaches the help branch
// when invoked with -help=true so the flag registration and parsing at
// the top of main are exercised without blocking on NATS or the API.
func TestMainHelpFlagExitsCleanly(t *testing.T) {
	// re-exec the test binary in main-help mode; a no-match test filter
	// prevents any real test from running before TestMain hands off to main().
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), helperMainVar+"=1")
	out, runErr := cmd.Output()
	// main() -> showHelpAndExit(0) -> os.Exit(0) surfaces as a nil ExitError.
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			t.Fatalf("expected exit 0 from main -help, got %d, stderr=%q", ee.ExitCode(), string(ee.Stderr))
		}
		t.Fatalf("failed to run helper: %v", runErr)
	}
	// verify main routed through showHelpAndExit's usage banner.
	if !strings.Contains(string(out), "Usage: threeport-terraform-controller") {
		t.Fatalf("main -help stdout missing usage banner: %q", string(out))
	}
}
