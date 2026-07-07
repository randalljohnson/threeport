package v0

import (
	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// ErrWithEvent carries a specific event to record alongside an error.
// EventRecorder.HandleEventOverride unwraps this via errors.As and
// substitutes Event for the generic wrapper event, so the failure is
// stored as one specific-reason row instead of a paired
// specific-plus-generic pair.
type ErrWithEvent struct {
	// Message is the error message
	Message string

	// Event is the event that caused the error
	Event v0.Event
}

// Error returns the wrapped error message.
func (e *ErrWithEvent) Error() string {
	return e.Message
}
