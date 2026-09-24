/*
Copyright © 2023 Threeport admin@threeport.io
*/
package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/threeport/threeport/internal/version"
)

// TestExecuteRunsVersionSubcommand drives Execute() with argv set to
// `version`, exercising the RootCmd.Execute() body along with the wired
// versionCmd.Run closure. Execute() calls os.Exit(1) on error, so we
// keep the argv on a known-successful subcommand to avoid tripping that
// branch during tests.
func TestExecuteRunsVersionSubcommand(t *testing.T) {
	// stash and restore RootCmd argv so the test does not leak state.
	origArgs := os.Args
	defer func() {
		os.Args = origArgs
		RootCmd.SetArgs(nil)
	}()

	// route through SetArgs so cobra ignores os.Args for this run.
	RootCmd.SetArgs([]string{"version"})

	// redirect stdout so the version banner is not printed by the test
	// runner; the version command prints via fmt.Println to os.Stdout.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to open stdout pipe: %v", err)
	}
	os.Stdout = w

	// invoke Execute; it must return without exiting so the deferred
	// restore below runs.
	Execute()

	// close the write end so the reader unblocks.
	w.Close()
	os.Stdout = origStdout

	// slurp captured stdout so we can assert the printed version.
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read captured stdout: %v", err)
	}

	// the version command must print the same value the internal helper
	// returns, so operators inspecting the binary get a stable value.
	got := strings.TrimSpace(buf.String())
	want := strings.TrimSpace(version.GetVersion())
	if got != want {
		t.Errorf("version subcommand printed %q, want %q", got, want)
	}
}

// TestVersionCmdRunEmitsGetVersion invokes versionCmd.Run directly so the
// Run closure body is exercised independently of Execute's dispatch, and
// verifies it emits the value returned by version.GetVersion() so the CLI
// stays aligned with the embedded build metadata.
func TestVersionCmdRunEmitsGetVersion(t *testing.T) {
	// swap stdout for a pipe so we can capture what the Run closure prints.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to open stdout pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	// call the Run closure with an empty args slice, mimicking cobra's
	// dispatch when the user types `threeport-sdk version`.
	versionCmd.Run(versionCmd, []string{})

	// unblock the reader so io.Copy returns.
	w.Close()

	// drain the pipe and compare against GetVersion().
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read captured stdout: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	want := strings.TrimSpace(version.GetVersion())
	if got != want {
		t.Errorf("versionCmd.Run printed %q, want %q", got, want)
	}
}
