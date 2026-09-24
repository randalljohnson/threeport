package util

import (
	"strings"
	"testing"

	"gorm.io/datatypes"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// jsonDef returns a *datatypes.JSON with the given raw JSON so tests can build
// KubernetesWorkloadResource{Definition,Instance} fixtures inline.
func jsonDef(raw string) *datatypes.JSON {
	j := datatypes.JSON([]byte(raw))
	return &j
}

// makeInstances builds a slice of KubernetesWorkloadResourceInstance from raw
// JSON strings so each test sets up its own manifest list without duplication.
func makeInstances(defs ...string) []v0.KubernetesWorkloadResourceInstance {
	out := make([]v0.KubernetesWorkloadResourceInstance, 0, len(defs))
	for _, d := range defs {
		out = append(out, v0.KubernetesWorkloadResourceInstance{JSONDefinition: jsonDef(d)})
	}
	return out
}

// makeDefinitions builds a slice of KubernetesWorkloadResourceDefinition from
// raw JSON strings for the definition-side helpers.
func makeDefinitions(defs ...string) []v0.KubernetesWorkloadResourceDefinition {
	out := make([]v0.KubernetesWorkloadResourceDefinition, 0, len(defs))
	for _, d := range defs {
		out = append(out, v0.KubernetesWorkloadResourceDefinition{JSONDefinition: jsonDef(d)})
	}
	return out
}

// canonical JSON manifests used across the test cases below.
const (
	svcFooJSON        = `{"kind":"Service","metadata":{"name":"foo"}}`
	svcBarJSON        = `{"kind":"Service","metadata":{"name":"bar"}}`
	deployFooJSON     = `{"kind":"Deployment","metadata":{"name":"foo"}}`
	invalidJSON       = `{not json`
	kindMissingJSON   = `{"metadata":{"name":"foo"}}`
	nameMissingJSON   = `{"kind":"Service","metadata":{}}`
	noMetadataMapJSON = `{"kind":"Service","metadata":"stringNotMap"}`
)

// jsonMarshalOK exercises the round-trip that util.UnmarshalJSON does, so a
// change in that helper surfaces as a failing test here rather than a mystery.
func TestUnmarshalJSONSanity(t *testing.T) {
	// setup: canonical service manifest fixture
	m, err := util.UnmarshalJSON(*jsonDef(svcFooJSON))
	// action: unmarshal via the same helper the util package uses
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// assertion: kind field survived the round trip
	if m["kind"] != "Service" {
		t.Fatalf("kind = %v, want Service", m["kind"])
	}
}

// covers GetUniqueKubernetesWorkloadResourceInstance returning the sole match
// when exactly one manifest of the given kind is present.
func TestGetUniqueKubernetesWorkloadResourceInstance_ReturnsSoleMatch(t *testing.T) {
	// setup: one Service and one Deployment
	insts := makeInstances(svcFooJSON, deployFooJSON)

	// action: request the sole Service
	got, err := GetUniqueKubernetesWorkloadResourceInstance(&insts, "Service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// assertion: the returned instance is the Service one
	if got == nil || got.JSONDefinition == nil {
		t.Fatalf("got nil instance")
	}
	if string(*got.JSONDefinition) != svcFooJSON {
		t.Errorf("returned wrong instance: %s", string(*got.JSONDefinition))
	}
}

// rejects the call when no manifest of the requested kind exists.
func TestGetUniqueKubernetesWorkloadResourceInstance_RejectsNotFound(t *testing.T) {
	// setup: only a Deployment
	insts := makeInstances(deployFooJSON)
	// action: ask for a Service (absent)
	_, err := GetUniqueKubernetesWorkloadResourceInstance(&insts, "Service")
	// assertion: not-found error returned
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

// rejects the call when multiple matches exist because uniqueness is required.
func TestGetUniqueKubernetesWorkloadResourceInstance_RejectsMultiple(t *testing.T) {
	// setup: two Services
	insts := makeInstances(svcFooJSON, svcBarJSON)
	// action: ask for Service
	_, err := GetUniqueKubernetesWorkloadResourceInstance(&insts, "Service")
	// assertion: multiple-found error returned
	if err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected multiple-found error, got %v", err)
	}
}

// rejects the call when a manifest fails to parse; a bad manifest anywhere in
// the slice must surface as an unmarshal error.
func TestGetUniqueKubernetesWorkloadResourceInstance_RejectsBadJSON(t *testing.T) {
	// setup: unparseable JSON
	insts := makeInstances(invalidJSON)
	// action: any lookup drives the unmarshal
	_, err := GetUniqueKubernetesWorkloadResourceInstance(&insts, "Service")
	// assertion: unmarshal failure surfaces
	if err == nil || !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected unmarshal error, got %v", err)
	}
}

// covers GetUniqueKubernetesWorkloadResourceInstanceByName returning the sole
// match when a manifest of the given kind and name exists.
func TestGetUniqueKubernetesWorkloadResourceInstanceByName_ReturnsSoleMatch(t *testing.T) {
	// setup: two Services with different names
	insts := makeInstances(svcFooJSON, svcBarJSON)
	// action: ask for Service/bar
	got, err := GetUniqueKubernetesWorkloadResourceInstanceByName(&insts, "Service", "bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// assertion: correct match returned
	if string(*got.JSONDefinition) != svcBarJSON {
		t.Errorf("returned wrong instance: %s", string(*got.JSONDefinition))
	}
}

// rejects a manifest without a kind field: the NestedString check must fire.
func TestGetUniqueKubernetesWorkloadResourceInstanceByName_RejectsMissingKind(t *testing.T) {
	// setup: manifest missing the kind field
	insts := makeInstances(kindMissingJSON)
	// action: any lookup exercises the NestedString probe
	_, err := GetUniqueKubernetesWorkloadResourceInstanceByName(&insts, "Service", "foo")
	// assertion: kind-not-found error surfaces
	if err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("expected kind error, got %v", err)
	}
}

// rejects a manifest whose metadata is missing the name field.
func TestGetUniqueKubernetesWorkloadResourceInstanceByName_RejectsMissingName(t *testing.T) {
	// setup: manifest whose metadata has no name
	insts := makeInstances(nameMissingJSON)
	// action: name lookup drives the second NestedString probe
	_, err := GetUniqueKubernetesWorkloadResourceInstanceByName(&insts, "Service", "foo")
	// assertion: name-not-found error surfaces
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("expected name error, got %v", err)
	}
}

