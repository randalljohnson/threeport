package v0

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	threeport "github.com/threeport/threeport/pkg/threeport-installer/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// testControlPlaneConfig returns a threeport config entry for a kind-hosted
// control plane, holding the base64 encoded kube connection info and
// credentials a genesis install records.
func testControlPlaneConfig(name string) *ControlPlane {
	return &ControlPlane{
		Name:        name,
		AuthEnabled: true,
		Genesis:     true,
		APIServer:   "localhost:1323",
		CACert:      util.Base64Encode("threeport-ca-cert"),
		Provider:    v0.KubernetesRuntimeInfraProviderKind,
		KubeAPI: KubeAPI{
			APIEndpoint:   "https://127.0.0.1:6443",
			CACertificate: util.Base64Encode("kube-ca-cert"),
			Certificate:   util.Base64Encode("kube-client-cert"),
			Key:           util.Base64Encode("kube-client-key"),
		},
		Credentials: []Credential{
			{
				Name:       name,
				ClientCert: util.Base64Encode("threeport-client-cert"),
				ClientKey:  util.Base64Encode("threeport-client-key"),
			},
		},
	}
}

// testCloudControlPlaneConfig returns a threeport config entry for a control
// plane hosted on a cloud provider. A cloud install authenticates to the kube
// API with a bearer token, so it records no client certificate or key, and the
// token it does record goes stale about an hour later with no expiration
// written beside it.
func testCloudControlPlaneConfig(name string, infraProvider string) *ControlPlane {
	controlPlaneConfig := testControlPlaneConfig(name)
	controlPlaneConfig.Provider = infraProvider
	controlPlaneConfig.KubeAPI.APIEndpoint = "https://cloud-kube-api.example.com"
	controlPlaneConfig.KubeAPI.Certificate = ""
	controlPlaneConfig.KubeAPI.Key = ""
	controlPlaneConfig.KubeAPI.Token = util.Base64Encode("install-time-token")

	return controlPlaneConfig
}

