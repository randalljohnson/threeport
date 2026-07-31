package v0

import (
	"context"
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

// testNamespaceMapper returns a mapper that resolves the Namespace kind,
// which is the only kind the tier lookup maps.
func testNamespaceMapper() meta.RESTMapper {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, meta.RESTScopeRoot)

	return mapper
}

// testNamespace returns a namespace object carrying the supplied tier
// label. An empty tier produces a namespace with no labels at all,
// standing in for one installed before the tier was recorded.
func testNamespace(name, tier string) *unstructured.Unstructured {
	namespace := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]interface{}{
				"name": name,
			},
		},
	}
	if tier != "" {
		namespace.SetLabels(map[string]string{LabelTier: tier})
	}

	return namespace
}

// testKubeClient returns a fake dynamic client seeded with the supplied
// objects and a scheme that knows the Namespace list kind.
func testKubeClient(objects ...runtime.Object) dynamic.Interface {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Version: "v1", Kind: "NamespaceList"},
		&unstructured.UnstructuredList{},
	)

	return dynamicfake.NewSimpleDynamicClient(scheme, objects...)
}

// TestDropDatabaseRejectsNonDevelopmentControlPlane asserts that the
// database drop refuses every control plane it cannot confirm is a
// development installation, and that it deletes nothing when it
// refuses.
func TestDropDatabaseRejectsNonDevelopmentControlPlane(t *testing.T) {
	tests := []struct {
		name string
		tier string
	}{
		{
			name: "production tier is refused",
			tier: ControlPlaneTierProd,
		},
		{
			name: "unrecognized tier is refused",
			tier: "staging",
		},
		{
			name: "absent tier is refused",
			tier: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// seed a namespace at the tier under test alongside the
			// database resources the drop would delete
			namespace := "threeport-control-plane"
			kubeClient := testKubeClient(
				testNamespace(namespace, test.tier),
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "apps/v1",
						"kind":       "StatefulSet",
						"metadata": map[string]interface{}{
							"name":      crdbStatefulSetName,
							"namespace": namespace,
						},
					},
				},
			)
			mapper := testNamespaceMapper()
			cpi := &ControlPlaneInstaller{Opts: Options{Namespace: namespace}}

			err := cpi.DropDatabase(kubeClient, &mapper)

			// the refusal is reported as the tier error so callers can
			// match it without reading the message
			if err == nil {
				t.Fatal("expected drop to be refused, got nil error")
			}
			if !errors.Is(err, ErrControlPlaneNotDevelopment) {
				t.Errorf("expected error to match ErrControlPlaneNotDevelopment, got: %v", err)
			}

			// nothing is deleted on a refusal, so the database survives
			_, getErr := kubeClient.Resource(statefulSetGVR).Namespace(namespace).Get(
				context.Background(), crdbStatefulSetName, metav1.GetOptions{},
			)
			if getErr != nil {
				t.Errorf("expected database statefulset to survive a refused drop, got: %v", getErr)
			}
		})
	}
}

// TestDropDatabaseDeletesDatabaseOnDevelopmentControlPlane asserts that
// a development control plane's database and its data volume are both
// removed, while the message broker's data is left in place.
func TestDropDatabaseDeletesDatabaseOnDevelopmentControlPlane(t *testing.T) {
	// seed a development namespace with the database, its data volume,
	// and an unrelated statefulset that must not be touched
	namespace := "threeport-control-plane"
	kubeClient := testKubeClient(
		testNamespace(namespace, ControlPlaneTierDev),
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "StatefulSet",
				"metadata": map[string]interface{}{
					"name":      crdbStatefulSetName,
					"namespace": namespace,
				},
			},
		},
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "StatefulSet",
				"metadata": map[string]interface{}{
					"name":      "nats-js",
					"namespace": namespace,
				},
			},
		},
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "PersistentVolumeClaim",
				"metadata": map[string]interface{}{
					"name":      crdbDataVolumeClaimName,
					"namespace": namespace,
				},
			},
		},
	)
	mapper := testNamespaceMapper()
	cpi := &ControlPlaneInstaller{Opts: Options{Namespace: namespace}}

	if err := cpi.DropDatabase(kubeClient, &mapper); err != nil {
		t.Fatalf("expected drop to succeed on a development control plane, got: %v", err)
	}

	// the database and the volume holding its data are both gone
	if _, err := kubeClient.Resource(statefulSetGVR).Namespace(namespace).Get(
		context.Background(), crdbStatefulSetName, metav1.GetOptions{},
	); err == nil {
		t.Error("expected database statefulset to be deleted")
	}
	if _, err := kubeClient.Resource(volumeClaimGVR).Namespace(namespace).Get(
		context.Background(), crdbDataVolumeClaimName, metav1.GetOptions{},
	); err == nil {
		t.Error("expected database volume claim to be deleted")
	}

	// the message broker is a separate statefulset and keeps its data
	if _, err := kubeClient.Resource(statefulSetGVR).Namespace(namespace).Get(
		context.Background(), "nats-js", metav1.GetOptions{},
	); err != nil {
		t.Errorf("expected message broker statefulset to survive the drop, got: %v", err)
	}
}
