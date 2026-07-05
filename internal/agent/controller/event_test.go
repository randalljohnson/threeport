package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"github.com/threeport/threeport/internal/agent/notify"
)

// fakeSharedInformer captures the ResourceEventHandler passed to
// AddEventHandler so tests can trigger the callbacks directly. All other
// SharedInformer methods are no-ops; the code under test only touches
// AddEventHandler.
type fakeSharedInformer struct {
	handler cache.ResourceEventHandler
}

func (f *fakeSharedInformer) AddEventHandler(h cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	f.handler = h
	return nil, nil
}

func (f *fakeSharedInformer) AddEventHandlerWithResyncPeriod(h cache.ResourceEventHandler, _ time.Duration) (cache.ResourceEventHandlerRegistration, error) {
	f.handler = h
	return nil, nil
}

func (f *fakeSharedInformer) RemoveEventHandler(_ cache.ResourceEventHandlerRegistration) error {
	return nil
}
func (f *fakeSharedInformer) GetStore() cache.Store                     { return nil }
func (f *fakeSharedInformer) GetController() cache.Controller           { return nil }
func (f *fakeSharedInformer) Run(_ <-chan struct{})                     {}
func (f *fakeSharedInformer) HasSynced() bool                           { return true }
func (f *fakeSharedInformer) LastSyncResourceVersion() string           { return "" }
func (f *fakeSharedInformer) SetWatchErrorHandler(_ cache.WatchErrorHandler) error {
	return nil
}
func (f *fakeSharedInformer) SetTransform(_ cache.TransformFunc) error { return nil }
func (f *fakeSharedInformer) IsStopped() bool                          { return false }

// newTestReconciler builds a reconciler wired to a buffered notification
// channel large enough to hold the events a single test dispatches without
// blocking the AddFunc.
func newTestReconciler(bufSize int) (*ThreeportWorkloadReconciler, chan notify.ThreeportNotif) {
	ch := make(chan notify.ThreeportNotif, bufSize)
	r := &ThreeportWorkloadReconciler{
		NotifChan: &ch,
	}
	return r, ch
}

// buildEvent constructs a corev1.Event whose InvolvedObject UID and other
// fields the caller can set to steer the addEventEventHandlers logic.
func buildEvent(uid, involvedUID, namespace, kind, name, evtType, reason, message string, ts time.Time) *corev1.Event {
	return &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			UID: types.UID(uid),
		},
		InvolvedObject: corev1.ObjectReference{
			UID:       types.UID(involvedUID),
			Namespace: namespace,
			Kind:      kind,
			Name:      name,
		},
		LastTimestamp: metav1.NewTime(ts),
		Type:          evtType,
		Reason:        reason,
		Message:       message,
	}
}

// TestAddEventEventHandlers_RegistersHandlerOnInformer asserts the method
// installs its AddFunc onto the informer so subsequent add events reach the
// callback path.
func TestAddEventEventHandlers_RegistersHandlerOnInformer(t *testing.T) {
	// set up a reconciler and a fake informer that captures whatever handler
	// the method registers.
	r, _ := newTestReconciler(1)
	informer := &fakeSharedInformer{}

	// invoke the method under test to register the handler.
	r.addEventEventHandlers(context.Background(), "uid-1", "KubernetesWorkloadInstance", 1, 2, informer)

	// verify a handler was captured.
	if informer.handler == nil {
		t.Fatalf("expected AddEventHandler to be called; got nil handler")
	}
}

