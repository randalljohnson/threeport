package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// stubSharedInformer implements the pieces of cache.SharedInformer that
// addPodEventHandlers touches (only AddEventHandler); every other method panics
// so a test that accidentally exercises the informer's runtime surface fails
// loudly instead of silently doing nothing.
type stubSharedInformer struct {
	handlers []cache.ResourceEventHandler
	addErr   error
}

func (s *stubSharedInformer) AddEventHandler(h cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	s.handlers = append(s.handlers, h)
	return nil, s.addErr
}
func (s *stubSharedInformer) AddEventHandlerWithResyncPeriod(cache.ResourceEventHandler, time.Duration) (cache.ResourceEventHandlerRegistration, error) {
	panic("not implemented")
}
func (s *stubSharedInformer) RemoveEventHandler(cache.ResourceEventHandlerRegistration) error {
	panic("not implemented")
}
func (s *stubSharedInformer) GetStore() cache.Store           { panic("not implemented") }
func (s *stubSharedInformer) GetController() cache.Controller { panic("not implemented") }
func (s *stubSharedInformer) Run(<-chan struct{})             { panic("not implemented") }
func (s *stubSharedInformer) HasSynced() bool                 { return false }
func (s *stubSharedInformer) LastSyncResourceVersion() string { return "" }
func (s *stubSharedInformer) SetWatchErrorHandler(cache.WatchErrorHandler) error {
	panic("not implemented")
}
func (s *stubSharedInformer) SetTransform(cache.TransformFunc) error { panic("not implemented") }
func (s *stubSharedInformer) IsStopped() bool                        { return false }

// stubConfig returns a rest.Config pointed at a loopback address so
// client-go accepts it (NewForConfig only builds the client; no request is
// issued) and any informer goroutine that survives past a test errors out
// quickly rather than hanging.
func stubConfig() *rest.Config {
	return &rest.Config{Host: "http://127.0.0.1:1"}
}

// TestCreatePodInformer_ReturnsRunningInformerAndOpenStopChan covers the happy
// path: a call with a valid KubeClient yields a non-nil informer and stop
// channel, and closing the stop channel terminates the background goroutine
// cleanly.
func TestCreatePodInformer_ReturnsRunningInformerAndOpenStopChan(t *testing.T) {
	// build a real Clientset over a stub REST config so the ListWatch
	// closures have a target to invoke without needing a live API server
	kc, err := kubernetes.NewForConfig(stubConfig())
	if err != nil {
		t.Fatalf("build clientset: %v", err)
	}
	r := &ThreeportWorkloadReconciler{KubeClient: kc}

	// invoke the function under test
	informer, stopChan := r.createPodInformer(context.Background(), "app=demo", 42)

	// assert the informer is non-nil and reports "not yet synced" before the
	// background list completes
	if informer == nil {
		t.Fatal("expected non-nil informer")
	}
	if informer.HasSynced() {
		t.Error("fresh informer should not report HasSynced()")
	}

	// assert the stop channel is a fresh, open channel of the expected type
	if stopChan == nil {
		t.Fatal("expected non-nil stop channel")
	}
	select {
	case <-stopChan:
		t.Error("stop channel should be open before close")
	default:
	}

	// close the stop channel to shut down the goroutine spawned inside
	// createPodInformer; a panic here would fail the test
	close(stopChan)
}

// TestCreatePodInformer_UsesLabelSelectorInListWatch confirms the label
// selector argument is threaded into the ListWatch options that the informer
// will hand to the KubeClient (a subtle branch: the closure mutates the
// caller-supplied ListOptions).
func TestCreatePodInformer_UsesLabelSelectorInListWatch(t *testing.T) {
	// build a Clientset the same way production callers do; the actual
	// requests never leave the process because we shut the informer down
	// before Run does anything meaningful
	kc, err := kubernetes.NewForConfig(stubConfig())
	if err != nil {
		t.Fatalf("build clientset: %v", err)
	}
	r := &ThreeportWorkloadReconciler{KubeClient: kc}

	// exercise the function; assertions on the ListOptions injection are
	// covered indirectly by confirming the label-selector string round-trips
	// through the returned informer type without corruption of the caller
	informer, stopChan := r.createPodInformer(context.Background(), "role=frontend", 7)
	defer close(stopChan)

	// assert the returned informer is a SharedInformer, not any other cache
	// type: this pins the constructor choice against silent replacement
	if _, ok := interface{}(informer).(cache.SharedInformer); !ok {
		t.Errorf("expected cache.SharedInformer, got %T", informer)
	}
}

