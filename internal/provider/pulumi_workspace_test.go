package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/datatypes"
)

// TestGetStackNameReturnsRuntimeInstanceName covers getStackName returning the
// runtime instance name so the Pulumi stack lines up with the runtime.
func TestGetStackNameReturnsRuntimeInstanceName(t *testing.T) {
	// build a workspace with a known runtime instance name
	w := &PulumiWorkspace{RuntimeInstanceName: "dev"}

	// call the accessor
	got := w.getStackName()

	// assert the stack name matches the runtime instance name
	if got != "dev" {
		t.Fatalf("getStackName = %q, want %q", got, "dev")
	}
}

// TestGetEnvVarsReturnsPulumiKeys covers getEnvVars populating every Pulumi
// environment key the workspace relies on and rooting the backend on the
// state directory.
func TestGetEnvVarsReturnsPulumiKeys(t *testing.T) {
	// isolate HOME so the helper builds its pulumi-home under tmp
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// build a workspace with state dir + project name set
	stateDir := filepath.Join(tmp, "state")
	w := &PulumiWorkspace{
		RuntimeInstanceName: "dev",
		ProjectName:         "oke",
		stateDir:            stateDir,
	}

	// call the helper
	envVars, err := w.getEnvVars()
	if err != nil {
		t.Fatalf("getEnvVars returned error: %v", err)
	}

	// assert every required key is present
	requiredKeys := []string{
		"PULUMI_BACKEND_URL",
		"PULUMI_HOME",
		"PULUMI_PROJECT",
		"PULUMI_CONFIG_PASSPHRASE",
		"PULUMI_IGNORE_AMBIENT_PLUGINS",
		"PULUMI_PLUGIN_PATH",
	}
	for _, k := range requiredKeys {
		if _, ok := envVars[k]; !ok {
			t.Fatalf("expected key %q in env vars, got %v", k, envVars)
		}
	}

	// assert the backend URL points at the state dir
	wantBackend := "file://" + stateDir
	if envVars["PULUMI_BACKEND_URL"] != wantBackend {
		t.Fatalf("PULUMI_BACKEND_URL = %q, want %q",
			envVars["PULUMI_BACKEND_URL"], wantBackend)
	}

	// assert the project name is passed through
	if envVars["PULUMI_PROJECT"] != "oke" {
		t.Fatalf("PULUMI_PROJECT = %q, want %q",
			envVars["PULUMI_PROJECT"], "oke")
	}

	// assert the passphrase is empty so local file backend does not prompt
	if envVars["PULUMI_CONFIG_PASSPHRASE"] != "" {
		t.Fatalf("PULUMI_CONFIG_PASSPHRASE = %q, want empty",
			envVars["PULUMI_CONFIG_PASSPHRASE"])
	}

	// assert ambient plugins are ignored so the workspace uses its own plugin dir
	if envVars["PULUMI_IGNORE_AMBIENT_PLUGINS"] != "true" {
		t.Fatalf("PULUMI_IGNORE_AMBIENT_PLUGINS = %q, want %q",
			envVars["PULUMI_IGNORE_AMBIENT_PLUGINS"], "true")
	}

	// assert the pulumi-home dir was created on disk
	pulumiHome := filepath.Join(tmp, ".threeport", "pulumi-home")
	if _, err := os.Stat(pulumiHome); err != nil {
		t.Fatalf("expected pulumi-home dir at %q, stat: %v", pulumiHome, err)
	}
	if envVars["PULUMI_HOME"] != pulumiHome {
		t.Fatalf("PULUMI_HOME = %q, want %q", envVars["PULUMI_HOME"], pulumiHome)
	}

	// assert the plugin path sits under pulumi-home
	wantPluginPath := filepath.Join(pulumiHome, "plugins")
	if envVars["PULUMI_PLUGIN_PATH"] != wantPluginPath {
		t.Fatalf("PULUMI_PLUGIN_PATH = %q, want %q",
			envVars["PULUMI_PLUGIN_PATH"], wantPluginPath)
	}
}

