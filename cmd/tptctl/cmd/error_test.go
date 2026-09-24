package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureErrStdout redirects os.Stdout while fn runs and returns whatever fn wrote.
func captureErrStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	fn()
	_ = w.Close()
	os.Stdout = orig
	<-done
	_ = r.Close()
	return buf.String()
}

// TestMissingErr covers the missing-subcommand message shape for a range of parent commands.
func TestMissingErr(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{name: "single word command", command: "get"},
		{name: "multi word command", command: "get aws"},
		{name: "empty command", command: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// capture what missingErr writes to stdout
			got := captureErrStdout(t, func() {
				missingErr(tc.command)
			})

			// output must carry the standard error prefix
			if !strings.Contains(got, "Error:") {
				t.Errorf("output %q missing Error: prefix", got)
			}

			// the missing-subcommand phrase must appear verbatim
			if !strings.Contains(got, "missing subcommand") {
				t.Errorf("output %q missing 'missing subcommand' text", got)
			}

			// the parent command name must be included so the user can see the usage hint
			if tc.command != "" && !strings.Contains(got, tc.command) {
				t.Errorf("output %q missing command %q", got, tc.command)
			}

			// the usage hint must reference the -h flag
			if !strings.Contains(got, "-h") {
				t.Errorf("output %q missing -h usage hint", got)
			}
		})
	}
}

// TestUnknownErr covers the unknown-subcommand message shape, asserting the subcommand and parent both appear.
func TestUnknownErr(t *testing.T) {
	cases := []struct {
		name       string
		command    string
		subcommand string
	}{
		{name: "simple parent and child", command: "get", subcommand: "widgets"},
		{name: "multi word parent", command: "config current-control-plane", subcommand: "foo"},
		{name: "empty subcommand", command: "get", subcommand: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// capture what unknownErr writes to stdout
			got := captureErrStdout(t, func() {
				unknownErr(tc.command, tc.subcommand)
			})

			// output must carry the standard error prefix
			if !strings.Contains(got, "Error:") {
				t.Errorf("output %q missing Error: prefix", got)
			}

			// the unknown-subcommand phrase must appear (production string is misspelled 'unkown subcomand'; assert verbatim)
			if !strings.Contains(got, "unkown subcomand") {
				t.Errorf("output %q missing 'unkown subcomand' text", got)
			}

			// the parent command name must appear so the user can look up usage
			if !strings.Contains(got, tc.command) {
				t.Errorf("output %q missing command %q", got, tc.command)
			}

			// the unrecognized subcommand token must be echoed back
			if tc.subcommand != "" && !strings.Contains(got, tc.subcommand) {
				t.Errorf("output %q missing subcommand %q", got, tc.subcommand)
			}

			// the usage hint must reference the -h flag
			if !strings.Contains(got, "-h") {
				t.Errorf("output %q missing -h usage hint", got)
			}
		})
	}
}
