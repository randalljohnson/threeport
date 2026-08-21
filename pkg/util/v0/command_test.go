package v0

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStdout runs fn with os.Stdout replaced by a pipe and returns
// everything written to it.  The helpers under test print with fmt.Println,
// which writes to os.Stdout directly, so reading their output means replacing
// that file for the duration of the call.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	read, write, err := os.Pipe()
	require.NoError(t, err, "failed to open a pipe to replace stdout")

	original := os.Stdout
	os.Stdout = write

	captured := make(chan string, 1)
	go func() {
		var builder strings.Builder
		scanner := bufio.NewScanner(read)
		for scanner.Scan() {
			builder.WriteString(scanner.Text())
			builder.WriteString("\n")
		}
		captured <- builder.String()
	}()

	fn()

	os.Stdout = original
	require.NoError(t, write.Close(), "failed to close the replacement stdout")

	return <-captured
}

// TestRunCommandStreamOutputInDirRunsFromTheGivenDirectory asserts that the
// directory applies to the command alone, leaving the calling process where it
// was so a later command still resolves paths against it.
func TestRunCommandStreamOutputInDirRunsFromTheGivenDirectory(t *testing.T) {
	// setup: a directory holding one file, named so a listing identifies it
	dir := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(dir+"/marker-file", []byte("marker"), 0o600),
		"failed to write the marker file",
	)

	before, err := os.Getwd()
	require.NoError(t, err, "failed to read the working directory")

	// action: list the temporary directory from the caller's own directory
	output := captureStdout(t, func() {
		assert.NoError(t, RunCommandStreamOutputInDir(dir, "ls"), "the listing should succeed")
	})

	// assert the command saw the given directory
	assert.Contains(t, output, "marker-file", "the command should run from the given directory")

	// assert the caller's own directory is untouched
	after, err := os.Getwd()
	require.NoError(t, err, "failed to re-read the working directory")
	assert.Equal(t, before, after, "the calling process should stay where it was")
}
