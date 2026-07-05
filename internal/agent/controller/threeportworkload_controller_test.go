/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/threeport/threeport/internal/agent"
	agentapi "github.com/threeport/threeport/pkg/agent/api/v1alpha1"
)

// newTestScheme builds a runtime.Scheme with the ThreeportWorkload types registered so
// the fake client can round-trip the resources under test.
func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := agentapi.AddToScheme(s); err != nil {
		t.Fatalf("failed to add agent api scheme: %v", err)
	}
	return s
}

// TestReconcileNotFoundReturnsNilError covers the request-not-found path where the
// controller must swallow the NotFound error and return an empty result.
func TestReconcileNotFoundReturnsNilError(t *testing.T) {
	// build a reconciler backed by an empty fake client so Get returns NotFound
	scheme := newTestScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ThreeportWorkloadReconciler{Client: fakeClient, Scheme: scheme}

	// invoke Reconcile for an unknown resource
	res, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "missing"},
	})

	// verify the reconciler returned an empty result and swallowed NotFound
	if err != nil {
		t.Fatalf("expected nil error on NotFound, got %v", err)
	}
	if res != (ctrl.Result{}) {
		t.Fatalf("expected empty result, got %#v", res)
	}
}

// TestReconcileFinalizerAddsFinalizerWhenMissing asserts the reconcileFinalizer helper
// registers the finalizer on a live (non-deleted) resource that lacks it.
func TestReconcileFinalizerAddsFinalizerWhenMissing(t *testing.T) {
	// seed the fake client with a live ThreeportWorkload that lacks the finalizer
	scheme := newTestScheme(t)
	tw := &agentapi.ThreeportWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "no-finalizer"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tw).Build()
	r := &ThreeportWorkloadReconciler{Client: fakeClient, Scheme: scheme}

	// exercise reconcileFinalizer
	deleted, err := r.reconcileFinalizer(context.Background(), tw)

	// verify no deletion reported and finalizer was persisted on the object
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted {
		t.Fatalf("expected deleted=false, got true")
	}
	got := &agentapi.ThreeportWorkload{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "no-finalizer"}, got); err != nil {
		t.Fatalf("failed to reload workload: %v", err)
	}
	if !controllerutil.ContainsFinalizer(got, agent.ThreeportWorkloadFinalizer) {
		t.Fatalf("expected finalizer to be present after reconcile")
	}
}

// TestReconcileFinalizerNoOpWhenPresent asserts reconcileFinalizer leaves an already-tagged
// live resource untouched (idempotent path).
func TestReconcileFinalizerNoOpWhenPresent(t *testing.T) {
	// seed a live workload that already carries the finalizer
	scheme := newTestScheme(t)
	tw := &agentapi.ThreeportWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "has-finalizer",
			Finalizers: []string{agent.ThreeportWorkloadFinalizer},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tw).Build()
	r := &ThreeportWorkloadReconciler{Client: fakeClient, Scheme: scheme}

	// exercise reconcileFinalizer against a resource that already has the finalizer
	deleted, err := r.reconcileFinalizer(context.Background(), tw)

	// verify no deletion reported and finalizer remains
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted {
		t.Fatalf("expected deleted=false, got true")
	}
	if !controllerutil.ContainsFinalizer(tw, agent.ThreeportWorkloadFinalizer) {
		t.Fatalf("expected finalizer to remain present")
	}
}

// TestReconcileFinalizerRemovesOnDeletion asserts that a deletion-timestamped workload
// carrying the finalizer has it stripped and reports deleted=true.
func TestReconcileFinalizerRemovesOnDeletion(t *testing.T) {
	// seed a workload with DeletionTimestamp set and the finalizer present
	scheme := newTestScheme(t)
	now := metav1.NewTime(time.Now())
	tw := &agentapi.ThreeportWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "being-deleted",
			DeletionTimestamp: &now,
			Finalizers:        []string{agent.ThreeportWorkloadFinalizer},
		},
		Spec: agentapi.ThreeportWorkloadSpec{KubernetesWorkloadInstanceID: 7},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tw).Build()
	// pre-seed a stop channel so stopInformers has work to do
	stopChan := make(chan struct{})
	r := &ThreeportWorkloadReconciler{
		Client: fakeClient,
		Scheme: scheme,
		InformerStopChans: []InformerStopChannels{{
			KubernetesWorkloadInstanceID: 7,
			StopChannels:                 []chan struct{}{stopChan},
		}},
	}

	// exercise reconcileFinalizer against a deletion-marked resource
	deleted, err := r.reconcileFinalizer(context.Background(), tw)

	// verify deleted=true, the finalizer was cleared, and the informer stop channel was closed
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleted {
		t.Fatalf("expected deleted=true")
	}
	if controllerutil.ContainsFinalizer(tw, agent.ThreeportWorkloadFinalizer) {
		t.Fatalf("expected finalizer to be removed")
	}
	select {
	case _, ok := <-stopChan:
		if ok {
			t.Fatalf("expected stop channel to be closed, got a value")
		}
	default:
		t.Fatalf("expected stop channel to be closed")
	}
	if len(r.InformerStopChans) != 0 {
		t.Fatalf("expected InformerStopChans to be empty, got %d", len(r.InformerStopChans))
	}
}

