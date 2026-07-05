package versions

import (
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	apiserver_v0 "github.com/threeport/threeport/pkg/api-server/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
)

// TestAddAttachedObjectReferenceVersions covers
// AddAttachedObjectReferenceVersions: populating the per-object
// TaggedFields map, registering the version object in
// ObjectTaggedFields, and adding the object under ObjectVersions.
func TestAddAttachedObjectReferenceVersions(t *testing.T) {
	// isolate the package-level maps for this subtest
	defer resetObjectVersionsMap(t)()
	defer resetObjectTaggedFields(t)()

	// action: register the AttachedObjectReference version
	AddAttachedObjectReferenceVersions()

	// assert: side effects landed on all three maps
	assertObservabilityVersionRegistered(
		t,
		apiserver_v0.AttachedObjectReferenceTaggedFields,
		string(api_v0.ObjectTypeAttachedObjectReference),
	)
}

// TestAddEventVersions covers AddEventVersions and the same three side
// effects as its siblings.
func TestAddEventVersions(t *testing.T) {
	defer resetObjectVersionsMap(t)()
	defer resetObjectTaggedFields(t)()

	AddEventVersions()

	assertObservabilityVersionRegistered(
		t,
		apiserver_v0.EventTaggedFields,
		string(api_v0.ObjectTypeEvent),
	)
}

// TestAddVersions covers the AddVersions orchestrator: every Add*Versions
// call it dispatches runs without panicking, and a representative sample
// of object registrations lands in ObjectVersions.
func TestAddVersions(t *testing.T) {
	// isolate the package-level maps for this subtest
	defer resetObjectVersionsMap(t)()
	defer resetObjectTaggedFields(t)()

	// action: run the orchestrator that boot code invokes once at startup
	AddVersions()

	// assert: a representative sample of expected object registrations
	// exist under ObjectVersions with a v0 entry. The sample covers
	// several codegen buckets (core, attached-object, event, module,
	// per-provider machine/kubernetes runtime) so a regression in any of
	// the dispatched Add*Versions functions surfaces here.
	expected := []string{
		string(api_v0.ObjectTypeAttachedObjectReference),
		string(api_v0.ObjectTypeEvent),
		string(api_v0.ObjectTypeProfile),
		string(api_v0.ObjectTypeTier),
		string(api_v0.ObjectTypeControlPlaneDefinition),
		string(api_v0.ObjectTypeControlPlaneInstance),
		string(api_v0.ObjectTypeKubernetesRuntimeDefinition),
		string(api_v0.ObjectTypeKubernetesRuntimeInstance),
		string(api_v0.ObjectTypeAwsProvider),
		string(api_v0.ObjectTypeGcpProvider),
		string(api_v0.ObjectTypeOciProvider),
		string(api_v0.ObjectTypeMachineRuntimeDefinition),
		string(api_v0.ObjectTypeMachineRuntimeInstance),
		string(api_v0.ObjectTypeMachineWorkloadDefinition),
		string(api_v0.ObjectTypeMachineWorkloadInstance),
		string(api_v0.ObjectTypeHelmWorkloadDefinition),
		string(api_v0.ObjectTypeHelmWorkloadInstance),
		string(api_v0.ObjectTypeKubernetesWorkloadDefinition),
		string(api_v0.ObjectTypeKubernetesWorkloadInstance),
		string(api_v0.ObjectTypeSecretDefinition),
		string(api_v0.ObjectTypeSecretInstance),
		string(api_v0.ObjectTypeTerraformDefinition),
		string(api_v0.ObjectTypeTerraformInstance),
		string(api_v0.ObjectTypeGatewayDefinition),
		string(api_v0.ObjectTypeGatewayInstance),
		string(api_v0.ObjectTypeLoggingDefinition),
		string(api_v0.ObjectTypeLoggingInstance),
		string(api_v0.ObjectTypeMetricsDefinition),
		string(api_v0.ObjectTypeMetricsInstance),
		string(api_v0.ObjectTypeObservabilityStackDefinition),
		string(api_v0.ObjectTypeObservabilityStackInstance),
		string(api_v0.ObjectTypeModuleApi),
		string(api_v0.ObjectTypeModuleApiRoute),
		string(api_v0.ObjectTypeModuleController),
		string(api_v0.ObjectTypeModuleObject),
	}
	for _, obj := range expected {
		versions, ok := apiserver_lib.ObjectVersions[obj]
		if !ok {
			t.Errorf("ObjectVersions missing key %q after AddVersions()", obj)
			continue
		}
		if versions.API != obj {
			t.Errorf("ObjectVersions[%q].API = %q, want %q", obj, versions.API, obj)
		}
		if len(versions.Versions) != 1 || versions.Versions[0] != "v0" {
			t.Errorf("ObjectVersions[%q].Versions = %v, want [v0]", obj, versions.Versions)
		}
	}
}
