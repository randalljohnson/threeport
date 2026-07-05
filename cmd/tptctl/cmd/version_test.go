package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/threeport/threeport/internal/version"
)

// TestVersionCmdMetadata asserts version command metadata (Use, Short, Long, Run).
func TestVersionCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if versionCmd.Use != "version" {
		t.Errorf("versionCmd.Use = %q, want %q", versionCmd.Use, "version")
	}
	// verify short description populated so `tptctl --help` lists the command
	if versionCmd.Short == "" {
		t.Errorf("versionCmd.Short is empty, want non-empty description")
	}
	// verify long description populated
	if versionCmd.Long == "" {
		t.Errorf("versionCmd.Long is empty, want non-empty description")
	}
	// verify Run hook wired so `tptctl version` executes the print
	if versionCmd.Run == nil {
		t.Errorf("versionCmd.Run = nil, want a function")
	}
}

// TestVersionCmdRegistered asserts versionCmd is a subcommand of rootCmd.
func TestVersionCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(rootCmd, versionCmd) {
		t.Errorf("versionCmd not registered under rootCmd")
	}
}

// TestVersionCmdRunPrintsVersion asserts Run prints the current tptctl version to stdout.
func TestVersionCmdRunPrintsVersion(t *testing.T) {
	// redirect stdout so we can capture the Run output
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	// invoke Run against the real command; args are ignored
	versionCmd.Run(versionCmd, []string{})

	// close writer and restore stdout before reading captured bytes
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	os.Stdout = origStdout

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}

	// verify output equals GetVersion() plus the fmt.Println newline
	got := buf.String()
	want := version.GetVersion() + "\n"
	if got != want {
		t.Errorf("versionCmd.Run stdout = %q, want %q", got, want)
	}
	// verify a trailing newline exists (fmt.Println contract)
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("versionCmd.Run stdout missing trailing newline: %q", got)
	}
}

// TestVersionCmdRunIgnoresArgs asserts Run behaves identically regardless of positional args.
func TestVersionCmdRunIgnoresArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"nil args", nil},
		{"empty args", []string{}},
		{"extra args", []string{"unexpected", "positional"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// redirect stdout to capture output for this case
			origStdout := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("os.Pipe: %v", err)
			}
			os.Stdout = w

			// invoke Run under the case's args
			versionCmd.Run(versionCmd, tc.args)

			// restore stdout and drain the pipe
			if err := w.Close(); err != nil {
				t.Fatalf("close pipe writer: %v", err)
			}
			os.Stdout = origStdout

			var buf bytes.Buffer
			if _, err := io.Copy(&buf, r); err != nil {
				t.Fatalf("read captured stdout: %v", err)
			}

			// verify output matches the version string irrespective of args
			want := version.GetVersion() + "\n"
			if got := buf.String(); got != want {
				t.Errorf("Run(args=%v) stdout = %q, want %q", tc.args, got, want)
			}
		})
	}
}
