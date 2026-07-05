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

// TestGetOciProviderByDefaultProvider_HappyPath asserts that a 200 response
// with a populated data slice decodes into the expected *v0.OciProvider and
// that the request targets the default=true query string.
func TestGetOciProviderByDefaultProvider_HappyPath(t *testing.T) {
	// setup: server returns one OciProvider row and captures the request path
	var gotPath string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		writeOKResponse(t, w, []apiserver_lib.Object{
			v0.OciProvider{Name: strPtr("default-oci"), CompartmentOCID: strPtr("ocid1.compartment.oc1..abc")},
		})
	})
	defer server.Close()

	// action: call the lookup
	got, err := GetOciProviderByDefaultProvider(&http.Client{}, apiAddr)

	// assert: no error, provider decoded, and query carries the default flag
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.Name == nil || *got.Name != "default-oci" {
		t.Fatalf("expected provider name default-oci, got %+v", got)
	}
	if got.CompartmentOCID == nil || *got.CompartmentOCID != "ocid1.compartment.oc1..abc" {
		t.Errorf("expected compartment OCID ocid1.compartment.oc1..abc, got %+v", got.CompartmentOCID)
	}
	if !strings.Contains(gotPath, "default=true") {
		t.Errorf("expected default=true in path, got %q", gotPath)
	}
	if !strings.Contains(gotPath, "/v0/oci-providers") {
		t.Errorf("expected /v0/oci-providers path, got %q", gotPath)
	}
}

// TestGetOciProviderByDefaultProvider_EmptyData asserts the "no default OCI
// provider found" error branch when the response data slice is empty.
func TestGetOciProviderByDefaultProvider_EmptyData(t *testing.T) {
	// setup: server returns 200 with an empty data slice
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeOKResponse(t, w, []apiserver_lib.Object{})
	})
	defer server.Close()

	// action: call the lookup
	got, err := GetOciProviderByDefaultProvider(&http.Client{}, apiAddr)

	// assert: non-nil zero-value pointer and the missing-default error
	if err == nil {
		t.Fatal("expected error for empty data, got nil")
	}
	if err.Error() != "no default OCI provider found" {
		t.Errorf("expected 'no default OCI provider found', got %q", err.Error())
	}
	if got == nil {
		t.Fatal("expected non-nil zero-value pointer on empty data")
	}
}

// TestGetOciProviderByDefaultProvider_APIError asserts a non-200 response
// returns a wrapped error carrying the underlying sentinel.
func TestGetOciProviderByDefaultProvider_APIError(t *testing.T) {
	// setup: server returns 404 with a threeport-shaped error body
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusNotFound, "no default provider")
	})
	defer server.Close()

	// action: call the lookup
	_, err := GetOciProviderByDefaultProvider(&http.Client{}, apiAddr)

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

// TestGetOciProviderByCompartmentID covers happy-path decoding across
// representative compartment ID values and asserts the query string carries
// the argument.
func TestGetOciProviderByCompartmentID(t *testing.T) {
	tests := []struct {
		name          string
		compartmentID string
		wantInPath    string
	}{
		{name: "normal-compartment", compartmentID: "ocid1.compartment.oc1..xyz", wantInPath: "compartmentocid=ocid1.compartment.oc1..xyz"},
		{name: "empty-compartment", compartmentID: "", wantInPath: "compartmentocid="},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// setup: server verifies the query string carries the compartmentID
			var gotPath string
			server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path + "?" + r.URL.RawQuery
				writeOKResponse(t, w, []apiserver_lib.Object{
					v0.OciProvider{CompartmentOCID: strPtr(tc.compartmentID)},
				})
			})
			defer server.Close()

			// action: call the lookup with a specific compartmentID
			got, err := GetOciProviderByCompartmentID(&http.Client{}, apiAddr, tc.compartmentID)

			// assert: no error, decoded compartmentID matches, query carries value
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil || got.CompartmentOCID == nil || *got.CompartmentOCID != tc.compartmentID {
				t.Fatalf("expected CompartmentOCID %q, got %+v", tc.compartmentID, got)
			}
			if !strings.Contains(gotPath, tc.wantInPath) {
				t.Errorf("expected %q in path, got %q", tc.wantInPath, gotPath)
			}
			if !strings.Contains(gotPath, "/v0/oci-providers") {
				t.Errorf("expected /v0/oci-providers path, got %q", gotPath)
			}
		})
	}
}

