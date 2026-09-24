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
// matches the endpoint form expected by client.GetResponse(), which prepends
// its own scheme.
func stripScheme(u string) string {
	return strings.TrimPrefix(u, "http://")
}

// ptrUint returns a pointer to the given uint literal so test fixtures can
// populate *uint ID fields without a throwaway local.
func ptrUint(v uint) *uint {
	return &v
}

// TestGetKubernetesRuntimeDefinitionStatus covers the definition status
// helper's happy path, API error surfacing, and query-string construction.
func TestGetKubernetesRuntimeDefinitionStatus(t *testing.T) {
	tests := []struct {
		name                string
		definitionID        uint
		serverStatus        int
		serverInstances     []v0.KubernetesRuntimeInstance
		wantInstanceCount   int
		wantErr             bool
		wantErrSubstring    string
		wantQueryStringHas  string
	}{
		{
			name:              "happy path returns instances associated with the definition",
			definitionID:      42,
			serverStatus:      http.StatusOK,
			serverInstances:   []v0.KubernetesRuntimeInstance{{Common: v0.Common{ID: ptrUint(1)}}, {Common: v0.Common{ID: ptrUint(2)}}},
			wantInstanceCount: 2,
			wantErr:           false,
			wantQueryStringHas: "kubernetesruntimedefinitionid=42",
		},
		{
			name:              "empty result set returns zero instances without error",
			definitionID:      7,
			serverStatus:      http.StatusOK,
			serverInstances:   []v0.KubernetesRuntimeInstance{},
			wantInstanceCount: 0,
			wantErr:           false,
			wantQueryStringHas: "kubernetesruntimedefinitionid=7",
		},
		{
			name:               "non-200 API response is wrapped with the helper's error prefix",
			definitionID:       99,
			serverStatus:       http.StatusInternalServerError,
			serverInstances:    nil,
			wantErr:            true,
			wantErrSubstring:   "failed to retrieve kubernetes runtime instances",
			wantQueryStringHas: "kubernetesruntimedefinitionid=99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// track the query string the helper submits so we can assert
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
					Type:   "KubernetesRuntimeInstance",
					Data:   data,
					Status: apiserver_lib.Status{Code: http.StatusOK, Message: http.StatusText(http.StatusOK)},
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			// build the input definition; strip the scheme because
			// client.GetResponse() prepends "http://"
			def := &v0.KubernetesRuntimeDefinition{Common: v0.Common{ID: ptrUint(tt.definitionID)}}
			endpoint := stripScheme(server.URL)

			// invoke the function under test
			got, err := GetKubernetesRuntimeDefinitionStatus(&http.Client{}, endpoint, def)

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
				// even on error, the query string still had to be built from the ID
				if !strings.Contains(seenQuery, tt.wantQueryStringHas) {
					t.Errorf("server saw query %q, expected substring %q", seenQuery, tt.wantQueryStringHas)
				}
				return
			}

			// happy-path assertions: no error, non-nil detail, correct instance count
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil || got.KubernetesRuntimeInstances == nil {
				t.Fatalf("expected non-nil status detail with instances slice")
			}
			if n := len(*got.KubernetesRuntimeInstances); n != tt.wantInstanceCount {
				t.Errorf("got %d instances, want %d", n, tt.wantInstanceCount)
			}

			// confirm the helper embedded the definition id in the query string
			if !strings.Contains(seenQuery, tt.wantQueryStringHas) {
				t.Errorf("server saw query %q, expected substring %q", seenQuery, tt.wantQueryStringHas)
			}
		})
	}
}

// TestGetKubernetesRuntimeDefinitionStatusEndpointFailure covers the case
// where the transport layer fails outright (endpoint unreachable): the helper
// must return a non-nil status detail alongside a wrapped error.
func TestGetKubernetesRuntimeDefinitionStatusEndpointFailure(t *testing.T) {
	// point at a closed server so the underlying Do() call fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint := stripScheme(server.URL)
	server.Close()

	def := &v0.KubernetesRuntimeDefinition{Common: v0.Common{ID: ptrUint(1)}}

	// invoke against the dead endpoint
	got, err := GetKubernetesRuntimeDefinitionStatus(&http.Client{}, endpoint, def)

	// transport failure must be wrapped with the helper's prefix and still
	// yield a usable pointer to the (empty) status detail
	if err == nil {
		t.Fatalf("expected error against closed server, got nil")
	}
	if !strings.Contains(err.Error(), "failed to retrieve kubernetes runtime instances") {
		t.Errorf("error %q missing expected prefix", err.Error())
	}
	if got == nil {
		t.Errorf("expected non-nil status detail even on transport error, got nil")
	}
}

// TestGetKubernetesRuntimeDefinitionStatusQueryStringFormat pins the exact
// query-string form produced from a definition id: any change to the filter
// key or format shape would break the API contract with the server.
func TestGetKubernetesRuntimeDefinitionStatusQueryStringFormat(t *testing.T) {
	var seenQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// capture the query string the helper built
		seenQuery = r.URL.RawQuery
		resp := apiserver_lib.Response{
			Meta:   apiserver_lib.Meta{ObjectCount: 0},
			Type:   "KubernetesRuntimeInstance",
			Data:   []apiserver_lib.Object{},
			Status: apiserver_lib.Status{Code: http.StatusOK, Message: http.StatusText(http.StatusOK)},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	def := &v0.KubernetesRuntimeDefinition{Common: v0.Common{ID: ptrUint(123)}}

	// invoke the helper so the server captures the outbound query
	_, err := GetKubernetesRuntimeDefinitionStatus(&http.Client{}, stripScheme(server.URL), def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// pin the exact form: filter key is kubernetesruntimedefinitionid and
	// the value is the raw integer id
	want := fmt.Sprintf("kubernetesruntimedefinitionid=%d", 123)
	if seenQuery != want {
		t.Errorf("query string %q, want %q", seenQuery, want)
	}
}
