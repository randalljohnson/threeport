package v0

import (
	"net/http"
	"strings"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// TestGetControlPlaneInstancesByControlPlaneDefinitionID_HappyPath asserts a
// 200 response with two instances decodes into the returned slice and the
// request URL carries the controlplanedefinitionid query string built from the
// given id.
func TestGetControlPlaneInstancesByControlPlaneDefinitionID_HappyPath(t *testing.T) {
	// setup: server returns two ControlPlaneInstance rows and captures the query
	var gotQuery string
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		writeOKResponse(t, w, []apiserver_lib.Object{
			v0.ControlPlaneInstance{Namespace: strPtr("ns-a")},
			v0.ControlPlaneInstance{Namespace: strPtr("ns-b")},
		})
	})
	defer server.Close()

	// action: fetch instances by definition id
	got, err := GetControlPlaneInstancesByControlPlaneDefinitionID(http.DefaultClient, apiAddr, 42)

	// assert: no error, both rows returned in order
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(*got) != 2 {
		t.Fatalf("expected 2 instances, got %#v", got)
	}
	if (*got)[0].Namespace == nil || *(*got)[0].Namespace != "ns-a" {
		t.Errorf("unexpected first row: %#v", (*got)[0])
	}
	if (*got)[1].Namespace == nil || *(*got)[1].Namespace != "ns-b" {
		t.Errorf("unexpected second row: %#v", (*got)[1])
	}

	// assert: query string encodes the id parameter
	if gotQuery != "controlplanedefinitionid=42" {
		t.Errorf("expected query controlplanedefinitionid=42, got %q", gotQuery)
	}
}

// TestGetControlPlaneInstancesByControlPlaneDefinitionID_EmptyResult asserts a
// 200 response with no data yields an empty (non-nil) slice and no error.
func TestGetControlPlaneInstancesByControlPlaneDefinitionID_EmptyResult(t *testing.T) {
	// setup: server returns an empty data slice
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeOKResponse(t, w, []apiserver_lib.Object{})
	})
	defer server.Close()

	// action: fetch instances by definition id
	got, err := GetControlPlaneInstancesByControlPlaneDefinitionID(http.DefaultClient, apiAddr, 1)

	// assert: no error, empty slice returned
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil slice pointer")
	}
	if len(*got) != 0 {
		t.Errorf("expected empty slice, got %d", len(*got))
	}
}

// TestGetControlPlaneInstancesByControlPlaneDefinitionID_ErrorPropagates
// asserts an upstream 500 response is wrapped in the expected error message.
func TestGetControlPlaneInstancesByControlPlaneDefinitionID_ErrorPropagates(t *testing.T) {
	// setup: server returns 500 error
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusInternalServerError, "boom")
	})
	defer server.Close()

	// action: fetch instances by definition id
	got, err := GetControlPlaneInstancesByControlPlaneDefinitionID(http.DefaultClient, apiAddr, 7)

	// assert: error surfaces with the wrapping prefix, slice returned is nil
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "could not retrieve control plane instances with definition id") {
		t.Errorf("expected wrapping prefix in error, got %q", err.Error())
	}
	if got != nil {
		t.Errorf("expected nil slice on error, got %#v", got)
	}
}

// TestGetSelfControlPlaneInstance covers the three switch branches: exactly
// one instance returns that instance, empty list returns "no local control
// plane instance", and more-than-one returns a count-bearing error.
func TestGetSelfControlPlaneInstance(t *testing.T) {
	tests := []struct {
		// name of the branch under test
		name string
		// data the fake server returns
		data []apiserver_lib.Object
		// wantErr is a substring the returned error must contain; "" means no error
		wantErr string
		// wantNamespace is the namespace on the returned single instance (only set on success)
		wantNamespace string
	}{
		{
			// happy path: exactly one instance in the response
			name:          "exactly one instance returned",
			data:          []apiserver_lib.Object{v0.ControlPlaneInstance{Namespace: strPtr("self-ns")}},
			wantNamespace: "self-ns",
		},
		{
			// empty branch: none returned yields the "no local" error
			name:    "zero instances returned",
			data:    []apiserver_lib.Object{},
			wantErr: "no local control plane instance",
		},
		{
			// too-many branch: two returned yields the count-bearing error
			name: "more than one instance returned",
			data: []apiserver_lib.Object{
				v0.ControlPlaneInstance{Namespace: strPtr("a")},
				v0.ControlPlaneInstance{Namespace: strPtr("b")},
			},
			wantErr: "more than one local control plane instance 2 returned",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// setup: server returns the configured data and captures the query
			var gotQuery string
			server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.RawQuery
				writeOKResponse(t, w, tt.data)
			})
			defer server.Close()

			// action: fetch the self control plane instance
			got, err := GetSelfControlPlaneInstance(http.DefaultClient, apiAddr)

			// assert: query string is the isself=true filter regardless of outcome
			if gotQuery != "isself=true" {
				t.Errorf("expected query isself=true, got %q", gotQuery)
			}

			// assert: error branch reports the expected substring; happy path returns the instance
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil || got.Namespace == nil || *got.Namespace != tt.wantNamespace {
				t.Errorf("expected namespace %q, got %#v", tt.wantNamespace, got)
			}
		})
	}
}

