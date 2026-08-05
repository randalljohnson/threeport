package v0

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// moduleRegistryApiServer is a threeport API stand-in for the registry
// lookups module discovery makes: the registered module APIs, then the
// controllers each one registered.
type moduleRegistryApiServer struct {
	// controllersByApiId maps a module API id to the namespace-qualified
	// deployment names its controllers registered.
	controllersByApiId map[uint][]string
	// apis are returned from the module API list endpoint in order.
	apis []v0.ModuleApi
}

// serve starts the stand-in and returns the client and address to reach
// it at. The address carries no scheme because GetResponse prepends one
// itself.
func (s *moduleRegistryApiServer) serve(t *testing.T) (*http.Client, string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasPrefix(r.URL.Path, v0.PathModuleApis):
			data := []apiserver_lib.Object{}
			for i := range s.apis {
				data = append(data, s.apis[i])
			}
			s.write(t, w, data)
		case strings.HasPrefix(r.URL.Path, v0.PathModuleControllers):
			// the caller narrows to one module API by query string
			apiId := uint(0)
			for _, api := range s.apis {
				if api.ID == nil {
					continue
				}
				if strings.Contains(r.URL.RawQuery, fmt.Sprintf("moduleapiid=%d", *api.ID)) {
					apiId = *api.ID
					break
				}
			}
			data := []apiserver_lib.Object{}
			for _, deploymentName := range s.controllersByApiId[apiId] {
				name := deploymentName
				data = append(data, v0.ModuleController{DeploymentName: &name})
			}
			s.write(t, w, data)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	return &http.Client{}, strings.TrimPrefix(server.URL, "http://")
}

// write sends a threeport API response carrying the supplied objects.
func (s *moduleRegistryApiServer) write(t *testing.T, w http.ResponseWriter, data []apiserver_lib.Object) {
	t.Helper()

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(apiserver_lib.Response{Data: data}); err != nil {
		t.Errorf("failed to encode response: %v", err)
	}
}

// testModuleApi returns a registered module API, marked as core when the
// registry entry stands for the control plane's own API.
func testModuleApi(id uint, name string, core bool) v0.ModuleApi {
	moduleApi := v0.ModuleApi{
		Common: v0.Common{ID: &id},
		Name:   &name,
	}
	if core {
		isCore := true
		moduleApi.Core = &isCore
	}

	return moduleApi
}

// testModuleDeployment returns a deployment in a module namespace at the
// given replica count, with no ready replicas so the drain wait passes
// without holding the test for its full deadline.
func testModuleDeployment(name, namespace string, replicas int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"replicas": replicas,
			},
		},
	}
}

