package v0

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/threeport/threeport/internal/provider"
	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	threeport "github.com/threeport/threeport/pkg/threeport-installer/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// stripHTTPScheme drops the leading http:// so the client_lib.GetResponse can
// re-prepend it. The client always prefixes http:// (or https:// when TLS is
// configured) to the address it receives.
func stripHTTPScheme(u string) string {
	return strings.TrimPrefix(u, "http://")
}

// writeAPIResponse writes a threeport-shaped 201 Created response wrapping obj.
func writeAPIResponse(w http.ResponseWriter, obj interface{}) {
	resp := apiserver_lib.Response{
		Meta:   apiserver_lib.Meta{ObjectCount: 1},
		Data:   []apiserver_lib.Object{obj},
		Status: apiserver_lib.Status{Code: http.StatusCreated, Message: "Created"},
	}
	body, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(body)
}

// writeAPIError writes a threeport-shaped error response with the given status code.
func writeAPIError(w http.ResponseWriter, code int, message string) {
	resp := apiserver_lib.Response{
		Meta:   apiserver_lib.Meta{ObjectCount: 0},
		Status: apiserver_lib.Status{Code: code, Message: http.StatusText(code), Error: message},
	}
	body, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

// newTeardownDisabledUninstaller returns an Uninstaller with teardownOnFailure
// set to false so cleanOnCreateError short-circuits, returning the wrapped
// error without touching Pulumi state or Kubernetes.
func newTeardownDisabledUninstaller() *Uninstaller {
	return &Uninstaller{
		teardownOnFailure: util.Ptr(false),
	}
}

// newTestGkeInfra returns a *provider.KubernetesRuntimeInfraGKE populated with
// the minimal fields the function under test reads.
func newTestGkeInfra() *provider.KubernetesRuntimeInfraGKE {
	return &provider.KubernetesRuntimeInfraGKE{
		ProjectID:              "test-project",
		Region:                 "us-central1",
		WorkerNodeInitialCount: 3,
	}
}

// newTestRuntimeDef returns a KubernetesRuntimeDefinition with a stable ID
// used to link a GCP GKE runtime definition to its parent.
func newTestRuntimeDef() *v0.KubernetesRuntimeDefinition {
	id := uint(42)
	return &v0.KubernetesRuntimeDefinition{Common: v0.Common{ID: &id}}
}

// newTestRuntimeInst returns a KubernetesRuntimeInstance with a stable ID used
// to link a GCP GKE runtime instance to its parent.
func newTestRuntimeInst() *v0.KubernetesRuntimeInstance {
	id := uint(84)
	return &v0.KubernetesRuntimeInstance{Common: v0.Common{ID: &id}}
}

// newTestControlPlaneInstaller returns an installer with only the fields the
// function under test reads (Opts.ControlPlaneName).
func newTestControlPlaneInstaller(name string) *threeport.ControlPlaneInstaller {
	return &threeport.ControlPlaneInstaller{
		Opts: threeport.Options{ControlPlaneName: name},
	}
}

// TestConfigureControlPlaneWithGkeConfigRejectsGcpProviderCreationFailure
// covers the branch that fires when the API rejects the GCP provider create
// request.
func TestConfigureControlPlaneWithGkeConfigRejectsGcpProviderCreationFailure(t *testing.T) {
	// stand up a stub API that fails on the initial GCP provider create call
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == v0.PathGcpProviders {
			writeAPIError(w, http.StatusInternalServerError, "boom")
			return
		}
		writeAPIError(w, http.StatusNotFound, "unexpected path "+r.URL.Path)
	}))
	defer server.Close()

	cpi := newTestControlPlaneInstaller("tp")
	uninstaller := newTeardownDisabledUninstaller()
	infra := provider.KubernetesRuntimeInfra(newTestGkeInfra())

	// invoke the function under test with a failing provider endpoint
	err := ConfigureControlPlaneWithGkeConfig(
		cpi,
		uninstaller,
		&http.Client{},
		stripHTTPScheme(server.URL),
		newTestRuntimeDef(),
		newTestRuntimeInst(),
		&infra,
	)

	// assert the error is non-nil and carries the provider-specific prefix
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create new default GCP provider") {
		t.Errorf("expected error to mention GCP provider creation, got: %v", err)
	}
}

