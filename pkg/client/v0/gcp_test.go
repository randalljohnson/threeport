package v0

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	client_lib "github.com/threeport/threeport/pkg/client/lib/v0"
)

// TestGetGcpProviderByDefaultProvider_HappyPath asserts a 200 response with a
// populated data slice decodes into the expected *v0.GcpProvider and the
// request targets the default-provider query string.
func TestGetGcpProviderByDefaultProvider_HappyPath(t *testing.T) {
	// setup: server returns one GcpProvider row and captures the request path
	var gotPath string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		writeOKResponse(t, w, []apiserver_lib.Object{
			v0.GcpProvider{Name: strPtr("default-gcp"), ProjectID: strPtr("proj-123")},
		})
	})
	defer server.Close()

	// action: call the lookup
	got, err := GetGcpProviderByDefaultProvider(&http.Client{}, apiAddr)

	// assert: no error, provider decoded, and query carries the default flag
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.Name == nil || *got.Name != "default-gcp" {
		t.Fatalf("expected provider name default-gcp, got %+v", got)
	}
	if got.ProjectID == nil || *got.ProjectID != "proj-123" {
		t.Errorf("expected projectID proj-123, got %+v", got.ProjectID)
	}
	if !strings.Contains(gotPath, "defaultprovider=true") {
		t.Errorf("expected defaultprovider=true in path, got %q", gotPath)
	}
	if !strings.Contains(gotPath, "/v0/gcp-providers") {
		t.Errorf("expected /v0/gcp-providers path, got %q", gotPath)
	}
}

// TestGetGcpProviderByDefaultProvider_EmptyData asserts the "no default GCP
// provider found" error branch when the response data slice is empty.
func TestGetGcpProviderByDefaultProvider_EmptyData(t *testing.T) {
	// setup: server returns 200 with an empty data slice
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeOKResponse(t, w, []apiserver_lib.Object{})
	})
	defer server.Close()

	// action: call the lookup
	got, err := GetGcpProviderByDefaultProvider(&http.Client{}, apiAddr)

	// assert: non-nil zero-value pointer and the missing-default error
	if err == nil {
		t.Fatal("expected error for empty data, got nil")
	}
	if err.Error() != "no default GCP provider found" {
		t.Errorf("expected 'no default GCP provider found', got %q", err.Error())
	}
	if got == nil {
		t.Fatal("expected non-nil zero-value pointer on empty data")
	}
}

// TestGetGcpProviderByDefaultProvider_APIError asserts a non-200 response
// returns a wrapped error carrying the underlying sentinel.
func TestGetGcpProviderByDefaultProvider_APIError(t *testing.T) {
	// setup: server returns 404 with a threeport-shaped error body
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusNotFound, "no default provider")
	})
	defer server.Close()

	// action: call the lookup
	_, err := GetGcpProviderByDefaultProvider(&http.Client{}, apiAddr)

	// assert: ErrObjectNotFound reachable via errors.Is and outer wrapper present
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrObjectNotFound) {
		t.Errorf("expected ErrObjectNotFound in chain, got %v", err)
	}
	if !strings.Contains(err.Error(), "call to threeport API returned unexpected response") {
		t.Errorf("expected upstream-error wrapper, got %v", err)
	}
}

// TestGetGcpProviderByProjectID covers happy-path decoding across representative
// project ID values and asserts the query string carries the argument.
func TestGetGcpProviderByProjectID(t *testing.T) {
	tests := []struct {
		name       string
		projectID  string
		wantInPath string
	}{
		{name: "normal-project", projectID: "my-project-123", wantInPath: "projectid=my-project-123"},
		{name: "empty-project", projectID: "", wantInPath: "projectid="},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// setup: server verifies the query string carries the projectID
			var gotPath string
			server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path + "?" + r.URL.RawQuery
				writeOKResponse(t, w, []apiserver_lib.Object{
					v0.GcpProvider{ProjectID: strPtr(tc.projectID)},
				})
			})
			defer server.Close()

			// action: call the lookup
			got, err := GetGcpProviderByProjectID(&http.Client{}, apiAddr, tc.projectID)

			// assert: no error, decoded projectID matches, query carries value
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil || got.ProjectID == nil || *got.ProjectID != tc.projectID {
				t.Fatalf("expected ProjectID %q, got %+v", tc.projectID, got)
			}
			if !strings.Contains(gotPath, tc.wantInPath) {
				t.Errorf("expected %q in path, got %q", tc.wantInPath, gotPath)
			}
			if !strings.Contains(gotPath, "/v0/gcp-providers") {
				t.Errorf("expected /v0/gcp-providers path, got %q", gotPath)
			}
		})
	}
}

