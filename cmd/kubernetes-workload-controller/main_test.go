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
const helperEnvVar = "KUBERNETES_WORKLOAD_CTRL_HELP_EXIT_CODE"

// mainHelpEnvVar signals to the test binary that it should invoke
// main with a --help arg, exercising the flag-parse and help
// branch inside main before any blocking I/O.
const mainHelpEnvVar = "KUBERNETES_WORKLOAD_CTRL_MAIN_HELP"

// TestMain intercepts the process before the testing framework starts
// so a re-exec of this binary under a helper env var exercises the
// target function directly. Both helpers terminate the process via
// os.Exit, which cannot be observed from an in-process call.
func TestMain(m *testing.M) {
	// helper: call showHelpAndExit with the caller-supplied exit code.
	if raw, ok := os.LookupEnv(helperEnvVar); ok {
		code, err := strconv.Atoi(raw)
		if err != nil {
			os.Exit(2)
		}
		showHelpAndExit(code)
		// unreachable; showHelpAndExit calls os.Exit.
		return
	}
	// helper: rewrite os.Args to a --help invocation and call main so
	// the flag-registration and help-branch statements at the top of
	// main are covered. main os.Exits from showHelpAndExit before any
	// networked or blocking initialization runs.
	if _, ok := os.LookupEnv(mainHelpEnvVar); ok {
		os.Args = []string{"threeport-kubernetes-workload-controller", "-help"}
		main()
		// unreachable; main os.Exits via showHelpAndExit.
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
	wantPrefix := "Usage: threeport-kubernetes-workload-controller [options]"
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

// runMainHelper re-execs the test binary in main-help mode so the test
// can observe main's flag-parse and help-branch behavior end-to-end.
func runMainHelper(t *testing.T) (stdout string, exitErr *exec.ExitError, err error) {
	t.Helper()
	// invoke self with a no-match test filter so only TestMain's helper branch runs.
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), mainHelpEnvVar+"=1")
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

// TestMainHelpFlagExitsZero verifies that main invoked with -help parses
// flags, hits the help branch, and exits zero via showHelpAndExit.
func TestMainHelpFlagExitsZero(t *testing.T) {
	// re-exec main with os.Args set to -help; the help branch runs before
	// any NATS or API-server initialization, so the process must exit cleanly.
	_, exitErr, err := runMainHelper(t)
	if err != nil {
		t.Fatalf("failed to run main helper: %v", err)
	}
	// -help routes through showHelpAndExit(0), which surfaces as a nil ExitError.
	if exitErr != nil {
		t.Fatalf("expected exit code 0, got %d", exitErr.ExitCode())
	}
}

// TestMainHelpFlagPrintsUsageBanner verifies that main's help path emits
// the same usage banner as a direct showHelpAndExit call, confirming the
// flag package is wired up correctly.
func TestMainHelpFlagPrintsUsageBanner(t *testing.T) {
	// capture stdout from the -help invocation of main and check the banner.
	stdout, _, err := runMainHelper(t)
	if err != nil {
		t.Fatalf("failed to run main helper: %v", err)
	}
	// verify the leading banner names the binary, confirming showHelpAndExit ran from main.
	wantPrefix := "Usage: threeport-kubernetes-workload-controller [options]"
	if !strings.HasPrefix(stdout, wantPrefix) {
		t.Fatalf("stdout prefix = %q, want prefix %q", stdout, wantPrefix)
	}
	// verify the options header is present, confirming flag.PrintDefaults executed.
	if !strings.Contains(stdout, "options:") {
		t.Fatalf("stdout missing options header: %q", stdout)
	}
}
