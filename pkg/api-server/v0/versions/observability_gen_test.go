package versions

import (
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	apiserver_v0 "github.com/threeport/threeport/pkg/api-server/v0"
	api_lib "github.com/threeport/threeport/pkg/api/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
)

// resetObjectVersionsMap swaps in a clean ObjectVersions map so each test
// starts from a known-empty registration state without leaking into siblings.
func resetObjectVersionsMap(t *testing.T) func() {
	t.Helper()
	prev := apiserver_lib.ObjectVersions
	apiserver_lib.ObjectVersions = make(map[string]apiserver_lib.ApiObjectVersions)
	return func() { apiserver_lib.ObjectVersions = prev }
}

// resetObjectTaggedFields swaps in a clean ObjectTaggedFields map so each test
// verifies the current call's insertion without leaking into siblings.
func resetObjectTaggedFields(t *testing.T) func() {
	t.Helper()
	prev := apiserver_lib.ObjectTaggedFields
	apiserver_lib.ObjectTaggedFields = make(map[apiserver_lib.VersionObject]*apiserver_lib.FieldsByTag)
	return func() { apiserver_lib.ObjectTaggedFields = prev }
}

// assertObservabilityVersionRegistered checks the three side effects each
// Add*Versions function is expected to produce: the per-object TaggedFields
// map has a validate-tag entry, the global ObjectTaggedFields map has the
// matching version object, and the global ObjectVersions map records the
// object under its own key with a v0 version slice.
func assertObservabilityVersionRegistered(
	t *testing.T,
	perObjectMap map[string]*apiserver_lib.FieldsByTag,
	objectName string,
) {
	t.Helper()

	// verify the per-object map has a validate-tag entry populated
	entry, ok := perObjectMap[string(api_lib.ValidateTag)]
	if !ok {
		t.Fatalf("per-object map missing key %q for %s", api_lib.ValidateTag, objectName)
	}
	if entry == nil {
		t.Fatalf("per-object entry for %s is nil", objectName)
	}
	if entry.TagName != string(api_lib.ValidateTag) {
		t.Errorf("TagName = %q, want %q", entry.TagName, api_lib.ValidateTag)
	}

	// verify the global ObjectTaggedFields map has the version object
	versionObj := apiserver_lib.VersionObject{Object: objectName, Version: "v0"}
	globalEntry, ok := apiserver_lib.ObjectTaggedFields[versionObj]
	if !ok {
		t.Fatalf("ObjectTaggedFields missing entry for %+v", versionObj)
	}
	if globalEntry != entry {
		t.Errorf("global ObjectTaggedFields entry != per-object validate entry for %s", objectName)
	}

	// verify AddObjectVersion recorded the object in ObjectVersions
	versions, ok := apiserver_lib.ObjectVersions[objectName]
	if !ok {
		t.Fatalf("ObjectVersions missing key %q", objectName)
	}
	if versions.API != objectName {
		t.Errorf("ObjectVersions[%q].API = %q, want %q", objectName, versions.API, objectName)
	}
	if len(versions.Versions) != 1 || versions.Versions[0] != "v0" {
		t.Errorf("ObjectVersions[%q].Versions = %v, want [v0]", objectName, versions.Versions)
	}
}

// TestAddLoggingDefinitionVersions covers AddLoggingDefinitionVersions:
// populating the per-object TaggedFields map, registering the version object
// in ObjectTaggedFields, and adding the object under ObjectVersions.
func TestAddLoggingDefinitionVersions(t *testing.T) {
	// isolate the package-level maps for this subtest
	defer resetObjectVersionsMap(t)()
	defer resetObjectTaggedFields(t)()

	// action: register the LoggingDefinition version
	AddLoggingDefinitionVersions()

	// assert: side effects landed on all three maps
	assertObservabilityVersionRegistered(
		t,
		apiserver_v0.LoggingDefinitionTaggedFields,
		string(api_v0.ObjectTypeLoggingDefinition),
	)
}

// TestAddLoggingInstanceVersions covers AddLoggingInstanceVersions.
func TestAddLoggingInstanceVersions(t *testing.T) {
	defer resetObjectVersionsMap(t)()
	defer resetObjectTaggedFields(t)()

	AddLoggingInstanceVersions()

	assertObservabilityVersionRegistered(
		t,
		apiserver_v0.LoggingInstanceTaggedFields,
		string(api_v0.ObjectTypeLoggingInstance),
	)
}

