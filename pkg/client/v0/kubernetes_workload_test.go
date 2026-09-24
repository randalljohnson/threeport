package v0

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	client_lib "github.com/threeport/threeport/pkg/client/lib/v0"
)

// writePaginatedResponse encodes a Response with the given data slice plus a
// pagination window, mimicking a page in a paginated API result.
func writePaginatedResponse(
	t *testing.T,
	w http.ResponseWriter,
	data []apiserver_lib.Object,
	hasMore bool,
	nextCursor uint,
	queryId string,
) {
	t.Helper()
	resp := apiserver_lib.Response{
		Data: data,
		Meta: apiserver_lib.Meta{
			Pagination: apiserver_lib.Pagination{
				HasMore:    hasMore,
				NextCursor: nextCursor,
				QueryId:    queryId,
			},
		},
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal paginated response: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		t.Fatalf("failed to write paginated response: %v", err)
	}
}

// TestCreateKubernetesWorkloadResourceDefinitions_HappyPath asserts a 201
// response decodes back into the workload resource definitions slice, and that
// the request targets the resource-definition-sets path with method POST.
func TestCreateKubernetesWorkloadResourceDefinitions_HappyPath(t *testing.T) {
	// setup: server captures the incoming path plus method and echoes a
	// created payload with an ID stamped in
	var gotPath, gotMethod string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		resp := apiserver_lib.Response{
			Data: []apiserver_lib.Object{
				v0.KubernetesWorkloadResourceDefinition{
					KubernetesWorkloadDefinitionID: uintPtr(7),
				},
			},
		}
		body, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("failed to marshal response: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if _, err := w.Write(body); err != nil {
			t.Fatalf("failed to write response body: %v", err)
		}
	})
	defer server.Close()

	// action: create with a single definition
	input := []v0.KubernetesWorkloadResourceDefinition{{
		KubernetesWorkloadDefinitionID: uintPtr(7),
	}}
	got, err := CreateKubernetesWorkloadResourceDefinitions(&http.Client{}, apiAddr, &input)

	// assert: decode succeeded, request hit the sets path with POST
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(*got) != 1 {
		t.Fatalf("expected one definition, got %+v", got)
	}
	if (*got)[0].KubernetesWorkloadDefinitionID == nil || *(*got)[0].KubernetesWorkloadDefinitionID != 7 {
		t.Errorf("expected KubernetesWorkloadDefinitionID 7, got %+v", (*got)[0].KubernetesWorkloadDefinitionID)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("expected method POST, got %q", gotMethod)
	}
	if !strings.Contains(gotPath, v0.PathKubernetesWorkloadResourceDefinitionSets) {
		t.Errorf("expected path %q, got %q", v0.PathKubernetesWorkloadResourceDefinitionSets, gotPath)
	}
}

// TestCreateKubernetesWorkloadResourceDefinitions_APIError asserts that a 4xx
// response from the API surfaces as a wrapped client-lib sentinel and returns
// the original input slice unchanged.
func TestCreateKubernetesWorkloadResourceDefinitions_APIError(t *testing.T) {
	// setup: server returns 409 conflict
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusConflict, "already exists")
	})
	defer server.Close()

	// action: create with a definition
	input := []v0.KubernetesWorkloadResourceDefinition{{
		KubernetesWorkloadDefinitionID: uintPtr(1),
	}}
	got, err := CreateKubernetesWorkloadResourceDefinitions(&http.Client{}, apiAddr, &input)

	// assert: wrapped conflict error surfaces, and input pointer is returned
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrConflict) {
		t.Errorf("expected ErrConflict in chain, got %v", err)
	}
	if got != &input {
		t.Errorf("expected the input slice pointer returned on API error, got %p vs %p", got, &input)
	}
}

// TestGetKubernetesWorkloadResourceDefinitionsByID_HappyPath asserts a single
// page of data decodes into the resource-definition slice and the query string
// carries the workload-definition ID under kubernetesworkloaddefinitionid.
func TestGetKubernetesWorkloadResourceDefinitionsByID_HappyPath(t *testing.T) {
	// setup: server returns a single non-paginated page keyed by the id filter
	var gotRawQuery string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		writePaginatedResponse(t, w, []apiserver_lib.Object{
			v0.KubernetesWorkloadResourceDefinition{KubernetesWorkloadDefinitionID: uintPtr(42)},
		}, false, 0, "")
	})
	defer server.Close()

	// action: look up by workload definition ID
	got, err := GetKubernetesWorkloadResourceDefinitionsByID(&http.Client{}, apiAddr, 42)

	// assert: one row decoded and the id landed in the query string
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(*got) != 1 {
		t.Fatalf("expected one row, got %+v", got)
	}
	if !strings.Contains(gotRawQuery, "kubernetesworkloaddefinitionid=42") {
		t.Errorf("expected kubernetesworkloaddefinitionid=42 in query, got %q", gotRawQuery)
	}
}

