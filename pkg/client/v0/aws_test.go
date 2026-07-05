package v0

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	client_lib "github.com/threeport/threeport/pkg/client/lib/v0"
)

// newTestServer starts an httptest.Server whose handler is invoked with the
// request path plus raw query string; it returns the server and an apiAddr
// stripped of scheme (as callers in this package pass it).
func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	// GetResponse prepends "http://" to the URL, so strip the scheme from the
	// httptest URL before passing it as apiAddr.
	apiAddr := strings.TrimPrefix(server.URL, "http://")
	return server, apiAddr
}

// writeOKResponse encodes an apiserver_lib.Response with the given data slice
// and writes it to w as a 200 response body.
func writeOKResponse(t *testing.T, w http.ResponseWriter, data []apiserver_lib.Object) {
	t.Helper()
	resp := apiserver_lib.Response{Data: data}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		t.Fatalf("failed to write response body: %v", err)
	}
}

// writeErrorResponse writes a threeport-shaped error response with the given
// status code and message.
func writeErrorResponse(t *testing.T, w http.ResponseWriter, code int, message string) {
	t.Helper()
	resp := apiserver_lib.Response{
		Status: apiserver_lib.Status{Code: code, Message: message, Error: message},
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal error response: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if _, err := w.Write(body); err != nil {
		t.Fatalf("failed to write error response: %v", err)
	}
}

// strPtr and uintPtr build addressable literals inline; use plain locals here
// rather than util.Ptr to avoid pulling the util package into a test file.
func strPtr(s string) *string { return &s }
func uintPtr(u uint) *uint    { return &u }

// TestGetAwsProviderByDefaultProvider_HappyPath asserts a 200 response with a
// populated data slice decodes into the expected *v0.AwsProvider and the
// request targets the default-provider query string.
func TestGetAwsProviderByDefaultProvider_HappyPath(t *testing.T) {
	// setup: server returns one AwsProvider row and captures the request path
	var gotPath string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		writeOKResponse(t, w, []apiserver_lib.Object{
			v0.AwsProvider{Name: strPtr("default-aws"), AccountID: strPtr("1234")},
		})
	})
	defer server.Close()

	// action: call the lookup
	got, err := GetAwsProviderByDefaultProvider(&http.Client{}, apiAddr)

	// assert: no error, provider decoded, and query string carries the flag
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.Name == nil || *got.Name != "default-aws" {
		t.Fatalf("expected provider name default-aws, got %+v", got)
	}
	if !strings.Contains(gotPath, "defaultprovider=true") {
		t.Errorf("expected defaultprovider=true in path, got %q", gotPath)
	}
	if !strings.Contains(gotPath, "/v0/aws-providers") {
		t.Errorf("expected /v0/aws-providers in path, got %q", gotPath)
	}
}

// TestGetAwsProviderByDefaultProvider_APIError asserts that a non-200 status
// from the API returns a wrapped error and does not shadow the sentinel.
func TestGetAwsProviderByDefaultProvider_APIError(t *testing.T) {
	// setup: server returns 404 with a threeport-shaped error body
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusNotFound, "no default provider")
	})
	defer server.Close()

	// action: call the lookup
	_, err := GetAwsProviderByDefaultProvider(&http.Client{}, apiAddr)

	// assert: the sentinel ErrObjectNotFound is reachable via errors.Is
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrObjectNotFound) {
		t.Errorf("expected ErrObjectNotFound in chain, got %v", err)
	}
}

