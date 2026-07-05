package v0

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	client_lib "github.com/threeport/threeport/pkg/client/lib/v0"
)

// writeCreatedResponse encodes an apiserver_lib.Response with the given data
// slice and writes it as a 201 response body.
func writeCreatedResponse(t *testing.T, w http.ResponseWriter, data []apiserver_lib.Object) {
	t.Helper()
	resp := apiserver_lib.Response{Data: data}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal created response: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if _, err := w.Write(body); err != nil {
		t.Fatalf("failed to write created response body: %v", err)
	}
}

// TestCreateModuleApiRouteWithModuleObjectReferences_HappyPath asserts a 201
// response decodes into the returned ModuleApiRoute, the request targets the
// module-api-route path with POST, and the marshalled request body carries
// the caller's payload.
func TestCreateModuleApiRouteWithModuleObjectReferences_HappyPath(t *testing.T) {
	// setup: capture the request and reply with a 201 carrying a route row
	var gotMethod, gotPath string
	var gotBody []byte
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		writeCreatedResponse(t, w, []apiserver_lib.Object{
			v0.ModuleApiRoute{Path: strPtr("/things")},
		})
	})
	defer server.Close()

	// action: create the route
	in := &v0.ModuleApiRoute{Path: strPtr("/things")}
	got, err := CreateModuleApiRouteWithModuleObjectReferences(&http.Client{}, apiAddr, in)

	// assert: no error and returned route decoded
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.Path == nil || *got.Path != "/things" {
		t.Fatalf("expected decoded route with Path=/things, got %+v", got)
	}
	// assert: request method and path match the create-route endpoint
	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %q", gotMethod)
	}
	if gotPath != v0.PathModuleApiRouteWithModuleObjectReferences {
		t.Errorf("expected path %q, got %q", v0.PathModuleApiRouteWithModuleObjectReferences, gotPath)
	}
	// assert: request body carries the marshalled input payload
	if !strings.Contains(string(gotBody), `"Path":"/things"`) {
		t.Errorf("expected request body to carry Path field, got %q", string(gotBody))
	}
}

// TestCreateModuleApiRouteWithModuleObjectReferences_APIError asserts that a
// non-201 status wraps into a "call to threeport API" error and does not
// return a decoded object.
func TestCreateModuleApiRouteWithModuleObjectReferences_APIError(t *testing.T) {
	// setup: server returns 409 conflict to simulate a duplicate route
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusConflict, "already exists")
	})
	defer server.Close()

	// action: attempt to create the route
	in := &v0.ModuleApiRoute{Path: strPtr("/dup")}
	got, err := CreateModuleApiRouteWithModuleObjectReferences(&http.Client{}, apiAddr, in)

	// assert: ErrConflict sentinel is reachable and payload input is returned as-is
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrConflict) {
		t.Errorf("expected ErrConflict in chain, got %v", err)
	}
	if !strings.Contains(err.Error(), "call to threeport API returned unexpected response") {
		t.Errorf("expected upstream-error wrapper, got %v", err)
	}
	if got != in {
		t.Errorf("expected input pointer returned on error, got %+v", got)
	}
}

// TestGetModuleObjectsWithModuleApiRoutes_HappyPathSinglePage asserts a single
// page response decodes into a slice of ModuleObjects and the request targets
// the module-objects endpoint with GET.
func TestGetModuleObjectsWithModuleApiRoutes_HappyPathSinglePage(t *testing.T) {
	// setup: capture the request and reply with one page, HasMore=false
	var gotMethod, gotPath string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		writePaginatedResponse(t, w, []apiserver_lib.Object{
			v0.ModuleObject{Name: strPtr("things")},
			v0.ModuleObject{Name: strPtr("widgets")},
		}, false, 0, "")
	})
	defer server.Close()

	// action: list module objects
	got, err := GetModuleObjectsWithModuleApiRoutes(&http.Client{}, apiAddr)

	// assert: no error, both rows decoded, and request targets the list endpoint
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil slice pointer")
	}
	if len(*got) != 2 {
		t.Fatalf("expected 2 objects, got %d", len(*got))
	}
	if (*got)[0].Name == nil || *(*got)[0].Name != "things" {
		t.Errorf("expected first object name=things, got %+v", (*got)[0])
	}
	if gotMethod != http.MethodGet {
		t.Errorf("expected GET, got %q", gotMethod)
	}
	if gotPath != v0.PathModuleObjectsWithModuleApiRoutes {
		t.Errorf("expected path %q, got %q", v0.PathModuleObjectsWithModuleApiRoutes, gotPath)
	}
}

