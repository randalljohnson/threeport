package status

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// stripScheme returns the httptest server URL without the leading scheme,
// matching what threeport client callers pass as apiEndpoint (the client
// helpers prepend the scheme themselves).
func stripScheme(rawURL string) string {
	return strings.TrimPrefix(rawURL, "http://")
}

// TestGetAwsProviderStatus_HappyPath asserts a 200 response with two EKS
// runtime instances flows through into AwsEksKubernetesRuntimeInstances.
func TestGetAwsProviderStatus_HappyPath(t *testing.T) {
	// stand up a fake threeport API returning two EKS runtime instances scoped
	// to the requested provider id
	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		name1 := "runtime-a"
		name2 := "runtime-b"
		id1 := uint(1)
		id2 := uint(2)
		resp := apiserver_lib.Response{
			Data: []apiserver_lib.Object{
				v0.AwsEksKubernetesRuntimeInstance{Instance: v0.Instance{Name: &name1}, Common: v0.Common{ID: &id1}},
				v0.AwsEksKubernetesRuntimeInstance{Instance: v0.Instance{Name: &name2}, Common: v0.Common{ID: &id2}},
			},
			Status: apiserver_lib.Status{Code: http.StatusOK, Message: "OK"},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// invoke the function under test with the fake API endpoint
	status, err := GetAwsProviderStatus(srv.Client(), stripScheme(srv.URL), 42)

	// assert no error and both instances round-tripped
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if status == nil || status.AwsEksKubernetesRuntimeInstances == nil {
		t.Fatalf("expected non-nil status with instances, got %+v", status)
	}
	if got := len(*status.AwsEksKubernetesRuntimeInstances); got != 2 {
		t.Fatalf("expected 2 instances, got %d", got)
	}

	// assert the query string carried the provider id filter
	if want := "awsproviderid=42"; capturedQuery != want {
		t.Errorf("expected query %q, got %q", want, capturedQuery)
	}
}

// TestGetAwsProviderStatus_EmptyList asserts a 200 with an empty data slice
// yields an empty (non-nil) instance list and no error.
func TestGetAwsProviderStatus_EmptyList(t *testing.T) {
	// serve an empty data slice
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := apiserver_lib.Response{
			Data:   []apiserver_lib.Object{},
			Status: apiserver_lib.Status{Code: http.StatusOK, Message: "OK"},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// call with zero provider id (a boundary value the code accepts)
	status, err := GetAwsProviderStatus(srv.Client(), stripScheme(srv.URL), 0)

	// assert success and an empty (non-nil) list
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if status == nil || status.AwsEksKubernetesRuntimeInstances == nil {
		t.Fatalf("expected non-nil status with non-nil (empty) instances, got %+v", status)
	}
	if got := len(*status.AwsEksKubernetesRuntimeInstances); got != 0 {
		t.Fatalf("expected 0 instances, got %d", got)
	}
}

// TestGetAwsProviderStatus_ErrorPaths covers non-2xx responses from the API
// and asserts the returned error wraps the client failure.
func TestGetAwsProviderStatus_ErrorPaths(t *testing.T) {
	cases := []struct {
		name   string
		status int
	}{
		{name: "500 internal", status: http.StatusInternalServerError},
		{name: "404 not found", status: http.StatusNotFound},
		{name: "401 unauthorized", status: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// serve the failing status with a valid Response envelope so the
			// client can unmarshal it
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := apiserver_lib.Response{
					Status: apiserver_lib.Status{Code: tc.status, Message: "err", Error: "boom"},
				}
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer srv.Close()

			// invoke and expect an error wrapping the client-side failure
			status, err := GetAwsProviderStatus(srv.Client(), stripScheme(srv.URL), 7)

			// assert an error is returned mentioning the retrieval failure
			if err == nil {
				t.Fatalf("expected error for status %d, got nil", tc.status)
			}
			if !strings.Contains(err.Error(), "failed to retrieve AWS EKS Kubernetes runtime instances") {
				t.Errorf("expected error to mention retrieval failure, got %v", err)
			}
			// assert the returned status wrapper is non-nil even on error (the
			// function returns a pointer to its local, empty struct)
			if status == nil {
				t.Errorf("expected non-nil status wrapper on error, got nil")
			}
		})
	}
}

// TestGetAwsProviderStatus_UnreachableEndpoint asserts a transport-level
// failure surfaces as a wrapped error.
func TestGetAwsProviderStatus_UnreachableEndpoint(t *testing.T) {
	// point at an address with no listener (port 1 is reserved)
	status, err := GetAwsProviderStatus(&http.Client{}, "127.0.0.1:1", 1)

	// assert the failure is reported with the expected wrapping message
	if err == nil {
		t.Fatalf("expected error contacting unreachable endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "failed to retrieve AWS EKS Kubernetes runtime instances") {
		t.Errorf("expected wrapped retrieval error, got %v", err)
	}
	if status == nil {
		t.Errorf("expected non-nil status wrapper on error, got nil")
	}
}
