package controlplane

import (
	"testing"

	logr "github.com/go-logr/logr"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
)

// newDefinitionTestLogger returns a discard-backed logger that satisfies the
// *logr.Logger signature used by the reconciliation entry points.
func newDefinitionTestLogger() *logr.Logger {
	l := logr.Discard()
	return &l
}

// TestV0ControlPlaneDefinitionCreated_NoOp asserts the create reconciler is a
// stub returning zero requeue and nil error regardless of input state.
func TestV0ControlPlaneDefinitionCreated_NoOp(t *testing.T) {
	// nil reconciler and empty definition are safe because the function does not read them
	def := &v0.ControlPlaneDefinition{}

	// invoke the create reconciler with a discard logger
	requeue, err := v0ControlPlaneDefinitionCreated(nil, def, newDefinitionTestLogger())

	// assert no error and zero requeue delay
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0ControlPlaneDefinitionUpdated_NoOp asserts the update reconciler is a
// stub returning zero requeue and nil error regardless of input state.
func TestV0ControlPlaneDefinitionUpdated_NoOp(t *testing.T) {
	// nil reconciler and nil definition are safe because the function does not read them
	requeue, err := v0ControlPlaneDefinitionUpdated(nil, nil, newDefinitionTestLogger())

	// assert no error and zero requeue delay
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0ControlPlaneDefinitionDeleted_NoOp asserts the delete reconciler is a
// stub returning zero requeue and nil error regardless of input state.
func TestV0ControlPlaneDefinitionDeleted_NoOp(t *testing.T) {
	// pass an empty reconciler and definition; the function reads neither
	requeue, err := v0ControlPlaneDefinitionDeleted(&controller.Reconciler{}, &v0.ControlPlaneDefinition{}, newDefinitionTestLogger())

	// assert no error and zero requeue delay
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}
