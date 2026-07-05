package v0

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
	client_lib "github.com/threeport/threeport/pkg/client/lib/v0"
)

// newKriTestServer starts an httptest.Server backed by the given handler and
// returns the apiAddr that the client v0 helpers expect: scheme stripped, since
// client_lib.GetResponse prepends "http://" on its own.
func newKriTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(handler)
	apiAddr := strings.TrimPrefix(srv.URL, "http://")
	return srv, apiAddr
}

// writeKriResponse serializes and writes a Response body with the given status,
// data, and pagination metadata.
func writeKriResponse(t *testing.T, w http.ResponseWriter, status int, data []apiserver_lib.Object, pagination apiserver_lib.Pagination) {
	t.Helper()
	resp := apiserver_lib.Response{
		Data: data,
		Meta: apiserver_lib.Meta{Pagination: pagination},
		Status: apiserver_lib.Status{
			Code:    status,
			Message: http.StatusText(status),
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// writeKriErrorResponse writes a Response with the given non-2xx status and an
// error message in the Status field so client_lib.GetResponse maps it to the
// right typed error.
func writeKriErrorResponse(t *testing.T, w http.ResponseWriter, status int, message string) {
	t.Helper()
	resp := apiserver_lib.Response{
		Status: apiserver_lib.Status{
			Code:    status,
			Message: http.StatusText(status),
			Error:   message,
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal error response: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// TestGetDefaultKubernetesRuntimeInstance_ReturnsInstanceOnHappyPath asserts
// that a single-item Data payload decodes into the returned instance.
func TestGetDefaultKubernetesRuntimeInstance_ReturnsInstanceOnHappyPath(t *testing.T) {
	// stub API returns a single default instance
	want := api_v0.KubernetesRuntimeInstance{
		Common:                        api_v0.Common{ID: uintPtr(7)},
		KubernetesRuntimeDefinitionID: uintPtr(42),
	}
	srv, apiAddr := newKriTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// url carries the defaultruntime=true selector
		if !strings.Contains(r.URL.RawQuery, "defaultruntime=true") {
			t.Errorf("expected defaultruntime=true, got %q", r.URL.RawQuery)
		}
		writeKriResponse(t, w, http.StatusOK, []apiserver_lib.Object{want}, apiserver_lib.Pagination{})
	})
	defer srv.Close()

	// call under test
	got, err := GetDefaultKubernetesRuntimeInstance(srv.Client(), apiAddr)

	// no error and returned instance carries the encoded ID
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.ID == nil || *got.ID != 7 {
		t.Errorf("expected instance ID 7, got %+v", got)
	}
}

// TestGetDefaultKubernetesRuntimeInstance_ErrorsWhenNoDefault asserts that an
// empty Data slice surfaces the "no default kubernetes runtime instance found"
// error.
func TestGetDefaultKubernetesRuntimeInstance_ErrorsWhenNoDefault(t *testing.T) {
	// stub API returns zero data
	srv, apiAddr := newKriTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeKriResponse(t, w, http.StatusOK, []apiserver_lib.Object{}, apiserver_lib.Pagination{})
	})
	defer srv.Close()

	// call under test
	got, err := GetDefaultKubernetesRuntimeInstance(srv.Client(), apiAddr)

	// error surfaces and non-nil zero-value instance returned per contract
	if err == nil {
		t.Fatal("expected error for empty default response")
	}
	if !strings.Contains(err.Error(), "no default kubernetes runtime instance found") {
		t.Errorf("unexpected error message: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil zero-value instance, got nil")
	}
}

// TestGetDefaultKubernetesRuntimeInstance_ErrorsWhenMultipleDefaults asserts
// that more than one default instance yields the ambiguity error.
func TestGetDefaultKubernetesRuntimeInstance_ErrorsWhenMultipleDefaults(t *testing.T) {
	// stub API returns two instances
	srv, apiAddr := newKriTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeKriResponse(t, w, http.StatusOK, []apiserver_lib.Object{
			api_v0.KubernetesRuntimeInstance{Common: api_v0.Common{ID: uintPtr(1)}},
			api_v0.KubernetesRuntimeInstance{Common: api_v0.Common{ID: uintPtr(2)}},
		}, apiserver_lib.Pagination{})
	})
	defer srv.Close()

	// call under test
	_, err := GetDefaultKubernetesRuntimeInstance(srv.Client(), apiAddr)

	// error names the multiple-default condition
	if err == nil {
		t.Fatal("expected error for multiple defaults")
	}
	if !strings.Contains(err.Error(), "multiple kubernetes runtime instances marked as default") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestGetDefaultKubernetesRuntimeInstance_ErrorsWhenApiReturnsFailure asserts
// that an upstream non-200 propagates as a wrapped error.
func TestGetDefaultKubernetesRuntimeInstance_ErrorsWhenApiReturnsFailure(t *testing.T) {
	// stub API returns 401 Unauthorized
	srv, apiAddr := newKriTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeKriErrorResponse(t, w, http.StatusUnauthorized, "token expired")
	})
	defer srv.Close()

	// call under test
	_, err := GetDefaultKubernetesRuntimeInstance(srv.Client(), apiAddr)

	// wrapped chain preserves ErrUnauthorized and the caller's own prefix
	if err == nil {
		t.Fatal("expected error for API 401")
	}
	if !errors.Is(err, client_lib.ErrUnauthorized) {
		t.Errorf("expected error to wrap ErrUnauthorized, got %v", err)
	}
	if !strings.Contains(err.Error(), "call to threeport API returned unexpected response") {
		t.Errorf("unexpected outer error message: %v", err)
	}
}