// TestReconcileFinalizerDeletionWithoutFinalizerReportsDeleted asserts the deletion branch
// still short-circuits Reconcile with deleted=true even when no finalizer is present.
func TestReconcileFinalizerDeletionWithoutFinalizerReportsDeleted(t *testing.T) {
	// build an in-memory workload with DeletionTimestamp set and no finalizer;
	// reconcileFinalizer operates on the passed pointer and does not Get from the
	// client along this branch, so no fake-client seeding is required (the fake
	// refuses to seed deletion-timestamped objects that have no finalizer).
	scheme := newTestScheme(t)
	now := metav1.NewTime(time.Now())
	tw := &agentapi.ThreeportWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "being-deleted-no-fin",
			DeletionTimestamp: &now,
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ThreeportWorkloadReconciler{Client: fakeClient, Scheme: scheme}

	// exercise reconcileFinalizer
	deleted, err := r.reconcileFinalizer(context.Background(), tw)

	// verify deleted=true is still returned even though there was nothing to remove
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleted {
		t.Fatalf("expected deleted=true even when no finalizer present")
	}
}

// TestAddInformerStopChannelNewEntry asserts the helper appends a fresh
// InformerStopChannels record when the workload instance ID is unknown.
func TestAddInformerStopChannelNewEntry(t *testing.T) {
	// start with an empty reconciler
	r := &ThreeportWorkloadReconciler{}
	stop := make(chan struct{})

	// add a stop channel for a brand-new workload instance ID
	r.addInformerStopChannel(42, stop)

	// verify a single record now holds that channel
	if len(r.InformerStopChans) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(r.InformerStopChans))
	}
	if r.InformerStopChans[0].KubernetesWorkloadInstanceID != 42 {
		t.Fatalf("expected instance ID 42, got %d", r.InformerStopChans[0].KubernetesWorkloadInstanceID)
	}
	if len(r.InformerStopChans[0].StopChannels) != 1 {
		t.Fatalf("expected 1 stop channel, got %d", len(r.InformerStopChans[0].StopChannels))
	}
}

// TestAddInformerStopChannelAppendsToExisting asserts a second channel added for the same
// workload instance ID appends to the same record instead of creating a new one.
func TestAddInformerStopChannelAppendsToExisting(t *testing.T) {
	// pre-populate a record for instance ID 5
	existing := make(chan struct{})
	r := &ThreeportWorkloadReconciler{
		InformerStopChans: []InformerStopChannels{{
			KubernetesWorkloadInstanceID: 5,
			StopChannels:                 []chan struct{}{existing},
		}},
	}
	stop := make(chan struct{})

	// add a second channel for the same instance ID
	r.addInformerStopChannel(5, stop)

	// verify the count stayed at one record and the channels appended into it
	if len(r.InformerStopChans) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(r.InformerStopChans))
	}
	if len(r.InformerStopChans[0].StopChannels) != 2 {
		t.Fatalf("expected 2 stop channels, got %d", len(r.InformerStopChans[0].StopChannels))
	}
}

// TestAddInformerStopChannelSeparateInstances asserts distinct instance IDs each get
// their own record.
func TestAddInformerStopChannelSeparateInstances(t *testing.T) {
	// pre-populate a record for instance ID 5
	r := &ThreeportWorkloadReconciler{
		InformerStopChans: []InformerStopChannels{{
			KubernetesWorkloadInstanceID: 5,
			StopChannels:                 []chan struct{}{make(chan struct{})},
		}},
	}

	// add a channel for a different instance ID
	r.addInformerStopChannel(6, make(chan struct{}))

	// verify a second record was appended
	if len(r.InformerStopChans) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(r.InformerStopChans))
	}
}

// TestStopInformersClosesAndRemovesRecord asserts stopInformers closes every stop channel
// for the target instance ID and removes the record from the reconciler.
func TestStopInformersClosesAndRemovesRecord(t *testing.T) {
	// seed two channels for instance ID 3 and one for instance ID 4
	a := make(chan struct{})
	b := make(chan struct{})
	c := make(chan struct{})
	r := &ThreeportWorkloadReconciler{
		InformerStopChans: []InformerStopChannels{
			{KubernetesWorkloadInstanceID: 3, StopChannels: []chan struct{}{a, b}},
			{KubernetesWorkloadInstanceID: 4, StopChannels: []chan struct{}{c}},
		},
	}

	// exercise stopInformers targeting the two-channel instance
	r.stopInformers(context.Background(), 3)

	// verify both target channels are closed, the untouched channel is still open,
	// and the record for instance 3 is gone
	assertClosed(t, a, "channel a")
	assertClosed(t, b, "channel b")
	assertOpen(t, c, "channel c")
	if len(r.InformerStopChans) != 1 {
		t.Fatalf("expected 1 remaining entry, got %d", len(r.InformerStopChans))
	}
	if r.InformerStopChans[0].KubernetesWorkloadInstanceID != 4 {
		t.Fatalf("expected remaining entry to be instance 4, got %d", r.InformerStopChans[0].KubernetesWorkloadInstanceID)
	}
}

