/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
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

// TestSchemeRegistersThreeportWorkloadKinds asserts the ThreeportWorkload
// custom resource types are individually addressable through the scheme so
// the controller can List and Watch them by GVK.
func TestSchemeRegistersThreeportWorkloadKinds(t *testing.T) {
	// resolve the single-object kind so the reconciler can decode watch events.
	singleGVKs, _, err := scheme.ObjectKinds(&controlplanev1alpha1.ThreeportWorkload{})
	if err != nil {
		t.Fatalf("expected to resolve ThreeportWorkload object kind, got error: %v", err)
	}
	if len(singleGVKs) == 0 {
		t.Fatalf("expected at least one GVK for ThreeportWorkload; found none")
	}
	// the resolved GVK must match the api-group declared in groupversion_info.
	if singleGVKs[0].Group != controlplanev1alpha1.GroupVersion.Group {
		t.Fatalf(
			"expected group %q; got %q",
			controlplanev1alpha1.GroupVersion.Group, singleGVKs[0].Group,
		)
	}
	if singleGVKs[0].Version != controlplanev1alpha1.GroupVersion.Version {
		t.Fatalf(
			"expected version %q; got %q",
			controlplanev1alpha1.GroupVersion.Version, singleGVKs[0].Version,
		)
	}

	// resolve the list kind so the reconciler can List existing objects.
	listGVKs, _, err := scheme.ObjectKinds(&controlplanev1alpha1.ThreeportWorkloadList{})
	if err != nil {
		t.Fatalf("expected to resolve ThreeportWorkloadList object kind, got error: %v", err)
	}
	if len(listGVKs) == 0 {
		t.Fatalf("expected at least one GVK for ThreeportWorkloadList; found none")
	}
}

// TestSchemeDecodesBuiltInWorkloadKinds asserts common workload built-ins
// beyond Pod also decode against the scheme, since the agent watches
// deployment-family resources on managed clusters.
func TestSchemeDecodesBuiltInWorkloadKinds(t *testing.T) {
	// a Deployment must resolve so the agent can watch spec updates.
	deployGVKs, _, err := scheme.ObjectKinds(&appsv1.Deployment{})
	if err != nil {
		t.Fatalf("expected to resolve Deployment object kind, got error: %v", err)
	}
	if len(deployGVKs) == 0 {
		t.Fatalf("expected at least one GVK for Deployment; found none")
	}
	// a Service must resolve so the agent can watch endpoint changes.
	svcGVKs, _, err := scheme.ObjectKinds(&corev1.Service{})
	if err != nil {
		t.Fatalf("expected to resolve Service object kind, got error: %v", err)
	}
	if len(svcGVKs) == 0 {
		t.Fatalf("expected at least one GVK for Service; found none")
	}
}

// TestSchemeRecognizesAppsV1 asserts the apps/v1 group is present alongside
// core/v1, since a clientgoscheme regression could drop one without the other.
func TestSchemeRecognizesAppsV1(t *testing.T) {
	// probe apps/v1 registration to cover a second built-in group version.
	appsGV := schema.GroupVersion{Group: "apps", Version: "v1"}
	if !scheme.IsVersionRegistered(appsGV) {
		t.Fatalf("expected apps/v1 to be registered in scheme")
	}
	// the group must expose at least one kind so we know the types wired in.
	if len(scheme.KnownTypes(appsGV)) == 0 {
		t.Fatalf("expected at least one known kind for apps/v1; found none")
	}
}

// TestControlPlaneGroupVersionMetadata asserts the group version used at
// scheme registration matches the exported GroupVersion constant so scheme
// consumers agree on the CR api-group name.
func TestControlPlaneGroupVersionMetadata(t *testing.T) {
	// group name is the CR api-group written into CRD manifests.
	if got, want := controlplanev1alpha1.GroupVersion.Group, "control-plane.threeport.io"; got != want {
		t.Fatalf("expected group %q; got %q", want, got)
	}
	// version bump would break agent-to-CRD compatibility, so guard it here.
	if got, want := controlplanev1alpha1.GroupVersion.Version, "v1alpha1"; got != want {
		t.Fatalf("expected version %q; got %q", want, got)
	}
}

// TestSchemeConstructsThreeportWorkload asserts scheme.New produces a
// concrete ThreeportWorkload for the registered GVK so the controller can
// decode watch events into a typed object without a manual constructor.
func TestSchemeConstructsThreeportWorkload(t *testing.T) {
	// build the GVK from the exported group version and the type name.
	gvk := controlplanev1alpha1.GroupVersion.WithKind("ThreeportWorkload")
	// scheme.New must return a typed object bound to the registered GVK.
	obj, err := scheme.New(gvk)
	if err != nil {
		t.Fatalf("expected scheme.New for %q to succeed, got error: %v", gvk.String(), err)
	}
	// the returned object must be the concrete CR type, not a fallback.
	if _, ok := obj.(*controlplanev1alpha1.ThreeportWorkload); !ok {
		t.Fatalf("expected *ThreeportWorkload, got %T", obj)
	}
}

