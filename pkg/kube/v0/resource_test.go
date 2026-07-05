package v0

import (
	"errors"
	"strings"
	"testing"

	kubeerr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	kubemetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// resourceTestGVK describes one GVK the fake REST mapper is aware of, along
// with its scope so RESTMapping resolves to the right resource.
type resourceTestGVK struct {
	group   string
	version string
	kind    string
	scope   meta.RESTScope
}

// resourceTestGVKs returns the GVK set that resource.go tests exercise. Kept
// in one place so the fake scheme and mapper stay in lockstep.
func resourceTestGVKs() []resourceTestGVK {
	return []resourceTestGVK{
		{"", "v1", "Namespace", meta.RESTScopeRoot},
		{"", "v1", "ConfigMap", meta.RESTScopeNamespace},
		{"", "v1", "Service", meta.RESTScopeNamespace},
		{"", "v1", "Pod", meta.RESTScopeNamespace},
	}
}

// newResourceTestScheme registers every GVK from resourceTestGVKs as an
// unstructured type on a fresh runtime.Scheme so the fake dynamic client's
// tracker can convert back to *unstructured.Unstructured.
func newResourceTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	for _, g := range resourceTestGVKs() {
		gvk := schema.GroupVersionKind{Group: g.group, Version: g.version, Kind: g.kind}
		scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		listGVK := gvk
		listGVK.Kind += "List"
		scheme.AddKnownTypeWithName(listGVK, &unstructured.UnstructuredList{})
	}
	return scheme
}

// newResourceTestMapper returns a RESTMapper seeded with the GVK set the
// resource.go tests exercise, so RESTMapping calls resolve without a real
// discovery client.
func newResourceTestMapper() meta.RESTMapper {
	gvs := []schema.GroupVersion{}
	seen := map[schema.GroupVersion]bool{}
	for _, g := range resourceTestGVKs() {
		gv := schema.GroupVersion{Group: g.group, Version: g.version}
		if seen[gv] {
			continue
		}
		seen[gv] = true
		gvs = append(gvs, gv)
	}
	m := meta.NewDefaultRESTMapper(gvs)
	for _, g := range resourceTestGVKs() {
		gvk := schema.GroupVersionKind{Group: g.group, Version: g.version, Kind: g.kind}
		m.Add(gvk, g.scope)
	}
	return m
}

// newResourceTestClient returns a fake dynamic client seeded with objs and
// wired to the same scheme as newResourceTestMapper.
func newResourceTestClient(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClient(newResourceTestScheme(), objs...)
}

// TestGetJsonResourcesFromYamlDocEmpty covers the empty-input path: an empty
// YAML document yields no JSON objects and no error.
func TestGetJsonResourcesFromYamlDocEmpty(t *testing.T) {
	// empty input decodes to zero nodes
	got, err := GetJsonResourcesFromYamlDoc("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// no JSON objects should be returned
	if len(got) != 0 {
		t.Errorf("expected 0 objects, got %d", len(got))
	}
}

// TestGetJsonResourcesFromYamlDocSingle covers the single-document happy path:
// one YAML doc converts to one JSON blob carrying the same fields.
func TestGetJsonResourcesFromYamlDocSingle(t *testing.T) {
	yamlDoc := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm-a\n"

	// a single document decodes to a single JSON blob
	got, err := GetJsonResourcesFromYamlDoc(yamlDoc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 object, got %d", len(got))
	}

	// the JSON blob preserves the kind field
	if !strings.Contains(string(got[0]), `"kind":"ConfigMap"`) {
		t.Errorf("expected converted JSON to contain kind ConfigMap, got %s", string(got[0]))
	}
}

// TestGetJsonResourcesFromYamlDocMulti covers a multi-doc YAML separated by
// `---`: each document lands as its own JSON blob in order.
func TestGetJsonResourcesFromYamlDocMulti(t *testing.T) {
	yamlDoc := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm-a\n---\napiVersion: v1\nkind: Service\nmetadata:\n  name: svc-a\n"

	// two documents decode to two JSON blobs
	got, err := GetJsonResourcesFromYamlDoc(yamlDoc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 objects, got %d", len(got))
	}

	// the first JSON blob carries the ConfigMap
	if !strings.Contains(string(got[0]), "ConfigMap") {
		t.Errorf("expected first blob to be ConfigMap, got %s", string(got[0]))
	}

	// the second JSON blob carries the Service
	if !strings.Contains(string(got[1]), "Service") {
		t.Errorf("expected second blob to be Service, got %s", string(got[1]))
	}
}

// TestGetJsonResourcesFromYamlDocInvalid covers the parse-error path: a
// malformed YAML document returns a wrapped decode error.
func TestGetJsonResourcesFromYamlDocInvalid(t *testing.T) {
	// unclosed bracket triggers a yaml decoder error
	_, err := GetJsonResourcesFromYamlDoc("apiVersion: v1\nkind: [not closed\n")
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}

	// the wrapper prefix should surface for the caller to distinguish decode
	// failure from conversion failure downstream
	if !strings.Contains(err.Error(), "failed to decode yaml node") {
		t.Errorf("expected decode-error prefix, got: %v", err)
	}
}