// recordDeploymentScaling makes the fake client accept replica patches
// and records each one as namespace/name=replicas. The fake client
// cannot apply a strategic merge patch to an unstructured object, so the
// reactor answers on its behalf.
func recordDeploymentScaling(kubeClient *dynamicfake.FakeDynamicClient, patched *[]string) {
	kubeClient.PrependReactor("patch", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		patch := action.(k8stesting.PatchAction)

		var replicas struct {
			Spec struct {
				Replicas int64 `json:"replicas"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(patch.GetPatch(), &replicas); err != nil {
			return true, nil, err
		}
		*patched = append(*patched, fmt.Sprintf(
			"%s/%s=%d", patch.GetNamespace(), patch.GetName(), replicas.Spec.Replicas,
		))

		return true, testModuleDeployment(patch.GetName(), patch.GetNamespace(), replicas.Spec.Replicas), nil
	})
}

// TestDiscoverModuleNamespacesReadsTheRegistry asserts that the
// namespaces come from the control plane's registry of registered
// modules, that the core API's own namespace is not among them, and that
// a namespace shared by several controllers is returned once.
func TestDiscoverModuleNamespacesReadsTheRegistry(t *testing.T) {
	apiServer := &moduleRegistryApiServer{
		apis: []v0.ModuleApi{
			testModuleApi(1, "threeport", true),
			testModuleApi(2, "example-module", false),
			testModuleApi(3, "other-module", false),
		},
		controllersByApiId: map[uint][]string{
			1: {"threeport-control-plane/threeport-secret-controller"},
			2: {
				"example-namespace/threeport-example-controller",
				"example-namespace/threeport-second-controller",
			},
			3: {"other-namespace/threeport-other-controller"},
		},
	}
	apiClient, apiAddr := apiServer.serve(t)
	cpi := &ControlPlaneInstaller{Opts: Options{Namespace: "threeport-control-plane"}}

	namespaces, err := cpi.DiscoverModuleNamespaces(apiClient, apiAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"example-namespace", "other-namespace"}
	if len(namespaces) != len(want) {
		t.Fatalf("expected namespaces %v, got %v", want, namespaces)
	}
	for i, namespace := range want {
		if namespaces[i] != namespace {
			t.Errorf("expected namespace %s at position %d, got %s", namespace, i, namespaces[i])
		}
	}
}

// TestDiscoverModuleNamespacesExcludesTheControlPlane asserts that a
// module installed into the control plane's own namespace is not
// returned. Its deployments are the ones the reinstall already scales,
// and returning it would scale them twice and restore them out of step
// with the install.
func TestDiscoverModuleNamespacesExcludesTheControlPlane(t *testing.T) {
	apiServer := &moduleRegistryApiServer{
		apis: []v0.ModuleApi{testModuleApi(2, "example-module", false)},
		controllersByApiId: map[uint][]string{
			2: {"threeport-control-plane/threeport-example-controller"},
		},
	}
	apiClient, apiAddr := apiServer.serve(t)
	cpi := &ControlPlaneInstaller{Opts: Options{Namespace: "threeport-control-plane"}}

	namespaces, err := cpi.DiscoverModuleNamespaces(apiClient, apiAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(namespaces) != 0 {
		t.Errorf("expected the control plane's own namespace to be excluded, got %v", namespaces)
	}
}

// TestScaleDownModulesAndRestore asserts that every deployment in a
// module namespace is scaled to zero, not only the controllers the
// registry names, and that each one is put back at the count it was
// running at.
func TestScaleDownModulesAndRestore(t *testing.T) {
	namespace := "example-namespace"
	kubeClient := testKubeClient(
		// the module's API server is not a registered controller, and
		// is the component that reads and writes most
		testModuleDeployment("threeport-example-rest-api", namespace, 1),
		testModuleDeployment("threeport-example-controller", namespace, 2),
	)
	var patched []string
	recordDeploymentScaling(kubeClient, &patched)
	cpi := &ControlPlaneInstaller{Opts: Options{Namespace: "threeport-control-plane"}}

	scales, err := cpi.ScaleDownModules(kubeClient, []string{namespace})
	if err != nil {
		t.Fatalf("unexpected error scaling down: %v", err)
	}

	if len(scales) != 2 {
		t.Fatalf("expected both deployments to be recorded, got %v", scales)
	}
	for _, want := range []string{
		"example-namespace/threeport-example-rest-api=0",
		"example-namespace/threeport-example-controller=0",
	} {
		if !containsString(patched, want) {
			t.Errorf("expected scale-down patch %s, got %v", want, patched)
		}
	}

	patched = nil
	if err := cpi.RestoreModuleScale(kubeClient, scales); err != nil {
		t.Fatalf("unexpected error restoring: %v", err)
	}

	// each deployment goes back to what it was running, not to a
	// uniform replica count
	for _, want := range []string{
		"example-namespace/threeport-example-rest-api=1",
		"example-namespace/threeport-example-controller=2",
	} {
		if !containsString(patched, want) {
			t.Errorf("expected restore patch %s, got %v", want, patched)
		}
	}
}

// TestScaleDownModulesSkipsDeploymentsAlreadyStopped asserts that a
// deployment already at zero is left out of the record, so restoring
// never starts something that was deliberately down.
func TestScaleDownModulesSkipsDeploymentsAlreadyStopped(t *testing.T) {
	namespace := "example-namespace"
	kubeClient := testKubeClient(
		testModuleDeployment("threeport-example-rest-api", namespace, 1),
		testModuleDeployment("threeport-disabled-controller", namespace, 0),
	)
	var patched []string
	recordDeploymentScaling(kubeClient, &patched)
	cpi := &ControlPlaneInstaller{Opts: Options{Namespace: "threeport-control-plane"}}

	scales, err := cpi.ScaleDownModules(kubeClient, []string{namespace})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(scales) != 1 || scales[0].Name != "threeport-example-rest-api" {
		t.Errorf("expected only the running deployment to be recorded, got %v", scales)
	}
	for _, unwanted := range patched {
		if strings.Contains(unwanted, "threeport-disabled-controller") {
			t.Errorf("expected the stopped deployment to be left alone, got patch %s", unwanted)
		}
	}
}

// TestRestoreModuleScaleToleratesRemovedDeployment asserts that a
// deployment removed while the control plane was down is skipped rather
// than reported, so a module uninstalled in the meantime does not stop
// the rest from being restored.
func TestRestoreModuleScaleToleratesRemovedDeployment(t *testing.T) {
	namespace := "example-namespace"
	kubeClient := testKubeClient()
	cpi := &ControlPlaneInstaller{Opts: Options{Namespace: "threeport-control-plane"}}

	err := cpi.RestoreModuleScale(kubeClient, []ModuleDeploymentScale{
		{Namespace: namespace, Name: "threeport-uninstalled-controller", Replicas: 1},
	})
	if err != nil {
		t.Fatalf("expected a removed deployment to be skipped, got: %v", err)
	}

	// nothing was recreated in its place
	if _, err := kubeClient.Resource(deploymentGVR).Namespace(namespace).Get(
		context.Background(), "threeport-uninstalled-controller", metav1.GetOptions{},
	); err == nil {
		t.Error("expected the removed deployment to stay removed")
	}
}

// containsString reports whether the slice holds the given value.
func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}