// TestGetGcpProviderByProjectID_EmptyData asserts the projectID-aware
// missing-provider error when the response data slice is empty.
func TestGetGcpProviderByProjectID_EmptyData(t *testing.T) {
	// setup: server returns 200 with empty data
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeOKResponse(t, w, []apiserver_lib.Object{})
	})
	defer server.Close()

	// action: call the lookup with a specific projectID
	got, err := GetGcpProviderByProjectID(&http.Client{}, apiAddr, "missing-proj")

	// assert: error names the missing projectID and pointer is non-nil
	if err == nil {
		t.Fatal("expected error for empty data, got nil")
	}
	want := "no GCP provider found with project ID missing-proj"
	if err.Error() != want {
		t.Errorf("expected %q, got %q", want, err.Error())
	}
	if got == nil {
		t.Fatal("expected non-nil zero-value pointer on empty data")
	}
}

// TestGetGcpProviderByProjectID_APIError asserts a 401 upstream response
// surfaces as a wrapped ErrUnauthorized.
func TestGetGcpProviderByProjectID_APIError(t *testing.T) {
	// setup: server returns 401 unauthorized
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusUnauthorized, "no token")
	})
	defer server.Close()

	// action: call the lookup
	_, err := GetGcpProviderByProjectID(&http.Client{}, apiAddr, "proj-x")

	// assert: ErrUnauthorized reachable via errors.Is
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized in chain, got %v", err)
	}
}

// TestGetGcpGkeKubernetesRuntimeDefinitionByK8sRuntimeDef_HappyPath asserts a
// populated data slice decodes and the ID is embedded in the query string.
func TestGetGcpGkeKubernetesRuntimeDefinitionByK8sRuntimeDef_HappyPath(t *testing.T) {
	// setup: server returns one definition row and captures the request path
	var gotPath string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		writeOKResponse(t, w, []apiserver_lib.Object{
			v0.GcpGkeKubernetesRuntimeDefinition{
				KubernetesRuntimeDefinitionID: uintPtr(42),
			},
		})
	})
	defer server.Close()

	// action: call the lookup with a specific ID
	got, err := GetGcpGkeKubernetesRuntimeDefinitionByK8sRuntimeDef(&http.Client{}, apiAddr, 42)

	// assert: no error, decoded ID matches, and query carries the ID
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.KubernetesRuntimeDefinitionID == nil || *got.KubernetesRuntimeDefinitionID != 42 {
		t.Fatalf("expected KubernetesRuntimeDefinitionID 42, got %+v", got)
	}
	if !strings.Contains(gotPath, "kubernetesruntimedefinitionid=42") {
		t.Errorf("expected kubernetesruntimedefinitionid=42 in path, got %q", gotPath)
	}
	if !strings.Contains(gotPath, "/v0/gcp-gke-kubernetes-runtime-definitions") {
		t.Errorf("expected /v0/gcp-gke-kubernetes-runtime-definitions path, got %q", gotPath)
	}
}

// TestGetGcpGkeKubernetesRuntimeDefinitionByK8sRuntimeDef_EmptyData asserts the
// "no object found with ID N" error branch when Data is empty.
func TestGetGcpGkeKubernetesRuntimeDefinitionByK8sRuntimeDef_EmptyData(t *testing.T) {
	// setup: server returns 200 with an empty data slice
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeOKResponse(t, w, []apiserver_lib.Object{})
	})
	defer server.Close()

	// action: call the lookup
	got, err := GetGcpGkeKubernetesRuntimeDefinitionByK8sRuntimeDef(&http.Client{}, apiAddr, 7)

	// assert: returns non-nil zero-value pointer and the missing-object error
	if err == nil {
		t.Fatal("expected error for empty data, got nil")
	}
	want := fmt.Sprintf("no object found with ID %d", 7)
	if err.Error() != want {
		t.Errorf("expected %q, got %q", want, err.Error())
	}
	if got == nil {
		t.Fatal("expected non-nil zero-value pointer on empty data")
	}
}

