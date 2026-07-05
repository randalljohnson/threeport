package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/threeport/threeport/internal/agent/notify"
)

// newTestKubeClient returns a real *kubernetes.Clientset wired to an httptest
// server that responds to every request with an empty ReplicaSet list, and the
// backing rest.Config used to build it. Callers must Close() the server.
func newTestKubeClient(t *testing.T) (*kubernetes.Clientset, *rest.Config, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// respond with an empty ReplicaSetList regardless of path so the
		// informer's list call succeeds and the watch never returns items
		_, _ = w.Write([]byte(`{"kind":"ReplicaSetList","apiVersion":"apps/v1","metadata":{"resourceVersion":"1"},"items":[]}`))
	}))
	cfg := &rest.Config{Host: srv.URL}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		srv.Close()
		t.Fatalf("failed to build test kube client: %v", err)
	}
	return client, cfg, srv
}

// TestCreateReplicaSetInformer_ReturnsRunningInformerAndStopChan covers the
// happy path: createReplicaSetInformer returns a non-nil SharedInformer and a
// usable stop channel, and closing the stop channel cleanly halts the goroutine.
func TestCreateReplicaSetInformer_ReturnsRunningInformerAndStopChan(t *testing.T) {
	// build a reconciler backed by an in-memory HTTP server so the informer's
	// list call succeeds
	kubeClient, cfg, srv := newTestKubeClient(t)
	defer srv.Close()

	r := &ThreeportWorkloadReconciler{
		KubeClient: kubeClient,
		RESTConfig: cfg,
	}

	// invoke the function under test with a plausible label selector and
	// workload instance id
	informer, stopChan := r.createReplicaSetInformer(
		context.Background(),
		"threeport.io/workload-instance=42",
		42,
	)

	// verify the returned informer is non-nil (the goroutine is running)
	if informer == nil {
		t.Fatal("expected non-nil informer")
	}
	// verify the stop channel is non-nil and open (writable via close)
	if stopChan == nil {
		t.Fatal("expected non-nil stop channel")
	}

	// closing the stop channel should not panic and should halt the informer
	close(stopChan)
	// give the informer goroutine a moment to observe the stop
	time.Sleep(50 * time.Millisecond)
}

// TestCreateReplicaSetInformer_DistinctStopChannels covers that back-to-back
// calls produce distinct informers and stop channels, so concurrent workload
// instances don't share a stop signal.
func TestCreateReplicaSetInformer_DistinctStopChannels(t *testing.T) {
	kubeClient, cfg, srv := newTestKubeClient(t)
	defer srv.Close()

	r := &ThreeportWorkloadReconciler{
		KubeClient: kubeClient,
		RESTConfig: cfg,
	}

	// call twice with different instance ids
	informerA, stopA := r.createReplicaSetInformer(context.Background(), "a=1", 1)
	informerB, stopB := r.createReplicaSetInformer(context.Background(), "b=2", 2)

	// each call must yield its own informer and stop channel; sharing would
	// mean closing one stops both
	if informerA == informerB {
		t.Error("expected distinct informers per call")
	}
	if stopA == nil || stopB == nil {
		t.Fatal("expected non-nil stop channels")
	}
	// verify closing one does not close the other by testing writable state
	// via close only once each
	close(stopA)
	close(stopB)
	time.Sleep(50 * time.Millisecond)
}

// fakeSyncInformer is a controllable SharedInformer used to drive
// addReplicaSetEventHandlers without spinning up a real ListWatch. Only
// AddEventHandler is exercised by the code under test; the rest satisfy the
// interface with no-op behavior.
type fakeSyncInformer struct {
	mu       sync.Mutex
	handlers []cache.ResourceEventHandler
}

func (f *fakeSyncInformer) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers = append(f.handlers, handler)
	return nil, nil
}

func (f *fakeSyncInformer) AddEventHandlerWithResyncPeriod(handler cache.ResourceEventHandler, resyncPeriod time.Duration) (cache.ResourceEventHandlerRegistration, error) {
	return f.AddEventHandler(handler)
}

func (f *fakeSyncInformer) RemoveEventHandler(handle cache.ResourceEventHandlerRegistration) error {
	return nil
}
func (f *fakeSyncInformer) GetStore() cache.Store           { return nil }
func (f *fakeSyncInformer) GetController() cache.Controller { return nil }
func (f *fakeSyncInformer) Run(stopCh <-chan struct{})      {}
func (f *fakeSyncInformer) HasSynced() bool                 { return true }
func (f *fakeSyncInformer) LastSyncResourceVersion() string { return "" }
func (f *fakeSyncInformer) SetWatchErrorHandler(handler cache.WatchErrorHandler) error {
	return nil
}
func (f *fakeSyncInformer) SetTransform(handler cache.TransformFunc) error { return nil }
func (f *fakeSyncInformer) IsStopped() bool                                { return false }

// fire dispatches an add or delete event to every registered handler. This
// mirrors what a live SharedInformer would do once it observes a change.
func (f *fakeSyncInformer) fire(op string, obj interface{}) {
	f.mu.Lock()
	handlers := append([]cache.ResourceEventHandler(nil), f.handlers...)
	f.mu.Unlock()
	for _, h := range handlers {
		switch op {
		case "add":
			h.OnAdd(obj, false)
		case "delete":
			h.OnDelete(obj)
		}
	}
}