// TestCreateResourceNewNamespaced covers the create path for a namespaced
// resource: a fresh ConfigMap lands and the returned object carries the name.
func TestCreateResourceNewNamespaced(t *testing.T) {
	client := newResourceTestClient()
	mapper := newResourceTestMapper()

	// build a minimal ConfigMap so DeepCopyJSON does not choke on Go int types
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "cm-a",
			"namespace": "default",
		},
	}}

	// creating against an empty tracker succeeds and returns the object
	got, err := CreateResource(cm, client, mapper)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GetName() != "cm-a" {
		t.Errorf("expected name cm-a, got %q", got.GetName())
	}
}

// TestCreateResourceAlreadyExists covers the AlreadyExists branch: the second
// Create call is swallowed and the input object is returned.
func TestCreateResourceAlreadyExists(t *testing.T) {
	// seed the tracker with the object so the first Create call errors
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "cm-existing",
			"namespace": "default",
		},
	}}
	client := newResourceTestClient(cm)
	mapper := newResourceTestMapper()

	// AlreadyExists must be swallowed and the input returned unchanged
	got, err := CreateResource(cm, client, mapper)
	if err != nil {
		t.Fatalf("expected AlreadyExists to be swallowed, got: %v", err)
	}
	if got.GetName() != "cm-existing" {
		t.Errorf("expected input object returned, got name %q", got.GetName())
	}
}

// TestCreateResourceMappingFailure covers the RESTMapping error path: an
// unknown Kind produces a wrapped "failed to get REST mapping" error.
func TestCreateResourceMappingFailure(t *testing.T) {
	client := newResourceTestClient()
	mapper := newResourceTestMapper()

	// UnknownKind is not registered in the mapper
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "UnknownKind",
		"metadata":   map[string]interface{}{"name": "x", "namespace": "default"},
	}}

	// the mapping failure should propagate as a wrapped error
	_, err := CreateResource(obj, client, mapper)
	if err == nil {
		t.Fatal("expected mapping error for unknown kind")
	}
	if !strings.Contains(err.Error(), "failed to get REST mapping") {
		t.Errorf("expected mapping-error prefix, got: %v", err)
	}
}

// TestGetResourceNamespaced covers the namespaced-lookup path: a seeded
// ConfigMap is retrieved by name and namespace.
func TestGetResourceNamespaced(t *testing.T) {
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "cm-a",
			"namespace": "ns-a",
		},
	}}
	client := newResourceTestClient(cm)
	mapper := newResourceTestMapper()

	// GetResource looks up by API version, kind, namespace and name
	got, err := GetResource("core", "v1", "ConfigMap", "ns-a", "cm-a", client, mapper)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GetName() != "cm-a" {
		t.Errorf("expected name cm-a, got %q", got.GetName())
	}
}

// TestGetResourceNonNamespaced covers the cluster-scoped lookup path: an empty
// namespace argument selects the non-namespaced code branch.
func TestGetResourceNonNamespaced(t *testing.T) {
	ns := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]interface{}{"name": "ns-a"},
	}}
	client := newResourceTestClient(ns)
	mapper := newResourceTestMapper()

	// empty namespace routes through the non-namespaced Resource() branch
	got, err := GetResource("", "v1", "Namespace", "", "ns-a", client, mapper)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GetName() != "ns-a" {
		t.Errorf("expected name ns-a, got %q", got.GetName())
	}
}

// TestGetResourceNotFound covers the missing-resource path: the client's
// NotFound error is wrapped with the "failed to get resource" prefix.
func TestGetResourceNotFound(t *testing.T) {
	client := newResourceTestClient()
	mapper := newResourceTestMapper()

	// nothing seeded, so the Get call returns NotFound
	_, err := GetResource("core", "v1", "ConfigMap", "default", "missing", client, mapper)
	if err == nil {
		t.Fatal("expected NotFound error")
	}
	if !strings.Contains(err.Error(), "failed to get resource") {
		t.Errorf("expected get-error prefix, got: %v", err)
	}
}

