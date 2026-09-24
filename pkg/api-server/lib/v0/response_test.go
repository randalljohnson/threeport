package v0

import (
	"errors"
	"net/http"
	"testing"
)

// TestCreateResponse_SingleObject asserts a non-slice object is wrapped into a
// one-element Data slice and status defaults to 200 OK.
func TestCreateResponse_SingleObject(t *testing.T) {
	// build meta and a plain struct payload
	meta := SingleObjectMeta()
	obj := struct{ Name string }{Name: "widget"}

	// call under test
	resp, err := CreateResponse(meta, obj, "Widget")

	// no error and response is populated
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	// data length is exactly one
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 data element, got %d", len(resp.Data))
	}

	// type and meta are propagated
	if resp.Type != "Widget" {
		t.Errorf("expected Type=Widget, got %q", resp.Type)
	}
	if resp.Meta.ObjectCount != 1 {
		t.Errorf("expected ObjectCount=1, got %d", resp.Meta.ObjectCount)
	}

	// status defaults to 200 OK with empty error
	if resp.Status.Code != http.StatusOK {
		t.Errorf("expected status code %d, got %d", http.StatusOK, resp.Status.Code)
	}
	if resp.Status.Message != http.StatusText(http.StatusOK) {
		t.Errorf("expected status message %q, got %q", http.StatusText(http.StatusOK), resp.Status.Message)
	}
	if resp.Status.Error != "" {
		t.Errorf("expected empty error, got %q", resp.Status.Error)
	}
}

// TestCreateResponse_Slice asserts a slice payload has its elements copied
// element-by-element into the Data slice.
func TestCreateResponse_Slice(t *testing.T) {
	// build meta and a slice of two structs
	meta := &Meta{ObjectCount: 2}
	items := []struct{ ID int }{{ID: 1}, {ID: 2}}

	// call under test
	resp, err := CreateResponse(meta, items, "Item")

	// no error and data length matches input
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 data elements, got %d", len(resp.Data))
	}

	// elements are preserved in order
	first, ok := resp.Data[0].(struct{ ID int })
	if !ok || first.ID != 1 {
		t.Errorf("expected first element ID=1, got %+v", resp.Data[0])
	}
	second, ok := resp.Data[1].(struct{ ID int })
	if !ok || second.ID != 2 {
		t.Errorf("expected second element ID=2, got %+v", resp.Data[1])
	}
}

// TestCreateResponse_EmptySlice asserts an empty slice yields an empty Data
// slice, not nil.
func TestCreateResponse_EmptySlice(t *testing.T) {
	// slice with zero elements
	meta := &Meta{}
	items := []int{}

	// call under test
	resp, err := CreateResponse(meta, items, "Int")

	// no error and Data is a non-nil zero-length slice
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Data == nil {
		t.Fatal("expected non-nil Data slice")
	}
	if len(resp.Data) != 0 {
		t.Errorf("expected 0 data elements, got %d", len(resp.Data))
	}
}

// TestCreateResponse_NilArgs asserts nil meta or nil obj produces an error.
func TestCreateResponse_NilArgs(t *testing.T) {
	cases := []struct {
		name string
		meta *Meta
		obj  interface{}
	}{
		{name: "nil meta", meta: nil, obj: struct{}{}},
		{name: "nil obj", meta: &Meta{}, obj: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// call with nil argument
			resp, err := CreateResponse(tc.meta, tc.obj, "T")

			// error is returned and response is nil
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if resp != nil {
				t.Errorf("expected nil response, got %+v", resp)
			}
		})
	}
}

// TestSingleObjectMeta asserts the helper returns Meta with ObjectCount=1 and
// zero pagination.
func TestSingleObjectMeta(t *testing.T) {
	// call the helper
	meta := SingleObjectMeta()

	// ObjectCount is 1
	if meta == nil {
		t.Fatal("expected non-nil meta")
	}
	if meta.ObjectCount != 1 {
		t.Errorf("expected ObjectCount=1, got %d", meta.ObjectCount)
	}

	// pagination is the zero value
	if meta.Pagination != (Pagination{}) {
		t.Errorf("expected zero Pagination, got %+v", meta.Pagination)
	}
}

