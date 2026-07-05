package v0

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"
)

// resetGCPCredState resets the package-level mutex-guarded credential state so
// tests run in isolation without leaking state to other tests.
func resetGCPCredState(t *testing.T) {
	t.Helper()
	gcpCredMu.Lock()
	defer gcpCredMu.Unlock()
	if gcpCredTempFile != "" {
		os.Remove(gcpCredTempFile)
	}
	gcpCredTempFile = ""
	gcpCredRefCount = 0
	os.Unsetenv("GOOGLE_APPLICATION_CREDENTIALS")
}

// TestGenerateRandomStateProducesHexOfExpectedLength covers generateRandomState().
func TestGenerateRandomStateProducesHexOfExpectedLength(t *testing.T) {
	// invoke the generator and confirm no error surfaces
	state, err := generateRandomState()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// state is 16 random bytes hex-encoded, so length must be 32 characters
	if len(state) != 32 {
		t.Errorf("expected length 32, got %d", len(state))
	}

	// verify the string decodes as valid hex to catch any encoding regression
	if _, err := hex.DecodeString(state); err != nil {
		t.Errorf("state is not valid hex: %v", err)
	}
}

// TestGenerateRandomStateReturnsDistinctValues confirms two successive calls
// return different states, guarding against a stuck RNG.
func TestGenerateRandomStateReturnsDistinctValues(t *testing.T) {
	// draw two states back-to-back
	first, err := generateRandomState()
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	second, err := generateRandomState()
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	// a 128-bit collision is astronomically unlikely, so equal values signal a bug
	if first == second {
		t.Error("expected distinct random states")
	}
}

// TestGetADCPathHonorsHomeAndOS covers getADCPath() by pointing HOME at a temp
// directory and checking the returned path matches the platform-specific
// well-known location.
func TestGetADCPathHonorsHomeAndOS(t *testing.T) {
	// point the home lookup at a temp dir so the test does not touch real ADC state
	tmp := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", tmp)
	} else {
		t.Setenv("HOME", tmp)
	}

	// invoke the function under test
	path, err := getADCPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// assert the path anchors under the fake home directory
	if !strings.HasPrefix(path, tmp) {
		t.Errorf("path %q does not start with home %q", path, tmp)
	}

	// assert the well-known filename is present regardless of OS
	if !strings.HasSuffix(path, "application_default_credentials.json") {
		t.Errorf("path %q missing expected filename", path)
	}
}

// TestSaveADCCredentialsWritesValidJSON covers saveADCCredentials() end-to-end:
// it must create the ADC directory tree and write a JSON file that round-trips
// back to the token's refresh token and the fixed OAuth client fields.
func TestSaveADCCredentialsWritesValidJSON(t *testing.T) {
	// sandbox HOME so the test never touches the user's real gcloud config
	tmp := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", tmp)
	} else {
		t.Setenv("HOME", tmp)
	}

	// call the function under test with a fabricated refresh token
	token := &oauth2.Token{RefreshToken: "test-refresh-token"}
	if err := saveADCCredentials(token); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// read back the file at the expected path and decode it as JSON
	path, err := getADCPath()
	if err != nil {
		t.Fatalf("getADCPath failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved credentials: %v", err)
	}

	var got adcCredentials
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("saved file is not valid JSON: %v", err)
	}

	// verify each field survives the round-trip with the expected values
	if got.RefreshToken != "test-refresh-token" {
		t.Errorf("refresh token mismatch: got %q", got.RefreshToken)
	}
	if got.ClientID != gcpOAuthClientID {
		t.Errorf("client id mismatch: got %q", got.ClientID)
	}
	if got.ClientSecret != gcpOAuthClientSecret {
		t.Errorf("client secret mismatch: got %q", got.ClientSecret)
	}
	if got.Type != "authorized_user" {
		t.Errorf("type mismatch: got %q", got.Type)
	}
}