// TestBootstrapKubernetesRuntimeInstance asserts the kubernetes runtime
// instance is rebuilt from the threeport config with the connection info
// decoded and the flags that make it discoverable as the default runtime and
// the control plane's host. A local cluster authenticates with a client
// certificate, which the rebuild carries over.
func TestBootstrapKubernetesRuntimeInstance(t *testing.T) {
	controlPlaneConfig := testControlPlaneConfig("dev-0")

	kubernetesRuntimeInstance, err := bootstrapKubernetesRuntimeInstance(controlPlaneConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := *kubernetesRuntimeInstance.Name; got != "threeport-dev-0" {
		t.Errorf("expected name threeport-dev-0, got %s", got)
	}
	if got := *kubernetesRuntimeInstance.APIEndpoint; got != "https://127.0.0.1:6443" {
		t.Errorf("expected API endpoint https://127.0.0.1:6443, got %s", got)
	}
	if got := *kubernetesRuntimeInstance.CACertificate; got != "kube-ca-cert" {
		t.Errorf("expected decoded CA certificate kube-ca-cert, got %s", got)
	}
	if got := *kubernetesRuntimeInstance.Certificate; got != "kube-client-cert" {
		t.Errorf("expected decoded certificate kube-client-cert, got %s", got)
	}
	if got := *kubernetesRuntimeInstance.CertificateKey; got != "kube-client-key" {
		t.Errorf("expected decoded certificate key kube-client-key, got %s", got)
	}
	// the certificate pair authenticates on its own, so the token is not
	// carried over beside it
	if kubernetesRuntimeInstance.ConnectionToken != nil {
		t.Errorf("expected no connection token, got %s", *kubernetesRuntimeInstance.ConnectionToken)
	}
	if got := *kubernetesRuntimeInstance.Location; got != "Local" {
		t.Errorf("expected location Local, got %s", got)
	}
	if !*kubernetesRuntimeInstance.DefaultRuntime {
		t.Error("expected the runtime to be marked as the default runtime")
	}
	if !*kubernetesRuntimeInstance.ThreeportControlPlaneHost {
		t.Error("expected the runtime to be marked as the control plane host")
	}
	if !*kubernetesRuntimeInstance.Reconciled {
		t.Error("expected the runtime to be marked reconciled")
	}
}

// TestBootstrapKubernetesRuntimeInstanceOnTokenMintingProviders asserts that a
// control plane on GKE or OKE is rebuilt even though its threeport config holds
// no client certificate or key, and that the install-time token is written to
// the record in their place. The running control plane mints a token per
// request from its own infra provider and never reads this one, but a client
// that does not know how to mint looks here, and a record carrying no
// credential at all fails in a way that reads as a broken restore. The rebuild
// also has no location to work from, since the config never records the one the
// install derived from its region.
func TestBootstrapKubernetesRuntimeInstanceOnTokenMintingProviders(t *testing.T) {
	tests := []struct {
		name     string
		provider string
	}{
		{
			name:     "gke control plane is rebuilt with no kube API credential",
			provider: v0.KubernetesRuntimeInfraProviderGKE,
		},
		{
			name:     "oke control plane is rebuilt with no kube API credential",
			provider: v0.KubernetesRuntimeInfraProviderOKE,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controlPlaneConfig := testCloudControlPlaneConfig("dev-0", test.provider)

			kubernetesRuntimeInstance, err := bootstrapKubernetesRuntimeInstance(controlPlaneConfig)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := *kubernetesRuntimeInstance.Name; got != "threeport-dev-0" {
				t.Errorf("expected name threeport-dev-0, got %s", got)
			}
			if got := *kubernetesRuntimeInstance.APIEndpoint; got != "https://cloud-kube-api.example.com" {
				t.Errorf("expected API endpoint https://cloud-kube-api.example.com, got %s", got)
			}
			if got := *kubernetesRuntimeInstance.CACertificate; got != "kube-ca-cert" {
				t.Errorf("expected decoded CA certificate kube-ca-cert, got %s", got)
			}
			if got := *kubernetesRuntimeInstance.Location; got != localRuntimeLocation {
				t.Errorf("expected location %s, got %s", localRuntimeLocation, got)
			}
			if kubernetesRuntimeInstance.Certificate != nil {
				t.Errorf("expected no client certificate, got %s", *kubernetesRuntimeInstance.Certificate)
			}
			if kubernetesRuntimeInstance.CertificateKey != nil {
				t.Errorf("expected no client certificate key, got %s", *kubernetesRuntimeInstance.CertificateKey)
			}
			// the token is decoded on the way through, like every other
			// credential the config holds base64 encoded
			if kubernetesRuntimeInstance.ConnectionToken == nil {
				t.Fatal("expected the install-time token to be carried over as the fallback credential")
			}
			if got := *kubernetesRuntimeInstance.ConnectionToken; got != "install-time-token" {
				t.Errorf("expected decoded connection token install-time-token, got %s", got)
			}
			// nothing may treat the carried-over token as refreshable,
			// since the config records no expiration to refresh against
			if kubernetesRuntimeInstance.ConnectionTokenExpiration != nil {
				t.Errorf("expected no connection token expiration, got %v", *kubernetesRuntimeInstance.ConnectionTokenExpiration)
			}
			if !*kubernetesRuntimeInstance.DefaultRuntime {
				t.Error("expected the runtime to be marked as the default runtime")
			}
			if !*kubernetesRuntimeInstance.ThreeportControlPlaneHost {
				t.Error("expected the runtime to be marked as the control plane host")
			}
		})
	}
}

// TestBootstrapKubernetesRuntimeInstanceRefusesEks asserts the rebuild refuses
// an EKS control plane rather than registering a runtime with no way to reach
// its kube API. EKS is the one provider that authenticates with the token
// stored on the runtime record, and the only copy the threeport config holds
// expired about an hour after the install.
func TestBootstrapKubernetesRuntimeInstanceRefusesEks(t *testing.T) {
	controlPlaneConfig := testCloudControlPlaneConfig("dev-0", v0.KubernetesRuntimeInfraProviderEKS)

	_, err := bootstrapKubernetesRuntimeInstance(controlPlaneConfig)
	if err == nil {
		t.Fatal("expected an error, got none")
	}
	if !strings.Contains(err.Error(), v0.KubernetesRuntimeInfraProviderEKS) {
		t.Errorf("expected error to name the provider %q, got %q", v0.KubernetesRuntimeInfraProviderEKS, err.Error())
	}
}