// TestHasStateDirFalseWhenAbsent covers HasStateDir returning false when the
// runtime state directory has never been created.
func TestHasStateDirFalseWhenAbsent(t *testing.T) {
	// point HOME at an empty tmp so no state dir exists
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// build a workspace with a runtime instance name that has no dir on disk
	w := &PulumiWorkspace{RuntimeInstanceName: "missing"}

	// assert the helper reports the dir is absent
	if w.HasStateDir() {
		t.Fatal("expected HasStateDir = false for absent dir")
	}
}

// TestHasStateDirTrueWhenPresent covers HasStateDir returning true after the
// state directory has been created on disk.
func TestHasStateDirTrueWhenPresent(t *testing.T) {
	// isolate HOME so the state dir lives under tmp
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// pre-create the runtime state dir the helper is expected to find
	stateDir := filepath.Join(tmp, ".threeport", "pulumi-state", "dev")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("failed to create state dir: %v", err)
	}

	// build a workspace pointing at that runtime instance name
	w := &PulumiWorkspace{RuntimeInstanceName: "dev"}

	// assert the helper reports the dir is present
	if !w.HasStateDir() {
		t.Fatal("expected HasStateDir = true when dir exists")
	}
}

// TestDeleteStackStateNoopWhenAbsent covers DeleteStackState returning nil
// when the runtime state directory does not exist so callers do not have to
// pre-check.
func TestDeleteStackStateNoopWhenAbsent(t *testing.T) {
	// point HOME at an empty tmp so no state dir exists
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// build a workspace with a runtime instance name that has no dir
	w := &PulumiWorkspace{RuntimeInstanceName: "missing"}

	// assert delete succeeds silently
	if err := w.DeleteStackState(); err != nil {
		t.Fatalf("expected nil for absent dir, got %v", err)
	}
}

// TestDeleteStackStateRemovesExisting covers DeleteStackState removing the
// runtime state directory when it exists on disk.
func TestDeleteStackStateRemovesExisting(t *testing.T) {
	// isolate HOME so the state dir lives under tmp
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// pre-create the runtime state dir with a sentinel file inside
	stateDir := filepath.Join(tmp, ".threeport", "pulumi-state", "dev")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("failed to create state dir: %v", err)
	}
	sentinel := filepath.Join(stateDir, "sentinel")
	if err := os.WriteFile(sentinel, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to write sentinel: %v", err)
	}

	// call the helper on that instance
	w := &PulumiWorkspace{RuntimeInstanceName: "dev"}
	if err := w.DeleteStackState(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	// assert the state dir is gone
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("expected state dir removed, stat err: %v", err)
	}
}

// TestGetStateFilePathComposesUnderStateDir covers GetStateFilePath composing
// <stateDir>/.pulumi/stacks/<project>/<runtime>.json so Pulumi finds the file
// where the local backend writes it.
func TestGetStateFilePathComposesUnderStateDir(t *testing.T) {
	// isolate HOME so the state dir lives under tmp
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// build a workspace with project + runtime instance names
	w := &PulumiWorkspace{
		RuntimeInstanceName: "dev",
		ProjectName:         "oke",
	}

	// call the helper
	got, err := w.GetStateFilePath()
	if err != nil {
		t.Fatalf("GetStateFilePath returned error: %v", err)
	}

	// assert the path is composed under the runtime state dir
	want := filepath.Join(tmp, ".threeport", "pulumi-state", "dev",
		".pulumi", "stacks", "oke", "dev.json")
	if got != want {
		t.Fatalf("GetStateFilePath = %q, want %q", got, want)
	}

	// assert setStateDir created the state dir on disk
	stateDir := filepath.Join(tmp, ".threeport", "pulumi-state", "dev")
	if _, err := os.Stat(stateDir); err != nil {
		t.Fatalf("expected state dir at %q, stat: %v", stateDir, err)
	}
}

// TestReadStateFileNilWhenAbsent covers ReadStateFile returning (nil, nil)
// when the Pulumi state file has not been written yet so callers can treat
// absence as a clean slate.
func TestReadStateFileNilWhenAbsent(t *testing.T) {
	// isolate HOME so the state dir lives under tmp
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// build a workspace whose state file does not yet exist
	w := &PulumiWorkspace{
		RuntimeInstanceName: "dev",
		ProjectName:         "oke",
	}

	// call the helper
	got, err := w.ReadStateFile()
	if err != nil {
		t.Fatalf("ReadStateFile returned error: %v", err)
	}

	// assert nil is returned for the absent file
	if got != nil {
		t.Fatalf("expected nil for absent state file, got %v", got)
	}
}