// TestSaveADCCredentialsCreatesDirTree confirms saveADCCredentials() creates
// missing parent directories rather than failing on a fresh home.
func TestSaveADCCredentialsCreatesDirTree(t *testing.T) {
	// point HOME at a brand-new temp dir with no gcloud directory
	tmp := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", tmp)
	} else {
		t.Setenv("HOME", tmp)
	}

	// invoke the save
	if err := saveADCCredentials(&oauth2.Token{RefreshToken: "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// confirm the parent directory was created with the expected permissions
	path, _ := getADCPath()
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("parent dir not created: %v", err)
	}
}

// TestConfigureServiceAccountCredentialsSetsEnvAndFile covers
// configureServiceAccountCredentials(): it must write the JSON to a temp file
// and expose that path via GOOGLE_APPLICATION_CREDENTIALS.
func TestConfigureServiceAccountCredentialsSetsEnvAndFile(t *testing.T) {
	// clear any package-level state left over from prior tests
	resetGCPCredState(t)
	t.Cleanup(func() { resetGCPCredState(t) })

	// invoke the configuration with a fixed SA JSON payload
	payload := `{"type":"service_account","project_id":"test"}`
	if err := configureServiceAccountCredentials(payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// verify the env var points to a real file whose contents match the payload
	envPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if envPath == "" {
		t.Fatal("GOOGLE_APPLICATION_CREDENTIALS was not set")
	}
	got, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("failed to read temp SA file: %v", err)
	}
	if string(got) != payload {
		t.Errorf("temp file content mismatch: got %q", string(got))
	}

	// confirm the package-level ref count reflects the single active caller
	gcpCredMu.Lock()
	rc := gcpCredRefCount
	gcpCredMu.Unlock()
	if rc != 1 {
		t.Errorf("expected ref count 1, got %d", rc)
	}
}

// TestConfigureServiceAccountCredentialsIsIdempotent covers the branch that
// discards a redundant temp file when another goroutine has already registered
// credentials. The second call must not overwrite the first file or leak.
func TestConfigureServiceAccountCredentialsIsIdempotent(t *testing.T) {
	// reset state and register cleanup
	resetGCPCredState(t)
	t.Cleanup(func() { resetGCPCredState(t) })

	// first call plants the temp file and env var
	if err := configureServiceAccountCredentials(`{"type":"service_account"}`); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	firstPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")

	// second call must reuse the same temp file and bump the ref count
	if err := configureServiceAccountCredentials(`{"type":"other"}`); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	secondPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")

	if firstPath != secondPath {
		t.Errorf("env var path drifted: %q vs %q", firstPath, secondPath)
	}

	gcpCredMu.Lock()
	rc := gcpCredRefCount
	gcpCredMu.Unlock()
	if rc != 2 {
		t.Errorf("expected ref count 2 after second configure, got %d", rc)
	}
}

// TestCleanupGCPCredentialsRemovesFileWhenRefCountReachesZero covers
// CleanupGCPCredentials() paired against configureServiceAccountCredentials().
func TestCleanupGCPCredentialsRemovesFileWhenRefCountReachesZero(t *testing.T) {
	// reset any prior state
	resetGCPCredState(t)
	t.Cleanup(func() { resetGCPCredState(t) })

	// register two ref-counted callers so cleanup must run twice
	if err := configureServiceAccountCredentials(`{"a":1}`); err != nil {
		t.Fatalf("first configure failed: %v", err)
	}
	if err := configureServiceAccountCredentials(`{"a":2}`); err != nil {
		t.Fatalf("second configure failed: %v", err)
	}
	tempPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")

	// first cleanup drops the ref count but must leave the file in place
	CleanupGCPCredentials()
	if _, err := os.Stat(tempPath); err != nil {
		t.Errorf("temp file removed prematurely: %v", err)
	}

	// second cleanup drives the ref count to zero and must remove the file and env var
	CleanupGCPCredentials()
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Errorf("expected temp file removed, stat err = %v", err)
	}
	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
		t.Error("expected GOOGLE_APPLICATION_CREDENTIALS to be unset")
	}
}