// TestGetResourceMappingFailure covers the mapper-error path from GetResource:
// an unknown Kind surfaces as a wrapped mapping error.
func TestGetResourceMappingFailure(t *testing.T) {
	client := newResourceTestClient()
	mapper := newResourceTestMapper()

	// UnknownKind is not registered in the mapper
	_, err := GetResource("", "v1", "UnknownKind", "", "x", client, mapper)
	if err == nil {
		t.Fatal("expected mapping error")
	}
	if !strings.Contains(err.Error(), "failed to map kubernetes API version and kind") {
		t.Errorf("expected mapping-error prefix, got: %v", err)
	}
}

// TestDeleteResourcePresent covers the delete happy path: a seeded ConfigMap
// is removed and the call returns nil.
func TestDeleteResourcePresent(t *testing.T) {
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "cm-a",
			"namespace": "default",
		},
	}}
	client := newResourceTestClient(cm)
	mapper := newResourceTestMapper()

	// deleting a present resource returns nil
	if err := DeleteResource(cm, client, mapper); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDeleteResourceNotFound covers the idempotent branch: a NotFound error
// from the Delete verb is swallowed so callers can retry safely.
func TestDeleteResourceNotFound(t *testing.T) {
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "missing",
			"namespace": "default",
		},
	}}
	client := newResourceTestClient()
	mapper := newResourceTestMapper()

	// nothing seeded, so Delete returns NotFound; the wrapper must swallow it
	if err := DeleteResource(cm, client, mapper); err != nil {
		t.Fatalf("expected NotFound to be swallowed, got: %v", err)
	}
}

// TestDeleteResourcePropagatesOtherErrors covers the non-NotFound error path:
// a reactor injects a generic error and the wrapper propagates it prefixed.
func TestDeleteResourcePropagatesOtherErrors(t *testing.T) {
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "cm-a",
			"namespace": "default",
		},
	}}
	client := newResourceTestClient()
	client.PrependReactor("delete", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom")
	})
	mapper := newResourceTestMapper()

	// a non-NotFound error must surface with the wrapper prefix
	err := DeleteResource(cm, client, mapper)
	if err == nil {
		t.Fatal("expected delete error")
	}
	if !strings.Contains(err.Error(), "failed to delete kubernetes resource") {
		t.Errorf("expected delete-error prefix, got: %v", err)
	}
}

// TestDeleteResourceMappingFailure covers the mapping-error path from
// DeleteResource: an unknown Kind surfaces as a wrapped mapping error.
func TestDeleteResourceMappingFailure(t *testing.T) {
	client := newResourceTestClient()
	mapper := newResourceTestMapper()

	// UnknownKind is not registered in the mapper
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "UnknownKind",
		"metadata":   map[string]interface{}{"name": "x", "namespace": "default"},
	}}

	err := DeleteResource(obj, client, mapper)
	if err == nil {
		t.Fatal("expected mapping error")
	}
	if !strings.Contains(err.Error(), "failed to get REST mapping") {
		t.Errorf("expected mapping-error prefix, got: %v", err)
	}
}

// TestCreateOrUpdateResourceCreate covers the create branch: an empty tracker
// gets a fresh ConfigMap and the returned object carries the name.
func TestCreateOrUpdateResourceCreate(t *testing.T) {
	client := newResourceTestClient()
	mapper := newResourceTestMapper()

	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "cm-a",
			"namespace": "default",
		},
	}}

	// initial create against empty tracker succeeds
	got, err := CreateOrUpdateResource(cm, client, mapper)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GetName() != "cm-a" {
		t.Errorf("expected name cm-a, got %q", got.GetName())
	}
}

// TestCreateOrUpdateResourceUpdate covers the AlreadyExists → update branch:
// a repeated call for a seeded object routes through UpdateResource.
func TestCreateOrUpdateResourceUpdate(t *testing.T) {
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":            "cm-a",
			"namespace":       "default",
			"resourceVersion": "1",
		},
		"data": map[string]interface{}{"k": "v"},
	}}
	client := newResourceTestClient(cm)
	mapper := newResourceTestMapper()

	// second create hits AlreadyExists then routes through UpdateResource
	updated := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "cm-a",
			"namespace": "default",
		},
		"data": map[string]interface{}{"k": "v2"},
	}}
	got, err := CreateOrUpdateResource(updated, client, mapper)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GetName() != "cm-a" {
		t.Errorf("expected name cm-a, got %q", got.GetName())
	}
}

// TestCreateOrUpdateResourceInvalidService covers the Service-specific
// IsInvalid branch: a nodeport-invalidated Service is routed to UpdateResource
// rather than returning the raw error.
func TestCreateOrUpdateResourceInvalidService(t *testing.T) {
	// seed the existing Service so UpdateResource's Get call finds it
	existing := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":            "svc-a",
			"namespace":       "default",
			"resourceVersion": "1",
		},
	}}
	client := newResourceTestClient(existing)
	mapper := newResourceTestMapper()

	// Create rejects with IsInvalid so the Service-specific branch fires
	client.PrependReactor("create", "services", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, kubeerr.NewInvalid(
			schema.GroupKind{Kind: "Service"},
			"svc-a",
			nil,
		)
	})

	// build the input; UpdateResource should be reached and succeed
	svc := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":      "svc-a",
			"namespace": "default",
		},
	}}
	got, err := CreateOrUpdateResource(svc, client, mapper)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GetName() != "svc-a" {
		t.Errorf("expected name svc-a, got %q", got.GetName())
	}
}