// TestGetGcpGkeKubernetesRuntimeDefinitionByK8sRuntimeDef_APIError asserts a
// 500-style error path returns a wrapped upstream error.
func TestGetGcpGkeKubernetesRuntimeDefinitionByK8sRuntimeDef_APIError(t *testing.T) {
	// setup: server returns 500 internal server error
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusInternalServerError, "boom")
	})
	defer server.Close()

	// action: call the lookup
	_, err := GetGcpGkeKubernetesRuntimeDefinitionByK8sRuntimeDef(&http.Client{}, apiAddr, 1)

	// assert: error is wrapped with the "call to threeport API" prefix
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "call to threeport API returned unexpected response") {
		t.Errorf("expected upstream-error wrapper, got %v", err)
	}
}

// TestGetGcpGkeKubernetesRuntimeInstanceByK8sRuntimeInst_HappyPath asserts an
// instance decodes and the ID appears in the query string.
func TestGetGcpGkeKubernetesRuntimeInstanceByK8sRuntimeInst_HappyPath(t *testing.T) {
	// setup: server returns one instance row and captures the request path
	var gotPath string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		writeOKResponse(t, w, []apiserver_lib.Object{
			v0.GcpGkeKubernetesRuntimeInstance{
				KubernetesRuntimeInstanceID: uintPtr(99),
				Region:                      strPtr("us-central1"),
			},
		})
	})
	defer server.Close()

	// action: call the lookup with the instance ID
	got, err := GetGcpGkeKubernetesRuntimeInstanceByK8sRuntimeInst(&http.Client{}, apiAddr, 99)

	// assert: no error, decoded instance matches, and query carries the ID
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.KubernetesRuntimeInstanceID == nil || *got.KubernetesRuntimeInstanceID != 99 {
		t.Fatalf("expected KubernetesRuntimeInstanceID 99, got %+v", got)
	}
	if got.Region == nil || *got.Region != "us-central1" {
		t.Errorf("expected region us-central1, got %+v", got.Region)
	}
	if !strings.Contains(gotPath, "kubernetesruntimeinstanceid=99") {
		t.Errorf("expected kubernetesruntimeinstanceid=99 in path, got %q", gotPath)
	}
	if !strings.Contains(gotPath, "/v0/gcp-gke-kubernetes-runtime-instances") {
		t.Errorf("expected /v0/gcp-gke-kubernetes-runtime-instances path, got %q", gotPath)
	}
}

// TestGetGcpGkeKubernetesRuntimeInstanceByK8sRuntimeInst_EmptyData asserts the
// "no object found with ID N" error branch when Data is empty.
func TestGetGcpGkeKubernetesRuntimeInstanceByK8sRuntimeInst_EmptyData(t *testing.T) {
	// setup: server returns 200 with an empty data slice
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeOKResponse(t, w, []apiserver_lib.Object{})
	})
	defer server.Close()

	// action: call the lookup
	got, err := GetGcpGkeKubernetesRuntimeInstanceByK8sRuntimeInst(&http.Client{}, apiAddr, 13)

	// assert: returns non-nil zero-value pointer and the missing-object error
	if err == nil {
		t.Fatal("expected error for empty data, got nil")
	}
	want := fmt.Sprintf("no object found with ID %d", 13)
	if err.Error() != want {
		t.Errorf("expected %q, got %q", want, err.Error())
	}
	if got == nil {
		t.Fatal("expected non-nil zero-value pointer on empty data")
	}
}

// TestGetGcpGkeKubernetesRuntimeInstanceByK8sRuntimeInst_APIError asserts a
// 403 upstream response returns a wrapped ErrForbidden.
func TestGetGcpGkeKubernetesRuntimeInstanceByK8sRuntimeInst_APIError(t *testing.T) {
	// setup: server returns 403 forbidden
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusForbidden, "no access")
	})
	defer server.Close()

	// action: call the lookup
	_, err := GetGcpGkeKubernetesRuntimeInstanceByK8sRuntimeInst(&http.Client{}, apiAddr, 1)

	// assert: ErrForbidden reachable via errors.Is
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrForbidden) {
		t.Errorf("expected ErrForbidden in chain, got %v", err)
	}
}
