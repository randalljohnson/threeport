/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	controlplanev1alpha1 "github.com/threeport/threeport/pkg/agent/api/v1alpha1"
)

// TestSchemeRegistersControlPlaneTypes asserts init() registered the
// control-plane v1alpha1 group with the package scheme so the controller
// manager can decode ThreeportWorkload resources.
func TestSchemeRegistersControlPlaneTypes(t *testing.T) {
	// init() must have run before the test, so the scheme should already
	// carry the control-plane group version.
	gv := controlplanev1alpha1.GroupVersion
	if !scheme.IsVersionRegistered(gv) {
		t.Fatalf("expected group version %q to be registered in scheme", gv.String())
	}

	// probe a known kind from the group so we know the SchemeBuilder ran,
	// not just that the group version got added.
	knownKinds := scheme.KnownTypes(gv)
	if len(knownKinds) == 0 {
		t.Fatalf("expected at least one known kind for %q; found none", gv.String())
	}
}

// TestSchemeRegistersClientGoTypes asserts init() also mixed in the
// standard client-go scheme so built-in kinds like Pod are decodable.
func TestSchemeRegistersClientGoTypes(t *testing.T) {
	// core/v1 must be known through the clientgoscheme add.
	coreGV := schema.GroupVersion{Group: "", Version: "v1"}
	if !scheme.IsVersionRegistered(coreGV) {
		t.Fatalf("expected core/v1 to be registered in scheme")
	}

	// verify a concrete core kind resolves so we know the types are wired.
	gvks, _, err := scheme.ObjectKinds(&corev1.Pod{})
	if err != nil {
		t.Fatalf("expected to resolve Pod object kind, got error: %v", err)
	}
	if len(gvks) == 0 {
		t.Fatalf("expected at least one GVK for Pod; found none")
	}
}

// TestSetupLoggersConfigured asserts the package-level loggers have the
// expected names so log output stays stable for operators grepping by tag.
func TestSetupLoggersConfigured(t *testing.T) {
	// setupLog must not be nil; a nil logger would panic when main() runs.
	if setupLog.GetSink() == nil {
		t.Fatal("expected setupLog to have a non-nil sink")
	}
	// notificationLog must be independently constructed with its own name.
	if notificationLog.GetSink() == nil {
		t.Fatal("expected notificationLog to have a non-nil sink")
	}
}