// TestGetKubernetesRuntimeInstancesByID_ReturnsSinglePage asserts that a
// non-paginated response is decoded into the returned slice.
func TestGetKubernetesRuntimeInstancesByID_ReturnsSinglePage(t *testing.T) {
	// stub API returns two instances with HasMore=false
	srv, apiAddr := newKriTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("kubernetesruntimedefinitionid"); got != "5" {
			t.Errorf("expected kubernetesruntimedefinitionid=5, got %q", got)
		}
		writeKriResponse(t, w, http.StatusOK, []apiserver_lib.Object{
			api_v0.KubernetesRuntimeInstance{Common: api_v0.Common{ID: uintPtr(10)}},
			api_v0.KubernetesRuntimeInstance{Common: api_v0.Common{ID: uintPtr(11)}},
		}, apiserver_lib.Pagination{HasMore: false})
	})
	defer srv.Close()

	// call under test
	got, err := GetKubernetesRuntimeInstancesByID(srv.Client(), apiAddr, 5)

	// both instances decoded
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(*got) != 2 {
		t.Fatalf("expected 2 instances, got %+v", got)
	}
	if (*got)[0].ID == nil || *(*got)[0].ID != 10 {
		t.Errorf("expected first ID 10, got %+v", (*got)[0].ID)
	}
}

// TestGetKubernetesRuntimeInstancesByID_FollowsPaginationCursor asserts that
// when the first page reports HasMore=true, the helper requests the next page
// using the returned queryid and cursor.
func TestGetKubernetesRuntimeInstancesByID_FollowsPaginationCursor(t *testing.T) {
	// stub API returns two pages: first HasMore=true, second HasMore=false
	var callCount int
	srv, apiAddr := newKriTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			// first request must not carry a cursor
			if r.URL.Query().Get("queryid") != "" {
				t.Errorf("first call should not carry queryid, got %q", r.URL.Query().Get("queryid"))
			}
			writeKriResponse(t, w, http.StatusOK, []apiserver_lib.Object{
				api_v0.KubernetesRuntimeInstance{Common: api_v0.Common{ID: uintPtr(1)}},
			}, apiserver_lib.Pagination{HasMore: true, QueryId: "q-abc", NextCursor: 100})
		case 2:
			// second request forwards queryid + cursor from the first response
			if got := r.URL.Query().Get("queryid"); got != "q-abc" {
				t.Errorf("expected queryid=q-abc, got %q", got)
			}
			if got := r.URL.Query().Get("cursor"); got != "100" {
				t.Errorf("expected cursor=100, got %q", got)
			}
			writeKriResponse(t, w, http.StatusOK, []apiserver_lib.Object{
				api_v0.KubernetesRuntimeInstance{Common: api_v0.Common{ID: uintPtr(2)}},
			}, apiserver_lib.Pagination{HasMore: false})
		default:
			t.Errorf("unexpected extra call %d", callCount)
		}
	})
	defer srv.Close()

	// call under test
	got, err := GetKubernetesRuntimeInstancesByID(srv.Client(), apiAddr, 9)

	// both pages accumulated into the returned slice
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls, got %d", callCount)
	}
	if got == nil || len(*got) != 2 {
		t.Fatalf("expected 2 aggregated instances, got %+v", got)
	}
	if *(*got)[0].ID != 1 || *(*got)[1].ID != 2 {
		t.Errorf("expected aggregated IDs [1,2], got [%d,%d]", *(*got)[0].ID, *(*got)[1].ID)
	}
}