// TestBootstrapKubernetesRuntimeInstanceOmitsHalfCertificatePair asserts that a
// certificate without its key, or the reverse, is left off the rebuilt record
// entirely. Half a pair authenticates to nothing, and registering it would
// send the kube client down the certificate path with a credential that cannot
// complete a handshake. A config that holds a token falls back to it, since
// half a pair is no better than none.
func TestBootstrapKubernetesRuntimeInstanceOmitsHalfCertificatePair(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*ControlPlane)
		wantToken bool
	}{
		{
			name:   "certificate without a key is omitted",
			mutate: func(c *ControlPlane) { c.KubeAPI.Key = "" },
		},
		{
			name:   "key without a certificate is omitted",
			mutate: func(c *ControlPlane) { c.KubeAPI.Certificate = "" },
		},
		{
			name: "half a pair falls back to the token",
			mutate: func(c *ControlPlane) {
				c.KubeAPI.Key = ""
				c.KubeAPI.Token = util.Base64Encode("install-time-token")
			},
			wantToken: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controlPlaneConfig := testControlPlaneConfig("dev-0")
			test.mutate(controlPlaneConfig)

			kubernetesRuntimeInstance, err := bootstrapKubernetesRuntimeInstance(controlPlaneConfig)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if kubernetesRuntimeInstance.Certificate != nil {
				t.Errorf("expected no client certificate, got %s", *kubernetesRuntimeInstance.Certificate)
			}
			if kubernetesRuntimeInstance.CertificateKey != nil {
				t.Errorf("expected no client certificate key, got %s", *kubernetesRuntimeInstance.CertificateKey)
			}

			if !test.wantToken {
				if kubernetesRuntimeInstance.ConnectionToken != nil {
					t.Errorf("expected no connection token, got %s", *kubernetesRuntimeInstance.ConnectionToken)
				}

				return
			}

			if kubernetesRuntimeInstance.ConnectionToken == nil {
				t.Fatal("expected the token to be used when the certificate pair is incomplete")
			}
			if got := *kubernetesRuntimeInstance.ConnectionToken; got != "install-time-token" {
				t.Errorf("expected decoded connection token install-time-token, got %s", got)
			}
		})
	}
}

// TestValidateCreateGenesisControlPlaneFlagsTier asserts that both recognized
// tiers are accepted on any provider, so a control plane on a cloud provider
// can be installed as a development one, and that anything else is refused with
// a message naming the values that are allowed. A typo that slipped through
// would produce a control plane the database drop treats as untrusted.
func TestValidateCreateGenesisControlPlaneFlagsTier(t *testing.T) {
	tests := []struct {
		name          string
		tier          string
		infraProvider string
		wantErr       bool
	}{
		{
			name:          "development tier on a cloud provider is accepted",
			tier:          threeport.ControlPlaneTierDev,
			infraProvider: v0.KubernetesRuntimeInfraProviderEKS,
			wantErr:       false,
		},
		{
			name:          "production tier on a cloud provider is accepted",
			tier:          threeport.ControlPlaneTierProd,
			infraProvider: v0.KubernetesRuntimeInfraProviderEKS,
			wantErr:       false,
		},
		{
			name:          "development tier on the local provider is accepted",
			tier:          threeport.ControlPlaneTierDev,
			infraProvider: v0.KubernetesRuntimeInfraProviderKind,
			wantErr:       false,
		},
		{
			name:          "unrecognized tier is refused",
			tier:          "staging",
			infraProvider: v0.KubernetesRuntimeInfraProviderEKS,
			wantErr:       true,
		},
		{
			name:          "misspelled tier is refused",
			tier:          "developement",
			infraProvider: v0.KubernetesRuntimeInfraProviderKind,
			wantErr:       true,
		},
		{
			name:          "empty tier is refused",
			tier:          "",
			infraProvider: v0.KubernetesRuntimeInfraProviderKind,
			wantErr:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateCreateGenesisControlPlaneFlags(
				"dev-0",
				test.infraProvider,
				test.tier,
				"",
				true,
				nil,
				false,
				"",
			)

			if !test.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			if err == nil {
				t.Fatal("expected an error, got none")
			}
			// the message has to name the values the caller may use,
			// since the tier is not otherwise discoverable from the error
			for _, want := range []string{
				test.tier,
				threeport.ControlPlaneTierDev,
				threeport.ControlPlaneTierProd,
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("expected error to name %q, got %q", want, err.Error())
				}
			}
		})
	}
}

