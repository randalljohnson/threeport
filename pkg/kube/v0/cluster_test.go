package v0

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	encryption "github.com/threeport/threeport/pkg/encryption/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// testEncryptionKey is a 32-byte AES-256 key, base64-encoded for the
// encryption helpers.
var testEncryptionKey = util.Base64Encode("0123456789abcdef0123456789abcdef")

// okeRuntimeApiServer is a threeport API stub that answers the runtime
// definition, OKE instance, and OCI provider lookups used to mint a per-request token.
type okeRuntimeApiServer struct {
	definitionRequests int
}

// serve starts the stub and returns a client and the host:port address the
// client library expects.
func (s *okeRuntimeApiServer) serve(t *testing.T) (*http.Client, string) {
	t.Helper()

	// encrypt the OCI provider key the way the API stores it
	privateKey, err := encryption.Encrypt(testEncryptionKey, "oci-api-signing-key")
	if err != nil {
		t.Fatalf("failed to encrypt the test provider key: %v", err)
	}

	// serve the runtime definition, OKE instance, and provider
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		infraProvider := v0.KubernetesRuntimeInfraProviderOKE
		ociProviderId := uint(3)
		clusterOcid := "ocid1.cluster.oc1..test"
		name := "oci-provider-0"
		userOcid := "ocid1.user.oc1..test"
		tenancyOcid := "ocid1.tenancy.oc1..test"
		region := "us-phoenix-1"
		fingerprint := "aa:bb:cc"

		switch {
		case strings.HasPrefix(r.URL.Path, v0.PathKubernetesRuntimeDefinitions):
			s.definitionRequests++
			s.write(t, w, []apiserver_lib.Object{
				v0.KubernetesRuntimeDefinition{InfraProvider: &infraProvider},
			})
		case strings.HasPrefix(r.URL.Path, "/v0/oci-oke-kubernetes-runtime-instances"):
			s.write(t, w, []apiserver_lib.Object{
				v0.OciOkeKubernetesRuntimeInstance{
					OciProviderID: &ociProviderId,
					ClusterOCID:   &clusterOcid,
				},
			})
		case strings.HasPrefix(r.URL.Path, v0.PathOciProviders):
			s.write(t, w, []apiserver_lib.Object{
				v0.OciProvider{
					Name:           &name,
					UserOCID:       &userOcid,
					TenancyOCID:    &tenancyOcid,
					DefaultRegion:  &region,
					KeyFingerprint: &fingerprint,
					PrivateKey:     &privateKey,
				},
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	// strip the scheme; the client library prepends one itself
	return &http.Client{}, strings.TrimPrefix(server.URL, "http://")
}

// write encodes objects as a threeport API success response.
func (s *okeRuntimeApiServer) write(t *testing.T, w http.ResponseWriter, data []apiserver_lib.Object) {
	t.Helper()

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(apiserver_lib.Response{Data: data}); err != nil {
		t.Errorf("failed to encode response: %v", err)
	}
}

// testTokenMintingRuntime returns a runtime with a definition ID and a client
// certificate both set. A live OKE record has no certificate.
func testTokenMintingRuntime() *v0.KubernetesRuntimeInstance {
	id := uint(1)
	definitionId := uint(2)
	endpoint := "https://oke-kube-api.example.com"
	caCertificate := "kube-ca-cert"
	certificate := "kube-client-cert"
	certificateKey := "kube-client-key"
	controlPlaneHost := true

	return &v0.KubernetesRuntimeInstance{
		Common:                        v0.Common{ID: &id},
		KubernetesRuntimeDefinitionID: &definitionId,
		APIEndpoint:                   &endpoint,
		CACertificate:                 &caCertificate,
		Certificate:                   &certificate,
		CertificateKey:                &certificateKey,
		ThreeportControlPlaneHost:     &controlPlaneHost,
	}
}

// TestGetRestConfigMintsTokenInsteadOfUsingCertificate covers an OKE runtime
// authenticating with a per-request token rather than its stored certificate.
// Kind never takes this path, so a green kind run does not cover it.
func TestGetRestConfigMintsTokenInsteadOfUsingCertificate(t *testing.T) {
	// stand up an OKE threeport API stub
	apiServer := &okeRuntimeApiServer{}
	apiClient, apiAddr := apiServer.serve(t)

	// build a rest config for a runtime that also has a client certificate
	restConfig, err := GetRestConfig(
		testTokenMintingRuntime(),
		false,
		apiClient,
		apiAddr,
		testEncryptionKey,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// check the runtime definition was looked up once
	if apiServer.definitionRequests != 1 {
		t.Errorf("expected the runtime definition to be looked up once, got %d", apiServer.definitionRequests)
	}

	// check a wrapped transport is set for per-request minting
	if restConfig.WrapTransport == nil {
		t.Error("expected the rest config to mint a token per request through a wrapped transport")
	}

	// check the stored client certificate is unused
	if len(restConfig.TLSClientConfig.CertData) > 0 {
		t.Errorf("expected no client certificate on the rest config, got %s", restConfig.TLSClientConfig.CertData)
	}
	if len(restConfig.TLSClientConfig.KeyData) > 0 {
		t.Error("expected no client key on the rest config")
	}

	// check no static bearer token is stored on the config
	if restConfig.BearerToken != "" {
		t.Errorf("expected no static bearer token on the rest config, got %s", restConfig.BearerToken)
	}

	// check the runtime's CA certificate is carried over
	if got := string(restConfig.TLSClientConfig.CAData); got != "kube-ca-cert" {
		t.Errorf("expected the runtime's CA certificate to be carried over, got %s", got)
	}
}

// TestGetRestConfigUsesCertificateWithoutTokenMintingProvider covers a kind
// runtime authenticating with its stored client certificate.
func TestGetRestConfigUsesCertificateWithoutTokenMintingProvider(t *testing.T) {
	// stand up a kind threeport API stub
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		infraProvider := v0.KubernetesRuntimeInfraProviderKind
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(apiserver_lib.Response{
			Data: []apiserver_lib.Object{
				v0.KubernetesRuntimeDefinition{InfraProvider: &infraProvider},
			},
		}); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	// pass an empty encryption key so the stored certificate key is used as-is
	restConfig, err := GetRestConfig(
		testTokenMintingRuntime(),
		false,
		&http.Client{},
		strings.TrimPrefix(server.URL, "http://"),
		"",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// check no per-request token minting is configured
	if restConfig.WrapTransport != nil {
		t.Error("expected no per-request token minting for a provider that has none")
	}

	// check the stored client certificate is used
	if got := string(restConfig.TLSClientConfig.CertData); got != "kube-client-cert" {
		t.Errorf("expected the client certificate to authenticate, got %s", got)
	}
	if got := string(restConfig.TLSClientConfig.KeyData); got != "kube-client-key" {
		t.Errorf("expected the client key to authenticate, got %s", got)
	}
}

// TestGetRestConfigRefusesRuntimeWithNoCredential covers a runtime with no
// definition ID, certificate, or connection token being refused.
func TestGetRestConfigRefusesRuntimeWithNoCredential(t *testing.T) {
	// drop the definition ID and client certificate; the fixture has no token
	runtime := testTokenMintingRuntime()
	runtime.KubernetesRuntimeDefinitionID = nil
	runtime.Certificate = nil
	runtime.CertificateKey = nil

	// ask for a rest config
	_, err := GetRestConfig(runtime, false, &http.Client{}, "", "")

	// check the runtime is refused
	if err == nil {
		t.Fatal("expected a runtime with no credential to be refused, got nil error")
	}
	if !strings.Contains(err.Error(), "no way to authenticate") {
		t.Errorf("expected the error to say the record cannot authenticate, got: %v", err)
	}
}
