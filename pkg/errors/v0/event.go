package v0

import (
	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// ErrWithEvent is an error that carries the event to record for the failure.
// Returning it in place of a plain error avoids a generic failure row beside it.
type ErrWithEvent struct {
	// Message is the error message
	Message string

	// Event is the event that caused the error
	Event v0.Event

	// Cause is the error this one wraps, when there is one.
	Cause error
}

// Unwrap returns Cause so errors.Is reaches the dial error.
func (e *ErrWithEvent) Unwrap() error {
	return e.Cause
}

// Error returns the Message field.
func (e *ErrWithEvent) Error() string {
	return e.Message
}