// TestCreateOrUpdateResourcePropagatesUnknownError covers the default branch:
// a non-AlreadyExists, non-Service-Invalid error surfaces with the wrapper
// prefix.
func TestCreateOrUpdateResourcePropagatesUnknownError(t *testing.T) {
	client := newResourceTestClient()
	client.PrependReactor("create", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom")
	})
	mapper := newResourceTestMapper()

	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "cm-a",
			"namespace": "default",
		},
	}}

	// an unrecognised Create error must propagate wrapped
	_, err := CreateOrUpdateResource(cm, client, mapper)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to create kubernetes resource") {
		t.Errorf("expected create-error prefix, got: %v", err)
	}
}

// TestUpdateResourceHappy covers the update happy path: a seeded ConfigMap
// gets its resource version filled in from Get and the Update call succeeds.
func TestUpdateResourceHappy(t *testing.T) {
	existing := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":            "cm-a",
			"namespace":       "default",
			"resourceVersion": "1",
		},
	}}
	client := newResourceTestClient(existing)
	mapper := newResourceTestMapper()

	// resolve the mapping for the Update call
	gvk := schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}

	// updating with a fresh copy resolves via UpdateResource
	update := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "cm-a",
			"namespace": "default",
		},
		"data": map[string]interface{}{"k": "v"},
	}}
	got, err := UpdateResource(update, client, mapper, mapping)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GetName() != "cm-a" {
		t.Errorf("expected name cm-a, got %q", got.GetName())
	}
}

// TestUpdateResourceGetMissing covers the pre-check error path: when the
// existing object is missing UpdateResource surfaces a wrapped Get error.
func TestUpdateResourceGetMissing(t *testing.T) {
	client := newResourceTestClient()
	mapper := newResourceTestMapper()

	gvk := schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}

	// nothing seeded, so the initial Get returns NotFound
	update := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "cm-a",
			"namespace": "default",
		},
	}}
	_, err = UpdateResource(update, client, mapper, mapping)
	if err == nil {
		t.Fatal("expected get-missing error")
	}
	if !strings.Contains(err.Error(), "failed to get existing resource") {
		t.Errorf("expected existing-resource-error prefix, got: %v", err)
	}
}

// TestDeletePodNoMatchingPods covers the empty-list branch: with no pods
// matching the label selector DeletePod returns nil.
func TestDeletePodNoMatchingPods(t *testing.T) {
	client := newResourceTestClient()
	mapper := newResourceTestMapper()

	// no pods seeded, so the list is empty and no deletes fire
	if err := DeletePod(client, &mapper, "worker", "default"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDeletePodDeletesMatched covers the match-and-delete branch: pods
// carrying the target app.kubernetes.io/name label are removed.
func TestDeletePodDeletesMatched(t *testing.T) {
	pod := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":      "pod-a",
			"namespace": "default",
			"labels": map[string]interface{}{
				"app.kubernetes.io/name": "threeport-worker",
			},
		},
	}}
	client := newResourceTestClient(pod)
	mapper := newResourceTestMapper()

	// deleting the matching pod returns nil and clears the tracker
	if err := DeletePod(client, &mapper, "worker", "default"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// verify the pod is gone by listing the pod resource
	gvk := schema.GroupVersionKind{Version: "v1", Kind: "Pod"}
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}
	list, err := client.Resource(mapping.Resource).Namespace("default").List(t.Context(), kubemetav1.ListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Items) != 0 {
		t.Errorf("expected pod deleted, got %d remaining", len(list.Items))
	}
}

// TestDeletePodMappingFailure covers the mapping-error path from DeletePod: an
// empty mapper cannot resolve the Pod GVK.
func TestDeletePodMappingFailure(t *testing.T) {
	client := newResourceTestClient()

	// empty mapper knows nothing, so the Pod mapping lookup fails
	empty := meta.NewDefaultRESTMapper(nil)
	var m meta.RESTMapper = empty

	err := DeletePod(client, &m, "worker", "default")
	if err == nil {
		t.Fatal("expected mapping error")
	}
	if !strings.Contains(err.Error(), "failed to get REST mapping") {
		t.Errorf("expected mapping-error prefix, got: %v", err)
	}
}
