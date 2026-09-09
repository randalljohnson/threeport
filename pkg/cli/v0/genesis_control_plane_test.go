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

// testControlPlaneConfig returns a genesis kind control plane config
// with the kube API certificate pair a genesis install records.
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

// testCloudControlPlaneConfig returns a cloud-provider control plane
// config with a token and no certificate pair.
func testCloudControlPlaneConfig(name string, infraProvider string) *ControlPlane {
	controlPlaneConfig := testControlPlaneConfig(name)
	controlPlaneConfig.Provider = infraProvider
	controlPlaneConfig.KubeAPI.APIEndpoint = "https://cloud-kube-api.example.com"
	controlPlaneConfig.KubeAPI.Certificate = ""
	controlPlaneConfig.KubeAPI.Key = ""
	controlPlaneConfig.KubeAPI.Token = util.Base64Encode("install-time-token")

	return controlPlaneConfig
}

// TestBootstrapKubernetesRuntimeInstance covers a kind runtime instance
// built from a genesis config with a complete kube API certificate pair.
func TestBootstrapKubernetesRuntimeInstance(t *testing.T) {
	// build a local genesis config with a complete certificate pair
	controlPlaneConfig := testControlPlaneConfig("dev-0")

	// build the kubernetes runtime instance from that config
	kubernetesRuntimeInstance, err := bootstrapKubernetesRuntimeInstance(controlPlaneConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// check the threeport-prefixed name and kube API endpoint
	if got := *kubernetesRuntimeInstance.Name; got != "threeport-dev-0" {
		t.Errorf("expected name threeport-dev-0, got %s", got)
	}
	if got := *kubernetesRuntimeInstance.APIEndpoint; got != "https://127.0.0.1:6443" {
		t.Errorf("expected API endpoint https://127.0.0.1:6443, got %s", got)
	}

	// check the decoded certificate pair without a token
	if got := *kubernetesRuntimeInstance.CACertificate; got != "kube-ca-cert" {
		t.Errorf("expected decoded CA certificate kube-ca-cert, got %s", got)
	}
	if got := *kubernetesRuntimeInstance.Certificate; got != "kube-client-cert" {
		t.Errorf("expected decoded certificate kube-client-cert, got %s", got)
	}
	if got := *kubernetesRuntimeInstance.CertificateKey; got != "kube-client-key" {
		t.Errorf("expected decoded certificate key kube-client-key, got %s", got)
	}
	if kubernetesRuntimeInstance.ConnectionToken != nil {
		t.Errorf("expected no connection token, got %s", *kubernetesRuntimeInstance.ConnectionToken)
	}

	// check location Local and the default, host, and reconciled flags
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

// TestBootstrapKubernetesRuntimeInstanceOnTokenMintingProviders covers a
// GKE runtime instance built from a token and no certificate pair.
func TestBootstrapKubernetesRuntimeInstanceOnTokenMintingProviders(t *testing.T) {
	tests := []struct {
		name     string
		provider string
	}{
		{
			name:     "gke control plane is rebuilt with no kube API credential",
			provider: v0.KubernetesRuntimeInfraProviderGKE,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// build a cloud config with a token and no certificate pair
			controlPlaneConfig := testCloudControlPlaneConfig("dev-0", test.provider)

			// build the kubernetes runtime instance from that config
			kubernetesRuntimeInstance, err := bootstrapKubernetesRuntimeInstance(controlPlaneConfig)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// check the threeport-prefixed name, endpoint, and CA
			if got := *kubernetesRuntimeInstance.Name; got != "threeport-dev-0" {
				t.Errorf("expected name threeport-dev-0, got %s", got)
			}
			if got := *kubernetesRuntimeInstance.APIEndpoint; got != "https://cloud-kube-api.example.com" {
				t.Errorf("expected API endpoint https://cloud-kube-api.example.com, got %s", got)
			}
			if got := *kubernetesRuntimeInstance.CACertificate; got != "kube-ca-cert" {
				t.Errorf("expected decoded CA certificate kube-ca-cert, got %s", got)
			}

			// check the location is Local; the config never stores the install-time location
			if got := *kubernetesRuntimeInstance.Location; got != localRuntimeLocation {
				t.Errorf("expected location %s, got %s", localRuntimeLocation, got)
			}

			// omit the certificate pair
			if kubernetesRuntimeInstance.Certificate != nil {
				t.Errorf("expected no client certificate, got %s", *kubernetesRuntimeInstance.Certificate)
			}
			if kubernetesRuntimeInstance.CertificateKey != nil {
				t.Errorf("expected no client certificate key, got %s", *kubernetesRuntimeInstance.CertificateKey)
			}

			// carry over the install-time token
			if kubernetesRuntimeInstance.ConnectionToken == nil {
				t.Fatal("expected the install-time token to be carried over as the fallback credential")
			}
			if got := *kubernetesRuntimeInstance.ConnectionToken; got != "install-time-token" {
				t.Errorf("expected decoded connection token install-time-token, got %s", got)
			}

			// leave expiration unset so nothing treats the token as refreshable
			if kubernetesRuntimeInstance.ConnectionTokenExpiration != nil {
				t.Errorf("expected no connection token expiration, got %v", *kubernetesRuntimeInstance.ConnectionTokenExpiration)
			}

			// check the runtime flags: default and control plane host
			if !*kubernetesRuntimeInstance.DefaultRuntime {
				t.Error("expected the runtime to be marked as the default runtime")
			}
			if !*kubernetesRuntimeInstance.ThreeportControlPlaneHost {
				t.Error("expected the runtime to be marked as the control plane host")
			}
		})
	}
}

// TestBootstrapKubernetesRuntimeInstanceRefusesEks covers refusing an
// EKS rebuild whose stored kube API token the threeport config cannot supply.
func TestBootstrapKubernetesRuntimeInstanceRefusesEks(t *testing.T) {
	// build an EKS config with a token and no certificate pair
	controlPlaneConfig := testCloudControlPlaneConfig("dev-0", v0.KubernetesRuntimeInfraProviderEKS)

	// refuse to build the kubernetes runtime instance
	_, err := bootstrapKubernetesRuntimeInstance(controlPlaneConfig)
	if err == nil {
		t.Fatal("expected an error, got none")
	}

	// check the error names the EKS provider
	if !strings.Contains(err.Error(), v0.KubernetesRuntimeInfraProviderEKS) {
		t.Errorf("expected error to name the provider %q, got %q", v0.KubernetesRuntimeInfraProviderEKS, err.Error())
	}
}

// TestBootstrapKubernetesRuntimeInstanceRefusesOke covers refusing an
// OKE rebuild whose token minting depends on provider rows a drop removes.
func TestBootstrapKubernetesRuntimeInstanceRefusesOke(t *testing.T) {
	controlPlaneConfig := testCloudControlPlaneConfig("dev-0", v0.KubernetesRuntimeInfraProviderOKE)

	_, err := bootstrapKubernetesRuntimeInstance(controlPlaneConfig)
	if err == nil {
		t.Fatal("expected an error, got none")
	}

	if !strings.Contains(err.Error(), v0.KubernetesRuntimeInfraProviderOKE) {
		t.Errorf("expected error to name the provider %q, got %q", v0.KubernetesRuntimeInfraProviderOKE, err.Error())
	}
}

// TestBootstrapKubernetesRuntimeInstanceOmitsHalfCertificatePair covers
// dropping an incomplete certificate pair and using a token when present.
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
			// apply the incomplete-pair mutation to a local genesis config
			controlPlaneConfig := testControlPlaneConfig("dev-0")
			test.mutate(controlPlaneConfig)

			// build the kubernetes runtime instance from that config
			kubernetesRuntimeInstance, err := bootstrapKubernetesRuntimeInstance(controlPlaneConfig)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// omit the incomplete pair so kube skips the certificate path
			if kubernetesRuntimeInstance.Certificate != nil {
				t.Errorf("expected no client certificate, got %s", *kubernetesRuntimeInstance.Certificate)
			}
			if kubernetesRuntimeInstance.CertificateKey != nil {
				t.Errorf("expected no client certificate key, got %s", *kubernetesRuntimeInstance.CertificateKey)
			}

			if !test.wantToken {
				// check no token when none was provided
				if kubernetesRuntimeInstance.ConnectionToken != nil {
					t.Errorf("expected no connection token, got %s", *kubernetesRuntimeInstance.ConnectionToken)
				}

				return
			}

			// check the token when one was provided
			if kubernetesRuntimeInstance.ConnectionToken == nil {
				t.Fatal("expected the token to be used when the certificate pair is incomplete")
			}
			if got := *kubernetesRuntimeInstance.ConnectionToken; got != "install-time-token" {
				t.Errorf("expected decoded connection token install-time-token, got %s", got)
			}
		})
	}
}