// TestAddEventEventHandlers_ForwardsMatchingEventWithResourceInstanceID
// covers the branch where a non-zero KubernetesWorkloadResourceInstanceID
// causes the EventSummary to carry both the workload-instance and
// workload-resource-instance IDs when the involved object UID matches.
func TestAddEventEventHandlers_ForwardsMatchingEventWithResourceInstanceID(t *testing.T) {
	// set up reconciler + informer with both workload IDs populated.
	r, ch := newTestReconciler(1)
	informer := &fakeSharedInformer{}
	ts := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// register the handler.
	r.addEventEventHandlers(context.Background(), "match-uid", "KubernetesWorkloadInstance", 7, 42, informer)

	// dispatch an event whose involved-object UID matches the resource UID.
	evt := buildEvent("evt-uid", "match-uid", "ns-a", "Pod", "pod-a", "Warning", "BackOff", "container backoff", ts)
	informer.handler.(cache.ResourceEventHandlerFuncs).AddFunc(evt)

	// verify a notification arrived and its EventSummary carries every
	// field including the resource-instance ID branch.
	select {
	case n := <-ch:
		if n.Event == nil {
			t.Fatalf("expected notification with populated Event; got nil")
		}
		if n.Event.EventUID != "evt-uid" {
			t.Errorf("EventUID: got %q, want %q", n.Event.EventUID, "evt-uid")
		}
		if n.Event.WorkloadType != "KubernetesWorkloadInstance" {
			t.Errorf("WorkloadType: got %q, want %q", n.Event.WorkloadType, "KubernetesWorkloadInstance")
		}
		if n.Event.KubernetesWorkloadInstanceID != 7 {
			t.Errorf("KubernetesWorkloadInstanceID: got %d, want 7", n.Event.KubernetesWorkloadInstanceID)
		}
		if n.Event.KubernetesWorkloadResourceInstanceID != 42 {
			t.Errorf("KubernetesWorkloadResourceInstanceID: got %d, want 42", n.Event.KubernetesWorkloadResourceInstanceID)
		}
		if n.Event.ObjectNamespace != "ns-a" || n.Event.ObjectKind != "Pod" || n.Event.ObjectName != "pod-a" {
			t.Errorf("involved-object fields: got %+v", n.Event)
		}
		if n.Event.Type != "Warning" || n.Event.Reason != "BackOff" || n.Event.Message != "container backoff" {
			t.Errorf("event content fields: got %+v", n.Event)
		}
		if !n.Event.Timestamp.Time.Equal(ts) {
			t.Errorf("Timestamp: got %v, want %v", n.Event.Timestamp.Time, ts)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for notification")
	}
}

// TestAddEventEventHandlers_ForwardsMatchingEventWithoutResourceInstanceID
// covers the branch where a zero KubernetesWorkloadResourceInstanceID takes
// the else path and the EventSummary omits the resource-instance ID.
func TestAddEventEventHandlers_ForwardsMatchingEventWithoutResourceInstanceID(t *testing.T) {
	// set up reconciler + informer with only the workload-instance ID
	// populated (resource-instance ID zero).
	r, ch := newTestReconciler(1)
	informer := &fakeSharedInformer{}
	ts := time.Date(2024, 2, 2, 8, 30, 0, 0, time.UTC)

	// register the handler.
	r.addEventEventHandlers(context.Background(), "match-uid", "HelmWorkloadInstance", 9, 0, informer)

	// dispatch a matching event.
	evt := buildEvent("evt-uid-2", "match-uid", "ns-b", "Deployment", "dep-b", "Normal", "Scheduled", "scheduled", ts)
	informer.handler.(cache.ResourceEventHandlerFuncs).AddFunc(evt)

	// verify the else-branch populated summary carries workload-instance ID
	// but leaves the resource-instance ID zero.
	select {
	case n := <-ch:
		if n.Event == nil {
			t.Fatalf("expected notification with populated Event; got nil")
		}
		if n.Event.KubernetesWorkloadInstanceID != 9 {
			t.Errorf("KubernetesWorkloadInstanceID: got %d, want 9", n.Event.KubernetesWorkloadInstanceID)
		}
		if n.Event.KubernetesWorkloadResourceInstanceID != 0 {
			t.Errorf("KubernetesWorkloadResourceInstanceID: got %d, want 0", n.Event.KubernetesWorkloadResourceInstanceID)
		}
		if n.Event.WorkloadType != "HelmWorkloadInstance" {
			t.Errorf("WorkloadType: got %q, want %q", n.Event.WorkloadType, "HelmWorkloadInstance")
		}
		if n.Event.EventUID != "evt-uid-2" {
			t.Errorf("EventUID: got %q, want %q", n.Event.EventUID, "evt-uid-2")
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for notification")
	}
}

// TestAddEventEventHandlers_SkipsEventWithNonMatchingUID asserts that events
// whose involved-object UID does not match the reconciler's resource UID are
// silently dropped, sending nothing on the notification channel.
func TestAddEventEventHandlers_SkipsEventWithNonMatchingUID(t *testing.T) {
	// set up reconciler + informer.
	r, ch := newTestReconciler(1)
	informer := &fakeSharedInformer{}

	// register the handler filtering on resource UID "expected-uid".
	r.addEventEventHandlers(context.Background(), "expected-uid", "KubernetesWorkloadInstance", 1, 2, informer)

	// dispatch an event whose involved object references a different UID.
	evt := buildEvent("evt-uid", "other-uid", "ns", "Pod", "other-pod", "Warning", "X", "Y", time.Now())
	informer.handler.(cache.ResourceEventHandlerFuncs).AddFunc(evt)

	// verify nothing was sent on the channel.
	select {
	case n := <-ch:
		t.Fatalf("expected no notification for non-matching UID; got %+v", n)
	case <-time.After(50 * time.Millisecond):
		// success: the handler correctly filtered the event out.
	}
}
