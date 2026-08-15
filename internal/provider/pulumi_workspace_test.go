package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requirePulumiCLI skips the test when the pulumi CLI is not on PATH,
// since every workspace and stack operation shells out to it. It also
// redirects the home dir to a temp dir so the pulumi home directory
// created during workspace setup never touches the real home dir, and
// disables the CLI update check to avoid network calls.
func requirePulumiCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("pulumi"); err != nil {
		t.Skip("pulumi CLI not found on PATH; skipping test that needs a real pulumi backend")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PULUMI_SKIP_UPDATE_CHECK", "true")
}

// checkpointState returns a minimal checkpoint-format state JSON for the
// given project and stack name. The top-level "checkpoint" key, and the
// absence of a top-level "deployment" key, routes SetStackState() to the
// direct file-write path. The marker lands in the resource URN so two
// payloads for the same stack can be told apart byte-for-byte.
func checkpointState(project, name, marker string) string {
	return fmt.Sprintf(
		`{"version":3,"checkpoint":{"stack":"organization/%s/%s","latest":{"manifest":{"time":"0001-01-01T00:00:00Z","magic":"","version":""},"resources":[{"urn":"urn:pulumi:%s::%s::pulumi:pulumi:Stack::%s","type":"pulumi:pulumi:Stack"}]}}}`,
		project, name, name, project, marker,
	)
}

