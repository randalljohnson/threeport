package v0

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	client_lib "github.com/threeport/threeport/pkg/client/lib/v0"
)

// TestGetEventsJoinAttachedObjectReferenceByQueryString_HappyPath asserts that
// a single-page 200 response decodes into a populated []Event and that the URL
// carries the caller's query string.
func TestGetEventsJoinAttachedObjectReferenceByQueryString_HappyPath(t *testing.T) {
	// setup: server captures the request URL and returns one Event row on a
	// single page (HasMore=false), so the pagination loop exits after one call
	var gotPath string
	var callCount int
	reason := "SomeReason"
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		resp := apiserver_lib.Response{
			Data: []apiserver_lib.Object{
				v0.Event{Reason: &reason},
			},
			Meta: apiserver_lib.Meta{
				Pagination: apiserver_lib.Pagination{HasMore: false},
			},
		}
		body, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("failed to marshal response: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(body); err != nil {
			t.Fatalf("failed to write response body: %v", err)
		}
	})
	defer server.Close()

	// action: call the lookup with a caller-supplied query string
	got, err := GetEventsJoinAttachedObjectReferenceByQueryString(
		&http.Client{},
		apiAddr,
		"objectid=42",
	)

	// assert: no error, one event decoded with the expected Reason
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil events slice pointer")
	}
	if len(*got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(*got))
	}
	if (*got)[0].Reason == nil || *(*got)[0].Reason != reason {
		t.Errorf("expected Reason=%q, got %+v", reason, (*got)[0].Reason)
	}

	// assert: URL targets the join endpoint with the caller's query string
	if !strings.Contains(gotPath, "/v0/events-join-attached-object-references") {
		t.Errorf("expected join endpoint path, got %q", gotPath)
	}
	if !strings.Contains(gotPath, "objectid=42") {
		t.Errorf("expected caller query in path, got %q", gotPath)
	}

	// assert: HasMore=false leaves only one API call
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}
}

// TestGetEventsJoinAttachedObjectReferenceByQueryString_MultiPage covers the
// pagination loop: the first response signals HasMore with a queryid and
// cursor, and the second response terminates the loop; both pages'
// events are concatenated into the returned slice.
func TestGetEventsJoinAttachedObjectReferenceByQueryString_MultiPage(t *testing.T) {
	// setup: server serves two pages of results; the first advertises HasMore,
	// the second finishes; capture every request's raw query so we can assert
	// the follow-up URL carries queryid and cursor
	reasonA := "A"
	reasonB := "B"
	var rawQueries []string
	call := 0
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		rawQueries = append(rawQueries, r.URL.RawQuery)
		call++
		var resp apiserver_lib.Response
		switch call {
		case 1:
			resp = apiserver_lib.Response{
				Data: []apiserver_lib.Object{v0.Event{Reason: &reasonA}},
				Meta: apiserver_lib.Meta{
					Pagination: apiserver_lib.Pagination{
						HasMore:    true,
						NextCursor: 7,
						QueryId:    "q-123",
					},
				},
			}
		case 2:
			resp = apiserver_lib.Response{
				Data: []apiserver_lib.Object{v0.Event{Reason: &reasonB}},
				Meta: apiserver_lib.Meta{
					Pagination: apiserver_lib.Pagination{HasMore: false},
				},
			}
		default:
			t.Fatalf("unexpected extra API call: %d", call)
		}
		body, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("failed to marshal response: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(body); err != nil {
			t.Fatalf("failed to write body: %v", err)
		}
	})
	defer server.Close()

	// action: call the lookup with an initial query string
	got, err := GetEventsJoinAttachedObjectReferenceByQueryString(
		&http.Client{},
		apiAddr,
		"objectid=42",
	)

	// assert: both pages decoded into the returned slice in order
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(*got) != 2 {
		t.Fatalf("expected 2 events, got %+v", got)
	}
	if *(*got)[0].Reason != "A" || *(*got)[1].Reason != "B" {
		t.Errorf("expected reasons [A, B], got [%v, %v]", (*got)[0].Reason, (*got)[1].Reason)
	}

	// assert: exactly two API calls were made
	if call != 2 {
		t.Errorf("expected 2 API calls, got %d", call)
	}

	// assert: first request carries only the caller's query; second request
	// appends queryid and cursor from the first response's pagination
	if !strings.Contains(rawQueries[0], "objectid=42") {
		t.Errorf("expected first request to carry caller query, got %q", rawQueries[0])
	}
	if strings.Contains(rawQueries[0], "queryid=") || strings.Contains(rawQueries[0], "cursor=") {
		t.Errorf("first request should not carry queryid/cursor, got %q", rawQueries[0])
	}
	if !strings.Contains(rawQueries[1], "queryid=q-123") {
		t.Errorf("expected queryid in follow-up query, got %q", rawQueries[1])
	}
	if !strings.Contains(rawQueries[1], "cursor=7") {
		t.Errorf("expected cursor=7 in follow-up query, got %q", rawQueries[1])
	}
	if !strings.Contains(rawQueries[1], "objectid=42") {
		t.Errorf("expected caller query preserved in follow-up, got %q", rawQueries[1])
	}
}

// TestGetEventsJoinAttachedObjectReferenceByQueryString_EmptyResult asserts
// that a single-page 200 with no data returns an empty (non-nil) slice.
func TestGetEventsJoinAttachedObjectReferenceByQueryString_EmptyResult(t *testing.T) {
	// setup: server returns an empty page with HasMore=false so the loop
	// exits immediately
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeOKResponse(t, w, []apiserver_lib.Object{})
	})
	defer server.Close()

	// action: call the lookup
	got, err := GetEventsJoinAttachedObjectReferenceByQueryString(
		&http.Client{},
		apiAddr,
		"objectid=42",
	)

	// assert: no error, non-nil slice pointer, and zero events
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil events slice pointer")
	}
	if len(*got) != 0 {
		t.Errorf("expected 0 events, got %d", len(*got))
	}
}

// TestGetEventsJoinAttachedObjectReferenceByQueryString_APIError asserts that
// a non-200 status from the API surfaces the client-lib sentinel through the
// wrapped error chain and that pagination stops on the first failure.
func TestGetEventsJoinAttachedObjectReferenceByQueryString_APIError(t *testing.T) {
	// setup: server always returns 404 with a threeport-shaped error body
	var callCount int
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		writeErrorResponse(t, w, http.StatusNotFound, "no events")
	})
	defer server.Close()

	// action: call the lookup
	_, err := GetEventsJoinAttachedObjectReferenceByQueryString(
		&http.Client{},
		apiAddr,
		"objectid=42",
	)

	// assert: sentinel ErrObjectNotFound is reachable via errors.Is
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrObjectNotFound) {
		t.Errorf("expected ErrObjectNotFound in chain, got %v", err)
	}

	// assert: pagination stopped after the first failing call
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}
}
