package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// helperShowHelpEnvVar signals to the test binary to re-invoke as a helper
// that calls showHelpAndExit directly and lets os.Exit end the process.
const helperShowHelpEnvVar = "TEST_SHOW_HELP_EXIT_CODE"

// helperMainHelpEnvVar signals to the test binary to re-invoke as a helper
// that calls main() with a "-help" arg, exercising main's flag setup and
// the help-branch path through showHelpAndExit.
const helperMainHelpEnvVar = "TEST_MAIN_HELP"

// TestMain intercepts the process before the testing framework runs so a
// re-exec of this binary under one of the helper env vars invokes the target
// code path directly. showHelpAndExit and main both terminate via os.Exit,
// which cannot be observed from an in-process call.
func TestMain(m *testing.M) {
	// showHelp helper: call showHelpAndExit with the caller-supplied code so
	// the parent test can observe the exit status and stdout banner.
	if raw, ok := os.LookupEnv(helperShowHelpEnvVar); ok {
		code, err := strconv.Atoi(raw)
		if err != nil {
			os.Exit(2)
		}
		showHelpAndExit(code)
		// unreachable; showHelpAndExit calls os.Exit.
		return
	}
	// mainHelp helper: rewrite os.Args so main's flag.Parse sees a "-help"
	// invocation with no test framework flags, then call main. Exits via
	// showHelpAndExit(0) before touching NATS or the API server.
	if _, ok := os.LookupEnv(helperMainHelpEnvVar); ok {
		os.Args = []string{"threeport-control-plane-controller", "-help"}
		main()
		// unreachable; main invokes showHelpAndExit(0) which calls os.Exit.
		return
	}
	// normal path: run the package tests.
	os.Exit(m.Run())
}

// runShowHelpHelper re-execs the test binary in showHelp-helper mode with the
// given exit code so the caller can observe showHelpAndExit's stdout and exit
// status.
func runShowHelpHelper(t *testing.T, code int) (stdout string, exitErr *exec.ExitError, err error) {
	t.Helper()
	// invoke self with a no-match test filter so only the TestMain helper branch runs.
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), helperShowHelpEnvVar+"="+strconv.Itoa(code))
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

// runMainHelpHelper re-execs the test binary in mainHelp-helper mode so the
// caller can observe main's help-branch behavior end to end. Captures both
// stdout and stderr because flag.PrintDefaults writes flag entries to stderr.
func runMainHelpHelper(t *testing.T) (combined string, exitErr *exec.ExitError, err error) {
	t.Helper()
	// invoke self with a no-match test filter so only the TestMain helper branch runs.
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), helperMainHelpEnvVar+"=1")
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		ee, ok := runErr.(*exec.ExitError)
		if !ok {
			return string(out), nil, runErr
		}
		return string(out), ee, nil
	}
	return string(out), nil, nil
}

// TestShowHelpAndExitExitCodeZero asserts a zero-code call terminates the
// process with a zero exit status, matching the --help flow.
func TestShowHelpAndExitExitCodeZero(t *testing.T) {
	// re-exec the binary so os.Exit(0) is observable as a normal termination.
	_, exitErr, err := runShowHelpHelper(t, 0)
	if err != nil {
		t.Fatalf("failed to run helper: %v", err)
	}
	// exit 0 surfaces as a nil *ExitError from cmd.Output.
	if exitErr != nil {
		t.Fatalf("expected exit code 0, got %d", exitErr.ExitCode())
	}
}

// TestShowHelpAndExitExitCodeNonZero asserts showHelpAndExit forwards its
// argument to os.Exit so callers can signal an error.
func TestShowHelpAndExitExitCodeNonZero(t *testing.T) {
	// re-exec asking for a specific non-zero code and confirm it round-trips.
	const want = 3
	_, exitErr, err := runShowHelpHelper(t, want)
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

// TestShowHelpAndExitPrintsUsage asserts the help output leads with the
// expected banner so operators can grep for the command name.
func TestShowHelpAndExitPrintsUsage(t *testing.T) {
	// helper writes to stdout before exiting; capture and inspect it.
	stdout, _, err := runShowHelpHelper(t, 0)
	if err != nil {
		t.Fatalf("failed to run helper: %v", err)
	}
	// verify the leading banner names the binary.
	wantPrefix := "Usage: threeport-control-plane-controller"
	if !strings.HasPrefix(stdout, wantPrefix) {
		t.Fatalf("stdout prefix = %q, want prefix %q", stdout, wantPrefix)
	}
	// verify the options header is emitted so flag.PrintDefaults is invoked.
	if !strings.Contains(stdout, "options:") {
		t.Fatalf("stdout missing options header: %q", stdout)
	}
}

// TestShowHelpAndExitEndsWithNewline asserts the banner ends with a newline
// so terminal output is not left mid-line before flag.PrintDefaults runs.
func TestShowHelpAndExitEndsWithNewline(t *testing.T) {
	// the banner is emitted by fmt.Printf plus fmt.Println, both of which
	// yield trailing newlines; the combined output must terminate with one.
	stdout, _, err := runShowHelpHelper(t, 0)
	if err != nil {
		t.Fatalf("failed to run helper: %v", err)
	}
	if !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("stdout missing trailing newline: %q", stdout)
	}
}

// TestMainHelpFlagExitsCleanly asserts that invoking main with "-help"
// registers every flag, parses successfully, hits the help branch, and
// terminates with exit code 0. This covers main's flag definitions and
// the early help-branch path without touching NATS or the API server.
func TestMainHelpFlagExitsCleanly(t *testing.T) {
	// re-exec so main runs to os.Exit(0) via showHelpAndExit.
	_, exitErr, err := runMainHelpHelper(t)
	if err != nil {
		t.Fatalf("failed to run helper: %v", err)
	}
	// exit 0 surfaces as a nil *ExitError from cmd.Output.
	if exitErr != nil {
		t.Fatalf("expected exit code 0 from main -help, got %d", exitErr.ExitCode())
	}
}

// TestMainHelpFlagPrintsAllFlags asserts main's help output enumerates the
// flags declared at the top of main so PrintDefaults sees each registration.
// If a future edit drops a flag registration, this test flags the loss.
func TestMainHelpFlagPrintsAllFlags(t *testing.T) {
	// capture the child's combined stdout+stderr for inspection.
	stdout, _, err := runMainHelpHelper(t)
	if err != nil {
		t.Fatalf("failed to run helper: %v", err)
	}
	// each flag name main declares should surface in the usage banner.
	wantFlags := []string{
		"control-plane-definition-concurrent-reconciles",
		"control-plane-instance-concurrent-reconciles",
		"api-server",
		"msg-broker-host",
		"msg-broker-port",
		"msg-broker-user",
		"msg-broker-password",
		"shutdown-port",
		"verbose",
		"help",
		"auth-enabled",
	}
	for _, name := range wantFlags {
		if !strings.Contains(stdout, name) {
			t.Errorf("main -help output missing flag %q; got:\n%s", name, stdout)
		}
	}
}
