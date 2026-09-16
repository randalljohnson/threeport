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
// Two concurrent operations for different service accounts have to use
// their own credentials. GOOGLE_APPLICATION_CREDENTIALS names one file
// for the whole process, so a write there is what the next ADC lookup
// sees. Callers attach the JSON when they build each GCP client;
// the service-account path stores nothing.

// serviceAccountJSON returns well-formed service-account JSON with a
// generated PKCS8 private key.
func serviceAccountJSON(t *testing.T, clientEmail string) string {
	t.Helper()
	// generate an RSA key and PKCS8 PEM
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	// fill the service-account credential document
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

// TestEnsureGCPAuthWithServiceAccountDoesNotSetProcessGlobal covers the
// service-account path leaving GOOGLE_APPLICATION_CREDENTIALS unset.
func TestEnsureGCPAuthWithServiceAccountDoesNotSetProcessGlobal(t *testing.T) {
	const envKey = "GOOGLE_APPLICATION_CREDENTIALS"
	// capture and restore the env var
	before, had := os.LookupEnv(envKey)
	t.Cleanup(func() {
		if had {
			os.Setenv(envKey, before)
		} else {
			os.Unsetenv(envKey)
		}
	})
	// start from an unset env var
	os.Unsetenv(envKey)

	// call EnsureGCPAuth with service-account JSON
	require.NoError(t, EnsureGCPAuth(serviceAccountJSON(t, "sa@test-project.iam.gserviceaccount.com")))

	// assert the env var remains unset
	_, set := os.LookupEnv(envKey)
	assert.False(t, set, "the service account path must not set the process-global credentials env var")
}

// TestEnsureGCPAuthWithServiceAccountWritesNoKeyFile covers the
// service-account path writing no key material to disk.
func TestEnsureGCPAuthWithServiceAccountWritesNoKeyFile(t *testing.T) {
	// point TMPDIR at an empty temp dir
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)

	// call EnsureGCPAuth with service-account JSON
	require.NoError(t, EnsureGCPAuth(serviceAccountJSON(t, "sa@test-project.iam.gserviceaccount.com")))

	// assert the temp dir is still empty
	entries, err := filepath.Glob(filepath.Join(tempDir, "*"))
	require.NoError(t, err)
	assert.Empty(t, entries, "the service account path must not write key material to disk")
}

// TestValidateServiceAccountCredentialsRejectsUnusableJSON rejects truncated
// JSON, an empty document, and an unsupported credential type at parse time.
func TestValidateServiceAccountCredentialsRejectsUnusableJSON(t *testing.T) {
	for name, payload := range map[string]string{
		"truncated json":              `{"type":"service_`,
		"empty document":              ``,
		"unsupported credential type": `{"type":"banana"}`,
	} {
		t.Run(name, func(t *testing.T) {
			// parse an unusable payload
			err := validateServiceAccountCredentials(context.Background(), payload)

			// assert the parse error
			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to parse service account credentials")
		})
	}
}

// TestValidateServiceAccountCredentialsAcceptsWellFormedKey accepts a
// well-formed service-account JSON document.
func TestValidateServiceAccountCredentialsAcceptsWellFormedKey(t *testing.T) {
	// parse a generated well-formed key
	err := validateServiceAccountCredentials(
		context.Background(),
		serviceAccountJSON(t, "sa@test-project.iam.gserviceaccount.com"),
	)

	// assert the document is accepted
	assert.NoError(t, err)
}