// TestGetSelfControlPlaneInstance_UpstreamError asserts an upstream error is
// wrapped with the self-instance-specific prefix.
func TestGetSelfControlPlaneInstance_UpstreamError(t *testing.T) {
	// setup: server returns 500 error
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusInternalServerError, "boom")
	})
	defer server.Close()

	// action: fetch the self control plane instance
	_, err := GetSelfControlPlaneInstance(http.DefaultClient, apiAddr)

	// assert: upstream error is wrapped by the self-instance prefix
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "could not retrieve self control plane instance") {
		t.Errorf("expected wrapping prefix, got %q", err.Error())
	}
}

// TestGetGenesisControlPlaneInstance covers the three switch branches of the
// genesis-instance lookup, mirroring the self-instance table.
func TestGetGenesisControlPlaneInstance(t *testing.T) {
	tests := []struct {
		// name of the branch under test
		name string
		// data the fake server returns
		data []apiserver_lib.Object
		// wantErr is a substring the returned error must contain; "" means no error
		wantErr string
		// wantNamespace is the namespace on the returned single instance (only set on success)
		wantNamespace string
	}{
		{
			// happy path: exactly one instance in the response
			name:          "exactly one instance returned",
			data:          []apiserver_lib.Object{v0.ControlPlaneInstance{Namespace: strPtr("genesis-ns")}},
			wantNamespace: "genesis-ns",
		},
		{
			// empty branch
			name:    "zero instances returned",
			data:    []apiserver_lib.Object{},
			wantErr: "no local control plane instance",
		},
		{
			// too-many branch
			name: "more than one instance returned",
			data: []apiserver_lib.Object{
				v0.ControlPlaneInstance{Namespace: strPtr("a")},
				v0.ControlPlaneInstance{Namespace: strPtr("b")},
				v0.ControlPlaneInstance{Namespace: strPtr("c")},
			},
			wantErr: "more than one local control plane instance 3 returned",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// setup: server returns the configured data and captures the query
			var gotQuery string
			server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.RawQuery
				writeOKResponse(t, w, tt.data)
			})
			defer server.Close()

			// action: fetch the genesis control plane instance
			got, err := GetGenesisControlPlaneInstance(http.DefaultClient, apiAddr)

			// assert: query string is the genesis=true filter regardless of outcome
			if gotQuery != "genesis=true" {
				t.Errorf("expected query genesis=true, got %q", gotQuery)
			}

			// assert: error branch reports the expected substring; happy path returns the instance
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil || got.Namespace == nil || *got.Namespace != tt.wantNamespace {
				t.Errorf("expected namespace %q, got %#v", tt.wantNamespace, got)
			}
		})
	}
}

// TestGetGenesisControlPlaneInstance_UpstreamError asserts an upstream error
// is wrapped with the genesis-instance-specific prefix.
func TestGetGenesisControlPlaneInstance_UpstreamError(t *testing.T) {
	// setup: server returns 500 error
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusInternalServerError, "boom")
	})
	defer server.Close()

	// action: fetch the genesis control plane instance
	_, err := GetGenesisControlPlaneInstance(http.DefaultClient, apiAddr)

	// assert: upstream error is wrapped by the genesis-instance prefix
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "could not retrieve genesis control plane instance") {
		t.Errorf("expected wrapping prefix, got %q", err.Error())
	}
}