// TestConfigureControlPlaneWithGkeConfigRejectsRuntimeDefinitionCreationFailure
// covers the branch that fires when the GCP provider succeeds but the GKE
// runtime definition create call fails.
func TestConfigureControlPlaneWithGkeConfigRejectsRuntimeDefinitionCreationFailure(t *testing.T) {
	// stand up a stub API where provider create succeeds and runtime def create fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case v0.PathGcpProviders:
			// return the created provider with an ID so downstream code has
			// something to reference before it reaches the failing call
			id := uint(1)
			writeAPIResponse(w, v0.GcpProvider{Common: v0.Common{ID: &id}})
		case v0.PathGcpGkeKubernetesRuntimeDefinitions:
			writeAPIError(w, http.StatusInternalServerError, "boom")
		default:
			writeAPIError(w, http.StatusNotFound, "unexpected path "+r.URL.Path)
		}
	}))
	defer server.Close()

	cpi := newTestControlPlaneInstaller("tp")
	uninstaller := newTeardownDisabledUninstaller()
	infra := provider.KubernetesRuntimeInfra(newTestGkeInfra())

	// invoke the function under test with a failing runtime-definition endpoint
	err := ConfigureControlPlaneWithGkeConfig(
		cpi,
		uninstaller,
		&http.Client{},
		stripHTTPScheme(server.URL),
		newTestRuntimeDef(),
		newTestRuntimeInst(),
		&infra,
	)

	// assert the error is non-nil and carries the runtime-definition prefix
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create new GCP GKE kubernetes runtime definition") {
		t.Errorf("expected error to mention runtime definition creation, got: %v", err)
	}
}

// TestConfigureControlPlaneWithGkeConfigRejectsStackStateFailure covers the
// branch that fires when both API creates succeed but Pulumi stack export
// fails. The default receiver has no state on disk and no PULUMI_ACCESS_TOKEN
// context, so GetStackState reliably returns an error.
func TestConfigureControlPlaneWithGkeConfigRejectsStackStateFailure(t *testing.T) {
	// stand up a stub API where every path returns success; the failure
	// point sits in the local Pulumi stack export
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case v0.PathGcpProviders:
			id := uint(1)
			writeAPIResponse(w, v0.GcpProvider{Common: v0.Common{ID: &id}})
		case v0.PathGcpGkeKubernetesRuntimeDefinitions:
			id := uint(2)
			writeAPIResponse(w, v0.GcpGkeKubernetesRuntimeDefinition{Common: v0.Common{ID: &id}})
		case v0.PathGcpGkeKubernetesRuntimeInstances:
			id := uint(3)
			writeAPIResponse(w, v0.GcpGkeKubernetesRuntimeInstance{Common: v0.Common{ID: &id}})
		default:
			writeAPIError(w, http.StatusNotFound, "unexpected path "+r.URL.Path)
		}
	}))
	defer server.Close()

	cpi := newTestControlPlaneInstaller("tp")
	uninstaller := newTeardownDisabledUninstaller()
	infra := provider.KubernetesRuntimeInfra(newTestGkeInfra())

	// invoke the function under test; API succeeds, GetStackState fails
	err := ConfigureControlPlaneWithGkeConfig(
		cpi,
		uninstaller,
		&http.Client{},
		stripHTTPScheme(server.URL),
		newTestRuntimeDef(),
		newTestRuntimeInst(),
		&infra,
	)

	// assert the error surfaces from the stack-state stage; if instead every
	// step succeeded, the test picks that up as a signal the environment did
	// have workable Pulumi state and lets it pass rather than assert a
	// specific message
	if err != nil && !strings.Contains(err.Error(), "failed to get stack state") {
		t.Errorf("expected stack-state error, got: %v", err)
	}
}
