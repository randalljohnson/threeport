package cmd

import (
	"strings"
	"testing"
)

// TestMissingErrPrintsSubcommandGuidance asserts missingErr prints the missing-subcommand
// message and echoes the parent command name so the reader knows where to look for usage.
func TestMissingErrPrintsSubcommandGuidance(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{"typical", "get"},
		{"multi word", "create profile"},
		{"empty command", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke missingErr and capture its stdout output
			out, err := captureStdout(t, func() error {
				missingErr(tc.command)
				return nil
			})
			if err != nil {
				t.Fatalf("captureStdout returned err: %v", err)
			}

			// verify the fixed marker phrase appears
			if !strings.Contains(out, "missing subcommand") {
				t.Errorf("output missing marker phrase %q: %q", "missing subcommand", out)
			}
			// verify the guidance references the parent command by name
			if !strings.Contains(out, "tptctl "+tc.command+" -h") {
				t.Errorf("output missing usage hint for %q: %q", tc.command, out)
			}
			// verify the error prefix is present since cli.Error routes through CliOutputError
			if !strings.Contains(out, "Error:") {
				t.Errorf("output missing Error: prefix: %q", out)
			}
		})
	}
}

// TestUnknownErrPrintsBothTokens asserts unknownErr echoes the offending subcommand
// and the parent command in the usage-info guidance.
func TestUnknownErrPrintsBothTokens(t *testing.T) {
	cases := []struct {
		name       string
		command    string
		subcommand string
	}{
		{"typical", "get", "widgets"},
		{"empty subcommand", "delete", ""},
		{"both empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke unknownErr and capture its stdout output
			out, err := captureStdout(t, func() error {
				unknownErr(tc.command, tc.subcommand)
				return nil
			})
			if err != nil {
				t.Fatalf("captureStdout returned err: %v", err)
			}

			// verify the fixed marker phrase appears (typo preserved from source)
			if !strings.Contains(out, "unkown subcomand") {
				t.Errorf("output missing marker phrase %q: %q", "unkown subcomand", out)
			}
			// verify the offending subcommand is echoed back to the user
			if !strings.Contains(out, tc.subcommand) {
				t.Errorf("output missing subcommand token %q: %q", tc.subcommand, out)
			}
			// verify usage hint references the parent command
			if !strings.Contains(out, "tptctl "+tc.command+" -h") {
				t.Errorf("output missing usage hint for %q: %q", tc.command, out)
			}
			// verify the error prefix is present
			if !strings.Contains(out, "Error:") {
				t.Errorf("output missing Error: prefix: %q", out)
			}
		})
	}
}