// TestValidateCreateGenesisControlPlaneFlagsTier covers accepted
// development and production tiers and refused unknown values.
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
			// validate the control plane tier
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

			// check the error names the refused tier plus development and production
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

// controlPlaneApiServer is a stub threeport API that answers lookups
// with whatever the test seeded and records posted objects.
type controlPlaneApiServer struct {
	definitionFound bool
	instanceFound   bool
	definitionsPost int
	instancesPost   int
	postedInstance  v0.ControlPlaneInstance
}

// serve starts a stub threeport API and returns a client plus a
// scheme-less address because the threeport client prepends a scheme itself.
func (s *controlPlaneApiServer) serve(t *testing.T) (*http.Client, string) {
	t.Helper()

	// start a stub threeport API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		definitionId := uint(7)
		instanceId := uint(9)

		switch {
		case r.Method == http.MethodGet && r.URL.Path == v0.PathControlPlaneDefinitions:
			// return an existing definition when found
			data := []apiserver_lib.Object{}
			if s.definitionFound {
				data = append(data, v0.ControlPlaneDefinition{Common: v0.Common{ID: &definitionId}})
			}
			s.write(t, w, http.StatusOK, data)
		case r.Method == http.MethodGet && r.URL.Path == v0.PathControlPlaneInstances:
			// return an existing instance when found
			data := []apiserver_lib.Object{}
			if s.instanceFound {
				data = append(data, v0.ControlPlaneInstance{Common: v0.Common{ID: &instanceId}})
			}
			s.write(t, w, http.StatusOK, data)
		case r.Method == http.MethodPost && r.URL.Path == v0.PathControlPlaneDefinitions:
			// record the definition create
			s.definitionsPost++
			s.write(t, w, http.StatusCreated, []apiserver_lib.Object{
				v0.ControlPlaneDefinition{Common: v0.Common{ID: &definitionId}},
			})
		case r.Method == http.MethodPost && r.URL.Path == v0.PathControlPlaneInstances:
			// record the instance create and capture the posted body
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

	// close the stub when the test ends
	t.Cleanup(server.Close)

	// return a vanilla HTTP client and a scheme-less address
	return &http.Client{}, strings.TrimPrefix(server.URL, "http://")
}

// write encodes a threeport API response envelope at status.
func (s *controlPlaneApiServer) write(t *testing.T, w http.ResponseWriter, status int, data []apiserver_lib.Object) {
	t.Helper()

	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(apiserver_lib.Response{Data: data}); err != nil {
		t.Errorf("failed to encode response: %v", err)
	}
}

// testInstaller returns a named control plane installer with auth enabled
// so a bootstrap restore copies the config certs onto the new instance.
func testInstaller(name string) *threeport.ControlPlaneInstaller {
	cpi := threeport.NewInstaller()
	cpi.Opts.ControlPlaneName = name
	cpi.Opts.Namespace = threeport.ControlPlaneNamespace
	cpi.Opts.AuthEnabled = true

	return cpi
}

// TestEnsureBootstrapControlPlaneCreatesMissingObjects covers creating
// both control plane objects after a database drop removed them.
func TestEnsureBootstrapControlPlaneCreatesMissingObjects(t *testing.T) {
	// start a stub threeport API with no existing objects
	apiServer := &controlPlaneApiServer{}
	apiClient, apiAddr := apiServer.serve(t)

	// point a genesis config at the stub
	controlPlaneConfig := testControlPlaneConfig("dev-0")
	controlPlaneConfig.APIServer = apiAddr
	kubernetesRuntimeInstanceId := uint(4)

	// create the missing control plane definition and instance
	if err := ensureBootstrapControlPlane(
		apiClient,
		testInstaller("dev-0"),
		controlPlaneConfig,
		&kubernetesRuntimeInstanceId,
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// check one definition create and one instance create
	if apiServer.definitionsPost != 1 {
		t.Errorf("expected 1 control plane definition created, got %d", apiServer.definitionsPost)
	}
	if apiServer.instancesPost != 1 {
		t.Errorf("expected 1 control plane instance created, got %d", apiServer.instancesPost)
	}

	// check the posted instance carries genesis, self, and runtime links
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

	// check the threeport API certs carry over encoded
	if instance.CACert == nil || *instance.CACert != controlPlaneConfig.CACert {
		t.Error("expected the CA cert to carry over from the config unchanged")
	}
	if instance.ClientCert == nil || *instance.ClientCert != controlPlaneConfig.Credentials[0].ClientCert {
		t.Error("expected the client cert to carry over from the config unchanged")
	}
	if instance.ClientKey == nil || *instance.ClientKey != controlPlaneConfig.Credentials[0].ClientKey {
		t.Error("expected the client key to carry over from the config unchanged")
	}

	// check the component list includes the rest api and agent
	if len(instance.CustomComponentInfo) < 2 {
		t.Errorf("expected the component list to include the rest api and agent, got %d components", len(instance.CustomComponentInfo))
	}
}

// TestEnsureBootstrapControlPlaneSkipsExistingObjects covers leaving
// existing records alone and creating only those that are missing.
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
			// start a stub threeport API with the existing objects
			apiServer := &controlPlaneApiServer{
				definitionFound: test.definitionFound,
				instanceFound:   test.instanceFound,
			}
			apiClient, apiAddr := apiServer.serve(t)

			// point a genesis config at the stub
			controlPlaneConfig := testControlPlaneConfig("dev-0")
			controlPlaneConfig.APIServer = apiAddr
			kubernetesRuntimeInstanceId := uint(4)

			// create only the missing control plane objects
			if err := ensureBootstrapControlPlane(
				apiClient,
				testInstaller("dev-0"),
				controlPlaneConfig,
				&kubernetesRuntimeInstanceId,
			); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// check the create counts match the case
			if apiServer.definitionsPost != test.definitionsPost {
				t.Errorf("expected %d control plane definitions created, got %d", test.definitionsPost, apiServer.definitionsPost)
			}
			if apiServer.instancesPost != test.instancesPost {
				t.Errorf("expected %d control plane instances created, got %d", test.instancesPost, apiServer.instancesPost)
			}
		})
	}
}
