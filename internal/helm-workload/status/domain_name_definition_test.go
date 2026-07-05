package status

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// stripScheme drops the "http://" prefix from an httptest server URL so it
// matches the endpoint form the client helper expects: the helper prepends its
// own scheme when building the request URL.
func stripScheme(u string) string {
	return strings.TrimPrefix(u, "http://")
}

// ptrUint returns a pointer to the given uint so test fixtures can populate
// *uint ID fields without a throwaway local.
func ptrUint(v uint) *uint {
	return &v
}

// TestGetHelmWorkloadDefinitionStatus covers the definition status helper's
// happy path, API error surfacing, and query-string construction.
func TestGetHelmWorkloadDefinitionStatus(t *testing.T) {
	tests := []struct {
		name               string
		definitionID       uint
		serverStatus       int
		serverInstances    []v0.HelmWorkloadInstance
		wantInstanceCount  int
		wantErr            bool
		wantErrSubstring   string
		wantQueryStringHas string
	}{
		{
			name:               "happy path returns instances associated with the definition",
			definitionID:       42,
			serverStatus:       http.StatusOK,
			serverInstances:    []v0.HelmWorkloadInstance{{Common: v0.Common{ID: ptrUint(1)}}, {Common: v0.Common{ID: ptrUint(2)}}},
			wantInstanceCount:  2,
			wantErr:            false,
			wantQueryStringHas: "domainnamedefinitionid=42",
		},
		{
			name:               "empty result set returns zero instances without error",
			definitionID:       7,
			serverStatus:       http.StatusOK,
			serverInstances:    []v0.HelmWorkloadInstance{},
			wantInstanceCount:  0,
			wantErr:            false,
			wantQueryStringHas: "domainnamedefinitionid=7",
		},
		{
			name:               "non-200 API response is wrapped with the helper's error prefix",
			definitionID:       99,
			serverStatus:       http.StatusInternalServerError,
			serverInstances:    nil,
			wantErr:            true,
			wantErrSubstring:   "failed to retrieve helm workload instances",
			wantQueryStringHas: "domainnamedefinitionid=99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// capture the query string the helper submits so we can assert
			// the definition id was propagated into the filter
			var seenQuery string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seenQuery = r.URL.RawQuery

				if tt.serverStatus != http.StatusOK {
					// exercise the wrapped-error branch: return a non-200 with a status body
					w.WriteHeader(tt.serverStatus)
					_ = json.NewEncoder(w).Encode(apiserver_lib.Response{
						Status: apiserver_lib.Status{
							Code:    tt.serverStatus,
							Message: http.StatusText(tt.serverStatus),
							Error:   "boom",
						},
					})
					return
				}

				// build a well-formed Response wrapping the fixture instances
				data := make([]apiserver_lib.Object, len(tt.serverInstances))
				for i, inst := range tt.serverInstances {
					data[i] = inst
				}
				resp := apiserver_lib.Response{
					Meta:   apiserver_lib.Meta{ObjectCount: int64(len(data))},
					Type:   "HelmWorkloadInstance",
					Data:   data,
					Status: apiserver_lib.Status{Code: http.StatusOK, Message: http.StatusText(http.StatusOK)},
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			// strip the scheme because the client helper prepends "http://"
			endpoint := stripScheme(server.URL)

			// invoke the function under test with the id from the fixture
			got, err := GetHelmWorkloadDefinitionStatus(&http.Client{}, endpoint, tt.definitionID)

			// error-path assertions: confirm wrapping and non-nil detail
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErrSubstring) {
					t.Errorf("error %q missing expected substring %q", err.Error(), tt.wantErrSubstring)
				}
				if got == nil {
					t.Errorf("expected non-nil status detail even on error, got nil")
				}
				// even on error, the query string still had to be built from the id
				if !strings.Contains(seenQuery, tt.wantQueryStringHas) {
					t.Errorf("server saw query %q, expected substring %q", seenQuery, tt.wantQueryStringHas)
				}
				return
			}

			// happy-path assertions: no error, non-nil detail, correct instance count
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil || got.HelmWorkloadInstances == nil {
				t.Fatalf("expected non-nil status detail with instances slice")
			}
			if n := len(*got.HelmWorkloadInstances); n != tt.wantInstanceCount {
				t.Errorf("got %d instances, want %d", n, tt.wantInstanceCount)
			}

			// confirm the helper embedded the definition id in the query string
			if !strings.Contains(seenQuery, tt.wantQueryStringHas) {
				t.Errorf("server saw query %q, expected substring %q", seenQuery, tt.wantQueryStringHas)
			}
		})
	}
}

// TestGetHelmWorkloadDefinitionStatusEndpointFailure covers the case where the
// transport layer fails outright (endpoint unreachable): the helper must
// return a non-nil status detail alongside a wrapped error.
func TestGetHelmWorkloadDefinitionStatusEndpointFailure(t *testing.T) {
	// point at a closed server so the underlying Do() call fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint := stripScheme(server.URL)
	server.Close()

	// invoke against the dead endpoint
	got, err := GetHelmWorkloadDefinitionStatus(&http.Client{}, endpoint, 1)

	// transport failure must be wrapped with the helper's prefix and still
	// yield a usable pointer to the (empty) status detail
	if err == nil {
		t.Fatalf("expected error against closed server, got nil")
	}
	if !strings.Contains(err.Error(), "failed to retrieve helm workload instances") {
		t.Errorf("error %q missing expected prefix", err.Error())
	}
	if got == nil {
		t.Errorf("expected non-nil status detail even on transport error, got nil")
	}
}

// TestGetHelmWorkloadDefinitionStatusQueryStringFormat asserts the exact
// query-string form produced from a definition id: the filter key is
// domainnamedefinitionid and the value is the raw integer id.
func TestGetHelmWorkloadDefinitionStatusQueryStringFormat(t *testing.T) {
	var seenQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// capture the query string the helper built
		seenQuery = r.URL.RawQuery
		resp := apiserver_lib.Response{
			Meta:   apiserver_lib.Meta{ObjectCount: 0},
			Type:   "HelmWorkloadInstance",
			Data:   []apiserver_lib.Object{},
			Status: apiserver_lib.Status{Code: http.StatusOK, Message: http.StatusText(http.StatusOK)},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// invoke the helper so the server captures the outbound query
	_, err := GetHelmWorkloadDefinitionStatus(&http.Client{}, stripScheme(server.URL), 123)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// assert the exact form: filter key domainnamedefinitionid and raw id value
	want := fmt.Sprintf("domainnamedefinitionid=%d", 123)
	if seenQuery != want {
		t.Errorf("query string %q, want %q", seenQuery, want)
	}
}