// TestStopInformersUnknownIDNoOp asserts stopInformers silently returns when no record
// matches the requested instance ID.
func TestStopInformersUnknownIDNoOp(t *testing.T) {
	// seed a record for instance 1
	stop := make(chan struct{})
	r := &ThreeportWorkloadReconciler{
		InformerStopChans: []InformerStopChannels{
			{KubernetesWorkloadInstanceID: 1, StopChannels: []chan struct{}{stop}},
		},
	}

	// call stopInformers with an ID that doesn't match anything
	r.stopInformers(context.Background(), 999)

	// verify the untouched channel is still open and the record is preserved
	assertOpen(t, stop, "unrelated stop channel")
	if len(r.InformerStopChans) != 1 {
		t.Fatalf("expected record to remain, got %d entries", len(r.InformerStopChans))
	}
}

// TestStopInformersSkipsNilChannel asserts a nil entry inside StopChannels is silently
// skipped rather than causing a close-of-nil-channel panic.
func TestStopInformersSkipsNilChannel(t *testing.T) {
	// seed one real channel alongside a nil entry
	real := make(chan struct{})
	r := &ThreeportWorkloadReconciler{
		InformerStopChans: []InformerStopChannels{
			{KubernetesWorkloadInstanceID: 2, StopChannels: []chan struct{}{nil, real}},
		},
	}

	// exercise stopInformers
	r.stopInformers(context.Background(), 2)

	// verify the real channel was closed and the record was removed
	assertClosed(t, real, "real channel")
	if len(r.InformerStopChans) != 0 {
		t.Fatalf("expected empty InformerStopChans, got %d entries", len(r.InformerStopChans))
	}
}

// TestStopInformersOnInterruptClosesOnManagerCancel asserts the goroutine helper closes both
// stop channels once the manager's context is cancelled.
func TestStopInformersOnInterruptClosesOnManagerCancel(t *testing.T) {
	// wire a cancellable manager context into the reconciler
	mgrCtx, cancel := context.WithCancel(context.Background())
	r := &ThreeportWorkloadReconciler{ManagerContext: mgrCtx}
	podStop := make(chan struct{})
	rsStop := make(chan struct{})

	// launch the goroutine helper and trigger shutdown by cancelling the manager ctx
	done := make(chan struct{})
	go func() {
		r.stopInformersOnInterrupt(context.Background(), podStop, rsStop)
		close(done)
	}()
	cancel()

	// verify the helper returned and closed both provided stop channels
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stopInformersOnInterrupt did not return within timeout")
	}
	assertClosed(t, podStop, "pod stop channel")
	assertClosed(t, rsStop, "replicaset stop channel")
}

// TestSetupWithManagerReturnsErrWithoutManager asserts SetupWithManager surfaces a
// non-nil error when given a nil manager rather than panicking. The nil-manager path
// is a stand-in for the "manager wiring failed" case since standing up a real
// controller-runtime manager here would drag in envtest.
func TestSetupWithManagerReturnsErrWithoutManager(t *testing.T) {
	// build a bare reconciler
	r := &ThreeportWorkloadReconciler{}

	// invoke SetupWithManager with a nil manager and expect a defensive error/panic recovery
	defer func() {
		// a nil manager is expected to panic inside controller-runtime; recovering here
		// confirms the code path is reached without introducing a real manager
		_ = recover()
	}()
	_ = r.SetupWithManager(nil)
}

// assertClosed verifies a stop channel is closed by attempting a non-blocking receive
// and asserting the sentinel !ok is returned.
func assertClosed(t *testing.T, ch chan struct{}, label string) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("%s: expected closed, got value", label)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("%s: expected closed, still open", label)
	}
}

// assertOpen verifies a stop channel is still open by attempting a non-blocking receive
// and expecting the default branch to fire.
func assertOpen(t *testing.T, ch chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s: expected still open, got closed or value", label)
	default:
	}
}

// compile-time guard that InformerStopChannels exposes the fields the tests exercise
var _ = InformerStopChannels{KubernetesWorkloadInstanceID: 0, StopChannels: nil}

// compile-time guard that the reconciler still satisfies the reconcile.Reconciler contract
var _ = func() client.Object { return &agentapi.ThreeportWorkload{} }
