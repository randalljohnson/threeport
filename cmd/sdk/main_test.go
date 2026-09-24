/*
Copyright © 2026 Threeport admin@threeport.io
*/
package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/threeport/threeport/internal/version"
)

// TestMainRunsVersionSubcommand drives main() with argv pointed at the
// version subcommand so the main() body is exercised without triggering
// its os.Exit(1) error branch. Capturing stdout confirms that main()
// delegates to cmd.Execute() and that the version subcommand emits the
// value returned by the internal version helper.
func TestMainRunsVersionSubcommand(t *testing.T) {
	// stash and restore os.Args so the test does not leak state to
	// sibling tests that share the process.
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// point argv at the version subcommand: cobra reads os.Args when no
	// SetArgs override is in effect, and version returns nil so main()
	// does not hit its os.Exit(1) branch.
	os.Args = []string{"threeport-sdk", "version"}

	// redirect stdout onto a pipe so the version banner does not leak
	// into the test runner output and can be asserted against.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to open stdout pipe: %v", err)
	}
	os.Stdout = w

	// invoke main() so the top-level entry point body is covered.
	main()

	// close the write end so the reader unblocks and restore stdout.
	w.Close()
	os.Stdout = origStdout

	// drain the pipe so we can compare against the expected version.
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read captured stdout: %v", err)
	}

	// verify main() routed to versionCmd and emitted GetVersion() so
	// the binary's version output stays aligned with build metadata.
	got := strings.TrimSpace(buf.String())
	want := strings.TrimSpace(version.GetVersion())
	if got != want {
		t.Errorf("main() with version arg printed %q, want %q", got, want)
	}
}