// TestGetAwsProviderByAccountID covers happy-path decoding and the query-string
// carrying the accountID argument.
func TestGetAwsProviderByAccountID(t *testing.T) {
	tests := []struct {
		name       string
		accountID  string
		wantInPath string
	}{
		{name: "numeric-account", accountID: "999888777", wantInPath: "accountid=999888777"},
		{name: "empty-account", accountID: "", wantInPath: "accountid="},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// setup: server verifies the query string carries the accountID
			var gotPath string
			server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path + "?" + r.URL.RawQuery
				writeOKResponse(t, w, []apiserver_lib.Object{
					v0.AwsProvider{AccountID: strPtr(tc.accountID)},
				})
			})
			defer server.Close()

			// action: call the lookup
			got, err := GetAwsProviderByAccountID(&http.Client{}, apiAddr, tc.accountID)

			// assert: no error, provider decoded, and query carries accountID
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil || got.AccountID == nil || *got.AccountID != tc.accountID {
				t.Fatalf("expected AccountID %q, got %+v", tc.accountID, got)
			}
			if !strings.Contains(gotPath, tc.wantInPath) {
				t.Errorf("expected %q in path, got %q", tc.wantInPath, gotPath)
			}
		})
	}
}

// TestGetAwsProviderByAccountID_APIError asserts that upstream 401 responses
// surface as a wrapped ErrUnauthorized.
func TestGetAwsProviderByAccountID_APIError(t *testing.T) {
	// setup: server returns 401 unauthorized
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusUnauthorized, "no token")
	})
	defer server.Close()

	// action: call the lookup
	_, err := GetAwsProviderByAccountID(&http.Client{}, apiAddr, "123")

	// assert: the sentinel ErrUnauthorized is reachable via errors.Is
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized in chain, got %v", err)
	}
}

// TestGetAwsEksKubernetesRuntimeDefinitionByK8sRuntimeDef_HappyPath asserts a
// populated data slice decodes and the ID is embedded in the query string.
func TestGetAwsEksKubernetesRuntimeDefinitionByK8sRuntimeDef_HappyPath(t *testing.T) {
	// setup: server returns one definition row and captures the request path
	var gotPath string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		writeOKResponse(t, w, []apiserver_lib.Object{
			v0.AwsEksKubernetesRuntimeDefinition{
				KubernetesRuntimeDefinitionID: uintPtr(42),
			},
		})
	})
	defer server.Close()

	// action: call the lookup with a specific ID
	got, err := GetAwsEksKubernetesRuntimeDefinitionByK8sRuntimeDef(&http.Client{}, apiAddr, 42)

	// assert: no error, definition decoded, and ID appears in query string
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.KubernetesRuntimeDefinitionID == nil || *got.KubernetesRuntimeDefinitionID != 42 {
		t.Fatalf("expected KubernetesRuntimeDefinitionID 42, got %+v", got)
	}
	if !strings.Contains(gotPath, "kubernetesruntimedefinitionid=42") {
		t.Errorf("expected kubernetesruntimedefinitionid=42 in path, got %q", gotPath)
	}
	if !strings.Contains(gotPath, "/v0/aws-eks-kubernetes-runtime-definitions") {
		t.Errorf("expected /v0/aws-eks-kubernetes-runtime-definitions path, got %q", gotPath)
	}
}

// TestGetAwsEksKubernetesRuntimeDefinitionByK8sRuntimeDef_EmptyData asserts the
// explicit "no object found with ID N" error branch when Data is empty.
func TestGetAwsEksKubernetesRuntimeDefinitionByK8sRuntimeDef_EmptyData(t *testing.T) {
	// setup: server returns 200 with an empty data slice
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeOKResponse(t, w, []apiserver_lib.Object{})
	})
	defer server.Close()

	// action: call the lookup
	got, err := GetAwsEksKubernetesRuntimeDefinitionByK8sRuntimeDef(&http.Client{}, apiAddr, 7)

	// assert: returns non-nil zero-value pointer and the missing-object error
	if err == nil {
		t.Fatal("expected error for empty data, got nil")
	}
	want := fmt.Sprintf("no object found with ID %d", 7)
	if err.Error() != want {
		t.Errorf("expected error %q, got %q", want, err.Error())
	}
	if got == nil {
		t.Fatal("expected non-nil zero-value pointer on empty data")
	}
}

