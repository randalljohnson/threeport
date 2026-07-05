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

// TestGetTerraformDefinitionStatus covers the definition status helper's
// happy path, empty result set, and API error surfacing.
func TestGetTerraformDefinitionStatus(t *testing.T) {
	tests := []struct {
		name               string
		definitionID       uint
		serverStatus       int
		serverInstances    []v0.TerraformInstance
		wantInstanceCount  int
		wantErr            bool
		wantErrSubstring   string
		wantQueryStringHas string
	}{
		{
			name:               "happy path returns instances associated with the definition",
			definitionID:       42,
			serverStatus:       http.StatusOK,
			serverInstances:    []v0.TerraformInstance{{Common: v0.Common{ID: ptrUint(1)}}, {Common: v0.Common{ID: ptrUint(2)}}},
			wantInstanceCount:  2,
			wantErr:            false,
			wantQueryStringHas: "terraformdefinitionid=42",
		},
		{
			name:               "empty result set returns zero instances without error",
			definitionID:       7,
			serverStatus:       http.StatusOK,
			serverInstances:    []v0.TerraformInstance{},
			wantInstanceCount:  0,
			wantErr:            false,
			wantQueryStringHas: "terraformdefinitionid=7",
		},
		{
			name:               "non-200 API response is wrapped with the helper's error prefix",
			definitionID:       99,
			serverStatus:       http.StatusInternalServerError,
			serverInstances:    nil,
			wantErr:            true,
			wantErrSubstring:   "failed to retrieve terraform instances related to terraform definition",
			wantQueryStringHas: "terraformdefinitionid=99",
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
					Type:   "TerraformInstance",
					Data:   data,
					Status: apiserver_lib.Status{Code: http.StatusOK, Message: http.StatusText(http.StatusOK)},
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			// strip the scheme because client.GetResponse() prepends "http://"
			endpoint := stripScheme(server.URL)

			// invoke the function under test
			got, err := GetTerraformDefinitionStatus(&http.Client{}, endpoint, tt.definitionID)

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
			if got == nil || got.TerraformInstances == nil {
				t.Fatalf("expected non-nil status detail with instances slice")
			}
			if n := len(*got.TerraformInstances); n != tt.wantInstanceCount {
				t.Errorf("got %d instances, want %d", n, tt.wantInstanceCount)
			}

			// confirm the helper embedded the definition id in the query string
			if !strings.Contains(seenQuery, tt.wantQueryStringHas) {
				t.Errorf("server saw query %q, expected substring %q", seenQuery, tt.wantQueryStringHas)
			}
		})
	}
}

// TestGetTerraformDefinitionStatusEndpointFailure covers the transport
// error path: a closed server forces the underlying Do() call to fail, and
// the helper must wrap the error and still return a usable detail pointer.
func TestGetTerraformDefinitionStatusEndpointFailure(t *testing.T) {
	// point at a closed server so the underlying transport call fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint := stripScheme(server.URL)
	server.Close()

	// invoke against the dead endpoint
	got, err := GetTerraformDefinitionStatus(&http.Client{}, endpoint, 1)

	// transport failure must be wrapped with the helper's prefix and still
	// yield a non-nil pointer to the (empty) status detail
	if err == nil {
		t.Fatalf("expected error against closed server, got nil")
	}
	if !strings.Contains(err.Error(), "failed to retrieve terraform instances related to terraform definition") {
		t.Errorf("error %q missing expected prefix", err.Error())
	}
	if got == nil {
		t.Errorf("expected non-nil status detail even on transport error, got nil")
	}
}

