package v0

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
)

// generateTestPEMKey produces a fresh PEM-encoded RSA private key so the OCI
// signer can consume it during GenerateOkeToken() tests without hitting disk.
func generateTestPEMKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return string(pem.EncodeToMemory(block))
}

// failingRegionProvider satisfies common.ConfigurationProvider and returns an
// error from Region(); every other method returns a placeholder success so the
// only failure surface is the region lookup GenerateOkeToken() reaches first.
type failingRegionProvider struct{}

func (failingRegionProvider) TenancyOCID() (string, error)    { return "ocid1.tenancy.oc1..t", nil }
func (failingRegionProvider) UserOCID() (string, error)       { return "ocid1.user.oc1..u", nil }
func (failingRegionProvider) KeyFingerprint() (string, error) { return "aa:bb", nil }
func (failingRegionProvider) Region() (string, error)         { return "", errors.New("boom-region") }
func (failingRegionProvider) KeyID() (string, error)          { return "kid", nil }
func (failingRegionProvider) PrivateRSAKey() (*rsa.PrivateKey, error) {
	return nil, errors.New("no key")
}
func (failingRegionProvider) AuthType() (common.AuthConfig, error) {
	return common.AuthConfig{AuthType: common.UnknownAuthenticationType}, nil
}

// TestGenerateOkeToken_ReturnsErrorWhenRegionLookupFails covers the earliest
// error branch: when the configuration provider cannot supply a region, the
// function must surface a wrapped "failed to get region" error and empty
// token / zero-value expiration.
func TestGenerateOkeToken_ReturnsErrorWhenRegionLookupFails(t *testing.T) {
	// drive GenerateOkeToken() with a provider whose Region() errors
	token, exp, err := GenerateOkeToken("cluster-abc", failingRegionProvider{})

	// assert the caller sees a failed-to-get-region wrapped error
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get region") {
		t.Fatalf("expected 'failed to get region' in error, got %q", err.Error())
	}

	// assert the zero token and zero expiration are returned alongside the error
	if token != "" {
		t.Fatalf("expected empty token, got %q", token)
	}
	if !exp.IsZero() {
		t.Fatalf("expected zero time, got %v", exp)
	}
}

// TestGenerateOkeToken_HappyPath covers the success path: with a valid raw
// configuration provider (real RSA key, well-known region), the function
// returns a non-empty base64-URL-encoded token whose decoded URL contains the
// cluster id, region, and both authorization + date query parameters, and an
// expiration set roughly four minutes into the future.
func TestGenerateOkeToken_HappyPath(t *testing.T) {
	// build a raw provider that satisfies IsConfigurationProviderValid()
	pemKey := generateTestPEMKey(t)
	provider := common.NewRawConfigurationProvider(
		"ocid1.tenancy.oc1..aaaaaaaatenancy",
		"ocid1.user.oc1..aaaaaaaauser",
		"us-phoenix-1",
		"aa:bb:cc:dd:ee:ff:00:11:22:33:44:55:66:77:88:99",
		pemKey,
		nil,
	)

	// invoke GenerateOkeToken() with a fixed cluster id and capture the wall clock
	// window during which the expiration must fall
	before := time.Now()
	token, exp, err := GenerateOkeToken("ocid1.cluster.oc1.phx.cluster123", provider)
	after := time.Now()

	// assert no error and non-empty token
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// assert token decodes to a URL that embeds region, cluster id, and both
	// signer-produced query parameters
	raw, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token is not base64-URL: %v", err)
	}
	decoded := string(raw)
	if !strings.Contains(decoded, "us-phoenix-1") {
		t.Fatalf("decoded URL missing region: %q", decoded)
	}
	if !strings.Contains(decoded, "ocid1.cluster.oc1.phx.cluster123") {
		t.Fatalf("decoded URL missing cluster id: %q", decoded)
	}
	u, err := url.Parse(decoded)
	if err != nil {
		t.Fatalf("failed to parse decoded URL: %v", err)
	}
	q := u.Query()
	if q.Get("authorization") == "" {
		t.Fatal("decoded URL missing authorization query param")
	}
	if q.Get("date") == "" {
		t.Fatal("decoded URL missing date query param")
	}

	// assert expiration is four minutes after the token-time captured internally;
	// tolerate a small window because tokenTime is captured mid-function
	minExp := before.Add(4 * time.Minute).Add(-2 * time.Second)
	maxExp := after.Add(4 * time.Minute).Add(2 * time.Second)
	if exp.Before(minExp) || exp.After(maxExp) {
		t.Fatalf("expiration %v not within [%v, %v]", exp, minExp, maxExp)
	}

	// assert the returned expiration parses back as GMT (fixed zone offset 0)
	_, offset := exp.Zone()
	if offset != 0 {
		t.Fatalf("expected GMT (offset 0), got offset %d", offset)
	}
}

// TestGenerateOkeToken_EmptyClusterID covers the boundary where the caller
// passes an empty cluster id: GenerateOkeToken() still succeeds, and the
// decoded URL ends with the cluster_request path segment followed by nothing,
// documenting that the function does not reject empty ids itself.
func TestGenerateOkeToken_EmptyClusterID(t *testing.T) {
	// build a valid raw provider and invoke with an empty cluster id
	pemKey := generateTestPEMKey(t)
	provider := common.NewRawConfigurationProvider(
		"ocid1.tenancy.oc1..aaaaaaaatenancy",
		"ocid1.user.oc1..aaaaaaaauser",
		"us-phoenix-1",
		"aa:bb:cc:dd:ee:ff:00:11:22:33:44:55:66:77:88:99",
		pemKey,
		nil,
	)
	token, _, err := GenerateOkeToken("", provider)

	// assert no error and the decoded URL has an empty trailing path segment
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token is not base64-URL: %v", err)
	}
	if !strings.Contains(string(raw), "cluster_request/?") {
		t.Fatalf("expected empty cluster segment before query, got %q", string(raw))
	}
}