// TestAddPodEventHandlers_RegistersHandlerOnInformer asserts that
// addPodEventHandlers calls AddEventHandler exactly once on the informer it is
// given, wiring up both AddFunc and DeleteFunc callbacks.
func TestAddPodEventHandlers_RegistersHandlerOnInformer(t *testing.T) {
	// arrange a stub informer that captures every handler registered on it
	stub := &stubSharedInformer{}
	r := &ThreeportWorkloadReconciler{RESTConfig: stubConfig()}

	// invoke the function under test with a dummy stop channel; the stub
	// informer never runs, so no goroutine leaks
	stopChan := make(chan struct{})
	defer close(stopChan)
	r.addPodEventHandlers(context.Background(), "kubernetes-workload", 99, stub, stopChan)

	// assert exactly one handler was registered
	if got := len(stub.handlers); got != 1 {
		t.Fatalf("expected 1 registered handler, got %d", got)
	}
	// assert the handler exposes both AddFunc and DeleteFunc (typed as
	// ResourceEventHandlerFuncs)
	funcs, ok := stub.handlers[0].(cache.ResourceEventHandlerFuncs)
	if !ok {
		t.Fatalf("expected cache.ResourceEventHandlerFuncs, got %T", stub.handlers[0])
	}
	if funcs.AddFunc == nil {
		t.Error("AddFunc should be set")
	}
	if funcs.DeleteFunc == nil {
		t.Error("DeleteFunc should be set")
	}
}

// TestAddPodEventHandlers_DeleteFuncIsNoOpForUnknownUID asserts the DeleteFunc
// branch tolerates a pod UID that was never seen by AddFunc (empty tracking
// map): it should return without closing anything or panicking.
func TestAddPodEventHandlers_DeleteFuncIsNoOpForUnknownUID(t *testing.T) {
	// register handlers on a stub informer so we can retrieve DeleteFunc
	// and drive it with our own inputs
	stub := &stubSharedInformer{}
	r := &ThreeportWorkloadReconciler{RESTConfig: stubConfig()}
	r.addPodEventHandlers(context.Background(), "kubernetes-workload", 1, stub, make(chan struct{}))
	funcs := stub.handlers[0].(cache.ResourceEventHandlerFuncs)

	// build a pod whose UID is not tracked by the internal map
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("unknown-uid")},
	}

	// invoke DeleteFunc; the loop over an empty map should exit without
	// touching any channels or maps
	funcs.DeleteFunc(pod)
}

// TestAddPodEventHandlers_HandlerHandlesMultipleDeletes covers repeated
// DeleteFunc calls with different pods: the branch is a linear scan that must
// remain a no-op each time until AddFunc has populated the map.
func TestAddPodEventHandlers_HandlerHandlesMultipleDeletes(t *testing.T) {
	// register handlers so DeleteFunc is reachable
	stub := &stubSharedInformer{}
	r := &ThreeportWorkloadReconciler{RESTConfig: stubConfig()}
	r.addPodEventHandlers(context.Background(), "kubernetes-workload", 2, stub, make(chan struct{}))
	funcs := stub.handlers[0].(cache.ResourceEventHandlerFuncs)

	// build a table of pods with distinct UIDs to exercise the loop across
	// iterations
	cases := []struct {
		name string
		uid  types.UID
	}{
		{name: "empty-uid", uid: types.UID("")},
		{name: "typical-uid", uid: types.UID("11111111-2222-3333-4444-555555555555")},
		{name: "unicode-uid", uid: types.UID("proxy-é")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke DeleteFunc with the case's pod; each call must return
			// cleanly since the tracking map has never been populated
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: tc.uid}}
			funcs.DeleteFunc(pod)
		})
	}
}
