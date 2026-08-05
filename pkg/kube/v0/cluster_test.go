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

// testEncryptionKey is a 32 byte AES key in the base64 form the encryption
// helpers expect. The credentials it protects in these tests are fabricated.
var testEncryptionKey = util.Base64Encode("0123456789abcdef0123456789abcdef")

// okeRuntimeApiServer is a threeport API stand-in for the three lookups that
// building an OKE token generator makes: the runtime's definition, the OKE
// runtime instance behind it, and the OCI provider holding the credentials the
// token is signed with.
type okeRuntimeApiServer struct {
	definitionRequests int
}

// serve starts the stand-in and returns the client and address to reach it at.
// The address carries no scheme because GetResponse prepends one itself.
func (s *okeRuntimeApiServer) serve(t *testing.T) (*http.Client, string) {
	t.Helper()

	privateKey, err := encryption.Encrypt(testEncryptionKey, "oci-api-signing-key")
	if err != nil {
		t.Fatalf("failed to encrypt the test provider key: %v", err)
	}

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

	return &http.Client{}, strings.TrimPrefix(server.URL, "http://")
}

// write sends a threeport API response carrying the supplied objects.
func (s *okeRuntimeApiServer) write(t *testing.T, w http.ResponseWriter, data []apiserver_lib.Object) {
	t.Helper()

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(apiserver_lib.Response{Data: data}); err != nil {
		t.Errorf("failed to encode response: %v", err)
	}
}

// testTokenMintingRuntime returns a kubernetes runtime instance for a cluster
// on a provider that mints kube API tokens per request, carrying a client
// certificate and key as well. A real record on such a provider carries no
// certificate, but a rebuilt one can, and the certificate must not be what
// the client authenticates with.
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

// TestGetRestConfigMintsTokenInsteadOfUsingCertificate asserts that a runtime
// on a token-minting provider authenticates with a per-request token, and that
// a certificate stored on the record does not divert it onto the certificate
// path.
//
// This is the branch a local cluster can never reach. Kind records a client
// certificate and no infra provider that mints, so it takes the certificate
// path every time, and a green run against it says nothing about whether the
// minting path still works.
func TestGetRestConfigMintsTokenInsteadOfUsingCertificate(t *testing.T) {
	apiServer := &okeRuntimeApiServer{}
	apiClient, apiAddr := apiServer.serve(t)

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

	// the provider is read off the runtime's definition, which is what
	// selects the minting path in the first place
	if apiServer.definitionRequests != 1 {
		t.Errorf("expected the runtime definition to be looked up once, got %d", apiServer.definitionRequests)
	}

	// a per-request token is injected by the transport, not carried on
	// the config, so a config with no transport wrapper is not minting
	if restConfig.WrapTransport == nil {
		t.Error("expected the rest config to mint a token per request through a wrapped transport")
	}

	// the certificate on the record must not be what authenticates:
	// each caller has to reach the kube API as its own identity
	if len(restConfig.TLSClientConfig.CertData) > 0 {
		t.Errorf("expected no client certificate on the rest config, got %s", restConfig.TLSClientConfig.CertData)
	}
	if len(restConfig.TLSClientConfig.KeyData) > 0 {
		t.Error("expected no client key on the rest config")
	}

	// a single bearer token baked into the config is the other way to
	// get this wrong: it would authenticate every caller as one identity
	if restConfig.BearerToken != "" {
		t.Errorf("expected no static bearer token on the rest config, got %s", restConfig.BearerToken)
	}

	if got := string(restConfig.TLSClientConfig.CAData); got != "kube-ca-cert" {
		t.Errorf("expected the runtime's CA certificate to be carried over, got %s", got)
	}
}

// TestGetRestConfigUsesCertificateWithoutTokenMintingProvider asserts that a
// runtime whose definition names no minting provider still authenticates with
// its client certificate. This is the path a local cluster takes, and it is
// the one that must not change when the minting path does.
func TestGetRestConfigUsesCertificateWithoutTokenMintingProvider(t *testing.T) {
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

	if restConfig.WrapTransport != nil {
		t.Error("expected no per-request token minting for a provider that has none")
	}
	if got := string(restConfig.TLSClientConfig.CertData); got != "kube-client-cert" {
		t.Errorf("expected the client certificate to authenticate, got %s", got)
	}
	if got := string(restConfig.TLSClientConfig.KeyData); got != "kube-client-key" {
		t.Errorf("expected the client key to authenticate, got %s", got)
	}
}

// TestGetRestConfigRefusesRuntimeWithNoCredential asserts that a runtime
// carrying no certificate pair, no token, and no minting provider is refused
// rather than returned as a config that cannot authenticate. Anything that
// writes a runtime record without one of the three fails here.
func TestGetRestConfigRefusesRuntimeWithNoCredential(t *testing.T) {
	runtime := testTokenMintingRuntime()
	runtime.KubernetesRuntimeDefinitionID = nil
	runtime.Certificate = nil
	runtime.CertificateKey = nil

	_, err := GetRestConfig(runtime, false, &http.Client{}, "", "")
	if err == nil {
		t.Fatal("expected a runtime with no credential to be refused, got nil error")
	}
	if !strings.Contains(err.Error(), "no way to authenticate") {
		t.Errorf("expected the error to say the record cannot authenticate, got: %v", err)
	}
}