// TestGetAwsEksKubernetesRuntimeDefinitionByK8sRuntimeDef_APIError asserts a
// 500-style error path returns a wrapped error rather than a decoded object.
func TestGetAwsEksKubernetesRuntimeDefinitionByK8sRuntimeDef_APIError(t *testing.T) {
	// setup: server returns 500 internal server error
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusInternalServerError, "boom")
	})
	defer server.Close()

	// action: call the lookup
	_, err := GetAwsEksKubernetesRuntimeDefinitionByK8sRuntimeDef(&http.Client{}, apiAddr, 1)

	// assert: error is wrapped with the "call to threeport API" prefix
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "call to threeport API returned unexpected response") {
		t.Errorf("expected upstream-error wrapper, got %v", err)
	}
}

// TestGetAwsEksKubernetesRuntimeInstanceByK8sRuntimeInst_HappyPath asserts an
// instance is decoded and its ID appears in the query string.
func TestGetAwsEksKubernetesRuntimeInstanceByK8sRuntimeInst_HappyPath(t *testing.T) {
	// setup: server returns one instance row and captures the request path
	var gotPath string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		writeOKResponse(t, w, []apiserver_lib.Object{
			v0.AwsEksKubernetesRuntimeInstance{
				KubernetesRuntimeInstanceID: uintPtr(99),
				Region:                      strPtr("us-west-2"),
			},
		})
	})
	defer server.Close()

	// action: call the lookup with the instance ID
	got, err := GetAwsEksKubernetesRuntimeInstanceByK8sRuntimeInst(&http.Client{}, apiAddr, 99)

	// assert: no error, instance decoded, and ID appears in query
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.KubernetesRuntimeInstanceID == nil || *got.KubernetesRuntimeInstanceID != 99 {
		t.Fatalf("expected KubernetesRuntimeInstanceID 99, got %+v", got)
	}
	if got.Region == nil || *got.Region != "us-west-2" {
		t.Errorf("expected region us-west-2, got %+v", got.Region)
	}
	if !strings.Contains(gotPath, "kubernetesruntimeinstanceid=99") {
		t.Errorf("expected kubernetesruntimeinstanceid=99 in path, got %q", gotPath)
	}
	if !strings.Contains(gotPath, "/v0/aws-eks-kubernetes-runtime-instances") {
		t.Errorf("expected /v0/aws-eks-kubernetes-runtime-instances path, got %q", gotPath)
	}
}

// TestGetAwsEksKubernetesRuntimeInstanceByK8sRuntimeInst_EmptyData asserts the
// explicit "no object found with ID N" error branch when Data is empty.
func TestGetAwsEksKubernetesRuntimeInstanceByK8sRuntimeInst_EmptyData(t *testing.T) {
	// setup: server returns 200 with an empty data slice
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeOKResponse(t, w, []apiserver_lib.Object{})
	})
	defer server.Close()

	// action: call the lookup
	got, err := GetAwsEksKubernetesRuntimeInstanceByK8sRuntimeInst(&http.Client{}, apiAddr, 13)

	// assert: returns non-nil zero-value pointer and the missing-object error
	if err == nil {
		t.Fatal("expected error for empty data, got nil")
	}
	want := fmt.Sprintf("no object found with ID %d", 13)
	if err.Error() != want {
		t.Errorf("expected error %q, got %q", want, err.Error())
	}
	if got == nil {
		t.Fatal("expected non-nil zero-value pointer on empty data")
	}
}

// TestGetAwsEksKubernetesRuntimeInstanceByK8sRuntimeInst_APIError asserts a
// 403 upstream response returns a wrapped ErrForbidden.
func TestGetAwsEksKubernetesRuntimeInstanceByK8sRuntimeInst_APIError(t *testing.T) {
	// setup: server returns 403 forbidden
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusForbidden, "no access")
	})
	defer server.Close()

	// action: call the lookup
	_, err := GetAwsEksKubernetesRuntimeInstanceByK8sRuntimeInst(&http.Client{}, apiAddr, 1)

	// assert: the sentinel ErrForbidden is reachable via errors.Is
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrForbidden) {
		t.Errorf("expected ErrForbidden in chain, got %v", err)
	}
}