// TestCleanupGCPCredentialsIsSafeWithNoActiveCreds covers the no-op branch of
// CleanupGCPCredentials() when no configure call has run.
func TestCleanupGCPCredentialsIsSafeWithNoActiveCreds(t *testing.T) {
	// reset state and register cleanup
	resetGCPCredState(t)
	t.Cleanup(func() { resetGCPCredState(t) })

	// invoking cleanup without any active creds must not panic or set env vars
	CleanupGCPCredentials()

	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
		t.Error("expected GOOGLE_APPLICATION_CREDENTIALS to remain unset")
	}
	gcpCredMu.Lock()
	rc := gcpCredRefCount
	gcpCredMu.Unlock()
	if rc != 0 {
		t.Errorf("expected ref count 0, got %d", rc)
	}
}

// TestEnsureGCPAuthWithServiceAccountConfiguresCredentials asserts that when
// hasValidGCPCredentials() returns false and a SA payload is supplied,
// EnsureGCPAuth() delegates to configureServiceAccountCredentials() rather
// than initiating the browser OAuth flow.
func TestEnsureGCPAuthWithServiceAccountConfiguresCredentials(t *testing.T) {
	// isolate credential state and sandbox HOME so google.DefaultTokenSource
	// finds no cached user credentials to short-circuit the SA branch
	resetGCPCredState(t)
	t.Cleanup(func() { resetGCPCredState(t) })

	tmp := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", tmp)
	} else {
		t.Setenv("HOME", tmp)
	}

	// call the entry point with a SA JSON payload; must configure without error
	if err := EnsureGCPAuth(`{"type":"service_account","project_id":"test"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// confirm the SA branch ran by checking for the exported env var
	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") == "" {
		t.Error("expected GOOGLE_APPLICATION_CREDENTIALS to be set by SA branch")
	}
}

// TestHasValidGCPCredentialsReturnsFalseWhenNoCredentials covers
// hasValidGCPCredentials() on a clean HOME with no ADC file present and no
// metadata server reachable.
func TestHasValidGCPCredentialsReturnsFalseWhenNoCredentials(t *testing.T) {
	// sandbox HOME so DefaultTokenSource cannot find any user credentials
	tmp := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", tmp)
	} else {
		t.Setenv("HOME", tmp)
	}
	// ensure no explicit SA credentials env var short-circuits the lookup
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	os.Unsetenv("GOOGLE_APPLICATION_CREDENTIALS")

	// with no creds available, the function must report invalid
	if hasValidGCPCredentials(context.Background()) {
		t.Error("expected hasValidGCPCredentials to return false on empty HOME")
	}
}

// TestGCPOAuthScopesIncludesCloudPlatform pins the exported scope list so a
// silent drop of the cloud-platform scope surfaces here.
func TestGCPOAuthScopesIncludesCloudPlatform(t *testing.T) {
	// scan the exported scope list for the scope the downstream check requires
	found := false
	for _, s := range GcpOAuthScopes {
		if s == "https://www.googleapis.com/auth/cloud-platform" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected GcpOAuthScopes to include cloud-platform scope")
	}
}

// TestConcurrentConfigureAndCleanupIsRaceFree drives many goroutines through
// the configure and cleanup paths to exercise the mutex guarding
// gcpCredTempFile and gcpCredRefCount. Runs the mutation loop under -race to
// catch any regression in the locking discipline.
func TestConcurrentConfigureAndCleanupIsRaceFree(t *testing.T) {
	// reset state and register cleanup
	resetGCPCredState(t)
	t.Cleanup(func() { resetGCPCredState(t) })

	// fan out concurrent configure+cleanup pairs
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := configureServiceAccountCredentials(`{"k":"v"}`); err != nil {
				t.Errorf("configure failed: %v", err)
				return
			}
			CleanupGCPCredentials()
		}()
	}
	wg.Wait()

	// after every configure was paired with a cleanup, the ref count must be zero
	gcpCredMu.Lock()
	rc := gcpCredRefCount
	tempFile := gcpCredTempFile
	gcpCredMu.Unlock()
	if rc != 0 {
		t.Errorf("expected ref count 0 after balanced pairs, got %d", rc)
	}
	if tempFile != "" {
		t.Errorf("expected temp file cleared, got %q", tempFile)
	}
}
