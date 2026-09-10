package v0

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Service account credentials must never become process state.
//
// Two concurrent operations for different service accounts have to
// authenticate against their own credentials. Exporting a key file path
// through GOOGLE_APPLICATION_CREDENTIALS makes the last writer decide which
// account every other in-flight operation uses, so callers thread the JSON
// into each GCP client per call instead and this package stores nothing.

// serviceAccountJSON builds well-formed GCP service_account credentials JSON
// with a throwaway RSA key and clientEmail so tests exercise a payload the
// Google SDK accepts rather than a stand-in the parser would reject.
func serviceAccountJSON(t *testing.T, clientEmail string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	creds := map[string]string{
		"type":                        "service_account",
		"project_id":                  "test-project",
		"private_key_id":              "test-key-id",
		"private_key":                 string(keyPEM),
		"client_email":                clientEmail,
		"client_id":                   "test-client-id",
		"token_uri":                   "https://oauth2.googleapis.com/token",
		"auth_uri":                    "https://accounts.google.com/o/oauth2/auth",
		"auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
	}
	b, err := json.Marshal(creds)
	require.NoError(t, err)
	return string(b)
}

// TestEnsureGCPAuthWithServiceAccountDoesNotSetProcessGlobal asserts the
// service account path leaves GOOGLE_APPLICATION_CREDENTIALS as it found it.
func TestEnsureGCPAuthWithServiceAccountDoesNotSetProcessGlobal(t *testing.T) {
	const envKey = "GOOGLE_APPLICATION_CREDENTIALS"
	before, had := os.LookupEnv(envKey)
	t.Cleanup(func() {
		if had {
			os.Setenv(envKey, before)
		} else {
			os.Unsetenv(envKey)
		}
	})
	os.Unsetenv(envKey)

	require.NoError(t, EnsureGCPAuth(serviceAccountJSON(t, "sa@test-project.iam.gserviceaccount.com")))

	_, set := os.LookupEnv(envKey)
	assert.False(t, set, "the service account path must not set the process-global credentials env var")
}

// TestEnsureGCPAuthWithServiceAccountWritesNoKeyFile asserts no service
// account key material is left on disk after EnsureGCPAuth.
func TestEnsureGCPAuthWithServiceAccountWritesNoKeyFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)

	require.NoError(t, EnsureGCPAuth(serviceAccountJSON(t, "sa@test-project.iam.gserviceaccount.com")))

	entries, err := filepath.Glob(filepath.Join(tempDir, "*"))
	require.NoError(t, err)
	assert.Empty(t, entries, "the service account path must not write key material to disk")
}

// TestValidateServiceAccountCredentialsRejectsUnusableJSON asserts unusable
// credentials fail at the auth check with a descriptive parse error.
func TestValidateServiceAccountCredentialsRejectsUnusableJSON(t *testing.T) {
	for name, payload := range map[string]string{
		"truncated json":              `{"type":"service_`,
		"empty document":              ``,
		"unsupported credential type": `{"type":"banana"}`,
	} {
		t.Run(name, func(t *testing.T) {
			err := validateServiceAccountCredentials(context.Background(), payload)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to parse service account credentials")
		})
	}
}

// TestValidateServiceAccountCredentialsAcceptsWellFormedKey asserts a valid
// service account JSON passes so the rejection cases exercise the payload.
func TestValidateServiceAccountCredentialsAcceptsWellFormedKey(t *testing.T) {
	err := validateServiceAccountCredentials(
		context.Background(),
		serviceAccountJSON(t, "sa@test-project.iam.gserviceaccount.com"),
	)

	assert.NoError(t, err)
}