// TestAddMetricsDefinitionVersions covers AddMetricsDefinitionVersions.
func TestAddMetricsDefinitionVersions(t *testing.T) {
	defer resetObjectVersionsMap(t)()
	defer resetObjectTaggedFields(t)()

	AddMetricsDefinitionVersions()

	assertObservabilityVersionRegistered(
		t,
		apiserver_v0.MetricsDefinitionTaggedFields,
		string(api_v0.ObjectTypeMetricsDefinition),
	)
}

// TestAddMetricsInstanceVersions covers AddMetricsInstanceVersions.
func TestAddMetricsInstanceVersions(t *testing.T) {
	defer resetObjectVersionsMap(t)()
	defer resetObjectTaggedFields(t)()

	AddMetricsInstanceVersions()

	assertObservabilityVersionRegistered(
		t,
		apiserver_v0.MetricsInstanceTaggedFields,
		string(api_v0.ObjectTypeMetricsInstance),
	)
}

// TestAddObservabilityDashboardDefinitionVersions covers
// AddObservabilityDashboardDefinitionVersions.
func TestAddObservabilityDashboardDefinitionVersions(t *testing.T) {
	defer resetObjectVersionsMap(t)()
	defer resetObjectTaggedFields(t)()

	AddObservabilityDashboardDefinitionVersions()

	assertObservabilityVersionRegistered(
		t,
		apiserver_v0.ObservabilityDashboardDefinitionTaggedFields,
		string(api_v0.ObjectTypeObservabilityDashboardDefinition),
	)
}

// TestAddObservabilityDashboardInstanceVersions covers
// AddObservabilityDashboardInstanceVersions.
func TestAddObservabilityDashboardInstanceVersions(t *testing.T) {
	defer resetObjectVersionsMap(t)()
	defer resetObjectTaggedFields(t)()

	AddObservabilityDashboardInstanceVersions()

	assertObservabilityVersionRegistered(
		t,
		apiserver_v0.ObservabilityDashboardInstanceTaggedFields,
		string(api_v0.ObjectTypeObservabilityDashboardInstance),
	)
}

// TestAddObservabilityStackDefinitionVersions covers
// AddObservabilityStackDefinitionVersions.
func TestAddObservabilityStackDefinitionVersions(t *testing.T) {
	defer resetObjectVersionsMap(t)()
	defer resetObjectTaggedFields(t)()

	AddObservabilityStackDefinitionVersions()

	assertObservabilityVersionRegistered(
		t,
		apiserver_v0.ObservabilityStackDefinitionTaggedFields,
		string(api_v0.ObjectTypeObservabilityStackDefinition),
	)
}

// TestAddObservabilityStackInstanceVersions covers
// AddObservabilityStackInstanceVersions.
func TestAddObservabilityStackInstanceVersions(t *testing.T) {
	defer resetObjectVersionsMap(t)()
	defer resetObjectTaggedFields(t)()

	AddObservabilityStackInstanceVersions()

	assertObservabilityVersionRegistered(
		t,
		apiserver_v0.ObservabilityStackInstanceTaggedFields,
		string(api_v0.ObjectTypeObservabilityStackInstance),
	)
}

// TestAddObservabilityVersions_Idempotent asserts that calling an Add*Versions
// function twice does not duplicate the entry in ObjectVersions; a second call
// with the same object+version pair is a no-op per AddObjectVersion semantics.
func TestAddObservabilityVersions_Idempotent(t *testing.T) {
	defer resetObjectVersionsMap(t)()
	defer resetObjectTaggedFields(t)()

	// action: register the same object twice
	AddLoggingDefinitionVersions()
	AddLoggingDefinitionVersions()

	// assert: Versions slice still has exactly one entry
	got := apiserver_lib.ObjectVersions[string(api_v0.ObjectTypeLoggingDefinition)].Versions
	if len(got) != 1 || got[0] != "v0" {
		t.Errorf("Versions = %v, want [v0] after duplicate call", got)
	}
}