// TestGetKubernetesRuntimeInstancesByID_ErrorsWhenApiReturnsFailure asserts
// that a non-2xx response short-circuits the pagination loop with a wrapped
// error.
func TestGetKubernetesRuntimeInstancesByID_ErrorsWhenApiReturnsFailure(t *testing.T) {
	// stub API returns 500 Internal Server Error
	srv, apiAddr := newKriTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeKriErrorResponse(t, w, http.StatusInternalServerError, "boom")
	})
	defer srv.Close()

	// call under test
	_, err := GetKubernetesRuntimeInstancesByID(srv.Client(), apiAddr, 1)

	// caller prefix wraps the response error
	if err == nil {
		t.Fatal("expected error for API 500")
	}
	if !strings.Contains(err.Error(), "call to threeport API returned unexpected response") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestGetThreeportControlPlaneKubernetesRuntimeInstance_ReturnsInstanceOnHappyPath
// asserts that the single-item Data slice decodes into the returned instance
// and the correct selector is sent.
func TestGetThreeportControlPlaneKubernetesRuntimeInstance_ReturnsInstanceOnHappyPath(t *testing.T) {
	// stub API returns a single control-plane host instance
	want := api_v0.KubernetesRuntimeInstance{Common: api_v0.Common{ID: uintPtr(3)}}
	srv, apiAddr := newKriTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "threeportcontrolplanehost=true") {
			t.Errorf("expected threeportcontrolplanehost=true, got %q", r.URL.RawQuery)
		}
		writeKriResponse(t, w, http.StatusOK, []apiserver_lib.Object{want}, apiserver_lib.Pagination{})
	})
	defer srv.Close()

	// call under test
	got, err := GetThreeportControlPlaneKubernetesRuntimeInstance(srv.Client(), apiAddr)

	// instance returned with the encoded ID
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.ID == nil || *got.ID != 3 {
		t.Errorf("expected instance ID 3, got %+v", got)
	}
}

// TestGetThreeportControlPlaneKubernetesRuntimeInstance_ErrorsWhenApiReturnsFailure
// asserts that a non-2xx propagates as a wrapped error.
func TestGetThreeportControlPlaneKubernetesRuntimeInstance_ErrorsWhenApiReturnsFailure(t *testing.T) {
	// stub API returns 403 Forbidden
	srv, apiAddr := newKriTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeKriErrorResponse(t, w, http.StatusForbidden, "nope")
	})
	defer srv.Close()

	// call under test
	_, err := GetThreeportControlPlaneKubernetesRuntimeInstance(srv.Client(), apiAddr)

	// wrapped chain preserves ErrForbidden
	if err == nil {
		t.Fatal("expected error for API 403")
	}
	if !errors.Is(err, client_lib.ErrForbidden) {
		t.Errorf("expected error to wrap ErrForbidden, got %v", err)
	}
}

