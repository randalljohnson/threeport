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

// TestGetAwsEksKubernetesRuntimeInstanceStatus_HappyPath asserts the two-step
// happy path: the definition-by-id lookup and the kubernetes-runtime-instance
// lookup both succeed and land on the returned status detail.
func TestGetAwsEksKubernetesRuntimeInstanceStatus_HappyPath(t *testing.T) {
	// stand up a fake threeport API that dispatches on the request path,
	// serving a single AwsEksKubernetesRuntimeDefinition for the def-by-id
	// call and a single KubernetesRuntimeInstance for the kri-by-id call
	var capturedDefPath string
	var capturedKriPath string
	defName := "eks-def-alpha"
	defID := uint(42)
	kriName := "kri-alpha"
	kriID := uint(84)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, v0.PathAwsEksKubernetesRuntimeDefinitions):
			capturedDefPath = r.URL.Path
			resp := apiserver_lib.Response{
				Data: []apiserver_lib.Object{
					v0.AwsEksKubernetesRuntimeDefinition{
						Definition: v0.Definition{Name: &defName},
						Common:     v0.Common{ID: &defID},
					},
				},
				Status: apiserver_lib.Status{Code: http.StatusOK, Message: "OK"},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case strings.HasPrefix(r.URL.Path, v0.PathKubernetesRuntimeInstances):
			capturedKriPath = r.URL.Path
			resp := apiserver_lib.Response{
				Data: []apiserver_lib.Object{
					v0.KubernetesRuntimeInstance{
						Instance: v0.Instance{Name: &kriName},
						Common:   v0.Common{ID: &kriID},
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

	// build the input instance referencing both the AWS EKS def and the KRI
	instID := uint(7)
	defRef := uint(42)
	kriRef := uint(84)
	input := &v0.AwsEksKubernetesRuntimeInstance{
		Instance:                             v0.Instance{Name: strPtr("eks-inst")},
		Common:                               v0.Common{ID: &instID},
		AwsEksKubernetesRuntimeDefinitionID:  &defRef,
		KubernetesRuntimeInstanceID:          &kriRef,
	}

	// invoke the function under test
	got, err := GetAwsEksKubernetesRuntimeInstanceStatus(srv.Client(), stripScheme(srv.URL), input)

	// assert success and both status fields populated from the two API calls
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got == nil {
		t.Fatalf("expected non-nil status detail")
	}
	if got.AwsEksKubernetesRuntimeDefinition == nil ||
		got.AwsEksKubernetesRuntimeDefinition.ID == nil ||
		*got.AwsEksKubernetesRuntimeDefinition.ID != 42 {
		t.Errorf("expected AWS EKS def id 42, got %+v", got.AwsEksKubernetesRuntimeDefinition)
	}
	if got.KubernetesRuntimeInstance == nil ||
		got.KubernetesRuntimeInstance.ID == nil ||
		*got.KubernetesRuntimeInstance.ID != 84 {
		t.Errorf("expected KRI id 84, got %+v", got.KubernetesRuntimeInstance)
	}

	// assert both upstream calls hit the correct by-id paths
	if want := v0.PathAwsEksKubernetesRuntimeDefinitions + "/42"; capturedDefPath != want {
		t.Errorf("expected def path %q, got %q", want, capturedDefPath)
	}
	if want := v0.PathKubernetesRuntimeInstances + "/84"; capturedKriPath != want {
		t.Errorf("expected KRI path %q, got %q", want, capturedKriPath)
	}
}

// TestGetAwsEksKubernetesRuntimeInstanceStatus_DefFetchFails asserts that when
// the first (def-by-id) call errors, the function returns a non-nil status
// wrapper and a wrapped error, and does NOT proceed to fetch the KRI.
func TestGetAwsEksKubernetesRuntimeInstanceStatus_DefFetchFails(t *testing.T) {
	// serve a 500 for the def route; assert the KRI route is never hit
	kriHit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, v0.PathAwsEksKubernetesRuntimeDefinitions):
			resp := apiserver_lib.Response{
				Status: apiserver_lib.Status{Code: http.StatusInternalServerError, Message: "err", Error: "boom"},
			}
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(resp)
		case strings.HasPrefix(r.URL.Path, v0.PathKubernetesRuntimeInstances):
			kriHit = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	// build a minimal input with the required IDs
	instID := uint(1)
	defRef := uint(2)
	kriRef := uint(3)
	input := &v0.AwsEksKubernetesRuntimeInstance{
		Common:                              v0.Common{ID: &instID},
		AwsEksKubernetesRuntimeDefinitionID: &defRef,
		KubernetesRuntimeInstanceID:         &kriRef,
	}

	// invoke and expect an error wrapping the def-fetch failure
	got, err := GetAwsEksKubernetesRuntimeInstanceStatus(srv.Client(), stripScheme(srv.URL), input)

	// assert the error message names the AWS EKS def retrieval failure
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to retrieve AWS EKS kubernetes runtime definition") {
		t.Errorf("expected def-fetch error wrap, got %v", err)
	}

	// assert the returned status wrapper is non-nil (function returns pointer
	// to its local struct even on error)
	if got == nil {
		t.Errorf("expected non-nil status detail wrapper on error, got nil")
	}

	// assert the KRI route was NOT invoked after the def call failed
	if kriHit {
		t.Errorf("expected KRI route to be skipped after def fetch failure")
	}
}

// TestGetAwsEksKubernetesRuntimeInstanceStatus_KriFetchFails asserts that when
// the def call succeeds but the KRI lookup fails, the returned status detail
// carries the def and the error wraps the KRI failure.
func TestGetAwsEksKubernetesRuntimeInstanceStatus_KriFetchFails(t *testing.T) {
	// serve an OK def response and a 404 KRI lookup
	defName := "eks-def"
	defID := uint(11)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, v0.PathAwsEksKubernetesRuntimeDefinitions):
			resp := apiserver_lib.Response{
				Data: []apiserver_lib.Object{
					v0.AwsEksKubernetesRuntimeDefinition{
						Definition: v0.Definition{Name: &defName},
						Common:     v0.Common{ID: &defID},
					},
				},
				Status: apiserver_lib.Status{Code: http.StatusOK, Message: "OK"},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
		case strings.HasPrefix(r.URL.Path, v0.PathKubernetesRuntimeInstances):
			resp := apiserver_lib.Response{
				Status: apiserver_lib.Status{Code: http.StatusNotFound, Message: "not found", Error: "missing"},
			}
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()

	instID := uint(5)
	defRef := uint(11)
	kriRef := uint(999)
	input := &v0.AwsEksKubernetesRuntimeInstance{
		Common:                              v0.Common{ID: &instID},
		AwsEksKubernetesRuntimeDefinitionID: &defRef,
		KubernetesRuntimeInstanceID:         &kriRef,
	}

	// invoke and expect an error wrapping the KRI-fetch failure
	got, err := GetAwsEksKubernetesRuntimeInstanceStatus(srv.Client(), stripScheme(srv.URL), input)

	// assert the error mentions the associated KRI retrieval failure
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to retrieve associated kubernetes runtime instances") {
		t.Errorf("expected KRI-fetch error wrap, got %v", err)
	}

	// assert the def was still recorded on the returned detail before the
	// KRI failure aborted the flow
	if got == nil {
		t.Fatalf("expected non-nil status detail wrapper on error, got nil")
	}
	if got.AwsEksKubernetesRuntimeDefinition == nil ||
		got.AwsEksKubernetesRuntimeDefinition.ID == nil ||
		*got.AwsEksKubernetesRuntimeDefinition.ID != 11 {
		t.Errorf("expected def id 11 recorded before KRI failure, got %+v", got.AwsEksKubernetesRuntimeDefinition)
	}

	// assert the KRI field remains nil since its fetch failed
	if got.KubernetesRuntimeInstance != nil {
		t.Errorf("expected nil KRI after fetch failure, got %+v", got.KubernetesRuntimeInstance)
	}
}

// TestGetAwsEksKubernetesRuntimeInstanceStatus_UnreachableEndpoint asserts a
// transport-level failure surfaces as a wrapped def-fetch error.
func TestGetAwsEksKubernetesRuntimeInstanceStatus_UnreachableEndpoint(t *testing.T) {
	// build a valid input; direct the client at an address with no listener
	instID := uint(1)
	defRef := uint(2)
	kriRef := uint(3)
	input := &v0.AwsEksKubernetesRuntimeInstance{
		Common:                              v0.Common{ID: &instID},
		AwsEksKubernetesRuntimeDefinitionID: &defRef,
		KubernetesRuntimeInstanceID:         &kriRef,
	}

	// invoke against an unreachable endpoint
	got, err := GetAwsEksKubernetesRuntimeInstanceStatus(&http.Client{}, "127.0.0.1:1", input)

	// assert the failure is reported with the def-retrieval wrap (first call)
	if err == nil {
		t.Fatalf("expected error contacting unreachable endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "failed to retrieve AWS EKS kubernetes runtime definition") {
		t.Errorf("expected wrapped def-retrieval error, got %v", err)
	}
	if got == nil {
		t.Errorf("expected non-nil status wrapper on error, got nil")
	}
}
