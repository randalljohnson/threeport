package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestValidArgs asserts validArgs returns every goose command plus initialize.
func TestValidArgs(t *testing.T) {
	// gather the returned slice under test
	got := validArgs()

	// verify each goose command is present in the returned slice
	for _, expected := range gooseCommands {
		if !contains(got, expected) {
			t.Errorf("validArgs missing goose command %q; got %v", expected, got)
		}
	}

	// verify initialize is appended alongside the goose commands
	if !contains(got, "initialize") {
		t.Errorf("validArgs missing \"initialize\"; got %v", got)
	}

	// verify length matches gooseCommands + initialize
	if want := len(gooseCommands) + 1; len(got) != want {
		t.Errorf("validArgs length = %d, want %d", len(got), want)
	}
}

// TestValidArgsDoesNotMutateGooseCommands asserts repeated calls preserve the
// package-level gooseCommands slice.
func TestValidArgsDoesNotMutateGooseCommands(t *testing.T) {
	// snapshot the package-level slice before any calls
	before := append([]string(nil), gooseCommands...)

	// invoke validArgs multiple times; append should not leak into gooseCommands
	_ = validArgs()
	_ = validArgs()

	// verify the underlying slice is unchanged
	if len(gooseCommands) != len(before) {
		t.Fatalf("gooseCommands length changed: got %d, want %d", len(gooseCommands), len(before))
	}
	for i := range before {
		if gooseCommands[i] != before[i] {
			t.Errorf("gooseCommands[%d] = %q, want %q", i, gooseCommands[i], before[i])
		}
	}
}

// TestUsagePrintsHelpText asserts usage writes the tool description and every
// valid argument to stdout.
func TestUsagePrintsHelpText(t *testing.T) {
	// redirect stdout to capture usage output
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	// invoke the function under test
	usage()

	// restore stdout and collect the captured bytes
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}
	os.Stdout = origStdout
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read pipe: %v", err)
	}
	out := buf.String()

	// verify the tool description is present
	if !strings.Contains(out, "database-migrator initializes and manages the database schema") {
		t.Errorf("usage output missing tool description; got %q", out)
	}

	// verify each valid argument appears in the printed help
	for _, arg := range validArgs() {
		if !strings.Contains(out, arg) {
			t.Errorf("usage output missing valid argument %q; got %q", arg, out)
		}
	}
}

// contains reports whether s contains target.
func contains(s []string, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}

// TestReturnErrExitsNonZero asserts returnErr writes usage info and terminates
// the process with a non-zero exit code. Because returnErr calls os.Exit, the
// assertion runs in a re-exec of the test binary so the current test process
// survives.
func TestReturnErrExitsNonZero(t *testing.T) {
	// child branch: when the sentinel env is set, invoke returnErr so its
	// os.Exit(1) terminates this subprocess with the expected code
	if os.Getenv("DB_MIGRATOR_TEST_RETURN_ERR") == "1" {
		returnErr("boom", errors.New("synthetic failure"))
		return
	}

	// parent branch: re-exec this test binary running only this test, with
	// the sentinel env set so the child hits the branch above
	cmd := exec.Command(os.Args[0], "-test.run=TestReturnErrExitsNonZero")
	cmd.Env = append(os.Environ(), "DB_MIGRATOR_TEST_RETURN_ERR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	// verify the subprocess exited with a non-zero status; a nil err would
	// mean returnErr did not trigger os.Exit(1)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected subprocess to exit with error, got err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	if exitErr.ExitCode() == 0 {
		t.Errorf("expected non-zero exit code, got 0")
	}

	// verify the subprocess emitted the usage banner returnErr calls into,
	// so the coverage exercises usage() as well as the exit path
	if !strings.Contains(stdout.String(), "database-migrator") {
		t.Errorf("subprocess stdout missing usage banner; got %q", stdout.String())
	}
}

// TestMainRejectsInvalidCommand asserts main exits non-zero when the caller
// passes a command that is not in validArgs. Runs under the re-exec pattern
// because main calls os.Exit on the failure path.
func TestMainRejectsInvalidCommand(t *testing.T) {
	// child branch: when the sentinel env is set, hand off to main with an
	// argv that carries an unknown command so it takes the invalid-arg exit
	if os.Getenv("DB_MIGRATOR_TEST_MAIN_INVALID") == "1" {
		os.Args = []string{"database-migrator", "not-a-real-command"}
		main()
		return
	}

	// parent branch: re-exec this test binary running only this test, with
	// the sentinel env set so the child hits the branch above
	cmd := exec.Command(os.Args[0], "-test.run=TestMainRejectsInvalidCommand")
	cmd.Env = append(os.Environ(), "DB_MIGRATOR_TEST_MAIN_INVALID=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	// verify the subprocess exited non-zero; the invalid-arg path in main
	// routes through returnErr which calls os.Exit(1)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected subprocess to exit with error, got err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	if exitErr.ExitCode() == 0 {
		t.Errorf("expected non-zero exit code, got 0")
	}

	// verify the invalid-command error surfaces so we know main reached the
	// validation branch rather than dying earlier
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "not-a-real-command") {
		t.Errorf("subprocess output missing invalid-command error; got %q", combined)
	}
}