// TestGetModuleObjectsWithModuleApiRoutes_HappyPathMultiPage asserts that the
// paginator issues follow-up requests carrying the queryid and cursor from the
// previous page and concatenates every page's data.
func TestGetModuleObjectsWithModuleApiRoutes_HappyPathMultiPage(t *testing.T) {
	// setup: server serves two pages; second request must include queryid + cursor
	var callCount int
	var secondQuery string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			// first page: HasMore=true, hand out cursor+queryid for the next request
			writePaginatedResponse(t, w, []apiserver_lib.Object{
				v0.ModuleObject{Name: strPtr("one")},
			}, true, 42, "q-abc")
		case 2:
			// second page: verify cursor+queryid propagated, then terminate pagination
			secondQuery = r.URL.RawQuery
			writePaginatedResponse(t, w, []apiserver_lib.Object{
				v0.ModuleObject{Name: strPtr("two")},
			}, false, 0, "")
		default:
			t.Fatalf("unexpected extra request #%d", callCount)
		}
	})
	defer server.Close()

	// action: list module objects across pages
	got, err := GetModuleObjectsWithModuleApiRoutes(&http.Client{}, apiAddr)

	// assert: no error, both pages concatenated
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
	if got == nil || len(*got) != 2 {
		t.Fatalf("expected 2 objects across pages, got %+v", got)
	}
	if (*got)[0].Name == nil || *(*got)[0].Name != "one" || *(*got)[1].Name != "two" {
		t.Errorf("expected concatenation order one,two got %+v", got)
	}
	// assert: pagination fields propagate to the second request
	if !strings.Contains(secondQuery, "queryid=q-abc") {
		t.Errorf("expected queryid=q-abc on second request, got %q", secondQuery)
	}
	if !strings.Contains(secondQuery, "cursor=42") {
		t.Errorf("expected cursor=42 on second request, got %q", secondQuery)
	}
}

// TestGetModuleObjectsWithModuleApiRoutes_APIError asserts an upstream 500
// returns a wrapped error and a non-nil zero-value slice pointer.
func TestGetModuleObjectsWithModuleApiRoutes_APIError(t *testing.T) {
	// setup: server returns 500 internal server error
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusInternalServerError, "boom")
	})
	defer server.Close()

	// action: list module objects
	got, err := GetModuleObjectsWithModuleApiRoutes(&http.Client{}, apiAddr)

	// assert: error wrapped with "call to threeport API" prefix and slice is non-nil
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "call to threeport API returned unexpected response") {
		t.Errorf("expected upstream-error wrapper, got %v", err)
	}
	if got == nil {
		t.Errorf("expected non-nil zero-value slice pointer on error")
	}
}

// TestGetModuleObjectWithModuleApiRoutesByID_HappyPath asserts the response
// decodes into a ModuleObject and the request path embeds the module object ID.
func TestGetModuleObjectWithModuleApiRoutesByID_HappyPath(t *testing.T) {
	// setup: capture the request path and reply with a single row
	var gotMethod, gotPath string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		writeOKResponse(t, w, []apiserver_lib.Object{
			v0.ModuleObject{Name: strPtr("thing"), Version: strPtr("v0")},
		})
	})
	defer server.Close()

	// action: look up module object by ID
	got, err := GetModuleObjectWithModuleApiRoutesByID(&http.Client{}, apiAddr, 77)

	// assert: no error and object decoded
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.Name == nil || *got.Name != "thing" {
		t.Fatalf("expected decoded object name=thing, got %+v", got)
	}
	// assert: request is GET to the ID-suffixed path
	if gotMethod != http.MethodGet {
		t.Errorf("expected GET, got %q", gotMethod)
	}
	wantPath := v0.PathModuleObjectsWithModuleApiRoutes + "/77"
	if gotPath != wantPath {
		t.Errorf("expected path %q, got %q", wantPath, gotPath)
	}
}

// TestGetModuleObjectWithModuleApiRoutesByID_APIError asserts a 404 upstream
// response surfaces as a wrapped ErrObjectNotFound.
func TestGetModuleObjectWithModuleApiRoutesByID_APIError(t *testing.T) {
	// setup: server returns 404 for the ID lookup
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusNotFound, "no such object")
	})
	defer server.Close()

	// action: look up module object by an unknown ID
	got, err := GetModuleObjectWithModuleApiRoutesByID(&http.Client{}, apiAddr, 999)

	// assert: sentinel ErrObjectNotFound in the error chain and non-nil zero-value pointer
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrObjectNotFound) {
		t.Errorf("expected ErrObjectNotFound in chain, got %v", err)
	}
	if got == nil {
		t.Errorf("expected non-nil zero-value object pointer on error")
	}
}
