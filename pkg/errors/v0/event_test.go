package v0

import (
	"errors"
	"testing"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestErrWithEventError covers ErrWithEvent.Error() returning the Message field verbatim.
func TestErrWithEventError(t *testing.T) {
	// build an ErrWithEvent with a known message and event
	reason := "SomeReason"
	err := &ErrWithEvent{
		Message: "failed to reconcile",
		Event: v0.Event{
			Reason: util.Ptr(reason),
		},
	}

	// Error() should return the Message field verbatim
	if got := err.Error(); got != "failed to reconcile" {
		t.Fatalf("Error() = %q, want %q", got, "failed to reconcile")
	}

	// empty message still returns the underlying string
	empty := &ErrWithEvent{}
	if got := empty.Error(); got != "" {
		t.Fatalf("Error() = %q, want empty string", got)
	}
}

// TestErrWithEventSatisfiesErrorInterface covers ErrWithEvent implementing the standard error interface.
func TestErrWithEventSatisfiesErrorInterface(t *testing.T) {
	// assign to error to prove interface satisfaction
	var e error = &ErrWithEvent{Message: "boom"}

	// errors.As should recover the concrete type
	var target *ErrWithEvent
	if !errors.As(e, &target) {
		t.Fatalf("errors.As failed to unwrap *ErrWithEvent")
	}

	// the recovered value should carry the original message
	if target.Message != "boom" {
		t.Fatalf("target.Message = %q, want %q", target.Message, "boom")
	}
}
