package main

import (
	"bytes"
	"io"
	"os"
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