// controlPlaneApiServer is a threeport API stand-in for the control plane
// definition and instance endpoints. It answers each lookup with whatever the
// caller seeded and records the objects posted back to it.
type controlPlaneApiServer struct {
	definitionFound bool
	instanceFound   bool
	definitionsPost int
	instancesPost   int
	postedInstance  v0.ControlPlaneInstance
}

// serve starts the stand-in and returns the client and address to reach it at.
// The address carries no scheme because GetResponse prepends one itself.
func (s *controlPlaneApiServer) serve(t *testing.T) (*http.Client, string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		definitionId := uint(7)
		instanceId := uint(9)

		switch {
		case r.Method == http.MethodGet && r.URL.Path == v0.PathControlPlaneDefinitions:
			data := []apiserver_lib.Object{}
			if s.definitionFound {
				data = append(data, v0.ControlPlaneDefinition{Common: v0.Common{ID: &definitionId}})
			}
			s.write(t, w, http.StatusOK, data)
		case r.Method == http.MethodGet && r.URL.Path == v0.PathControlPlaneInstances:
			data := []apiserver_lib.Object{}
			if s.instanceFound {
				data = append(data, v0.ControlPlaneInstance{Common: v0.Common{ID: &instanceId}})
			}
			s.write(t, w, http.StatusOK, data)
		case r.Method == http.MethodPost && r.URL.Path == v0.PathControlPlaneDefinitions:
			s.definitionsPost++
			s.write(t, w, http.StatusCreated, []apiserver_lib.Object{
				v0.ControlPlaneDefinition{Common: v0.Common{ID: &definitionId}},
			})
		case r.Method == http.MethodPost && r.URL.Path == v0.PathControlPlaneInstances:
			s.instancesPost++
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("failed to read request body: %v", err)
			}
			if err := json.Unmarshal(body, &s.postedInstance); err != nil {
				t.Errorf("failed to unmarshal posted control plane instance: %v", err)
			}
			s.write(t, w, http.StatusCreated, []apiserver_lib.Object{
				v0.ControlPlaneInstance{Common: v0.Common{ID: &instanceId}},
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
func (s *controlPlaneApiServer) write(t *testing.T, w http.ResponseWriter, status int, data []apiserver_lib.Object) {
	t.Helper()

	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(apiserver_lib.Response{Data: data}); err != nil {
		t.Errorf("failed to encode response: %v", err)
	}
}

// testInstaller returns an installer configured the way the reinstall
// configures it before the bootstrap objects are restored.
func testInstaller(name string) *threeport.ControlPlaneInstaller {
	cpi := threeport.NewInstaller()
	cpi.Opts.ControlPlaneName = name
	cpi.Opts.Namespace = threeport.ControlPlaneNamespace
	cpi.Opts.AuthEnabled = true

	return cpi
}

// TestEnsureBootstrapControlPlaneCreatesMissingObjects asserts that a control
// plane whose records the database drop removed gets a definition and a
// self-marked instance pointing at the runtime it is installed on.
func TestEnsureBootstrapControlPlaneCreatesMissingObjects(t *testing.T) {
	apiServer := &controlPlaneApiServer{}
	apiClient, apiAddr := apiServer.serve(t)

	controlPlaneConfig := testControlPlaneConfig("dev-0")
	controlPlaneConfig.APIServer = apiAddr
	kubernetesRuntimeInstanceId := uint(4)

	if err := ensureBootstrapControlPlane(
		apiClient,
		testInstaller("dev-0"),
		controlPlaneConfig,
		&kubernetesRuntimeInstanceId,
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if apiServer.definitionsPost != 1 {
		t.Errorf("expected 1 control plane definition created, got %d", apiServer.definitionsPost)
	}
	if apiServer.instancesPost != 1 {
		t.Errorf("expected 1 control plane instance created, got %d", apiServer.instancesPost)
	}

	instance := apiServer.postedInstance
	if instance.IsSelf == nil || !*instance.IsSelf {
		t.Error("expected the control plane instance to be marked as self")
	}
	if instance.Genesis == nil || !*instance.Genesis {
		t.Error("expected the control plane instance to carry the genesis flag from the config")
	}
	if instance.KubernetesRuntimeInstanceID == nil || *instance.KubernetesRuntimeInstanceID != kubernetesRuntimeInstanceId {
		t.Errorf("expected kubernetes runtime instance id %d, got %v", kubernetesRuntimeInstanceId, instance.KubernetesRuntimeInstanceID)
	}
	if instance.ControlPlaneDefinitionID == nil || *instance.ControlPlaneDefinitionID != 7 {
		t.Errorf("expected control plane definition id 7, got %v", instance.ControlPlaneDefinitionID)
	}
	if instance.Namespace == nil || *instance.Namespace != threeport.ControlPlaneNamespace {
		t.Errorf("expected namespace %s, got %v", threeport.ControlPlaneNamespace, instance.Namespace)
	}
	if instance.CACert == nil || *instance.CACert != controlPlaneConfig.CACert {
		t.Error("expected the CA cert to carry over from the config unchanged")
	}
	if instance.ClientCert == nil || *instance.ClientCert != controlPlaneConfig.Credentials[0].ClientCert {
		t.Error("expected the client cert to carry over from the config unchanged")
	}
	if instance.ClientKey == nil || *instance.ClientKey != controlPlaneConfig.Credentials[0].ClientKey {
		t.Error("expected the client key to carry over from the config unchanged")
	}
	if len(instance.CustomComponentInfo) < 2 {
		t.Errorf("expected the component list to include the rest api and agent, got %d components", len(instance.CustomComponentInfo))
	}
}

// TestEnsureBootstrapControlPlaneSkipsExistingObjects asserts that records
// already present are left alone, so a reinstall without a database drop and a
// repeat run create nothing.
func TestEnsureBootstrapControlPlaneSkipsExistingObjects(t *testing.T) {
	tests := []struct {
		name            string
		definitionFound bool
		instanceFound   bool
		definitionsPost int
		instancesPost   int
	}{
		{
			name:            "existing definition is reused",
			definitionFound: true,
			instanceFound:   false,
			definitionsPost: 0,
			instancesPost:   1,
		},
		{
			name:            "existing definition and instance create nothing",
			definitionFound: true,
			instanceFound:   true,
			definitionsPost: 0,
			instancesPost:   0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			apiServer := &controlPlaneApiServer{
				definitionFound: test.definitionFound,
				instanceFound:   test.instanceFound,
			}
			apiClient, apiAddr := apiServer.serve(t)

			controlPlaneConfig := testControlPlaneConfig("dev-0")
			controlPlaneConfig.APIServer = apiAddr
			kubernetesRuntimeInstanceId := uint(4)

			if err := ensureBootstrapControlPlane(
				apiClient,
				testInstaller("dev-0"),
				controlPlaneConfig,
				&kubernetesRuntimeInstanceId,
			); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if apiServer.definitionsPost != test.definitionsPost {
				t.Errorf("expected %d control plane definitions created, got %d", test.definitionsPost, apiServer.definitionsPost)
			}
			if apiServer.instancesPost != test.instancesPost {
				t.Errorf("expected %d control plane instances created, got %d", test.instancesPost, apiServer.instancesPost)
			}
		})
	}
}
