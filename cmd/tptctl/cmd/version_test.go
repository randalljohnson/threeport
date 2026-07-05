package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/threeport/threeport/internal/version"
)

// TestVersionCmdMetadata asserts versionCmd's Use, Short, and Long strings match documented values.
func TestVersionCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if versionCmd.Use != "version" {
		t.Errorf("versionCmd.Use = %q, want %q", versionCmd.Use, "version")
	}
	// verify Short description is set for cobra help output
	if versionCmd.Short != "Print the version of tptctl" {
		t.Errorf("versionCmd.Short = %q, want %q", versionCmd.Short, "Print the version of tptctl")
	}
	// verify Long description is set for cobra help output
	if versionCmd.Long != "Print the version of tptctl." {
		t.Errorf("versionCmd.Long = %q, want %q", versionCmd.Long, "Print the version of tptctl.")
	}
	// verify Run hook is wired so root cobra dispatch is honored
	if versionCmd.Run == nil {
		t.Errorf("versionCmd.Run = nil, want a function")
	}
}

// TestVersionCmdRegisteredOnRoot asserts versionCmd is a subcommand of rootCmd via init().
func TestVersionCmdRegisteredOnRoot(t *testing.T) {
	// verify subcommand registration by init() so the top-level `tptctl version` resolves
	if !hasSubcommand(rootCmd, versionCmd) {
		t.Errorf("versionCmd not registered under rootCmd")
	}
}

// TestVersionCmdRunPrintsVersion exercises versionCmd.Run and asserts it prints the embedded version.
func TestVersionCmdRunPrintsVersion(t *testing.T) {
	// capture stdout so the printed version is asserted against version.GetVersion()
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to open pipe: %v", err)
	}
	os.Stdout = w

	// invoke the Run hook directly; args are unused by the version command
	versionCmd.Run(versionCmd, []string{})

	// restore stdout before draining the pipe to avoid deadlock on write
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}
	os.Stdout = origStdout

	// read the captured output
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read captured stdout: %v", err)
	}

	// assert the printed line matches version.GetVersion() (fmt.Println adds a trailing newline)
	got := strings.TrimRight(buf.String(), "\n")
	want := version.GetVersion()
	if got != want {
		t.Errorf("versionCmd.Run stdout = %q, want %q", got, want)
	}
}