// TestNewPulumiWorkspace_WithStateDirRoot covers the constructor: the
// runtime instance name and project name are set from the arguments, the
// state dir root option makes the state dir resolve to <root>/<name>, and
// the state file path lands under the injected root at
// .pulumi/stacks/<project>/<name>.json.
func TestNewPulumiWorkspace_WithStateDirRoot(t *testing.T) {
	root := t.TempDir()
	w := NewPulumiWorkspace("instance-a", "oke", WithStateDirRoot(root))

	assert.Equal(t, "instance-a", w.RuntimeInstanceName)
	assert.Equal(t, "oke", w.ProjectName)
	assert.Equal(t, root, w.stateDirRoot)

	path, err := w.GetStateFilePath()
	require.NoError(t, err)
	assert.Equal(
		t,
		filepath.Join(root, "instance-a", ".pulumi", "stacks", "oke", "instance-a.json"),
		path,
	)

	// resolving the path builds <root>/<name> and creates it on disk
	assert.Equal(t, filepath.Join(root, "instance-a"), w.stateDir)
	info, err := os.Stat(w.stateDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

// TestNewPulumiWorkspace_DefaultRoot covers the fallback branch of state dir
// resolution: without the state dir root option, the state dir resolves
// under the home-dir runtime state path. The home dir is redirected to a
// temp dir so the side-effecting mkdir never touches the real home dir;
// the assertions check the prefix and suffix structure of the path.
func TestNewPulumiWorkspace_DefaultRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	w := NewPulumiWorkspace("instance-b", "eks")

	path, err := w.GetStateFilePath()
	require.NoError(t, err)

	wantSuffix := filepath.Join(
		".threeport", "pulumi-state", "instance-b",
		".pulumi", "stacks", "eks", "instance-b.json",
	)
	assert.True(
		t, strings.HasSuffix(path, wantSuffix),
		"path %q should end with %q", path, wantSuffix,
	)
	assert.True(
		t, strings.HasPrefix(path, home),
		"path %q should be under the redirected home dir %q", path, home,
	)

	// the state dir was created under the redirected home dir, proving
	// the default-root branch only ever touches the resolved home
	info, err := os.Stat(filepath.Join(home, ".threeport", "pulumi-state", "instance-b"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

// TestGetStateFilePath_EmptyName covers the empty-name guard: an empty
// runtime instance name returns an error refusing to build the path,
// before any filesystem side effects, so two unnamed instances can never
// collide on the same state file.
func TestGetStateFilePath_EmptyName(t *testing.T) {
	root := t.TempDir()
	w := NewPulumiWorkspace("", "oke", WithStateDirRoot(root))

	path, err := w.GetStateFilePath()
	require.Error(t, err)
	assert.Empty(t, path)
	assert.Contains(t, err.Error(), "runtime instance name is empty")

	// the guard fires before state dir creation, so the root stays empty
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// TestSetStackState_CheckpointRoundTrip covers the checkpoint-format branch
// of state restoration: JSON with a top-level "checkpoint" key, and no
// top-level "deployment" key, bypasses the backend import and is written
// directly to the state file, landing on disk byte-identical and reading
// back unchanged.
func TestSetStackState_CheckpointRoundTrip(t *testing.T) {
	requirePulumiCLI(t)

	root := t.TempDir()
	w := NewPulumiWorkspace("ckpt-instance", "ckptproj", WithStateDirRoot(root))

	state := checkpointState("ckptproj", "ckpt-instance", "round-trip")
	require.NoError(t, w.SetStackState(jsonPtr(state)))

	path, err := w.GetStateFilePath()
	require.NoError(t, err)
	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, state, string(onDisk), "checkpoint state must land on disk byte-identical")

	readBack, err := w.ReadStateFile()
	require.NoError(t, err)
	require.NotNil(t, readBack)
	assert.Equal(t, state, string(*readBack))
}

// TestSetStackState_AtomicTempThenRename covers the atomic
// temp-then-rename write of the checkpoint branch in two parts. On
// success, the target holds the content and no temp file remains. On a
// temp-write failure, forced by occupying the temp path with a directory,
// the error surfaces and the previously written state is left intact,
// which is the atomicity guarantee.
//
// The rename half of the pair is not covered here. The implementation
// exposes no injectable rename failure, and reaching it from outside means
// making the rename fail without making the backend stack upsert fail
// first, which needs a seam the code does not have.
func TestSetStackState_AtomicTempThenRename(t *testing.T) {
	requirePulumiCLI(t)

	root := t.TempDir()
	w := NewPulumiWorkspace("atomic-instance", "atomicproj", WithStateDirRoot(root))

	// part 1: successful write goes temp-then-rename and leaves no temp file
	first := checkpointState("atomicproj", "atomic-instance", "first")
	require.NoError(t, w.SetStackState(jsonPtr(first)))

	path, err := w.GetStateFilePath()
	require.NoError(t, err)
	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, first, string(onDisk))
	_, statErr := os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(statErr), "no temp file may remain after a successful write")

	// part 2: occupy the temp path with a directory so the temp write
	// fails; the error surfaces and the prior state survives untouched
	require.NoError(t, os.Mkdir(path+".tmp", 0755))
	second := checkpointState("atomicproj", "atomic-instance", "second")
	err = w.SetStackState(jsonPtr(second))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write temporary state file")
	onDisk, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, first, string(onDisk), "failed temp write must leave the previous state intact")
	require.NoError(t, os.Remove(path+".tmp"))
}

// TestSetStackState_ExportFormatRequiresBackend covers the export-format
// branch of state restoration: JSON with a top-level "deployment" key is
// routed through the backend stack import, which converts it to checkpoint
// format on disk, and a subsequent state export returns deployment-format
// JSON. This needs a real pulumi backend, so the test skips when the CLI
// is unavailable.
func TestSetStackState_ExportFormatRequiresBackend(t *testing.T) {
	requirePulumiCLI(t)

	root := t.TempDir()
	w := NewPulumiWorkspace("export-instance", "exportproj", WithStateDirRoot(root))

	exportState := `{"version":3,"deployment":{"manifest":{"time":"0001-01-01T00:00:00Z","magic":"","version":""}}}`
	require.NoError(t, w.SetStackState(jsonPtr(exportState)))

	// the import converts to the backend's checkpoint format on disk
	onDisk, err := w.ReadStateFile()
	require.NoError(t, err)
	require.NotNil(t, onDisk)
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(*onDisk, &parsed))
	assert.Contains(t, parsed, "checkpoint")
	assert.NotContains(t, parsed, "deployment")

	// round trip: exporting the state returns deployment-format JSON
	stateJSON, err := w.GetStackState()
	require.NoError(t, err)
	require.NotNil(t, stateJSON)
	var deployment apitype.UntypedDeployment
	require.NoError(t, json.Unmarshal(*stateJSON, &deployment))
	assert.Equal(t, 3, deployment.Version)
	assert.NotNil(t, deployment.Deployment)
}