// TestGetKubernetesWorkloadResourceDefinitionsByID_Pagination asserts that when
// the first page reports HasMore, the client re-issues the request carrying the
// server-provided queryid and cursor until HasMore is false, and returns the
// combined data.
func TestGetKubernetesWorkloadResourceDefinitionsByID_Pagination(t *testing.T) {
	// setup: server returns two pages: the first with HasMore true and a
	// cursor, the second with HasMore false to terminate the loop
	var callCount int32
	var secondQuery string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		switch n {
		case 1:
			writePaginatedResponse(t, w, []apiserver_lib.Object{
				v0.KubernetesWorkloadResourceDefinition{KubernetesWorkloadDefinitionID: uintPtr(1)},
			}, true, 5, "q-abc")
		default:
			secondQuery = r.URL.RawQuery
			writePaginatedResponse(t, w, []apiserver_lib.Object{
				v0.KubernetesWorkloadResourceDefinition{KubernetesWorkloadDefinitionID: uintPtr(2)},
			}, false, 0, "")
		}
	})
	defer server.Close()

	// action: fetch the paginated result set
	got, err := GetKubernetesWorkloadResourceDefinitionsByID(&http.Client{}, apiAddr, 100)

	// assert: both pages concatenated, cursor+queryid propagated to page two
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(*got) != 2 {
		t.Fatalf("expected 2 rows across pages, got %+v", got)
	}
	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("expected 2 upstream calls, got %d", callCount)
	}
	if !strings.Contains(secondQuery, "queryid=q-abc") {
		t.Errorf("expected queryid=q-abc in second-page query, got %q", secondQuery)
	}
	if !strings.Contains(secondQuery, "cursor=5") {
		t.Errorf("expected cursor=5 in second-page query, got %q", secondQuery)
	}
}

// TestGetKubernetesWorkloadResourceDefinitionsByID_APIError asserts a 500-style
// upstream error is wrapped and does not silently return an empty slice.
func TestGetKubernetesWorkloadResourceDefinitionsByID_APIError(t *testing.T) {
	// setup: server returns 500 internal server error
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusInternalServerError, "boom")
	})
	defer server.Close()

	// action: fetch with any id
	_, err := GetKubernetesWorkloadResourceDefinitionsByID(&http.Client{}, apiAddr, 1)

	// assert: wrapped upstream-error prefix present
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "call to threeport API returned unexpected response") {
		t.Errorf("expected upstream-error wrapper, got %v", err)
	}
}

// TestGetKubernetesWorkloadInstancesByID_HappyPath asserts a single-page fetch
// decodes into the workload-instances slice and the id lands in the query.
func TestGetKubernetesWorkloadInstancesByID_HappyPath(t *testing.T) {
	// setup: server returns one workload instance keyed by workload def id
	var gotRawQuery, gotPath string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawQuery = r.URL.RawQuery
		writePaginatedResponse(t, w, []apiserver_lib.Object{
			v0.KubernetesWorkloadInstance{KubernetesWorkloadDefinitionID: uintPtr(3)},
		}, false, 0, "")
	})
	defer server.Close()

	// action: fetch by workload definition ID
	got, err := GetKubernetesWorkloadInstancesByID(&http.Client{}, apiAddr, 3)

	// assert: one row decoded, path and query string match the workload-instances endpoint
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(*got) != 1 {
		t.Fatalf("expected one row, got %+v", got)
	}
	if gotPath != v0.PathKubernetesWorkloadInstances {
		t.Errorf("expected path %q, got %q", v0.PathKubernetesWorkloadInstances, gotPath)
	}
	if !strings.Contains(gotRawQuery, "kubernetesworkloaddefinitionid=3") {
		t.Errorf("expected kubernetesworkloaddefinitionid=3 in query, got %q", gotRawQuery)
	}
}

