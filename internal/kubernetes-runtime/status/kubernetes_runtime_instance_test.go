package status

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	client_lib "github.com/threeport/threeport/pkg/client/lib/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// newTestServer starts a test HTTP server with the given handler and returns
// the schemeless address expected by client.GetKubernetesRuntimeDefinitionByID.
func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	addr := strings.TrimPrefix(server.URL, "http://")
	return server, addr
}

// writeDefinitionResponse writes a successful threeport API response body
// carrying the supplied KubernetesRuntimeDefinition object.
func writeDefinitionResponse(t *testing.T, w http.ResponseWriter, def *v0.KubernetesRuntimeDefinition) {
	t.Helper()
	resp, err := apiserver_lib.CreateResponse(apiserver_lib.SingleObjectMeta(), def, "KubernetesRuntimeDefinition")
	if err != nil {
		t.Fatalf("failed to build response: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Fatalf("failed to encode response: %v", err)
	}
}

// TestGetKubernetesRuntimeInstanceStatus_HappyPath asserts that the function
// fetches the referenced kubernetes runtime definition and returns it under
// the status detail's KubernetesRuntimeDefinition field.
func TestGetKubernetesRuntimeInstanceStatus_HappyPath(t *testing.T) {
	// arrange: build an instance pointing at definition id 42 and a server
	// that returns a definition with a matching id
	defID := uint(42)
	provider := "eks"
	server, addr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// verify the client hits the by-id path for the requested definition
		if !strings.HasSuffix(r.URL.Path, "/v0/kubernetes-runtime-definitions/42") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		writeDefinitionResponse(t, w, &v0.KubernetesRuntimeDefinition{
			Common:        v0.Common{ID: util.Ptr(defID)},
			InfraProvider: util.Ptr(provider),
		})
	})
	defer server.Close()

	instance := &v0.KubernetesRuntimeInstance{
		KubernetesRuntimeDefinitionID: util.Ptr(defID),
	}

	// act: call the function under test
	detail, err := GetKubernetesRuntimeInstanceStatus(http.DefaultClient, addr, instance)

	// assert: no error and the returned detail carries the fetched definition
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail == nil {
		t.Fatal("detail should not be nil")
	}
	if detail.KubernetesRuntimeDefinition == nil {
		t.Fatal("KubernetesRuntimeDefinition should be populated")
	}
	if detail.KubernetesRuntimeDefinition.ID == nil || *detail.KubernetesRuntimeDefinition.ID != defID {
		t.Errorf("expected definition ID %d, got %+v", defID, detail.KubernetesRuntimeDefinition.ID)
	}
	if detail.KubernetesRuntimeDefinition.InfraProvider == nil || *detail.KubernetesRuntimeDefinition.InfraProvider != provider {
		t.Errorf("expected InfraProvider %q, got %+v", provider, detail.KubernetesRuntimeDefinition.InfraProvider)
	}
}

// TestGetKubernetesRuntimeInstanceStatus_ServerErrors asserts that failure
// responses from the definition endpoint propagate as a wrapped error and the
// returned detail struct has no definition populated.
func TestGetKubernetesRuntimeInstanceStatus_ServerErrors(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		wantErr    error
	}{
		{
			name:       "not found returns wrapped ErrObjectNotFound",
			statusCode: http.StatusNotFound,
			wantErr:    client_lib.ErrObjectNotFound,
		},
		{
			name:       "unauthorized returns wrapped ErrUnauthorized",
			statusCode: http.StatusUnauthorized,
			wantErr:    client_lib.ErrUnauthorized,
		},
		{
			name:       "server error returns a non-nil error",
			statusCode: http.StatusInternalServerError,
			wantErr:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// arrange: server always returns the failure status
			server, addr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				resp := apiserver_lib.CreateResponseErrorWithStatus(
					nil,
					apiserver_lib.CreateStatus(tc.statusCode, http.StatusText(tc.statusCode), "boom"),
					"KubernetesRuntimeDefinition",
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				_ = json.NewEncoder(w).Encode(resp)
			})
			defer server.Close()

			instance := &v0.KubernetesRuntimeInstance{
				KubernetesRuntimeDefinitionID: util.Ptr(uint(1)),
			}

			// act
			detail, err := GetKubernetesRuntimeInstanceStatus(http.DefaultClient, addr, instance)

			// assert: an error surfaces and the empty detail struct is still returned
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("expected error to wrap %v, got %v", tc.wantErr, err)
			}
			if detail == nil {
				t.Fatal("detail pointer should not be nil even on error")
			}
			if detail.KubernetesRuntimeDefinition != nil {
				t.Errorf("expected definition to be nil on error, got %+v", detail.KubernetesRuntimeDefinition)
			}
		})
	}
}

// TestGetKubernetesRuntimeInstanceStatus_UnreachableEndpoint asserts that a
// transport-level failure surfaces as a wrapped error without a populated
// definition.
func TestGetKubernetesRuntimeInstanceStatus_UnreachableEndpoint(t *testing.T) {
	// arrange: use an address that will fail to connect
	instance := &v0.KubernetesRuntimeInstance{
		KubernetesRuntimeDefinitionID: util.Ptr(uint(1)),
	}

	// act: the closed server address triggers a dial error
	detail, err := GetKubernetesRuntimeInstanceStatus(http.DefaultClient, "127.0.0.1:1", instance)

	// assert: error surfaces and detail is empty
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if detail == nil {
		t.Fatal("detail pointer should not be nil even on error")
	}
	if detail.KubernetesRuntimeDefinition != nil {
		t.Errorf("expected definition to be nil on error, got %+v", detail.KubernetesRuntimeDefinition)
	}
}