// TestPulumiWorkspace_ZeroValueStillWorks asserts zero-value compatibility
// for the embedder pattern: a workspace built as a plain struct literal
// with only the name fields set, no constructor and no options, still
// resolves the state file path through the home-dir fallback. The home dir
// is redirected to a temp dir so the path resolution side effects stay out
// of the real home dir.
func TestPulumiWorkspace_ZeroValueStillWorks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	w := &PulumiWorkspace{
		RuntimeInstanceName: "instance-z",
		ProjectName:         "gke",
	}

	path, err := w.GetStateFilePath()
	require.NoError(t, err)

	wantSuffix := filepath.Join(
		".threeport", "pulumi-state", "instance-z",
		".pulumi", "stacks", "gke", "instance-z.json",
	)
	assert.True(
		t, strings.HasSuffix(path, wantSuffix),
		"path %q should end with %q", path, wantSuffix,
	)
	assert.True(
		t, strings.HasPrefix(path, home),
		"path %q should be under the redirected home dir %q", path, home,
	)
}

// TestResolveStateDir_EmptyName covers every path that resolves a state
// directory, not just the one that formats a file path. An empty instance
// name joins to the shared base directory, so two unnamed instances would
// read and write the same state.
func TestResolveStateDir_EmptyName(t *testing.T) {
	w := NewPulumiWorkspace("", "oke", WithStateDirRoot(t.TempDir()))

	dir, err := w.resolveStateDir()
	require.Error(t, err)
	assert.Empty(t, dir)
	assert.Contains(t, err.Error(), "runtime instance name is empty")

	require.Error(t, w.setStateDir(), "setStateDir must refuse an empty name")

	path, err := w.GetStateFilePath()
	require.Error(t, err)
	assert.Empty(t, path)

	assert.False(t, w.HasStateDir(), "an unnamed workspace claims no state dir")
	require.Error(t, w.DeleteStackState(), "DeleteStackState must refuse an empty name")
}

// TestStateDirRoot_HonoredByEveryMethod proves the injected root governs
// existence checks and deletion as well as writes. A method resolving the
// default runtime state dir instead would delete real state while the
// caller believed it was confined to a temp dir.
func TestStateDirRoot_HonoredByEveryMethod(t *testing.T) {
	root := t.TempDir()
	w := NewPulumiWorkspace("instance-a", "oke", WithStateDirRoot(root))

	assert.False(t, w.HasStateDir(), "no state dir exists before one is created")

	require.NoError(t, w.setStateDir())
	stateDir := filepath.Join(root, "instance-a")
	assert.DirExists(t, stateDir, "the state dir lands under the injected root")
	assert.True(t, w.HasStateDir(), "the state dir is visible once created")

	marker := filepath.Join(stateDir, "marker")
	require.NoError(t, os.WriteFile(marker, []byte("state"), 0644))

	require.NoError(t, w.DeleteStackState())
	assert.NoDirExists(t, stateDir, "deletion removes the dir under the injected root")
	assert.False(t, w.HasStateDir(), "the state dir is gone after deletion")
}

// TestDeleteStackState_MissingDirIsNotAnError pins the tolerant delete: a
// second delete, or a delete of an instance whose infrastructure never
// reached the state-writing stage, is a no-op rather than a failure.
func TestDeleteStackState_MissingDirIsNotAnError(t *testing.T) {
	w := NewPulumiWorkspace("never-created", "oke", WithStateDirRoot(t.TempDir()))

	assert.NoError(t, w.DeleteStackState())
}