// TestGetOciProviderByCompartmentID_EmptyData asserts the compartmentID-aware
// missing-provider error when the response data slice is empty.
func TestGetOciProviderByCompartmentID_EmptyData(t *testing.T) {
	// setup: server returns 200 with empty data
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeOKResponse(t, w, []apiserver_lib.Object{})
	})
	defer server.Close()

	// action: call the lookup with a specific compartmentID
	got, err := GetOciProviderByCompartmentID(&http.Client{}, apiAddr, "missing-comp")

	// assert: error names the missing compartmentID and pointer is non-nil
	if err == nil {
		t.Fatal("expected error for empty data, got nil")
	}
	want := "no OCI provider found with compartment ID missing-comp"
	if err.Error() != want {
		t.Errorf("expected %q, got %q", want, err.Error())
	}
	if got == nil {
		t.Fatal("expected non-nil zero-value pointer on empty data")
	}
}

// TestGetOciProviderByCompartmentID_APIError asserts a 401 upstream response
// surfaces as a wrapped ErrUnauthorized.
func TestGetOciProviderByCompartmentID_APIError(t *testing.T) {
	// setup: server returns 401 unauthorized
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusUnauthorized, "no token")
	})
	defer server.Close()

	// action: call the lookup
	_, err := GetOciProviderByCompartmentID(&http.Client{}, apiAddr, "comp-x")

	// assert: ErrUnauthorized reachable via errors.Is
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized in chain, got %v", err)
	}
}

// TestGetOciOkeKubernetesRuntimeDefinitionByK8sRuntimeDef_HappyPath asserts a
// populated data slice decodes and the ID appears in the query string.
func TestGetOciOkeKubernetesRuntimeDefinitionByK8sRuntimeDef_HappyPath(t *testing.T) {
	// setup: server returns one definition row and captures the request path
	var gotPath string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		writeOKResponse(t, w, []apiserver_lib.Object{
			v0.OciOkeKubernetesRuntimeDefinition{
				KubernetesRuntimeDefinitionID: uintPtr(42),
				WorkerNodeShape:               strPtr("VM.Standard.A1.Flex"),
			},
		})
	})
	defer server.Close()

	// action: call the lookup with a specific ID
	got, err := GetOciOkeKubernetesRuntimeDefinitionByK8sRuntimeDef(&http.Client{}, apiAddr, 42)

	// assert: no error, decoded fields match, and query carries the ID
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.KubernetesRuntimeDefinitionID == nil || *got.KubernetesRuntimeDefinitionID != 42 {
		t.Fatalf("expected KubernetesRuntimeDefinitionID 42, got %+v", got)
	}
	if got.WorkerNodeShape == nil || *got.WorkerNodeShape != "VM.Standard.A1.Flex" {
		t.Errorf("expected worker node shape VM.Standard.A1.Flex, got %+v", got.WorkerNodeShape)
	}
	if !strings.Contains(gotPath, "kubernetesruntimedefinitionid=42") {
		t.Errorf("expected kubernetesruntimedefinitionid=42 in path, got %q", gotPath)
	}
	if !strings.Contains(gotPath, "/v0/oci-oke-kubernetes-runtime-definitions") {
		t.Errorf("expected /v0/oci-oke-kubernetes-runtime-definitions path, got %q", gotPath)
	}
}

// TestGetOciOkeKubernetesRuntimeDefinitionByK8sRuntimeDef_EmptyData asserts the
// "no object found with ID N" error branch when Data is empty.
func TestGetOciOkeKubernetesRuntimeDefinitionByK8sRuntimeDef_EmptyData(t *testing.T) {
	// setup: server returns 200 with an empty data slice
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeOKResponse(t, w, []apiserver_lib.Object{})
	})
	defer server.Close()

	// action: call the lookup with an ID that returns nothing
	got, err := GetOciOkeKubernetesRuntimeDefinitionByK8sRuntimeDef(&http.Client{}, apiAddr, 7)

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

