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

// TestBootstrapKubernetesRuntimeInstance asserts the kubernetes runtime
// instance is rebuilt from the threeport config with the connection info
// decoded and the flags that make it discoverable as the default runtime and
// the control plane's host.
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

// TestBootstrapKubernetesRuntimeInstanceRejectsIncompleteConfig asserts the
// rebuild refuses a config it cannot turn into a usable runtime instance
// rather than registering one that cannot reach the kube API.
func TestBootstrapKubernetesRuntimeInstanceRejectsIncompleteConfig(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*ControlPlane)
		errContains string
	}{
		{
			name:        "non-kind provider is refused",
			mutate:      func(c *ControlPlane) { c.Provider = v0.KubernetesRuntimeInfraProviderEKS },
			errContains: "only kind is supported",
		},
		{
			name:        "missing client certificate is refused",
			mutate:      func(c *ControlPlane) { c.KubeAPI.Certificate = "" },
			errContains: "no kubernetes API client certificate and key",
		},
		{
			name:        "missing client key is refused",
			mutate:      func(c *ControlPlane) { c.KubeAPI.Key = "" },
			errContains: "no kubernetes API client certificate and key",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controlPlaneConfig := testControlPlaneConfig("dev-0")
			test.mutate(controlPlaneConfig)

			_, err := bootstrapKubernetesRuntimeInstance(controlPlaneConfig)
			if err == nil {
				t.Fatal("expected an error, got none")
			}
			if !strings.Contains(err.Error(), test.errContains) {
				t.Errorf("expected error containing %q, got %q", test.errContains, err.Error())
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
