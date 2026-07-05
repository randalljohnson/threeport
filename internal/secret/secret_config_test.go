package secret

import (
	"testing"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestGetSecretInstanceWorkloadTypeAndIdResolvesKubernetesWorkload asserts that
// a SecretInstance carrying a KubernetesWorkloadInstanceID resolves to the
// KubernetesWorkloadInstance type name and returns that same ID.
func TestGetSecretInstanceWorkloadTypeAndIdResolvesKubernetesWorkload(t *testing.T) {
	// build a secret instance attached only to a kubernetes workload
	id := uint(42)
	si := &v0.SecretInstance{
		KubernetesWorkloadInstanceID: &id,
	}

	// invoke the resolver
	typeName, gotID, err := getSecretInstanceWorkloadTypeAndId(si)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// verify the type name matches util.TypeName for KubernetesWorkloadInstance
	want := util.TypeName(v0.KubernetesWorkloadInstance{})
	if typeName != want {
		t.Fatalf("typeName = %q, want %q", typeName, want)
	}

	// verify the returned pointer is the same ID field on the input
	if gotID == nil || *gotID != id {
		t.Fatalf("workloadInstanceID = %v, want %d", gotID, id)
	}
}

// TestGetSecretInstanceWorkloadTypeAndIdResolvesHelmWorkload asserts that a
// SecretInstance carrying a HelmWorkloadInstanceID resolves to the
// HelmWorkloadInstance type name and returns that same ID.
func TestGetSecretInstanceWorkloadTypeAndIdResolvesHelmWorkload(t *testing.T) {
	// build a secret instance attached only to a helm workload
	id := uint(7)
	si := &v0.SecretInstance{
		HelmWorkloadInstanceID: &id,
	}

	// invoke the resolver
	typeName, gotID, err := getSecretInstanceWorkloadTypeAndId(si)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// verify the type name matches util.TypeName for HelmWorkloadInstance
	want := util.TypeName(v0.HelmWorkloadInstance{})
	if typeName != want {
		t.Fatalf("typeName = %q, want %q", typeName, want)
	}

	// verify the returned pointer is the same ID field on the input
	if gotID == nil || *gotID != id {
		t.Fatalf("workloadInstanceID = %v, want %d", gotID, id)
	}
}

// TestGetSecretInstanceWorkloadTypeAndIdRejectsMissingWorkloadId asserts that a
// SecretInstance with neither workload attachment returns an error and empty
// resolution values.
func TestGetSecretInstanceWorkloadTypeAndIdRejectsMissingWorkloadId(t *testing.T) {
	// build a secret instance with no workload attachment
	si := &v0.SecretInstance{}

	// invoke the resolver
	typeName, gotID, err := getSecretInstanceWorkloadTypeAndId(si)

	// verify an error surfaces and no type or ID is returned
	if err == nil {
		t.Fatalf("expected error, got typeName=%q id=%v", typeName, gotID)
	}
	if typeName != "" {
		t.Fatalf("typeName = %q, want empty", typeName)
	}
	if gotID != nil {
		t.Fatalf("workloadInstanceID = %v, want nil", gotID)
	}
}

// TestPushSecretIsNoOpWhenNoProviderConfigured asserts that PushSecret() with a
// SecretDefinition that has no AwsProviderID takes the no-op path and returns
// nil without touching the reconciler.
func TestPushSecretIsNoOpWhenNoProviderConfigured(t *testing.T) {
	// build a config with a definition that has no provider configured
	c := &SecretDefinitionConfig{
		secretDefinition: &v0.SecretDefinition{},
	}

	// invoke PushSecret; the switch has no matching case, so it must return nil
	if err := c.PushSecret(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDeleteSecretIsNoOpWhenNoProviderConfigured asserts that DeleteSecret()
// with a SecretDefinition that has no AwsProviderID takes the no-op path and
// returns nil without touching the reconciler.
func TestDeleteSecretIsNoOpWhenNoProviderConfigured(t *testing.T) {
	// build a config with a definition that has no provider configured
	c := &SecretDefinitionConfig{
		secretDefinition: &v0.SecretDefinition{},
	}

	// invoke DeleteSecret; the switch has no matching case, so it must return nil
	if err := c.DeleteSecret(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestV0SecretDefinitionCreatedNoProviderReturnsZero asserts that
// v0SecretDefinitionCreated() with no provider configured drives PushSecret()
// down the no-op branch and returns a zero requeue delay with no error.
func TestV0SecretDefinitionCreatedNoProviderReturnsZero(t *testing.T) {
	// build a secret definition with no provider configured
	sd := &v0.SecretDefinition{}

	// invoke the created handler with nil reconciler and logger; both are
	// unreachable on the no-provider path
	delay, err := v0SecretDefinitionCreated(nil, sd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// verify the handler requests no requeue
	if delay != 0 {
		t.Fatalf("delay = %d, want 0", delay)
	}
}

// TestV0SecretDefinitionUpdatedIsNoOp asserts that v0SecretDefinitionUpdated()
// is a stub that always returns a zero requeue delay with no error.
func TestV0SecretDefinitionUpdatedIsNoOp(t *testing.T) {
	// invoke the updated handler with all nil arguments; the stub ignores them
	delay, err := v0SecretDefinitionUpdated(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// verify the stub requests no requeue
	if delay != 0 {
		t.Fatalf("delay = %d, want 0", delay)
	}
}

// TestV0SecretDefinitionDeletedNoProviderReturnsZero asserts that
// v0SecretDefinitionDeleted() with no provider configured drives DeleteSecret()
// down the no-op branch and returns a zero requeue delay with no error.
func TestV0SecretDefinitionDeletedNoProviderReturnsZero(t *testing.T) {
	// build a secret definition with no provider configured
	sd := &v0.SecretDefinition{}

	// invoke the deleted handler with nil reconciler and logger; both are
	// unreachable on the no-provider path
	delay, err := v0SecretDefinitionDeleted(nil, sd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// verify the handler requests no requeue
	if delay != 0 {
		t.Fatalf("delay = %d, want 0", delay)
	}
}

// TestGetSecretInstanceOperationsAppendsSecretObjectsOperation asserts that
// getSecretInstanceOperations() returns an Operations stack with exactly one
// operation named "secret objects" and non-nil Create and Delete functions.
func TestGetSecretInstanceOperationsAppendsSecretObjectsOperation(t *testing.T) {
	// build a bare config; the operation methods are not invoked here
	c := &SecretInstanceConfig{}

	// invoke the builder
	ops := c.getSecretInstanceOperations()

	// verify the stack holds a single operation
	if ops == nil {
		t.Fatal("expected non-nil operations")
	}
	if got := len(ops.Operations); got != 1 {
		t.Fatalf("operation count = %d, want 1", got)
	}

	// verify the operation is named "secret objects" and wires Create and Delete
	op := ops.Operations[0]
	if op.Name != "secret objects" {
		t.Fatalf("operation name = %q, want \"secret objects\"", op.Name)
	}
	if op.Create == nil {
		t.Fatal("operation Create is nil")
	}
	if op.Delete == nil {
		t.Fatal("operation Delete is nil")
	}
}
