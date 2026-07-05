package v0

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	v0api "github.com/threeport/threeport/pkg/api/v0"
	auth "github.com/threeport/threeport/pkg/auth/v0"
)

// newDepsTestInstaller returns a ControlPlaneInstaller wired up with a namespace
// and default RestApiInfo so dependency install paths do not nil-deref.
func newDepsTestInstaller(namespace, infraProvider string) *ControlPlaneInstaller {
	return &ControlPlaneInstaller{
		Opts: Options{
			Namespace:     namespace,
			InfraProvider: infraProvider,
			RestApiInfo: &v0api.ControlPlaneComponent{
				ServiceResourceName: "threeport-api-server",
			},
		},
	}
}

// newTestDbCreds returns a DbCreds populated with recognisable string values so
// tests can grep for a specific field in the resulting Secret.
func newTestDbCreds() *auth.DbCreds {
	return &auth.DbCreds{
		AuthConfig: &auth.AuthConfig{
			CAPemEncoded: "test-ca-pem",
		},
		NodeCert:      "node-cert",
		NodeKey:       "node-key",
		RootCert:      "root-cert",
		RootKey:       "root-key",
		ThreeportCert: "threeport-cert",
		ThreeportKey:  "threeport-key",
	}
}

// newFullRESTMapper returns a RESTMapper wired up with every GroupVersionKind
// the dependencies install path touches: v1 core resources, apps/v1 workloads,
// and policy/v1 PDBs. Callers deref the returned pointer for the kube helpers.
func newFullRESTMapper() *meta.RESTMapper {
	m := meta.NewDefaultRESTMapper([]schema.GroupVersion{
		{Group: "", Version: "v1"},
		{Group: "apps", Version: "v1"},
		{Group: "policy", Version: "v1"},
	})
	// cluster-scoped core resource
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}, meta.RESTScopeRoot)
	// namespaced core resources
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ServiceAccount"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"}, meta.RESTScopeNamespace)
	// apps workloads
	m.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"}, meta.RESTScopeNamespace)
	// policy PDB
	m.Add(schema.GroupVersionKind{Group: "policy", Version: "v1", Kind: "PodDisruptionBudget"}, meta.RESTScopeNamespace)
	var iface meta.RESTMapper = m
	return &iface
}

// emptyRESTMapper returns a mapper with no registrations so any mapping lookup
// fails; drives the error branches in CreateOrUpdateKubeResource.
func emptyRESTMapper() *meta.RESTMapper {
	m := meta.NewDefaultRESTMapper([]schema.GroupVersion{})
	var iface meta.RESTMapper = m
	return &iface
}

// newFakeDynamicClient returns a fake dynamic client backed by an empty scheme.
// The Create path only needs unstructured type handling, which the fake client
// registers on demand.
func newDepsFakeDynamicClient() *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
}

// getFromTracker fetches a created resource from the fake client's tracker; if
// the resource is missing, err bubbles up so the caller sees a real failure.
func getFromTracker(
	t *testing.T,
	client dynamic.Interface,
	gvr schema.GroupVersionResource,
	namespace, name string,
) *unstructured.Unstructured {
	t.Helper()
	got, err := client.Resource(gvr).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get %s/%s: %v", gvr.Resource, name, err)
	}
	return got
}

// TestCreateThreeportControlPlaneNamespace_CreatesNamespace covers the happy path
// where the installer emits a Namespace resource with the configured name.
func TestCreateThreeportControlPlaneNamespace_CreatesNamespace(t *testing.T) {
	// arrange installer with a specific namespace and a working mapper/client
	cpi := newDepsTestInstaller("my-ns", "kind")
	client := newDepsFakeDynamicClient()
	mapper := newFullRESTMapper()

	// act
	if err := cpi.CreateThreeportControlPlaneNamespace(client, mapper); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// assert the namespace object landed in the tracker under the configured name
	nsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	got := getFromTracker(t, client, nsGVR, "", "my-ns")
	if got.GetName() != "my-ns" {
		t.Fatalf("got namespace name %q, want %q", got.GetName(), "my-ns")
	}
	if got.GetKind() != "Namespace" {
		t.Fatalf("got kind %q, want Namespace", got.GetKind())
	}
}

