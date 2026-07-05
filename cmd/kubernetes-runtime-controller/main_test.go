package main

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// helperEnvVar signals to the test binary that it should invoke
// showHelpAndExit rather than run the parent test logic.
const helperEnvVar = "KUBERNETES_RUNTIME_CTRL_HELP_EXIT_CODE"

// mainHelperEnvVar signals to the test binary that it should invoke
// main() with a synthesized argv so the -help early-exit path can be
// exercised end-to-end with all flags registered.
const mainHelperEnvVar = "KUBERNETES_RUNTIME_CTRL_RUN_MAIN"

// TestMain intercepts the process before the testing framework starts
// so a re-exec of this binary under helperEnvVar exercises
// showHelpAndExit directly. showHelpAndExit terminates the process
// via os.Exit, which cannot be observed from an in-process call.
func TestMain(m *testing.M) {
	// if the main helper env var is set, drive main() itself with the
	// argv supplied via the env var so the -help early-exit path runs
	// with the real flag set registered.
	if argv, ok := os.LookupEnv(mainHelperEnvVar); ok {
		// namsral/flag reads os.Args[1:]; override so main() sees only
		// the caller-supplied flags rather than the test binary's own.
		os.Args = append([]string{"kubernetes-runtime-controller"}, strings.Fields(argv)...)
		main()
		// main() calls showHelpAndExit on the -help path, which os.Exits.
		// reaching here means main() returned; end with a distinguishable code.
		os.Exit(4)
	}
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
	wantPrefix := "Usage: threeport-kubernetes-runtime-controller [options]"
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

// TestShowHelpAndExitExitCodeOne verifies showHelpAndExit(1) round-trips
// so callers signaling generic failure land on the standard non-zero code.
func TestShowHelpAndExitExitCodeOne(t *testing.T) {
	// exit code 1 is the typical "generic failure" convention.
	const want = 1
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

// TestShowHelpAndExitBannerLineOrder verifies the Usage line precedes the
// options header so operators reading top-down see the command name first.
func TestShowHelpAndExitBannerLineOrder(t *testing.T) {
	// capture stdout and confirm Usage: appears at position 0 and options:
	// appears strictly after it.
	stdout, _, err := runHelper(t, 0)
	if err != nil {
		t.Fatalf("failed to run helper: %v", err)
	}
	// locate both markers.
	usageIdx := strings.Index(stdout, "Usage:")
	optionsIdx := strings.Index(stdout, "options:")
	if usageIdx != 0 {
		t.Fatalf("Usage: not at start of output: index = %d, stdout = %q", usageIdx, stdout)
	}
	if optionsIdx <= usageIdx {
		t.Fatalf("options: header does not follow Usage: line: usageIdx=%d optionsIdx=%d", usageIdx, optionsIdx)
	}
}

// runMainHelper re-execs the test binary with mainHelperEnvVar set so the
// helper branch invokes main() directly with the supplied argv. Returns
// stdout, stderr, and any *exec.ExitError signaling non-zero exit. Both
// streams are captured because flag.PrintDefaults writes to stderr while
// the banner writes to stdout.
func runMainHelper(t *testing.T, argv string) (stdout, stderr string, exitErr *exec.ExitError, err error) {
	t.Helper()
	// invoke self with a no-match test filter so only the main-helper branch runs.
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), mainHelperEnvVar+"="+argv)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	runErr := cmd.Run()
	if runErr != nil {
		ee, ok := runErr.(*exec.ExitError)
		if !ok {
			return outBuf.String(), errBuf.String(), nil, runErr
		}
		return outBuf.String(), errBuf.String(), ee, nil
	}
	return outBuf.String(), errBuf.String(), nil, nil
}

// TestMainHelpFlagPrintsFlagDefaults drives main() with the -help flag so
// the flag registration, parse, and help-early-exit path all run. The banner
// lands on stdout, flag descriptions land on stderr (namsral/flag defaults
// PrintDefaults output there); both must reflect the registered flags.
func TestMainHelpFlagPrintsFlagDefaults(t *testing.T) {
	// drive main() with -help so it registers all flags, then early-exits.
	stdout, stderr, exitErr, err := runMainHelper(t, "-help")
	if err != nil {
		t.Fatalf("failed to run main helper: %v", err)
	}
	// -help routes through showHelpAndExit(0), so exit is zero.
	if exitErr != nil {
		t.Fatalf("expected exit code 0, got %d, stdout=%q stderr=%q", exitErr.ExitCode(), stdout, stderr)
	}
	// verify the banner leads on stdout.
	if !strings.HasPrefix(stdout, "Usage: threeport-kubernetes-runtime-controller [options]") {
		t.Fatalf("stdout missing Usage banner: %q", stdout)
	}
	// verify at least one of the registered flags shows in PrintDefaults output;
	// api-server is a stable flag on main() that we can anchor on.
	if !strings.Contains(stderr, "-api-server") {
		t.Fatalf("stderr missing -api-server flag description: %q", stderr)
	}
	// verify a concurrency flag also shows, confirming all reconciler flags register.
	if !strings.Contains(stderr, "-kubernetes-runtime-definition-concurrent-reconciles") {
		t.Fatalf("stderr missing -kubernetes-runtime-definition-concurrent-reconciles flag: %q", stderr)
	}
	// verify the auth-enabled flag shows so the whole flag set is captured.
	if !strings.Contains(stderr, "-auth-enabled") {
		t.Fatalf("stderr missing -auth-enabled flag description: %q", stderr)
	}
}