// TestGetTerraformDefinitionStatusQueryStringFormat pins the exact query
// string form the helper builds from a definition id: any change to the
// filter key or value shape would break the API contract with the server.
func TestGetTerraformDefinitionStatusQueryStringFormat(t *testing.T) {
	var seenQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// capture the query string the helper built
		seenQuery = r.URL.RawQuery
		resp := apiserver_lib.Response{
			Meta:   apiserver_lib.Meta{ObjectCount: 0},
			Type:   "TerraformInstance",
			Data:   []apiserver_lib.Object{},
			Status: apiserver_lib.Status{Code: http.StatusOK, Message: http.StatusText(http.StatusOK)},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// invoke the helper so the server captures the outbound query
	_, err := GetTerraformDefinitionStatus(&http.Client{}, stripScheme(server.URL), 123)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// pin the exact form: filter key is terraformdefinitionid and the value
	// is the raw integer id
	want := fmt.Sprintf("terraformdefinitionid=%d", 123)
	if seenQuery != want {
		t.Errorf("query string %q, want %q", seenQuery, want)
	}
}

// TestGetTerraformInstanceStatus covers the instance status helper's happy
// path and API error surfacing.
func TestGetTerraformInstanceStatus(t *testing.T) {
	tests := []struct {
		name             string
		definitionID     uint
		serverStatus     int
		serverDef        *v0.TerraformDefinition
		wantErr          bool
		wantErrSubstring string
		wantPathHas      string
	}{
		{
			name:         "happy path returns the definition referenced by the instance",
			definitionID: 55,
			serverStatus: http.StatusOK,
			serverDef:    &v0.TerraformDefinition{Common: v0.Common{ID: ptrUint(55)}},
			wantErr:      false,
			wantPathHas:  "/terraform-definitions/55",
		},
		{
			name:             "non-200 API response is wrapped with the helper's error prefix",
			definitionID:     77,
			serverStatus:     http.StatusInternalServerError,
			serverDef:        nil,
			wantErr:          true,
			wantErrSubstring: "failed to retrieve terraform definition related to terraform instance",
			wantPathHas:      "/terraform-definitions/77",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// track the path the helper requested so we can assert the
			// definition id was embedded into the URL
			var seenPath string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seenPath = r.URL.Path

				if tt.serverStatus != http.StatusOK {
					// exercise the wrapped-error branch
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

				// return a single-object Data slice; the client picks Data[0]
				resp := apiserver_lib.Response{
					Meta:   apiserver_lib.Meta{ObjectCount: 1},
					Type:   "TerraformDefinition",
					Data:   []apiserver_lib.Object{*tt.serverDef},
					Status: apiserver_lib.Status{Code: http.StatusOK, Message: http.StatusText(http.StatusOK)},
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			// build the input instance carrying just the definition id pointer
			inst := &v0.TerraformInstance{
				TerraformDefinitionID: ptrUint(tt.definitionID),
			}
			endpoint := stripScheme(server.URL)

			// invoke the function under test
			got, err := GetTerraformInstanceStatus(&http.Client{}, endpoint, inst)

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
				// even on error the URL had to encode the definition id
				if !strings.Contains(seenPath, tt.wantPathHas) {
					t.Errorf("server saw path %q, expected substring %q", seenPath, tt.wantPathHas)
				}
				return
			}

			// happy-path assertions: no error, non-nil detail, correct id echoed
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil || got.TerraformDefinition == nil {
				t.Fatalf("expected non-nil status detail with definition")
			}
			if got.TerraformDefinition.ID == nil || *got.TerraformDefinition.ID != tt.definitionID {
				t.Errorf("returned definition id = %v, want %d", got.TerraformDefinition.ID, tt.definitionID)
			}

			// confirm the helper built the by-id URL path from the instance's fk
			if !strings.Contains(seenPath, tt.wantPathHas) {
				t.Errorf("server saw path %q, expected substring %q", seenPath, tt.wantPathHas)
			}
		})
	}
}

// TestGetTerraformInstanceStatusEndpointFailure covers the transport error
// path for the instance helper: the helper must wrap the error and still
// return a usable detail pointer even when the underlying transport fails.
func TestGetTerraformInstanceStatusEndpointFailure(t *testing.T) {
	// point at a closed server so the underlying transport call fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint := stripScheme(server.URL)
	server.Close()

	inst := &v0.TerraformInstance{TerraformDefinitionID: ptrUint(1)}

	// invoke against the dead endpoint
	got, err := GetTerraformInstanceStatus(&http.Client{}, endpoint, inst)

	// transport failure must be wrapped with the helper's prefix and still
	// yield a non-nil pointer to the (empty) status detail
	if err == nil {
		t.Fatalf("expected error against closed server, got nil")
	}
	if !strings.Contains(err.Error(), "failed to retrieve terraform definition related to terraform instance") {
		t.Errorf("error %q missing expected prefix", err.Error())
	}
	if got == nil {
		t.Errorf("expected non-nil status detail even on transport error, got nil")
	}
}