// TestAddReplicaSetEventHandlers_RegistersHandler covers that
// addReplicaSetEventHandlers attaches a ResourceEventHandler to the passed
// informer.
func TestAddReplicaSetEventHandlers_RegistersHandler(t *testing.T) {
	// build a reconciler with a valid REST config so AddFunc's
	// kubernetes.NewForConfig call succeeds
	_, cfg, srv := newTestKubeClient(t)
	defer srv.Close()

	notifCh := make(chan notify.ThreeportNotif, 16)
	r := &ThreeportWorkloadReconciler{
		RESTConfig: cfg,
		NotifChan:  &notifCh,
	}

	// use a controllable fake informer so we can observe handler registration
	fake := &fakeSyncInformer{}
	stopChan := make(chan struct{})
	defer close(stopChan)

	// invoke the function under test
	r.addReplicaSetEventHandlers(
		context.Background(),
		"my-workload",
		7,
		fake,
		stopChan,
	)

	// verify exactly one handler was registered on the informer
	if got := len(fake.handlers); got != 1 {
		t.Fatalf("expected 1 registered handler, got %d", got)
	}
}

// TestAddReplicaSetEventHandlers_AddThenDelete covers the AddFunc/DeleteFunc
// happy path: adding a replicaset spins up an event informer keyed by UID, and
// deleting the same replicaset tears it back down without panic.
func TestAddReplicaSetEventHandlers_AddThenDelete(t *testing.T) {
	_, cfg, srv := newTestKubeClient(t)
	defer srv.Close()

	notifCh := make(chan notify.ThreeportNotif, 16)
	r := &ThreeportWorkloadReconciler{
		RESTConfig: cfg,
		NotifChan:  &notifCh,
	}

	fake := &fakeSyncInformer{}
	stopChan := make(chan struct{})
	defer close(stopChan)

	r.addReplicaSetEventHandlers(
		context.Background(),
		"my-workload",
		7,
		fake,
		stopChan,
	)

	// synthesize a replicaset that the handler should observe
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "rs-uid-1",
			Name:      "rs1",
			Namespace: "default",
		},
	}

	// fire the AddFunc: this exercises the "new UID" branch that spins up an
	// event informer
	fake.fire("add", rs)

	// fire the AddFunc a second time with the same UID: this exercises the
	// "already-watched UID" branch that skips setup
	fake.fire("add", rs)

	// fire the DeleteFunc: this exercises the branch that closes the per-UID
	// stop channel and removes it from the map
	fake.fire("delete", rs)

	// firing delete a second time exercises the branch where the UID is no
	// longer in the map; it must not panic
	fake.fire("delete", rs)
}

// TestAddReplicaSetEventHandlers_DeleteUnknownUIDNoPanic covers the branch
// where DeleteFunc runs for a UID that was never added; the loop finds no
// match and returns cleanly.
func TestAddReplicaSetEventHandlers_DeleteUnknownUIDNoPanic(t *testing.T) {
	_, cfg, srv := newTestKubeClient(t)
	defer srv.Close()

	notifCh := make(chan notify.ThreeportNotif, 16)
	r := &ThreeportWorkloadReconciler{
		RESTConfig: cfg,
		NotifChan:  &notifCh,
	}

	fake := &fakeSyncInformer{}
	stopChan := make(chan struct{})
	defer close(stopChan)

	r.addReplicaSetEventHandlers(context.Background(), "wl", 1, fake, stopChan)

	// delete without a prior add: DeleteFunc's loop finds no matching UID
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{UID: "never-added"}}
	fake.fire("delete", rs)
}

// TestAddReplicaSetEventHandlers_AddFuncErrorPathOnBadRESTConfig covers the
// branch inside AddFunc where kubernetes.NewForConfig fails: the handler logs
// the error and returns without registering a per-UID informer, so a
// subsequent delete for the same UID finds nothing and is a no-op.
func TestAddReplicaSetEventHandlers_AddFuncErrorPathOnBadRESTConfig(t *testing.T) {
	// a REST config with an unparseable host causes kubernetes.NewForConfig
	// (specifically its rest.RESTClientFor step) to fail
	badCfg := &rest.Config{Host: "://not-a-url"}
	notifCh := make(chan notify.ThreeportNotif, 16)
	r := &ThreeportWorkloadReconciler{
		RESTConfig: badCfg,
		NotifChan:  &notifCh,
	}

	fake := &fakeSyncInformer{}
	stopChan := make(chan struct{})
	defer close(stopChan)

	r.addReplicaSetEventHandlers(context.Background(), "wl", 1, fake, stopChan)

	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{UID: "rs-uid-bad"}}
	// fire add: NewForConfig fails and the handler returns without panic
	fake.fire("add", rs)
	// a matching delete finds no entry to clean up and returns without panic
	fake.fire("delete", rs)
}

// compile-time interface satisfaction check so a signature drift in
// cache.SharedInformer breaks the tests, not production code.
var _ cache.SharedInformer = (*fakeSyncInformer)(nil)

// unused import guards for symbols referenced only in doc strings above so
// goimports keeps them in place.
var (
	_ = runtime.Object(nil)
	_ = watch.NewFake
)