// TestGetOciOkeKubernetesRuntimeDefinitionByK8sRuntimeDef_APIError asserts a
// 500-style error path returns a wrapped upstream error.
func TestGetOciOkeKubernetesRuntimeDefinitionByK8sRuntimeDef_APIError(t *testing.T) {
	// setup: server returns 500 internal server error
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusInternalServerError, "boom")
	})
	defer server.Close()

	// action: call the lookup
	_, err := GetOciOkeKubernetesRuntimeDefinitionByK8sRuntimeDef(&http.Client{}, apiAddr, 1)

	// assert: error is wrapped with the "call to threeport API" prefix
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "call to threeport API returned unexpected response") {
		t.Errorf("expected upstream-error wrapper, got %v", err)
	}
}

// TestGetOciOkeKubernetesRuntimeInstanceByK8sRuntimeInst_HappyPath asserts an
// instance decodes and the ID appears in the query string.
func TestGetOciOkeKubernetesRuntimeInstanceByK8sRuntimeInst_HappyPath(t *testing.T) {
	// setup: server returns one instance row and captures the request path
	var gotPath string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		writeOKResponse(t, w, []apiserver_lib.Object{
			v0.OciOkeKubernetesRuntimeInstance{
				KubernetesRuntimeInstanceID: uintPtr(99),
				Region:                      strPtr("us-ashburn-1"),
			},
		})
	})
	defer server.Close()

	// action: call the lookup with the instance ID
	got, err := GetOciOkeKubernetesRuntimeInstanceByK8sRuntimeInst(&http.Client{}, apiAddr, 99)

	// assert: no error, decoded instance matches, and query carries the ID
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.KubernetesRuntimeInstanceID == nil || *got.KubernetesRuntimeInstanceID != 99 {
		t.Fatalf("expected KubernetesRuntimeInstanceID 99, got %+v", got)
	}
	if got.Region == nil || *got.Region != "us-ashburn-1" {
		t.Errorf("expected region us-ashburn-1, got %+v", got.Region)
	}
	if !strings.Contains(gotPath, "kubernetesruntimeinstanceid=99") {
		t.Errorf("expected kubernetesruntimeinstanceid=99 in path, got %q", gotPath)
	}
	if !strings.Contains(gotPath, "/v0/oci-oke-kubernetes-runtime-instances") {
		t.Errorf("expected /v0/oci-oke-kubernetes-runtime-instances path, got %q", gotPath)
	}
}

// TestGetOciOkeKubernetesRuntimeInstanceByK8sRuntimeInst_EmptyData asserts the
// "no object found with ID N" error branch when Data is empty.
func TestGetOciOkeKubernetesRuntimeInstanceByK8sRuntimeInst_EmptyData(t *testing.T) {
	// setup: server returns 200 with an empty data slice
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeOKResponse(t, w, []apiserver_lib.Object{})
	})
	defer server.Close()

	// action: call the lookup
	got, err := GetOciOkeKubernetesRuntimeInstanceByK8sRuntimeInst(&http.Client{}, apiAddr, 13)

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

// TestGetOciOkeKubernetesRuntimeInstanceByK8sRuntimeInst_APIError asserts a
// 403 upstream response returns a wrapped ErrForbidden.
func TestGetOciOkeKubernetesRuntimeInstanceByK8sRuntimeInst_APIError(t *testing.T) {
	// setup: server returns 403 forbidden
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusForbidden, "no access")
	})
	defer server.Close()

	// action: call the lookup
	_, err := GetOciOkeKubernetesRuntimeInstanceByK8sRuntimeInst(&http.Client{}, apiAddr, 1)

	// assert: ErrForbidden reachable via errors.Is
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrForbidden) {
		t.Errorf("expected ErrForbidden in chain, got %v", err)
	}
}