// TestCreateThreeportControlPlaneNamespace_MapperErrorPropagates covers the
// error branch where mapping the Namespace kind fails and the wrapped error
// bubbles out.
func TestCreateThreeportControlPlaneNamespace_MapperErrorPropagates(t *testing.T) {
	// arrange installer with an empty mapper so the RESTMapping lookup fails
	cpi := newDepsTestInstaller("my-ns", "kind")
	client := newDepsFakeDynamicClient()
	mapper := emptyRESTMapper()

	// act
	err := cpi.CreateThreeportControlPlaneNamespace(client, mapper)

	// assert error is returned (mapper produced no match for Namespace)
	if err == nil {
		t.Fatalf("expected error from empty mapper, got nil")
	}
}

// TestInstallThreeportControlPlaneDependencies_MapperErrorPropagates covers the
// error branch where the first CreateOrUpdate call fails on an unmapped kind.
// A full happy-path test is not viable in isolation: the fake dynamic client's
// DeepCopyJSON rejects the untyped int literals baked into the dependency
// resource maps (PDB maxUnavailable, port numbers, replicas), so the tracker
// panics before assertions could run against the real dependency install.
func TestInstallThreeportControlPlaneDependencies_MapperErrorPropagates(t *testing.T) {
	// arrange installer with an empty mapper so namespace creation fails first
	cpi := newDepsTestInstaller("cp-ns", "kind")
	client := newDepsFakeDynamicClient()
	mapper := emptyRESTMapper()

	// act
	err := cpi.InstallThreeportControlPlaneDependencies(client, mapper, "k", newTestDbCreds())

	// assert error surfaces (mapper has no Namespace registration)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

// TestControlPlaneInstaller_getVolClaimTemplateSpec covers both the default
// path (any non-eks provider) and the eks path that adds gp2 storageClassName.
func TestControlPlaneInstaller_getVolClaimTemplateSpec(t *testing.T) {
	tests := []struct {
		name          string
		infraProvider string
		storage       string
		wantSC        string
		wantSCPresent bool
	}{
		{
			name:          "default kind provider omits storageClassName",
			infraProvider: "kind",
			storage:       "5Gi",
			wantSCPresent: false,
		},
		{
			name:          "eks provider sets gp2 storageClassName",
			infraProvider: "eks",
			storage:       "20Gi",
			wantSC:        "gp2",
			wantSCPresent: true,
		},
		{
			name:          "empty provider omits storageClassName",
			infraProvider: "",
			storage:       "1Gi",
			wantSCPresent: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// arrange installer with the case's infra provider
			cpi := &ControlPlaneInstaller{Opts: Options{InfraProvider: tc.infraProvider}}

			// act
			spec := cpi.getVolClaimTemplateSpec(tc.storage)

			// assert accessModes always contains ReadWriteOnce
			modes, ok := spec["accessModes"].([]interface{})
			if !ok || len(modes) != 1 || modes[0] != "ReadWriteOnce" {
				t.Fatalf("accessModes: got %v, want [ReadWriteOnce]", spec["accessModes"])
			}
			// assert resources.requests.storage carries the requested amount
			resources, _ := spec["resources"].(map[string]interface{})
			requests, _ := resources["requests"].(map[string]interface{})
			if requests["storage"] != tc.storage {
				t.Fatalf("storage: got %v, want %s", requests["storage"], tc.storage)
			}
			// assert storageClassName is (or is not) set based on provider
			sc, present := spec["storageClassName"]
			if present != tc.wantSCPresent {
				t.Fatalf("storageClassName presence: got %v, want %v", present, tc.wantSCPresent)
			}
			if tc.wantSCPresent && sc != tc.wantSC {
				t.Fatalf("storageClassName: got %v, want %s", sc, tc.wantSC)
			}
		})
	}
}