// rejects the call when nothing matches the requested kind+name pair.
func TestGetUniqueKubernetesWorkloadResourceInstanceByName_RejectsNotFound(t *testing.T) {
	insts := makeInstances(svcFooJSON)
	_, err := GetUniqueKubernetesWorkloadResourceInstanceByName(&insts, "Service", "missing")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

// rejects the call when multiple manifests share both kind and name.
func TestGetUniqueKubernetesWorkloadResourceInstanceByName_RejectsMultiple(t *testing.T) {
	// setup: two identical-named Services
	insts := makeInstances(svcFooJSON, svcFooJSON)
	_, err := GetUniqueKubernetesWorkloadResourceInstanceByName(&insts, "Service", "foo")
	if err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected multiple-found error, got %v", err)
	}
}

// covers the definition-side unique-by-kind helper's happy path.
func TestGetUniqueKubernetesWorkloadResourceDefinition_ReturnsSoleMatch(t *testing.T) {
	defs := makeDefinitions(svcFooJSON, deployFooJSON)
	got, err := GetUniqueKubernetesWorkloadResourceDefinition(&defs, "Service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(*got.JSONDefinition) != svcFooJSON {
		t.Errorf("returned wrong definition: %s", string(*got.JSONDefinition))
	}
}

// rejects when no definition of the requested kind exists.
func TestGetUniqueKubernetesWorkloadResourceDefinition_RejectsNotFound(t *testing.T) {
	defs := makeDefinitions(deployFooJSON)
	_, err := GetUniqueKubernetesWorkloadResourceDefinition(&defs, "Service")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

// rejects when multiple definitions share the requested kind.
func TestGetUniqueKubernetesWorkloadResourceDefinition_RejectsMultiple(t *testing.T) {
	defs := makeDefinitions(svcFooJSON, svcBarJSON)
	_, err := GetUniqueKubernetesWorkloadResourceDefinition(&defs, "Service")
	if err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected multiple-found error, got %v", err)
	}
}

// rejects when a definition's JSON is unparseable.
func TestGetUniqueKubernetesWorkloadResourceDefinition_RejectsBadJSON(t *testing.T) {
	defs := makeDefinitions(invalidJSON)
	_, err := GetUniqueKubernetesWorkloadResourceDefinition(&defs, "Service")
	if err == nil || !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected unmarshal error, got %v", err)
	}
}

// covers the by-name variant of the definition helper.
func TestGetUniqueKubernetesWorkloadResourceDefinitionByName_ReturnsSoleMatch(t *testing.T) {
	defs := makeDefinitions(svcFooJSON, svcBarJSON)
	got, err := GetUniqueKubernetesWorkloadResourceDefinitionByName(&defs, "Service", "bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(*got.JSONDefinition) != svcBarJSON {
		t.Errorf("returned wrong definition: %s", string(*got.JSONDefinition))
	}
}

