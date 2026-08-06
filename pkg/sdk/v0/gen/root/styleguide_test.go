package root

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdk "github.com/threeport/threeport/pkg/sdk/v0"
)

// chdirTemp moves the process into a fresh directory for the duration of the
// test, since the generator writes relative to the working directory the way
// threeport-sdk runs it.
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	previous, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previous)) })
	return dir
}

func TestGenStyleGuideWritesWhenAbsent(t *testing.T) {
	dir := chdirTemp(t)

	require.NoError(t, GenStyleGuide(nil, &sdk.SdkConfig{}))

	written, err := os.ReadFile(filepath.Join(dir, "docs", "dev", "style-guide.md"))
	require.NoError(t, err)
	assert.Equal(t, styleGuideContent, string(written))
}

func TestGenStyleGuideLeavesAnExistingGuideAlone(t *testing.T) {
	dir := chdirTemp(t)
	target := filepath.Join(dir, "docs", "dev", "style-guide.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
	require.NoError(t, os.WriteFile(target, []byte("module owns this"), 0644))

	require.NoError(t, GenStyleGuide(nil, &sdk.SdkConfig{}))

	written, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "module owns this", string(written))
}

func TestGenStyleGuideHonorsExcludeFiles(t *testing.T) {
	dir := chdirTemp(t)
	target := filepath.Join("docs", "dev", "style-guide.md")

	require.NoError(t, GenStyleGuide(nil, &sdk.SdkConfig{ExcludeFiles: []string{target}}))

	_, err := os.Stat(filepath.Join(dir, target))
	assert.True(t, os.IsNotExist(err))
}
