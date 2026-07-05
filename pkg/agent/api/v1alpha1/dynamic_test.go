package v1alpha1

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestUnstructuredThreeportWorkloadPopulatesTypeMeta asserts that the returned
// unstructured object carries the group-version and kind derived from the
// package's GroupVersion, plus the name from ObjectMeta.
func TestUnstructuredThreeportWorkloadPopulatesTypeMeta(t *testing.T) {
	// build a typed workload with a distinct name and spec so we can verify
	// each surface of the conversion
	tw := &ThreeportWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name: "workload-under-test",
		},
		Spec: ThreeportWorkloadSpec{
			WorkloadType:                 "KubernetesWorkloadInstance",
			KubernetesWorkloadInstanceID: 42,
			KubernetesWorkloadResourceInstances: []KubernetesWorkloadResourceInstance{
				{
					Name:        "resource-a",
					Namespace:   "ns-a",
					Group:       "apps",
					Version:     "v1",
					Kind:        "Deployment",
					ThreeportID: 7,
				},
			},
		},
	}

	// run the conversion under test
	u, err := UnstructuredThreeportWorkload(tw)
	if err != nil {
		t.Fatalf("UnstructuredThreeportWorkload returned error: %v", err)
	}
	if u == nil {
		t.Fatal("UnstructuredThreeportWorkload returned nil unstructured")
	}

	// verify api-version comes from GroupVersion (group + "/" + version)
	wantAPIVersion := fmt.Sprintf("%s/%s", GroupVersion.Group, GroupVersion.Version)
	if got := u.GetAPIVersion(); got != wantAPIVersion {
		t.Errorf("apiVersion: got %q, want %q", got, wantAPIVersion)
	}

	// verify kind matches the package constant
	if got := u.GetKind(); got != ThreeportWorkloadKind {
		t.Errorf("kind: got %q, want %q", got, ThreeportWorkloadKind)
	}

	// verify name propagates from ObjectMeta
	if got := u.GetName(); got != "workload-under-test" {
		t.Errorf("name: got %q, want %q", got, "workload-under-test")
	}

	// verify the spec map round-tripped through the converter
	spec, found, err := unstructuredNestedMap(u.Object, "spec")
	if err != nil {
		t.Fatalf("reading spec: %v", err)
	}
	if !found {
		t.Fatal("spec not found on unstructured object")
	}
	if got, want := spec["workloadType"], "KubernetesWorkloadInstance"; got != want {
		t.Errorf("spec.workloadType: got %v, want %v", got, want)
	}
}

// TestUnstructuredThreeportWorkloadEmptySpec covers a workload with no spec
// fields set; the conversion should still succeed and stamp type-meta plus
// name.
func TestUnstructuredThreeportWorkloadEmptySpec(t *testing.T) {
	// build a workload with only the name populated
	tw := &ThreeportWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name: "bare-workload",
		},
	}

	// run the conversion
	u, err := UnstructuredThreeportWorkload(tw)
	if err != nil {
		t.Fatalf("UnstructuredThreeportWorkload returned error: %v", err)
	}

	// verify kind and name still stamped
	if got := u.GetKind(); got != ThreeportWorkloadKind {
		t.Errorf("kind: got %q, want %q", got, ThreeportWorkloadKind)
	}
	if got := u.GetName(); got != "bare-workload" {
		t.Errorf("name: got %q, want %q", got, "bare-workload")
	}
}

// unstructuredNestedMap is a tiny helper that extracts a nested map[string]any
// from the unstructured Object without pulling in the full apimachinery
// helpers; keeps the test self-contained.
func unstructuredNestedMap(obj map[string]any, key string) (map[string]any, bool, error) {
	v, ok := obj[key]
	if !ok {
		return nil, false, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, true, fmt.Errorf("key %q is not a map", key)
	}
	return m, true, nil
}