// rejects a definition manifest missing kind.
func TestGetUniqueKubernetesWorkloadResourceDefinitionByName_RejectsMissingKind(t *testing.T) {
	defs := makeDefinitions(kindMissingJSON)
	_, err := GetUniqueKubernetesWorkloadResourceDefinitionByName(&defs, "Service", "foo")
	if err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("expected kind error, got %v", err)
	}
}

// rejects a definition manifest missing metadata.name.
func TestGetUniqueKubernetesWorkloadResourceDefinitionByName_RejectsMissingName(t *testing.T) {
	defs := makeDefinitions(nameMissingJSON)
	_, err := GetUniqueKubernetesWorkloadResourceDefinitionByName(&defs, "Service", "foo")
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("expected name error, got %v", err)
	}
}

// rejects when no definition matches the kind+name pair.
func TestGetUniqueKubernetesWorkloadResourceDefinitionByName_RejectsNotFound(t *testing.T) {
	defs := makeDefinitions(svcFooJSON)
	_, err := GetUniqueKubernetesWorkloadResourceDefinitionByName(&defs, "Service", "missing")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

// rejects when two definitions match the same kind+name.
func TestGetUniqueKubernetesWorkloadResourceDefinitionByName_RejectsMultiple(t *testing.T) {
	defs := makeDefinitions(svcFooJSON, svcFooJSON)
	_, err := GetUniqueKubernetesWorkloadResourceDefinitionByName(&defs, "Service", "foo")
	if err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected multiple-found error, got %v", err)
	}
}

// covers the non-unique lookup returning the sole kind+name match.
func TestGetKubernetesWorkloadResourceDefinition_ReturnsMatch(t *testing.T) {
	defs := makeDefinitions(svcFooJSON, svcBarJSON, deployFooJSON)
	got, err := GetKubernetesWorkloadResourceDefinition(&defs, "Service", "bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(*got.JSONDefinition) != svcBarJSON {
		t.Errorf("returned wrong definition: %s", string(*got.JSONDefinition))
	}
}

// rejects the non-unique lookup when nothing matches.
func TestGetKubernetesWorkloadResourceDefinition_RejectsNotFound(t *testing.T) {
	defs := makeDefinitions(svcFooJSON)
	_, err := GetKubernetesWorkloadResourceDefinition(&defs, "Service", "missing")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

// rejects the non-unique lookup when multiple manifests match.
func TestGetKubernetesWorkloadResourceDefinition_RejectsMultiple(t *testing.T) {
	defs := makeDefinitions(svcFooJSON, svcFooJSON)
	_, err := GetKubernetesWorkloadResourceDefinition(&defs, "Service", "foo")
	if err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected multiple-found error, got %v", err)
	}
}

// rejects the non-unique lookup when a manifest fails to parse.
func TestGetKubernetesWorkloadResourceDefinition_RejectsBadJSON(t *testing.T) {
	defs := makeDefinitions(invalidJSON)
	_, err := GetKubernetesWorkloadResourceDefinition(&defs, "Service", "foo")
	if err == nil || !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected unmarshal error, got %v", err)
	}
}

// covers the instance-side non-unique lookup returning the sole kind+name match.
func TestGetKubernetesWorkloadResourceInstance_ReturnsMatch(t *testing.T) {
	insts := makeInstances(svcFooJSON, svcBarJSON, deployFooJSON)
	got, err := GetKubernetesWorkloadResourceInstance(&insts, "Deployment", "foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(*got.JSONDefinition) != deployFooJSON {
		t.Errorf("returned wrong instance: %s", string(*got.JSONDefinition))
	}
}

// rejects when no instance matches the kind+name pair.
func TestGetKubernetesWorkloadResourceInstance_RejectsNotFound(t *testing.T) {
	insts := makeInstances(svcFooJSON)
	_, err := GetKubernetesWorkloadResourceInstance(&insts, "Service", "missing")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

// rejects when multiple instances match kind+name.
func TestGetKubernetesWorkloadResourceInstance_RejectsMultiple(t *testing.T) {
	insts := makeInstances(svcFooJSON, svcFooJSON)
	_, err := GetKubernetesWorkloadResourceInstance(&insts, "Service", "foo")
	if err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected multiple-found error, got %v", err)
	}
}

// rejects when an instance JSON is unparseable.
func TestGetKubernetesWorkloadResourceInstance_RejectsBadJSON(t *testing.T) {
	insts := makeInstances(invalidJSON)
	_, err := GetKubernetesWorkloadResourceInstance(&insts, "Service", "foo")
	if err == nil || !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected unmarshal error, got %v", err)
	}
}

// covers the instance unmarshal wrapper: returns the parsed manifest for the
// sole kind match.
func TestUnmarshalUniqueKubernetesWorkloadResourceInstance_ReturnsParsedManifest(t *testing.T) {
	insts := makeInstances(svcFooJSON, deployFooJSON)
	m, err := UnmarshalUniqueKubernetesWorkloadResourceInstance(&insts, "Service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["kind"] != "Service" {
		t.Errorf("kind = %v, want Service", m["kind"])
	}
}

