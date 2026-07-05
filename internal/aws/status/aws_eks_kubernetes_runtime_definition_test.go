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

// TestGetAwsEksKubernetesRuntimeDefinitionStatus_HappyPath asserts a full
// success flow: the instance-list route and the definition-by-id route both
// return data, and both land on the status detail struct.
func TestGetAwsEksKubernetesRuntimeDefinitionStatus_HappyPath(t *testing.T) {
	// stand up a fake threeport API that routes based on the request path,
	// serving the instance list for the query-string call and a single
	// kubernetes runtime definition for the by-id call
	var capturedInstQuery string
	var capturedKrdPath string
	instName := "eks-inst-a"
	instID := uint(11)
	krdName := "krd-alpha"
	krdID := uint(99)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, v0.PathAwsEksKubernetesRuntimeInstances):
			capturedInstQuery = r.URL.RawQuery
			resp := apiserver_lib.Response{
				Data: []apiserver_lib.Object{
					v0.AwsEksKubernetesRuntimeInstance{
						Instance: v0.Instance{Name: &instName},
						Common:   v0.Common{ID: &instID},
					},
				},
				Status: apiserver_lib.Status{Code: http.StatusOK, Message: "OK"},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case strings.HasPrefix(r.URL.Path, v0.PathKubernetesRuntimeDefinitions):
			capturedKrdPath = r.URL.Path
			resp := apiserver_lib.Response{
				Data: []apiserver_lib.Object{
					v0.KubernetesRuntimeDefinition{
						Definition: v0.Definition{Name: &krdName},
						Common:     v0.Common{ID: &krdID},
					},
				},
				Status: apiserver_lib.Status{Code: http.StatusOK, Message: "OK"},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// build the input definition referencing an ID and a related KRD
	defID := uint(7)
	krdRef := uint(99)
	input := &v0.AwsEksKubernetesRuntimeDefinition{
		Definition:                    v0.Definition{Name: strPtr("eks-def")},
		Common:                        v0.Common{ID: &defID},
		KubernetesRuntimeDefinitionID: &krdRef,
	}

	// invoke the function under test
	got, err := GetAwsEksKubernetesRuntimeDefinitionStatus(srv.Client(), stripScheme(srv.URL), input)

	// assert success and both fields populated from the two API calls
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got == nil {
		t.Fatalf("expected non-nil status detail")
	}
	if got.AwsEksKubernetesRuntimeInstances == nil || len(*got.AwsEksKubernetesRuntimeInstances) != 1 {
		t.Errorf("expected 1 instance, got %+v", got.AwsEksKubernetesRuntimeInstances)
	}
	if got.KubernetesRuntimeDefinition == nil || got.KubernetesRuntimeDefinition.ID == nil || *got.KubernetesRuntimeDefinition.ID != 99 {
		t.Errorf("expected KRD id 99, got %+v", got.KubernetesRuntimeDefinition)
	}

	// assert the two upstream calls carried the expected filters
	if want := "awsekskubernetesruntimedefinitionid=7"; capturedInstQuery != want {
		t.Errorf("expected instance query %q, got %q", want, capturedInstQuery)
	}
	if want := v0.PathKubernetesRuntimeDefinitions + "/99"; capturedKrdPath != want {
		t.Errorf("expected KRD path %q, got %q", want, capturedKrdPath)
	}
}

// TestGetAwsEksKubernetesRuntimeDefinitionStatus_InstanceFetchFails asserts
// that when the first (instance-list) call errors, the function returns a
// non-nil status wrapper and a wrapped error, and does NOT proceed to fetch
// the KRD.
func TestGetAwsEksKubernetesRuntimeDefinitionStatus_InstanceFetchFails(t *testing.T) {
	// serve a 500 for the instances route; the KRD route would 404 if hit,
	// but we assert it is NOT hit
	krdHit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, v0.PathAwsEksKubernetesRuntimeInstances):
			resp := apiserver_lib.Response{
				Status: apiserver_lib.Status{Code: http.StatusInternalServerError, Message: "err", Error: "boom"},
			}
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(resp)
		case strings.HasPrefix(r.URL.Path, v0.PathKubernetesRuntimeDefinitions):
			krdHit = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	// build a minimal input with the required IDs
	defID := uint(1)
	krdRef := uint(2)
	input := &v0.AwsEksKubernetesRuntimeDefinition{
		Common:                        v0.Common{ID: &defID},
		KubernetesRuntimeDefinitionID: &krdRef,
	}

	// invoke and expect an error wrapping the instance-fetch failure
	got, err := GetAwsEksKubernetesRuntimeDefinitionStatus(srv.Client(), stripScheme(srv.URL), input)

	// assert error is wrapped with the instance-retrieval message
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to retrieve AWS EKS kubernetes runtime instances") {
		t.Errorf("expected instance-fetch error wrap, got %v", err)
	}

	// assert the returned status wrapper is non-nil (function returns pointer
	// to its local struct even on error)
	if got == nil {
		t.Errorf("expected non-nil status detail wrapper on error, got nil")
	}

	// assert the KRD route was NOT invoked after the instance call failed
	if krdHit {
		t.Errorf("expected KRD route to be skipped after instance fetch failure")
	}
}

