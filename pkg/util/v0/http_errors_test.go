package v0

import (
	"errors"
	"net/http"
	"testing"
)

// TestHttpErrorError asserts Error() returns the Message field verbatim.
func TestHttpErrorError(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{"non-empty message", "something broke", "something broke"},
		{"empty message", "", ""},
		{"message with special chars", "err: 500 \"oops\"\n", "err: 500 \"oops\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// build the error under test with only Message set
			e := &HttpError{Message: tc.message}

			// invoke Error() and verify the returned string matches Message
			if got := e.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestHttpErrorSatisfiesErrorInterface asserts *HttpError satisfies the error interface.
func TestHttpErrorSatisfiesErrorInterface(t *testing.T) {
	// assign *HttpError to an error variable to prove interface satisfaction at compile time
	var err error = &HttpError{Message: "boom"}

	// verify the wrapped value is still reachable via errors.As
	var target *HttpError
	if !errors.As(err, &target) {
		t.Fatalf("errors.As failed to unwrap *HttpError")
	}
	if target.Message != "boom" {
		t.Errorf("unwrapped Message = %q, want %q", target.Message, "boom")
	}
}

// TestHttpErrorGetStatusCode asserts GetStatusCode() returns the StatusCode field verbatim.
func TestHttpErrorGetStatusCode(t *testing.T) {
	tests := []struct {
		name string
		code int
	}{
		{"zero", 0},
		{"400", http.StatusBadRequest},
		{"500", http.StatusInternalServerError},
		{"negative", -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// build the error with only StatusCode set
			e := &HttpError{StatusCode: tc.code}

			// verify GetStatusCode returns exactly the stored code
			if got := e.GetStatusCode(); got != tc.code {
				t.Errorf("GetStatusCode() = %d, want %d", got, tc.code)
			}
		})
	}
}

// TestNewErrorConstructors asserts each constructor builds an HttpError with the message it received and the status code implied by its name.
func TestNewErrorConstructors(t *testing.T) {
	tests := []struct {
		name    string
		factory func(string) *HttpError
		message string
		want    int
	}{
		{"bad request", NewBadRequestError, "bad input", http.StatusBadRequest},
		{"unauthorized", NewUnauthorizedError, "no token", http.StatusUnauthorized},
		{"forbidden", NewForbiddenError, "denied", http.StatusForbidden},
		{"not found", NewNotFoundError, "missing", http.StatusNotFound},
		{"conflict", NewConflictError, "already exists", http.StatusConflict},
		{"empty message still constructs", NewBadRequestError, "", http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// invoke the constructor with the test message
			got := tc.factory(tc.message)

			// verify a non-nil pointer is returned
			if got == nil {
				t.Fatalf("factory returned nil")
			}
			// verify the message is threaded through unchanged
			if got.Message != tc.message {
				t.Errorf("Message = %q, want %q", got.Message, tc.message)
			}
			// verify the status code matches the constructor's contract
			if got.StatusCode != tc.want {
				t.Errorf("StatusCode = %d, want %d", got.StatusCode, tc.want)
			}
			// verify the accessor methods agree with the fields
			if got.Error() != tc.message {
				t.Errorf("Error() = %q, want %q", got.Error(), tc.message)
			}
			if got.GetStatusCode() != tc.want {
				t.Errorf("GetStatusCode() = %d, want %d", got.GetStatusCode(), tc.want)
			}
		})
	}
}
