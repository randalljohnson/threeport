package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// hasSubcommand reports whether parent registered child in its command tree.
func hasSubcommand(parent, child *cobra.Command) bool {
	for _, c := range parent.Commands() {
		if c == child {
			return true
		}
	}
	return false
}

// assertFlags reports flag names missing from cmd's flag set.
func assertFlags(t *testing.T, cmd *cobra.Command, want []string) {
	t.Helper()
	for _, name := range want {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("expected flag %q on %q, not found", name, cmd.Use)
		}
	}
}
