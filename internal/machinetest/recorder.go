package machinetest

import (
	"sync"

	logr "github.com/go-logr/logr"

	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// RecordedEvent captures the args of one RecordEvent call.
type RecordedEvent struct {
	Event    *v0.Event
	ObjectID uint
	Type     string
}

// FakeRecorder satisfies controller.Recorder by appending each call to a
// thread-safe slice. RecordErr, if non-nil, is returned from RecordEvent so
// tests can drive the "event recording failed" branches.
type FakeRecorder struct {
	mu        sync.Mutex
	events    []RecordedEvent
	RecordErr error
}

// NewFakeRecorder returns a FakeRecorder ready for use.
func NewFakeRecorder() *FakeRecorder {
	return &FakeRecorder{}
}

// RecordEvent records the call and returns RecordErr.
func (r *FakeRecorder) RecordEvent(
	event *v0.Event,
	objectId uint,
	fullyQualifiedObjectType string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, RecordedEvent{
		Event:    event,
		ObjectID: objectId,
		Type:     fullyQualifiedObjectType,
	})
	return r.RecordErr
}

// HandleEventOverride is a no-op for tests; the controller.Recorder
// interface requires it.
func (r *FakeRecorder) HandleEventOverride(
	event *v0.Event,
	objectId uint,
	fullyQualifiedObjectType string,
	err error,
	log *logr.Logger,
) {
}

// GetEvents returns a copy of the recorded events.
func (r *FakeRecorder) GetEvents() []RecordedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RecordedEvent, len(r.events))
	copy(out, r.events)
	return out
}

// GetReasons returns the Reason field of every recorded event in order.
func (r *FakeRecorder) GetReasons() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.events))
	for _, e := range r.events {
		if e.Event != nil && e.Event.Reason != nil {
			out = append(out, *e.Event.Reason)
		} else {
			out = append(out, "")
		}
	}
	return out
}
