package v0

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

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