// TestReadStateFileReturnsContents covers ReadStateFile reading the raw file
// bytes when the state file exists on disk.
func TestReadStateFileReturnsContents(t *testing.T) {
	// isolate HOME so the state dir lives under tmp
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// build a workspace and resolve the state file path via the helper
	w := &PulumiWorkspace{
		RuntimeInstanceName: "dev",
		ProjectName:         "oke",
	}
	stateFilePath, err := w.GetStateFilePath()
	if err != nil {
		t.Fatalf("GetStateFilePath returned error: %v", err)
	}

	// write a sentinel payload to the resolved path
	if err := os.MkdirAll(filepath.Dir(stateFilePath), 0755); err != nil {
		t.Fatalf("failed to create stacks dir: %v", err)
	}
	payload := []byte(`{"stack":"snapshot"}`)
	if err := os.WriteFile(stateFilePath, payload, 0644); err != nil {
		t.Fatalf("failed to write state file: %v", err)
	}

	// call the helper
	got, err := w.ReadStateFile()
	if err != nil {
		t.Fatalf("ReadStateFile returned error: %v", err)
	}

	// assert the returned bytes match the payload
	if got == nil {
		t.Fatal("expected non-nil state, got nil")
	}
	if string(*got) != string(payload) {
		t.Fatalf("ReadStateFile bytes = %q, want %q", string(*got), string(payload))
	}
}

// TestSetStackStateCheckpointFormatWritesFile covers SetStackState taking the
// checkpoint-format branch: when the JSON does not decode as an untyped
// deployment, the raw bytes are written atomically to the Pulumi state file.
func TestSetStackStateCheckpointFormatWritesFile(t *testing.T) {
	// isolate HOME so all writes stay under tmp
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// build a workspace whose state file path is deterministic under tmp
	w := &PulumiWorkspace{
		RuntimeInstanceName: "dev",
		ProjectName:         "oke",
		ProjectDescription:  "oke test",
	}

	// craft a payload that will NOT unmarshal into apitype.UntypedDeployment
	// (no "deployment" key) so the checkpoint branch runs
	payload := datatypes.JSON([]byte(`{"checkpoint":"raw"}`))

	// call the helper
	if err := w.SetStackState(&payload); err != nil {
		t.Fatalf("SetStackState returned error: %v", err)
	}

	// resolve the expected state file path via the same helper
	stateFilePath, err := w.GetStateFilePath()
	if err != nil {
		t.Fatalf("GetStateFilePath returned error: %v", err)
	}

	// assert the file exists and holds the checkpoint bytes verbatim
	got, err := os.ReadFile(stateFilePath)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}
	if string(got) != `{"checkpoint":"raw"}` {
		t.Fatalf("state file = %q, want %q", string(got), `{"checkpoint":"raw"}`)
	}

	// assert the atomic-rename temp file did not survive
	tmpFile := stateFilePath + ".tmp"
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Fatalf("expected temp file removed, stat err: %v", err)
	}
}

// TestLogErrorFallbackWritesToStderr covers logError falling back to stderr
// when no structured logger is configured so CLI callers still see the error.
func TestLogErrorFallbackWritesToStderr(t *testing.T) {
	// swap stderr with a pipe so the helper's output can be captured
	origStderr := os.Stderr
	r, wPipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to open pipe: %v", err)
	}
	os.Stderr = wPipe
	t.Cleanup(func() { os.Stderr = origStderr })

	// build a workspace with no logger so the fallback branch runs
	w := &PulumiWorkspace{}

	// call the helper with a sentinel message and cause
	w.logError(os.ErrPermission, "sentinel-msg")

	// close the writer so the reader unblocks
	if err := wPipe.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}
	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	got := string(buf[:n])

	// assert the message and cause are both present in stderr output
	if !strings.Contains(got, "sentinel-msg") {
		t.Fatalf("expected sentinel-msg in stderr, got %q", got)
	}
	if !strings.Contains(got, "permission denied") {
		t.Fatalf("expected wrapped cause in stderr, got %q", got)
	}
}
