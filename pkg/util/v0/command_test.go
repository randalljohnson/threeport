package v0

import (
	"strings"
	"testing"
)

// TestRunCommandStreamOutput_HappyPathReturnsNilOnZeroExit verifies that a
// command exiting 0 produces no error and completes cleanly.
func TestRunCommandStreamOutput_HappyPathReturnsNilOnZeroExit(t *testing.T) {
	// run true(1), the canonical zero-exit no-op
	if err := RunCommandStreamOutput("true"); err != nil {
		t.Fatalf("expected nil error for true, got %v", err)
	}
}

// TestRunCommandStreamOutput_StreamsStdoutWithoutError verifies that a command
// producing stdout output completes without error.
func TestRunCommandStreamOutput_StreamsStdoutWithoutError(t *testing.T) {
	// echo writes to stdout and exits 0
	if err := RunCommandStreamOutput("echo", "hello", "world"); err != nil {
		t.Fatalf("expected nil error for echo, got %v", err)
	}
}

// TestRunCommandStreamOutput_StreamsStderrWithoutError verifies that a command
// writing to stderr but exiting 0 still returns nil.
func TestRunCommandStreamOutput_StreamsStderrWithoutError(t *testing.T) {
	// sh redirects a line to stderr, then exits 0
	if err := RunCommandStreamOutput("sh", "-c", "echo err-line 1>&2"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// TestRunCommandStreamOutput_NonZeroExitReturnsWrappedError verifies that a
// non-zero exit yields a wrapped "error waiting for command" error.
func TestRunCommandStreamOutput_NonZeroExitReturnsWrappedError(t *testing.T) {
	// false(1) exits 1, cmd.Wait returns an *exec.ExitError
	err := RunCommandStreamOutput("false")
	if err == nil {
		t.Fatal("expected error for false, got nil")
	}
	// verify the wrapper prefix from RunCommandStreamOutput's Wait branch
	if !strings.Contains(err.Error(), "error waiting for command") {
		t.Errorf("expected wait error wrapper, got %q", err.Error())
	}
}

// TestRunCommandStreamOutput_MissingBinaryReturnsStartError verifies that a
// nonexistent command surfaces a "error starting command" error.
func TestRunCommandStreamOutput_MissingBinaryReturnsStartError(t *testing.T) {
	// use a name that will not resolve on PATH so cmd.Start fails
	err := RunCommandStreamOutput("this-binary-should-not-exist-xyz-12345")
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
	// verify the wrapper prefix from RunCommandStreamOutput's Start branch
	if !strings.Contains(err.Error(), "error starting command") {
		t.Errorf("expected start error wrapper, got %q", err.Error())
	}
}

// TestRunCommandStreamOutput_MultilineOutputDrainsBothPipes verifies that a
// command producing multiple stdout and stderr lines is drained fully without
// deadlock.
func TestRunCommandStreamOutput_MultilineOutputDrainsBothPipes(t *testing.T) {
	// interleave stdout and stderr lines to exercise both scanner goroutines
	script := "for i in 1 2 3; do echo out-$i; echo err-$i 1>&2; done"
	if err := RunCommandStreamOutput("sh", "-c", script); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}