// TestUpdateResponseStatus asserts fields on Response.Status are overwritten
// with the given code, message, and error.
func TestUpdateResponseStatus(t *testing.T) {
	// start from a response with default status
	resp := &Response{Status: Status{Code: 200, Message: "OK", Error: ""}}

	// mutate to a 500 error status
	UpdateResponseStatus(resp, http.StatusInternalServerError, "Internal Server Error", "boom")

	// all three fields updated
	if resp.Status.Code != http.StatusInternalServerError {
		t.Errorf("expected code=500, got %d", resp.Status.Code)
	}
	if resp.Status.Message != "Internal Server Error" {
		t.Errorf("expected message=Internal Server Error, got %q", resp.Status.Message)
	}
	if resp.Status.Error != "boom" {
		t.Errorf("expected error=boom, got %q", resp.Status.Error)
	}
}

// TestCreateStatus asserts the constructor returns a Status matching all three
// inputs.
func TestCreateStatus(t *testing.T) {
	// build a Status via constructor
	s := CreateStatus(http.StatusTeapot, "I'm a teapot", "brew")

	// pointer non-nil and fields match
	if s == nil {
		t.Fatal("expected non-nil status")
	}
	if s.Code != http.StatusTeapot {
		t.Errorf("expected code=418, got %d", s.Code)
	}
	if s.Message != "I'm a teapot" {
		t.Errorf("unexpected message: %q", s.Message)
	}
	if s.Error != "brew" {
		t.Errorf("unexpected error: %q", s.Error)
	}
}

// TestCreateResponseErrorWithStatus asserts the response carries the given
// status, has an empty Meta with zero pagination, and nil Data.
func TestCreateResponseErrorWithStatus(t *testing.T) {
	// build a status and call the constructor
	status := &Status{Code: 404, Message: "Not Found", Error: "missing"}
	resp := CreateResponseErrorWithStatus(nil, status, "Widget")

	// status fields propagated
	if resp.Status.Code != 404 || resp.Status.Message != "Not Found" || resp.Status.Error != "missing" {
		t.Errorf("unexpected status: %+v", resp.Status)
	}

	// type propagated
	if resp.Type != "Widget" {
		t.Errorf("expected Type=Widget, got %q", resp.Type)
	}

	// Data is nil for error responses
	if resp.Data != nil {
		t.Errorf("expected nil Data, got %+v", resp.Data)
	}

	// Meta pagination is the zero value and object count is 0
	if resp.Meta.Pagination != (Pagination{}) {
		t.Errorf("expected zero Pagination, got %+v", resp.Meta.Pagination)
	}
	if resp.Meta.ObjectCount != 0 {
		t.Errorf("expected ObjectCount=0, got %d", resp.Meta.ObjectCount)
	}
}

// TestCreateResponseWithErrorNNN covers the numbered helpers, asserting each
// produces the matching HTTP status and standard message.
func TestCreateResponseWithErrorNNN(t *testing.T) {
	// table of helper + expected code
	cases := []struct {
		name string
		fn   func(*PageRequestParams, error, string) *Response
		code int
	}{
		{"400", CreateResponseWithError400, http.StatusBadRequest},
		{"401", CreateResponseWithError401, http.StatusUnauthorized},
		{"403", CreateResponseWithError403, http.StatusForbidden},
		{"404", CreateResponseWithError404, http.StatusNotFound},
		{"409", CreateResponseWithError409, http.StatusConflict},
		{"500", CreateResponseWithError500, http.StatusInternalServerError},
	}

	// common input error and object type
	inputErr := errors.New("something went wrong")
	const objType = "Widget"

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// call the numbered helper
			resp := tc.fn(nil, inputErr, objType)

			// code matches its HTTP status
			if resp.Status.Code != tc.code {
				t.Errorf("expected code %d, got %d", tc.code, resp.Status.Code)
			}
			// message is the standard http.StatusText
			if resp.Status.Message != http.StatusText(tc.code) {
				t.Errorf("expected message %q, got %q", http.StatusText(tc.code), resp.Status.Message)
			}
			// error is the input error's text
			if resp.Status.Error != inputErr.Error() {
				t.Errorf("expected error %q, got %q", inputErr.Error(), resp.Status.Error)
			}
			// type propagated
			if resp.Type != objType {
				t.Errorf("expected Type=%q, got %q", objType, resp.Type)
			}
			// no Data on error responses
			if resp.Data != nil {
				t.Errorf("expected nil Data, got %+v", resp.Data)
			}
		})
	}
}