// rejects the instance unmarshal wrapper when the underlying getter fails.
func TestUnmarshalUniqueKubernetesWorkloadResourceInstance_RejectsGetterError(t *testing.T) {
	insts := makeInstances(deployFooJSON)
	_, err := UnmarshalUniqueKubernetesWorkloadResourceInstance(&insts, "Service")
	if err == nil || !strings.Contains(err.Error(), "failed to get") {
		t.Fatalf("expected getter error, got %v", err)
	}
}

// covers the definition unmarshal wrapper's happy path.
func TestUnmarshalUniqueKubernetesWorkloadResourceDefinition_ReturnsParsedManifest(t *testing.T) {
	defs := makeDefinitions(svcFooJSON, deployFooJSON)
	m, err := UnmarshalUniqueKubernetesWorkloadResourceDefinition(&defs, "Deployment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["kind"] != "Deployment" {
		t.Errorf("kind = %v, want Deployment", m["kind"])
	}
}

// rejects the definition unmarshal wrapper when the underlying getter fails.
func TestUnmarshalUniqueKubernetesWorkloadResourceDefinition_RejectsGetterError(t *testing.T) {
	defs := makeDefinitions(svcFooJSON, svcBarJSON)
	_, err := UnmarshalUniqueKubernetesWorkloadResourceDefinition(&defs, "Service")
	if err == nil || !strings.Contains(err.Error(), "failed to get") {
		t.Fatalf("expected getter error, got %v", err)
	}
}

// covers the by-name definition unmarshal wrapper's happy path.
func TestUnmarshalUniqueKubernetesWorkloadResourceDefinitionByName_ReturnsParsedManifest(t *testing.T) {
	defs := makeDefinitions(svcFooJSON, svcBarJSON)
	m, err := UnmarshalUniqueKubernetesWorkloadResourceDefinitionByName(&defs, "Service", "bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["kind"] != "Service" {
		t.Errorf("kind = %v, want Service", m["kind"])
	}
	meta, ok := m["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata not a map: %T", m["metadata"])
	}
	if meta["name"] != "bar" {
		t.Errorf("name = %v, want bar", meta["name"])
	}
}

// rejects the by-name definition unmarshal wrapper on missing match.
func TestUnmarshalUniqueKubernetesWorkloadResourceDefinitionByName_RejectsGetterError(t *testing.T) {
	defs := makeDefinitions(svcFooJSON)
	_, err := UnmarshalUniqueKubernetesWorkloadResourceDefinitionByName(&defs, "Service", "missing")
	if err == nil || !strings.Contains(err.Error(), "failed to get") {
		t.Fatalf("expected getter error, got %v", err)
	}
}

// covers UnmarshalKubernetesWorkloadResourceDefinition's happy path.
func TestUnmarshalKubernetesWorkloadResourceDefinition_ReturnsParsedManifest(t *testing.T) {
	defs := makeDefinitions(svcFooJSON, svcBarJSON)
	m, err := UnmarshalKubernetesWorkloadResourceDefinition(&defs, "Service", "foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["kind"] != "Service" {
		t.Errorf("kind = %v, want Service", m["kind"])
	}
}

// rejects UnmarshalKubernetesWorkloadResourceDefinition when the getter fails.
func TestUnmarshalKubernetesWorkloadResourceDefinition_RejectsGetterError(t *testing.T) {
	defs := makeDefinitions(svcFooJSON)
	_, err := UnmarshalKubernetesWorkloadResourceDefinition(&defs, "Service", "missing")
	if err == nil || !strings.Contains(err.Error(), "failed to get") {
		t.Fatalf("expected getter error, got %v", err)
	}
}

// covers UnmarshalKubernetesWorkloadResourceInstance's happy path.
func TestUnmarshalKubernetesWorkloadResourceInstance_ReturnsParsedManifest(t *testing.T) {
	insts := makeInstances(svcFooJSON, deployFooJSON)
	m, err := UnmarshalKubernetesWorkloadResourceInstance(&insts, "Deployment", "foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["kind"] != "Deployment" {
		t.Errorf("kind = %v, want Deployment", m["kind"])
	}
}

// rejects UnmarshalKubernetesWorkloadResourceInstance when the getter fails.
func TestUnmarshalKubernetesWorkloadResourceInstance_RejectsGetterError(t *testing.T) {
	insts := makeInstances(svcFooJSON)
	_, err := UnmarshalKubernetesWorkloadResourceInstance(&insts, "Service", "missing")
	if err == nil || !strings.Contains(err.Error(), "failed to get") {
		t.Fatalf("expected getter error, got %v", err)
	}
}