// TestGetInfraProviderByKubernetesRuntimeInstanceID_ReturnsProviderOnHappyPath
// asserts that fetching the instance and then its definition yields the
// definition's InfraProvider value.
func TestGetInfraProviderByKubernetesRuntimeInstanceID_ReturnsProviderOnHappyPath(t *testing.T) {
	// stub API dispatches by path: instance-by-id then definition-by-id
	provider := "aws"
	srv, apiAddr := newKriTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, api_v0.PathKubernetesRuntimeInstances+"/"):
			// first lookup: KubernetesRuntimeInstance with a definition ref
			writeKriResponse(t, w, http.StatusOK, []apiserver_lib.Object{
				api_v0.KubernetesRuntimeInstance{
					Common:                        api_v0.Common{ID: uintPtr(1)},
					KubernetesRuntimeDefinitionID: uintPtr(2),
				},
			}, apiserver_lib.Pagination{})
		case strings.HasPrefix(r.URL.Path, api_v0.PathKubernetesRuntimeDefinitions+"/"):
			// second lookup: definition carrying the InfraProvider value
			writeKriResponse(t, w, http.StatusOK, []apiserver_lib.Object{
				api_v0.KubernetesRuntimeDefinition{
					Common:        api_v0.Common{ID: uintPtr(2)},
					InfraProvider: &provider,
				},
			}, apiserver_lib.Pagination{})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	// call under test
	id := uint(1)
	got, err := GetInfraProviderByKubernetesRuntimeInstanceID(srv.Client(), apiAddr, &id)

	// returned pointer carries the encoded provider
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || *got != "aws" {
		t.Errorf("expected provider aws, got %+v", got)
	}
}

// TestGetInfraProviderByKubernetesRuntimeInstanceID_ErrorsWhenInstanceFetchFails
// asserts that a failed instance lookup surfaces as a wrapped error naming the
// instance step.
func TestGetInfraProviderByKubernetesRuntimeInstanceID_ErrorsWhenInstanceFetchFails(t *testing.T) {
	// stub API returns 404 on the instance path; definition path never called
	srv, apiAddr := newKriTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, api_v0.PathKubernetesRuntimeDefinitions) {
			t.Errorf("definition endpoint should not be called after instance failure")
		}
		writeKriErrorResponse(t, w, http.StatusNotFound, "no such instance")
	})
	defer srv.Close()

	// call under test
	id := uint(1)
	got, err := GetInfraProviderByKubernetesRuntimeInstanceID(srv.Client(), apiAddr, &id)

	// error names the instance step and returned pointer is nil
	if err == nil {
		t.Fatal("expected error when instance fetch fails")
	}
	if got != nil {
		t.Errorf("expected nil provider, got %+v", got)
	}
	if !strings.Contains(err.Error(), "failed to get kubernetes runtime instance") {
		t.Errorf("unexpected error message: %v", err)
	}
	if !errors.Is(err, client_lib.ErrObjectNotFound) {
		t.Errorf("expected error to wrap ErrObjectNotFound, got %v", err)
	}
}

// TestGetInfraProviderByKubernetesRuntimeInstanceID_ErrorsWhenDefinitionFetchFails
// asserts that a successful instance lookup followed by a failed definition
// lookup surfaces as a wrapped error naming the definition step.
func TestGetInfraProviderByKubernetesRuntimeInstanceID_ErrorsWhenDefinitionFetchFails(t *testing.T) {
	// stub API returns the instance ok, then 500 on the definition lookup
	srv, apiAddr := newKriTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, api_v0.PathKubernetesRuntimeInstances+"/"):
			writeKriResponse(t, w, http.StatusOK, []apiserver_lib.Object{
				api_v0.KubernetesRuntimeInstance{
					Common:                        api_v0.Common{ID: uintPtr(1)},
					KubernetesRuntimeDefinitionID: uintPtr(2),
				},
			}, apiserver_lib.Pagination{})
		case strings.HasPrefix(r.URL.Path, api_v0.PathKubernetesRuntimeDefinitions+"/"):
			writeKriErrorResponse(t, w, http.StatusInternalServerError, "definition unavailable")
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	// call under test
	id := uint(1)
	got, err := GetInfraProviderByKubernetesRuntimeInstanceID(srv.Client(), apiAddr, &id)

	// error names the definition step and returned pointer is nil
	if err == nil {
		t.Fatal("expected error when definition fetch fails")
	}
	if got != nil {
		t.Errorf("expected nil provider, got %+v", got)
	}
	if !strings.Contains(err.Error(), "failed to get kubernetes runtime definition") {
		t.Errorf("unexpected error message: %v", err)
	}
}

