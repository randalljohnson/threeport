package v0

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/datatypes"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
	client_lib "github.com/threeport/threeport/pkg/client/lib/v0"
)

// newAwsEksTestServer starts an httptest.Server whose handler yields the given
// status code and response body, and returns the apiAddr the client v0 helpers
// expect (scheme stripped so client_lib.GetResponse can prepend "http://").
func newAwsEksTestServer(t *testing.T, statusCode int, body []byte) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write(body)
	}))
	apiAddr := strings.TrimPrefix(srv.URL, "http://")
	return srv, apiAddr
}

// encodeAekriResponse marshals a Response wrapping the given
// AwsEksKubernetesRuntimeInstance as the single Data element.
func encodeAekriResponse(t *testing.T, aekri api_v0.AwsEksKubernetesRuntimeInstance) []byte {
	t.Helper()
	resp := apiserver_lib.Response{
		Data: []apiserver_lib.Object{aekri},
		Status: apiserver_lib.Status{
			Code:    http.StatusOK,
			Message: "OK",
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return b
}

// TestGetResourceInventoryByK8sRuntimeInst_ReturnsInventoryOnHappyPath asserts
// that a well-formed API response yields a populated EksInventory value.
func TestGetResourceInventoryByK8sRuntimeInst_ReturnsInventoryOnHappyPath(t *testing.T) {
	// build a resource inventory JSON that will round-trip through Unmarshal
	invJSON := datatypes.JSON([]byte(`{"region":"us-east-1","cluster":{"cluster_name":"foo"}}`))
	aekri := api_v0.AwsEksKubernetesRuntimeInstance{
		ResourceInventory: &invJSON,
	}
	body := encodeAekriResponse(t, aekri)

	// stand up a stub API server that returns the encoded instance
	srv, apiAddr := newAwsEksTestServer(t, http.StatusOK, body)
	defer srv.Close()

	// call under test
	kri := uint(42)
	inv, err := GetResourceInventoryByK8sRuntimeInst(srv.Client(), apiAddr, &kri)

	// no error and inventory returned
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv == nil {
		t.Fatal("expected non-nil inventory")
	}
	// verify the inventory carries the region we encoded, confirming Unmarshal ran
	if inv.Region != "us-east-1" {
		t.Errorf("expected region us-east-1, got %q", inv.Region)
	}
}

// TestGetResourceInventoryByK8sRuntimeInst_ErrorsWhenInventoryIsNil asserts
// that an instance with a nil ResourceInventory field produces a descriptive
// error rather than a nil pointer deref.
func TestGetResourceInventoryByK8sRuntimeInst_ErrorsWhenInventoryIsNil(t *testing.T) {
	// instance without any resource inventory attached
	aekri := api_v0.AwsEksKubernetesRuntimeInstance{}
	body := encodeAekriResponse(t, aekri)

	srv, apiAddr := newAwsEksTestServer(t, http.StatusOK, body)
	defer srv.Close()

	// call under test
	kri := uint(1)
	inv, err := GetResourceInventoryByK8sRuntimeInst(srv.Client(), apiAddr, &kri)

	// error returned and inventory is nil
	if err == nil {
		t.Fatal("expected error for nil resource inventory")
	}
	if inv != nil {
		t.Errorf("expected nil inventory, got %+v", inv)
	}
	// error message mentions the missing inventory
	if !strings.Contains(err.Error(), "does not have a resource inventory") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestGetResourceInventoryByK8sRuntimeInst_ErrorsWhenInventoryUnmarshalFails
// asserts that an inventory payload whose shape does not match EksInventory
// surfaces a wrapped unmarshal error.
func TestGetResourceInventoryByK8sRuntimeInst_ErrorsWhenInventoryUnmarshalFails(t *testing.T) {
	// hand-craft a response where ResourceInventory is valid JSON but its
	// "region" field is an object where EksInventory expects a string, so
	// json.Unmarshal into EksInventory fails
	body := []byte(`{
		"Data": [
			{"ResourceInventory": {"region": {"unexpected": "object"}}}
		],
		"Status": {"code": 200, "message": "OK"}
	}`)

	srv, apiAddr := newAwsEksTestServer(t, http.StatusOK, body)
	defer srv.Close()

	// call under test
	kri := uint(7)
	inv, err := GetResourceInventoryByK8sRuntimeInst(srv.Client(), apiAddr, &kri)

	// error returned, inventory is nil, and message points at unmarshal step
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
	if inv != nil {
		t.Errorf("expected nil inventory, got %+v", inv)
	}
	if !strings.Contains(err.Error(), "failed to unmarshal resource inventory") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestGetResourceInventoryByK8sRuntimeInst_ErrorsWhenApiReturnsNotFound
// asserts that an upstream 404 propagates as ErrObjectNotFound wrapped in the
// caller's own "failed to get" prefix.
func TestGetResourceInventoryByK8sRuntimeInst_ErrorsWhenApiReturnsNotFound(t *testing.T) {
	// stub API returns 404 with a threeport-shaped error response body
	notFound := apiserver_lib.Response{
		Status: apiserver_lib.Status{
			Code:    http.StatusNotFound,
			Message: "Not Found",
			Error:   "no rows",
		},
	}
	body, err := json.Marshal(notFound)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	srv, apiAddr := newAwsEksTestServer(t, http.StatusNotFound, body)
	defer srv.Close()

	// call under test
	kri := uint(99)
	inv, callErr := GetResourceInventoryByK8sRuntimeInst(srv.Client(), apiAddr, &kri)

	// error returned, inventory nil
	if callErr == nil {
		t.Fatal("expected error for API 404")
	}
	if inv != nil {
		t.Errorf("expected nil inventory, got %+v", inv)
	}
	// wrapped chain preserves ErrObjectNotFound so callers can errors.Is it
	if !errors.Is(callErr, client_lib.ErrObjectNotFound) {
		t.Errorf("expected error to wrap ErrObjectNotFound, got %v", callErr)
	}
	// outer wrap identifies the calling helper
	if !strings.Contains(callErr.Error(), "failed to get aws eks kubernetes runtime instance") {
		t.Errorf("unexpected outer error message: %v", callErr)
	}
}

// TestGetResourceInventoryByK8sRuntimeInst_ErrorsWhenApiReturnsEmptyData
// asserts that an OK response with no data elements surfaces the "no object
// found" error from the inner get helper.
func TestGetResourceInventoryByK8sRuntimeInst_ErrorsWhenApiReturnsEmptyData(t *testing.T) {
	// stub API returns 200 with an empty Data slice
	empty := apiserver_lib.Response{
		Data: []apiserver_lib.Object{},
		Status: apiserver_lib.Status{
			Code:    http.StatusOK,
			Message: "OK",
		},
	}
	body, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	srv, apiAddr := newAwsEksTestServer(t, http.StatusOK, body)
	defer srv.Close()

	// call under test
	kri := uint(3)
	inv, callErr := GetResourceInventoryByK8sRuntimeInst(srv.Client(), apiAddr, &kri)

	// error surfaces from the inner helper wrapped in the caller's prefix
	if callErr == nil {
		t.Fatal("expected error for empty data response")
	}
	if inv != nil {
		t.Errorf("expected nil inventory, got %+v", inv)
	}
	if !strings.Contains(callErr.Error(), fmt.Sprintf("no object found with ID %d", 3)) {
		t.Errorf("unexpected error message: %v", callErr)
	}
}