// TestGetKubernetesWorkloadInstancesByID_EmptyPage asserts a page with no data
// and HasMore false yields an empty slice pointer with no error.
func TestGetKubernetesWorkloadInstancesByID_EmptyPage(t *testing.T) {
	// setup: server returns 200 with an empty data slice
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writePaginatedResponse(t, w, []apiserver_lib.Object{}, false, 0, "")
	})
	defer server.Close()

	// action: fetch with any id
	got, err := GetKubernetesWorkloadInstancesByID(&http.Client{}, apiAddr, 42)

	// assert: no error, non-nil pointer to empty slice
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil pointer, got nil")
	}
	if len(*got) != 0 {
		t.Errorf("expected empty slice, got %d rows", len(*got))
	}
}

// TestGetKubernetesWorkloadInstancesByID_APIError asserts upstream 401 responses
// surface as a wrapped ErrUnauthorized.
func TestGetKubernetesWorkloadInstancesByID_APIError(t *testing.T) {
	// setup: server returns 401 unauthorized
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusUnauthorized, "no token")
	})
	defer server.Close()

	// action: fetch with any id
	_, err := GetKubernetesWorkloadInstancesByID(&http.Client{}, apiAddr, 1)

	// assert: ErrUnauthorized is reachable via errors.Is
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized in chain, got %v", err)
	}
}

// TestGetKubernetesWorkloadResourceInstancesByID_HappyPath asserts a single-page
// fetch decodes into the resource-instances slice, and the query string filters
// under kubernetesworkloadinstanceid.
func TestGetKubernetesWorkloadResourceInstancesByID_HappyPath(t *testing.T) {
	// setup: server returns one resource instance keyed by workload instance id
	var gotRawQuery, gotPath string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawQuery = r.URL.RawQuery
		writePaginatedResponse(t, w, []apiserver_lib.Object{
			v0.KubernetesWorkloadResourceInstance{KubernetesWorkloadInstanceID: uintPtr(11)},
		}, false, 0, "")
	})
	defer server.Close()

	// action: fetch by workload instance ID
	got, err := GetKubernetesWorkloadResourceInstancesByID(&http.Client{}, apiAddr, 11)

	// assert: one row decoded, path and query string carry the filter
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(*got) != 1 {
		t.Fatalf("expected one row, got %+v", got)
	}
	if gotPath != v0.PathKubernetesWorkloadResourceInstances {
		t.Errorf("expected path %q, got %q", v0.PathKubernetesWorkloadResourceInstances, gotPath)
	}
	if !strings.Contains(gotRawQuery, "kubernetesworkloadinstanceid=11") {
		t.Errorf("expected kubernetesworkloadinstanceid=11 in query, got %q", gotRawQuery)
	}
}

// TestGetKubernetesWorkloadResourceInstancesByID_Pagination asserts multi-page
// fetches concatenate data and propagate queryid+cursor to the follow-up page.
func TestGetKubernetesWorkloadResourceInstancesByID_Pagination(t *testing.T) {
	// setup: server returns two pages, page one with HasMore true
	var callCount int32
	var secondQuery string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		switch n {
		case 1:
			writePaginatedResponse(t, w, []apiserver_lib.Object{
				v0.KubernetesWorkloadResourceInstance{KubernetesWorkloadInstanceID: uintPtr(1)},
			}, true, 9, "q-xyz")
		default:
			secondQuery = r.URL.RawQuery
			writePaginatedResponse(t, w, []apiserver_lib.Object{
				v0.KubernetesWorkloadResourceInstance{KubernetesWorkloadInstanceID: uintPtr(2)},
				v0.KubernetesWorkloadResourceInstance{KubernetesWorkloadInstanceID: uintPtr(3)},
			}, false, 0, "")
		}
	})
	defer server.Close()

	// action: fetch across both pages
	got, err := GetKubernetesWorkloadResourceInstancesByID(&http.Client{}, apiAddr, 55)

	// assert: three rows total, second-page query carries propagated cursor
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(*got) != 3 {
		t.Fatalf("expected 3 rows across pages, got %+v", got)
	}
	if !strings.Contains(secondQuery, "queryid=q-xyz") {
		t.Errorf("expected queryid=q-xyz in second-page query, got %q", secondQuery)
	}
	if !strings.Contains(secondQuery, "cursor=9") {
		t.Errorf("expected cursor=9 in second-page query, got %q", secondQuery)
	}
}