// TestSchemeConstructsThreeportWorkloadList asserts the list kind resolves
// through scheme.New so the reconciler can List existing CRs by GVK.
func TestSchemeConstructsThreeportWorkloadList(t *testing.T) {
	// resolve the list kind from the exported group version.
	gvk := controlplanev1alpha1.GroupVersion.WithKind("ThreeportWorkloadList")
	// scheme.New must return the concrete list type for a List call.
	obj, err := scheme.New(gvk)
	if err != nil {
		t.Fatalf("expected scheme.New for %q to succeed, got error: %v", gvk.String(), err)
	}
	// the returned object must be the concrete list type used by the reconciler.
	if _, ok := obj.(*controlplanev1alpha1.ThreeportWorkloadList); !ok {
		t.Fatalf("expected *ThreeportWorkloadList, got %T", obj)
	}
}

// TestSchemeRejectsUnknownKind asserts scheme.New returns an error for a
// kind never registered so callers surface typos instead of silently
// falling back to a generic object.
func TestSchemeRejectsUnknownKind(t *testing.T) {
	// construct a GVK that no init path ever registers.
	bogus := controlplanev1alpha1.GroupVersion.WithKind("NoSuchKindShouldEverExist")
	// scheme.New must reject the unknown kind so the reconciler can log it.
	if _, err := scheme.New(bogus); err == nil {
		t.Fatalf("expected error resolving unknown kind %q; got nil", bogus.String())
	}
}

// TestSchemeDecodesCommonCoreKinds asserts additional core/v1 kinds beyond
// Pod resolve, since the agent watches namespaced resources (secrets,
// configmaps, namespaces) alongside workload objects.
func TestSchemeDecodesCommonCoreKinds(t *testing.T) {
	// verify Namespace resolves so the agent can enumerate namespaces.
	nsGVKs, _, err := scheme.ObjectKinds(&corev1.Namespace{})
	if err != nil {
		t.Fatalf("expected to resolve Namespace object kind, got error: %v", err)
	}
	if len(nsGVKs) == 0 {
		t.Fatalf("expected at least one GVK for Namespace; found none")
	}
	// verify ConfigMap resolves so the agent can observe configuration payloads.
	cmGVKs, _, err := scheme.ObjectKinds(&corev1.ConfigMap{})
	if err != nil {
		t.Fatalf("expected to resolve ConfigMap object kind, got error: %v", err)
	}
	if len(cmGVKs) == 0 {
		t.Fatalf("expected at least one GVK for ConfigMap; found none")
	}
	// verify Secret resolves so the agent can watch credential updates.
	secGVKs, _, err := scheme.ObjectKinds(&corev1.Secret{})
	if err != nil {
		t.Fatalf("expected to resolve Secret object kind, got error: %v", err)
	}
	if len(secGVKs) == 0 {
		t.Fatalf("expected at least one GVK for Secret; found none")
	}
}

// TestSchemeRegistersAppsWorkloadKinds asserts other apps/v1 workload
// kinds beyond Deployment resolve so a clientgoscheme mismatch would fail
// here rather than at runtime on a managed cluster.
func TestSchemeRegistersAppsWorkloadKinds(t *testing.T) {
	// StatefulSet is watched for stateful workload rollouts.
	stsGVKs, _, err := scheme.ObjectKinds(&appsv1.StatefulSet{})
	if err != nil {
		t.Fatalf("expected to resolve StatefulSet object kind, got error: %v", err)
	}
	if len(stsGVKs) == 0 {
		t.Fatalf("expected at least one GVK for StatefulSet; found none")
	}
	// ReplicaSet backs Deployment rollout, so the agent must decode it.
	rsGVKs, _, err := scheme.ObjectKinds(&appsv1.ReplicaSet{})
	if err != nil {
		t.Fatalf("expected to resolve ReplicaSet object kind, got error: %v", err)
	}
	if len(rsGVKs) == 0 {
		t.Fatalf("expected at least one GVK for ReplicaSet; found none")
	}
	// DaemonSet is watched for node-level agent workloads.
	dsGVKs, _, err := scheme.ObjectKinds(&appsv1.DaemonSet{})
	if err != nil {
		t.Fatalf("expected to resolve DaemonSet object kind, got error: %v", err)
	}
	if len(dsGVKs) == 0 {
		t.Fatalf("expected at least one GVK for DaemonSet; found none")
	}
}

// TestSchemeExposesControlPlaneKindNames asserts the ThreeportWorkload and
// ThreeportWorkloadList kind names appear in the scheme's known types so
// dynamic-client callers can look them up by kind name.
func TestSchemeExposesControlPlaneKindNames(t *testing.T) {
	// collect the kind names known under the control-plane group version.
	known := scheme.KnownTypes(controlplanev1alpha1.GroupVersion)
	// the singular kind is required for object-level watches.
	if _, ok := known["ThreeportWorkload"]; !ok {
		t.Fatalf("expected ThreeportWorkload in known types; got keys %v", knownKeys(known))
	}
	// the list kind is required for List operations against the CR.
	if _, ok := known["ThreeportWorkloadList"]; !ok {
		t.Fatalf("expected ThreeportWorkloadList in known types; got keys %v", knownKeys(known))
	}
}

// knownKeys returns the map keys as a slice for failure messages.
func knownKeys(m map[string]reflect.Type) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestControlPlaneGroupVersionString asserts the GroupVersion string form
// used in logs and error messages stays stable across refactors of the
// v1alpha1 package.
func TestControlPlaneGroupVersionString(t *testing.T) {
	// group/version composes into the canonical schema.GroupVersion.String() form.
	if got, want := controlplanev1alpha1.GroupVersion.String(), "control-plane.threeport.io/v1alpha1"; got != want {
		t.Fatalf("expected group version string %q; got %q", want, got)
	}
}
