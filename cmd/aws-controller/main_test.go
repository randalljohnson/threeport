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
const helperEnvVar = "AWS_CTRL_HELP_EXIT_CODE"

// mainHelperEnvVar signals to the test binary that it should invoke
// main() with a caller-supplied argv rather than run the parent test
// logic. The env value is split on whitespace and prepended with a
// synthetic program name to form os.Args before main() runs.
const mainHelperEnvVar = "AWS_CTRL_RUN_MAIN_ARGV"

// TestMain intercepts the process before the testing framework starts
// so a re-exec of this binary under one of the helper env vars
// exercises the target directly. showHelpAndExit terminates the
// process via os.Exit, which cannot be observed from an in-process
// call; main() likewise runs flag.Parse against os.Args and eventually
// hits os.Exit, so it needs its own re-exec harness rather than a
// direct call from a test.
func TestMain(m *testing.M) {
	// if the showHelpAndExit helper env var is set, call the target
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
	// if the main helper env var is set, rewrite os.Args so main()'s
	// flag.Parse sees the caller-supplied flags rather than the go
	// test flags, then invoke main directly.
	if argv, ok := os.LookupEnv(mainHelperEnvVar); ok {
		args := []string{"aws-controller"}
		if argv != "" {
			args = append(args, strings.Fields(argv)...)
		}
		os.Args = args
		main()
		// unreachable on the exercised paths; main terminates via
		// showHelpAndExit or os.Exit before returning.
		return
	}
	// normal path: run the package tests.
	os.Exit(m.Run())
}

// runMainHelper re-execs the test binary in the main helper mode with
// the given argv so the test can drive main() through its flag-parsing
// and early-exit branches without depending on network reachability.
func runMainHelper(t *testing.T, argv string) (stdout string, exitErr *exec.ExitError, err error) {
	t.Helper()
	// invoke self with a no-match test filter so only TestMain's helper branch runs.
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), mainHelperEnvVar+"="+argv)
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
	wantPrefix := "Usage: threeport-aws-controller [options]"
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

// TestMainHelpFlagExitsZero verifies that invoking main() with the -help
// flag routes through the help-check branch to showHelpAndExit(0) so
// the process terminates cleanly.
func TestMainHelpFlagExitsZero(t *testing.T) {
	// drive main() with -help; the help branch calls showHelpAndExit(0).
	_, exitErr, err := runMainHelper(t, "-help")
	if err != nil {
		t.Fatalf("failed to run helper: %v", err)
	}
	// exit 0 surfaces as a nil *ExitError from cmd.Output.
	if exitErr != nil {
		t.Fatalf("expected exit code 0, got %d", exitErr.ExitCode())
	}
}

// TestMainHelpFlagPrintsUsage verifies that main() -help exercises the
// flag-parse and help-check statements and hands off to the usage
// banner rather than proceeding to NATS setup.
func TestMainHelpFlagPrintsUsage(t *testing.T) {
	// drive main() with -help and assert the banner is emitted before
	// os.Exit runs.
	stdout, _, err := runMainHelper(t, "-help")
	if err != nil {
		t.Fatalf("failed to run helper: %v", err)
	}
	// the banner names the binary; its presence proves flag.Parse ran
	// and the help branch was taken.
	if !strings.Contains(stdout, "Usage: threeport-aws-controller") {
		t.Fatalf("main -help missing usage banner: %q", stdout)
	}
	// flag.PrintDefaults emits the options header; verify it also ran.
	if !strings.Contains(stdout, "options:") {
		t.Fatalf("main -help missing options header: %q", stdout)
	}
}

// TestMainHelpFlagWithEncryptionKeyStillExitsZero verifies that setting
// the encryption key env var does not disturb the -help fast path;
// main() logs when the key is missing but the value is unused before
// the help branch runs.
func TestMainHelpFlagWithEncryptionKeyStillExitsZero(t *testing.T) {
	// prime the encryption key env var in the child so the missing-key
	// warning branch is skipped and the help path still exits zero.
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(
		os.Environ(),
		mainHelperEnvVar+"=-help",
		"ENCRYPTION_KEY=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	// help path terminates via os.Exit(0); a non-nil ExitError signals regression.
	if _, err := cmd.Output(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("main -help with encryption key exited %d, want 0", ee.ExitCode())
		}
		t.Fatalf("failed to run helper: %v", err)
	}
}