// TestGetKubernetesWorkloadResourceInstancesByID_APIError asserts a forbidden
// upstream response surfaces as a wrapped ErrForbidden.
func TestGetKubernetesWorkloadResourceInstancesByID_APIError(t *testing.T) {
	// setup: server returns 403 forbidden
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusForbidden, "no access")
	})
	defer server.Close()

	// action: fetch with any id
	_, err := GetKubernetesWorkloadResourceInstancesByID(&http.Client{}, apiAddr, 1)

	// assert: ErrForbidden is reachable via errors.Is
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrForbidden) {
		t.Errorf("expected ErrForbidden in chain, got %v", err)
	}
}

// TestGetKubernetesWorkloadInstancesByKubernetesRuntimeInstanceID_HappyPath
// asserts single-page decode and that the runtime instance ID lands in the
// kubernetesruntimeinstanceid query parameter.
func TestGetKubernetesWorkloadInstancesByKubernetesRuntimeInstanceID_HappyPath(t *testing.T) {
	// setup: server returns one workload instance filtered by runtime id
	var gotRawQuery, gotPath string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawQuery = r.URL.RawQuery
		writePaginatedResponse(t, w, []apiserver_lib.Object{
			v0.KubernetesWorkloadInstance{KubernetesRuntimeInstanceID: uintPtr(77)},
		}, false, 0, "")
	})
	defer server.Close()

	// action: fetch by runtime instance ID
	got, err := GetKubernetesWorkloadInstancesByKubernetesRuntimeInstanceID(&http.Client{}, apiAddr, 77)

	// assert: one row decoded, filter reaches the workload-instances endpoint
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(*got) != 1 {
		t.Fatalf("expected one row, got %+v", got)
	}
	if gotPath != v0.PathKubernetesWorkloadInstances {
		t.Errorf("expected path %q, got %q", v0.PathKubernetesWorkloadInstances, gotPath)
	}
	if !strings.Contains(gotRawQuery, "kubernetesruntimeinstanceid=77") {
		t.Errorf("expected kubernetesruntimeinstanceid=77 in query, got %q", gotRawQuery)
	}
}

// TestGetKubernetesWorkloadInstancesByKubernetesRuntimeInstanceID_Pagination
// asserts the follow-up page carries the server-provided queryid and cursor.
func TestGetKubernetesWorkloadInstancesByKubernetesRuntimeInstanceID_Pagination(t *testing.T) {
	// setup: server returns two pages, first with HasMore true
	var callCount int32
	var secondQuery string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		switch n {
		case 1:
			writePaginatedResponse(t, w, []apiserver_lib.Object{
				v0.KubernetesWorkloadInstance{KubernetesRuntimeInstanceID: uintPtr(1)},
			}, true, 12, "q-rt")
		default:
			secondQuery = r.URL.RawQuery
			writePaginatedResponse(t, w, []apiserver_lib.Object{
				v0.KubernetesWorkloadInstance{KubernetesRuntimeInstanceID: uintPtr(2)},
			}, false, 0, "")
		}
	})
	defer server.Close()

	// action: fetch across both pages
	got, err := GetKubernetesWorkloadInstancesByKubernetesRuntimeInstanceID(&http.Client{}, apiAddr, 200)

	// assert: two rows total, second-page query carries propagated cursor
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(*got) != 2 {
		t.Fatalf("expected 2 rows across pages, got %+v", got)
	}
	if !strings.Contains(secondQuery, "queryid=q-rt") {
		t.Errorf("expected queryid=q-rt in second-page query, got %q", secondQuery)
	}
	if !strings.Contains(secondQuery, "cursor=12") {
		t.Errorf("expected cursor=12 in second-page query, got %q", secondQuery)
	}
}

// TestGetKubernetesWorkloadInstancesByKubernetesRuntimeInstanceID_APIError
// asserts a not-found upstream response surfaces as a wrapped ErrObjectNotFound.
func TestGetKubernetesWorkloadInstancesByKubernetesRuntimeInstanceID_APIError(t *testing.T) {
	// setup: server returns 404 not found
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusNotFound, "missing")
	})
	defer server.Close()

	// action: fetch with any runtime id
	_, err := GetKubernetesWorkloadInstancesByKubernetesRuntimeInstanceID(&http.Client{}, apiAddr, 1)

	// assert: ErrObjectNotFound is reachable via errors.Is
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrObjectNotFound) {
		t.Errorf("expected ErrObjectNotFound in chain, got %v", err)
	}
}