// TestGetAwsEksKubernetesRuntimeDefinitionStatus_KrdFetchFails asserts that
// when the instance call succeeds but the KRD lookup fails, the returned
// status detail carries the instances, and the error wraps the KRD failure.
func TestGetAwsEksKubernetesRuntimeDefinitionStatus_KrdFetchFails(t *testing.T) {
	// serve an OK instance list and a 404 KRD lookup
	instName := "eks-inst-a"
	instID := uint(1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, v0.PathAwsEksKubernetesRuntimeInstances):
			resp := apiserver_lib.Response{
				Data: []apiserver_lib.Object{
					v0.AwsEksKubernetesRuntimeInstance{
						Instance: v0.Instance{Name: &instName},
						Common:   v0.Common{ID: &instID},
					},
				},
				Status: apiserver_lib.Status{Code: http.StatusOK, Message: "OK"},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case strings.HasPrefix(r.URL.Path, v0.PathKubernetesRuntimeDefinitions):
			resp := apiserver_lib.Response{
				Status: apiserver_lib.Status{Code: http.StatusNotFound, Message: "not found", Error: "missing"},
			}
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()

	defID := uint(3)
	krdRef := uint(4)
	input := &v0.AwsEksKubernetesRuntimeDefinition{
		Common:                        v0.Common{ID: &defID},
		KubernetesRuntimeDefinitionID: &krdRef,
	}

	// invoke and expect an error wrapping the KRD-fetch failure
	got, err := GetAwsEksKubernetesRuntimeDefinitionStatus(srv.Client(), stripScheme(srv.URL), input)

	// assert error mentions the KRD retrieval failure
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to retrieve associated kubernetes runtime definition") {
		t.Errorf("expected KRD-fetch error wrap, got %v", err)
	}

	// assert instances were still recorded on the returned detail before the
	// KRD failure aborted the flow
	if got == nil {
		t.Fatalf("expected non-nil status detail wrapper on error, got nil")
	}
	if got.AwsEksKubernetesRuntimeInstances == nil || len(*got.AwsEksKubernetesRuntimeInstances) != 1 {
		t.Errorf("expected 1 instance recorded before KRD failure, got %+v", got.AwsEksKubernetesRuntimeInstances)
	}

	// assert KRD field remains nil since its fetch failed
	if got.KubernetesRuntimeDefinition != nil {
		t.Errorf("expected nil KRD after fetch failure, got %+v", got.KubernetesRuntimeDefinition)
	}
}

// TestGetAwsEksKubernetesRuntimeDefinitionStatus_EmptyInstanceList asserts the
// boundary case where zero instances match: the flow still fetches the KRD
// and returns a populated detail with an empty (non-nil) instance list.
func TestGetAwsEksKubernetesRuntimeDefinitionStatus_EmptyInstanceList(t *testing.T) {
	// serve an empty instance list and a valid KRD
	krdName := "krd-empty"
	krdID := uint(50)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, v0.PathAwsEksKubernetesRuntimeInstances):
			resp := apiserver_lib.Response{
				Data:   []apiserver_lib.Object{},
				Status: apiserver_lib.Status{Code: http.StatusOK, Message: "OK"},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case strings.HasPrefix(r.URL.Path, v0.PathKubernetesRuntimeDefinitions):
			resp := apiserver_lib.Response{
				Data: []apiserver_lib.Object{
					v0.KubernetesRuntimeDefinition{
						Definition: v0.Definition{Name: &krdName},
						Common:     v0.Common{ID: &krdID},
					},
				},
				Status: apiserver_lib.Status{Code: http.StatusOK, Message: "OK"},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()

	defID := uint(0)
	krdRef := uint(50)
	input := &v0.AwsEksKubernetesRuntimeDefinition{
		Common:                        v0.Common{ID: &defID},
		KubernetesRuntimeDefinitionID: &krdRef,
	}

	// invoke and expect success with an empty (non-nil) instance list
	got, err := GetAwsEksKubernetesRuntimeDefinitionStatus(srv.Client(), stripScheme(srv.URL), input)

	// assert success
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// assert instance list is non-nil and empty
	if got.AwsEksKubernetesRuntimeInstances == nil {
		t.Errorf("expected non-nil (empty) instance list, got nil")
	}
	if len(*got.AwsEksKubernetesRuntimeInstances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(*got.AwsEksKubernetesRuntimeInstances))
	}

	// assert KRD was still fetched
	if got.KubernetesRuntimeDefinition == nil || *got.KubernetesRuntimeDefinition.ID != 50 {
		t.Errorf("expected KRD id 50, got %+v", got.KubernetesRuntimeDefinition)
	}
}

// TestGetAwsEksKubernetesRuntimeDefinitionStatus_UnreachableEndpoint asserts a
// transport-level failure surfaces as a wrapped instance-fetch error.
func TestGetAwsEksKubernetesRuntimeDefinitionStatus_UnreachableEndpoint(t *testing.T) {
	// build a valid input; direct the client at an address with no listener
	defID := uint(1)
	krdRef := uint(2)
	input := &v0.AwsEksKubernetesRuntimeDefinition{
		Common:                        v0.Common{ID: &defID},
		KubernetesRuntimeDefinitionID: &krdRef,
	}

	// invoke against an unreachable endpoint
	got, err := GetAwsEksKubernetesRuntimeDefinitionStatus(&http.Client{}, "127.0.0.1:1", input)

	// assert the failure is reported with the instance-retrieval wrap
	if err == nil {
		t.Fatalf("expected error contacting unreachable endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "failed to retrieve AWS EKS kubernetes runtime instances") {
		t.Errorf("expected wrapped instance-retrieval error, got %v", err)
	}
	if got == nil {
		t.Errorf("expected non-nil status wrapper on error, got nil")
	}
}

// strPtr is a local helper returning a pointer to the given string.
func strPtr(s string) *string { return &s }
