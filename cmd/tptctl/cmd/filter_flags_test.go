package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFilterCmd builds a command carrying three filter flags, standing in for
// the get-locations and get-machines commands without touching the package
// level variables those bind to.
func newFilterCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "get-things", Run: func(*cobra.Command, []string) {}}
	cmd.Flags().String("node-profile", "", "")
	cmd.Flags().String("node-size", "", "")
	cmd.Flags().String("aws-machine-type", "", "")
	return cmd
}

// TestValidateSingleFilterFlagAllowsNone covers an unfiltered invocation,
// which lists every row.
func TestValidateSingleFilterFlagAllowsNone(t *testing.T) {
	cmd := newFilterCmd()
	require.NoError(t, cmd.ParseFlags([]string{}))
	assert.NoError(t, validateSingleFilterFlag(cmd))
}

// TestValidateSingleFilterFlagAllowsOne covers the ordinary filtered case.
func TestValidateSingleFilterFlagAllowsOne(t *testing.T) {
	cmd := newFilterCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--node-profile", "Balanced"}))
	assert.NoError(t, validateSingleFilterFlag(cmd))
}

// TestValidateSingleFilterFlagRejectsTwo covers the case the check exists for,
// and asserts the error names both flags the caller actually set. Naming a
// flag the command does not accept is the bug that shipped when the list was
// written out by hand.
func TestValidateSingleFilterFlagRejectsTwo(t *testing.T) {
	cmd := newFilterCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--node-profile", "Balanced", "--node-size", "Small"}))

	err := validateSingleFilterFlag(cmd)

	require.Error(t, err, "two filters must be rejected")
	assert.ErrorContains(t, err, "--node-profile")
	assert.ErrorContains(t, err, "--node-size")
	assert.NotContains(t, err.Error(), "--aws-machine-type", "an unset flag must not be named")
}

// TestValidateSingleFilterFlagIgnoresInheritedFlags is why the check reads
// LocalFlags and not Flags. A global set alongside one filter is still one
// filter; reading cmd.Flags() would count the global and reject a valid
// invocation.
func TestValidateSingleFilterFlagIgnoresInheritedFlags(t *testing.T) {
	root := &cobra.Command{Use: "tptctl"}
	root.PersistentFlags().String("threeport-config", "", "")
	root.PersistentFlags().String("provider-config", "", "")
	cmd := newFilterCmd()
	root.AddCommand(cmd)

	require.NoError(t, cmd.ParseFlags([]string{
		"--threeport-config", "/tmp/config.yaml",
		"--node-profile", "Balanced",
	}))

	assert.NoError(t, validateSingleFilterFlag(cmd), "a global must not count as a filter")
}
