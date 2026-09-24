package kubernetesruntime

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	logr "github.com/go-logr/logr"
	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// newTestReconciler builds a *controller.Reconciler whose APIClient targets the
// given httptest server. The base URL is stripped of its scheme because
// client_lib.GetResponse re-prepends "http://" when no CustomTransport is set.
func newTestReconciler(server *httptest.Server) *controller.Reconciler {
	// strip the http:// prefix so GetResponse's own prefixing yields a valid URL
	addr := strings.TrimPrefix(server.URL, "http://")
	return &controller.Reconciler{
		Name:      "test",
		APIServer: addr,
		APIClient: server.Client(),
	}
}

// newDefinitionTestLogger returns a discard-backed logger that satisfies the
// *logr.Logger signature used by the reconciliation entry points.
func newDefinitionTestLogger() *logr.Logger {
	l := logr.Discard()
	return &l
}

// newCreatedResponse marshals the given object as the "Data[0]" body of an
// apiserver Response with StatusCreated status.
func newCreatedResponseBody(t *testing.T, obj interface{}) []byte {
	t.Helper()
	// wrap the object as apiserver Response.Data[0]
	resp := apiserver_lib.Response{
		Data: []apiserver_lib.Object{obj},
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response body: %v", err)
	}
	return body
}

// TestV0KubernetesRuntimeDefinitionCreated_ReconciledShortCircuits covers the
// branch where the incoming object is already Reconciled=true and no work is
// required.
func TestV0KubernetesRuntimeDefinitionCreated_ReconciledShortCircuits(t *testing.T) {
	// build a runtime definition already marked reconciled
	def := &v0.KubernetesRuntimeDefinition{
		Reconciliation: v0.Reconciliation{
			Reconciled: util.Ptr(true),
		},
		InfraProvider: util.Ptr(v0.KubernetesRuntimeInfraProviderEKS),
	}

	// invoke the reconciler with a nil-safe stub (no API call should occur)
	requeue, err := v0KubernetesRuntimeDefinitionCreated(&controller.Reconciler{}, def, newDefinitionTestLogger())

	// assert no error and zero requeue delay
	if err != nil {
		t.Fatalf("expected nil error on short-circuit, got %v", err)
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0KubernetesRuntimeDefinitionCreated_KindProviderNoOp covers the branch
// where the infra provider is kind: nothing is dispatched to the API.
func TestV0KubernetesRuntimeDefinitionCreated_KindProviderNoOp(t *testing.T) {
	// build a kind-provider definition that is not yet reconciled
	def := &v0.KubernetesRuntimeDefinition{
		Reconciliation: v0.Reconciliation{
			Reconciled: util.Ptr(false),
		},
		InfraProvider: util.Ptr(v0.KubernetesRuntimeInfraProviderKind),
	}

	// invoke reconciler; kind branch is a pure no-op
	requeue, err := v0KubernetesRuntimeDefinitionCreated(&controller.Reconciler{}, def, newDefinitionTestLogger())

	// assert no error and zero requeue delay
	if err != nil {
		t.Fatalf("expected nil error on kind no-op, got %v", err)
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0KubernetesRuntimeDefinitionCreated_UnknownProviderNoOp asserts a
// provider outside the switch falls through and returns without error.
func TestV0KubernetesRuntimeDefinitionCreated_UnknownProviderNoOp(t *testing.T) {
	// build a definition with an unhandled provider (oke)
	def := &v0.KubernetesRuntimeDefinition{
		Reconciliation: v0.Reconciliation{
			Reconciled: util.Ptr(false),
		},
		InfraProvider: util.Ptr(v0.KubernetesRuntimeInfraProviderOKE),
	}

	// invoke reconciler; the switch has no oke arm today so the outer function returns cleanly
	requeue, err := v0KubernetesRuntimeDefinitionCreated(&controller.Reconciler{}, def, newDefinitionTestLogger())

	// assert no error and zero requeue delay
	if err != nil {
		t.Fatalf("expected nil error for unknown provider fall-through, got %v", err)
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0KubernetesRuntimeDefinitionCreated_MachineTypeErrors covers the two
// mapping-failure branches (EKS and GKE) where an unsupported node
// profile/size combination surfaces a wrap error.
func TestV0KubernetesRuntimeDefinitionCreated_MachineTypeErrors(t *testing.T) {
	cases := []struct {
		name       string
		provider   string
		wantPrefix string
	}{
		{
			name:       "eks unsupported profile wraps aws message",
			provider:   v0.KubernetesRuntimeInfraProviderEKS,
			wantPrefix: "failed to map node size and profile to AWS machine type",
		},
		{
			name:       "gke unsupported profile wraps gcp message",
			provider:   v0.KubernetesRuntimeInfraProviderGKE,
			wantPrefix: "failed to map node size and profile to GCP machine type",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// build a definition with a non-existent node profile so mapping returns an error
			def := &v0.KubernetesRuntimeDefinition{
				Reconciliation: v0.Reconciliation{
					Reconciled: util.Ptr(false),
				},
				Definition:       v0.Definition{Name: util.Ptr("test")},
				InfraProvider:    util.Ptr(tc.provider),
				HighAvailability: util.Ptr(false),
				NodeProfile:      util.Ptr("BogusProfile"),
				NodeSize:         util.Ptr("Medium"),
				NodeMaximum:      util.Ptr(5),
			}

			// invoke reconciler; the mapping error should surface wrapped
			requeue, err := v0KubernetesRuntimeDefinitionCreated(&controller.Reconciler{}, def, newDefinitionTestLogger())

			// assert wrap-prefix error and zero requeue
			if err == nil {
				t.Fatalf("expected error for bogus node profile")
			}
			if !strings.HasPrefix(err.Error(), tc.wantPrefix) {
				t.Fatalf("expected error prefix %q, got %q", tc.wantPrefix, err.Error())
			}
			if requeue != 0 {
				t.Fatalf("expected 0 requeue delay, got %d", requeue)
			}
		})
	}
}

// TestV0KubernetesRuntimeDefinitionCreated_EKSHappyPath drives the EKS branch
// against a stub API returning 201 Created and verifies the request payload
// carries the mapped machine type and derived zoneCount.
func TestV0KubernetesRuntimeDefinitionCreated_EKSHappyPath(t *testing.T) {
	var gotPayload v0.AwsEksKubernetesRuntimeDefinition

	// stand up a stub API that captures the POST body and responds with the object echoed back
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// verify the path is the AWS EKS runtime-definition collection
		if !strings.HasSuffix(r.URL.Path, v0.PathAwsEksKubernetesRuntimeDefinitions) {
			t.Errorf("unexpected request path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// capture the marshaled payload for later assertions
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotPayload); err != nil {
			t.Errorf("failed to unmarshal request payload: %v", err)
		}

		// respond with a Created envelope echoing the received object
		w.WriteHeader(http.StatusCreated)
		w.Write(newCreatedResponseBody(t, gotPayload))
	}))
	defer server.Close()

	// build a HA=true definition so zoneCount resolves to 3
	def := &v0.KubernetesRuntimeDefinition{
		Common:           v0.Common{ID: util.Ptr(uint(42))},
		Reconciliation:   v0.Reconciliation{Reconciled: util.Ptr(false)},
		Definition:       v0.Definition{Name: util.Ptr("eks-cluster")},
		InfraProvider:    util.Ptr(v0.KubernetesRuntimeInfraProviderEKS),
		HighAvailability: util.Ptr(true),
		NodeProfile:      util.Ptr("Balanced"),
		NodeSize:         util.Ptr("Medium"),
		NodeMaximum:      util.Ptr(7),
	}

	// invoke reconciler against the stub server
	r := newTestReconciler(server)
	requeue, err := v0KubernetesRuntimeDefinitionCreated(r, def, newDefinitionTestLogger())

	// assert no error, zero requeue, and payload carries HA-derived zone count and mapped instance type
	if err != nil {
		t.Fatalf("expected nil error on happy path, got %v", err)
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
	if gotPayload.ZoneCount == nil || *gotPayload.ZoneCount != 3 {
		t.Fatalf("expected zoneCount=3 for HA, got %v", gotPayload.ZoneCount)
	}
	if gotPayload.DefaultNodeGroupInstanceType == nil || *gotPayload.DefaultNodeGroupInstanceType == "" {
		t.Fatalf("expected mapped machine type, got %v", gotPayload.DefaultNodeGroupInstanceType)
	}
	if gotPayload.DefaultNodeGroupInitialSize == nil || *gotPayload.DefaultNodeGroupInitialSize != 2 {
		t.Fatalf("expected initial size=2, got %v", gotPayload.DefaultNodeGroupInitialSize)
	}
	if gotPayload.DefaultNodeGroupMinimumSize == nil || *gotPayload.DefaultNodeGroupMinimumSize != 0 {
		t.Fatalf("expected min size=0, got %v", gotPayload.DefaultNodeGroupMinimumSize)
	}
	if gotPayload.DefaultNodeGroupMaximumSize == nil || *gotPayload.DefaultNodeGroupMaximumSize != 7 {
		t.Fatalf("expected max size=7, got %v", gotPayload.DefaultNodeGroupMaximumSize)
	}
	if gotPayload.KubernetesRuntimeDefinitionID == nil || *gotPayload.KubernetesRuntimeDefinitionID != 42 {
		t.Fatalf("expected linked definition ID=42, got %v", gotPayload.KubernetesRuntimeDefinitionID)
	}
}

// TestV0KubernetesRuntimeDefinitionCreated_GKEHappyPath drives the GKE branch
// against a stub API and verifies the request payload uses zoneCount=2 for
// non-HA and the GCP-mapped machine type.
func TestV0KubernetesRuntimeDefinitionCreated_GKEHappyPath(t *testing.T) {
	var gotPayload v0.GcpGkeKubernetesRuntimeDefinition

	// stand up a stub API that captures the POST body and responds with the object echoed back
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// verify the request targets the GCP GKE runtime-definition collection
		if !strings.HasSuffix(r.URL.Path, v0.PathGcpGkeKubernetesRuntimeDefinitions) {
			t.Errorf("unexpected request path %q", r.URL.Path)
		}

		// capture the marshaled payload for later assertions
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotPayload); err != nil {
			t.Errorf("failed to unmarshal request payload: %v", err)
		}

		// respond with a Created envelope echoing the received object
		w.WriteHeader(http.StatusCreated)
		w.Write(newCreatedResponseBody(t, gotPayload))
	}))
	defer server.Close()

	// build a HA=false definition so zoneCount resolves to 2
	def := &v0.KubernetesRuntimeDefinition{
		Common:           v0.Common{ID: util.Ptr(uint(7))},
		Reconciliation:   v0.Reconciliation{Reconciled: util.Ptr(false)},
		Definition:       v0.Definition{Name: util.Ptr("gke-cluster")},
		InfraProvider:    util.Ptr(v0.KubernetesRuntimeInfraProviderGKE),
		HighAvailability: util.Ptr(false),
		NodeProfile:      util.Ptr("Balanced"),
		NodeSize:         util.Ptr("Medium"),
		NodeMaximum:      util.Ptr(4),
	}

	// invoke reconciler against the stub server
	r := newTestReconciler(server)
	requeue, err := v0KubernetesRuntimeDefinitionCreated(r, def, newDefinitionTestLogger())

	// assert no error, zero requeue, and payload carries non-HA-derived zone count and mapped instance type
	if err != nil {
		t.Fatalf("expected nil error on happy path, got %v", err)
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
	if gotPayload.ZoneCount == nil || *gotPayload.ZoneCount != 2 {
		t.Fatalf("expected zoneCount=2 for non-HA, got %v", gotPayload.ZoneCount)
	}
	if gotPayload.DefaultNodeGroupInstanceType == nil || *gotPayload.DefaultNodeGroupInstanceType == "" {
		t.Fatalf("expected mapped machine type, got %v", gotPayload.DefaultNodeGroupInstanceType)
	}
	if gotPayload.KubernetesRuntimeDefinitionID == nil || *gotPayload.KubernetesRuntimeDefinitionID != 7 {
		t.Fatalf("expected linked definition ID=7, got %v", gotPayload.KubernetesRuntimeDefinitionID)
	}
}

// TestV0KubernetesRuntimeDefinitionCreated_EKSAPIError covers the branch where
// the AWS create call comes back with a non-201 status and the wrap error
// surfaces up to the caller.
func TestV0KubernetesRuntimeDefinitionCreated_EKSAPIError(t *testing.T) {
	// stand up a stub API that returns a 500 with a valid Response envelope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := apiserver_lib.Response{
			Status: apiserver_lib.Status{Error: "boom"},
		}
		body, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write(body)
	}))
	defer server.Close()

	// build a valid EKS definition; the client call should surface the server error
	def := &v0.KubernetesRuntimeDefinition{
		Common:           v0.Common{ID: util.Ptr(uint(1))},
		Reconciliation:   v0.Reconciliation{Reconciled: util.Ptr(false)},
		Definition:       v0.Definition{Name: util.Ptr("eks-cluster")},
		InfraProvider:    util.Ptr(v0.KubernetesRuntimeInfraProviderEKS),
		HighAvailability: util.Ptr(false),
		NodeProfile:      util.Ptr("Balanced"),
		NodeSize:         util.Ptr("Medium"),
		NodeMaximum:      util.Ptr(3),
	}

	// invoke reconciler; expect the outer wrap prefix on the returned error
	r := newTestReconciler(server)
	requeue, err := v0KubernetesRuntimeDefinitionCreated(r, def, newDefinitionTestLogger())

	// assert error wrapping and zero requeue
	if err == nil {
		t.Fatalf("expected error from api failure")
	}
	if !strings.HasPrefix(err.Error(), "failed to create new AWS EKS kubernetes runtime") {
		t.Fatalf("expected AWS wrap prefix, got %q", err.Error())
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0KubernetesRuntimeDefinitionCreated_GKEAPIError covers the branch where
// the GCP create call fails and the wrap error surfaces up to the caller.
func TestV0KubernetesRuntimeDefinitionCreated_GKEAPIError(t *testing.T) {
	// stand up a stub API that returns a 500 with a valid Response envelope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := apiserver_lib.Response{
			Status: apiserver_lib.Status{Error: "boom"},
		}
		body, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write(body)
	}))
	defer server.Close()

	// build a valid GKE definition; the client call should surface the server error
	def := &v0.KubernetesRuntimeDefinition{
		Common:           v0.Common{ID: util.Ptr(uint(2))},
		Reconciliation:   v0.Reconciliation{Reconciled: util.Ptr(false)},
		Definition:       v0.Definition{Name: util.Ptr("gke-cluster")},
		InfraProvider:    util.Ptr(v0.KubernetesRuntimeInfraProviderGKE),
		HighAvailability: util.Ptr(false),
		NodeProfile:      util.Ptr("Balanced"),
		NodeSize:         util.Ptr("Medium"),
		NodeMaximum:      util.Ptr(3),
	}

	// invoke reconciler; expect the outer wrap prefix on the returned error
	r := newTestReconciler(server)
	requeue, err := v0KubernetesRuntimeDefinitionCreated(r, def, newDefinitionTestLogger())

	// assert error wrapping and zero requeue
	if err == nil {
		t.Fatalf("expected error from api failure")
	}
	if !strings.HasPrefix(err.Error(), "failed to create new GCP GKE kubernetes runtime definition") {
		t.Fatalf("expected GCP wrap prefix, got %q", err.Error())
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0KubernetesRuntimeDefinitionUpdated_AlwaysNoOp asserts the update
// reconciler is a stub returning zero requeue and nil error regardless of
// input state.
func TestV0KubernetesRuntimeDefinitionUpdated_AlwaysNoOp(t *testing.T) {
	// nil reconciler and nil definition are safe because the function does not read them
	requeue, err := v0KubernetesRuntimeDefinitionUpdated(nil, nil, newDefinitionTestLogger())

	// assert no error and zero requeue
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0KubernetesRuntimeDefinitionDeleted covers the three delete branches:
// missing DeletionScheduled surfaces an error; a scheduled deletion returns
// cleanly; and a deletion already confirmed also returns cleanly.
func TestV0KubernetesRuntimeDefinitionDeleted(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name    string
		def     *v0.KubernetesRuntimeDefinition
		wantErr string
	}{
		{
			name: "missing schedule returns error",
			def: &v0.KubernetesRuntimeDefinition{
				Reconciliation: v0.Reconciliation{DeletionScheduled: nil},
			},
			wantErr: "deletion notification receieved but not scheduled",
		},
		{
			name: "scheduled deletion returns cleanly",
			def: &v0.KubernetesRuntimeDefinition{
				Reconciliation: v0.Reconciliation{DeletionScheduled: &now},
			},
		},
		{
			name: "already-confirmed deletion returns cleanly",
			def: &v0.KubernetesRuntimeDefinition{
				Reconciliation: v0.Reconciliation{
					DeletionScheduled: &now,
					DeletionConfirmed: &now,
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke reconciler; caller ignores reconciler content for the delete path
			requeue, err := v0KubernetesRuntimeDefinitionDeleted(&controller.Reconciler{}, tc.def, newDefinitionTestLogger())

			// assert requeue is always zero
			if requeue != 0 {
				t.Fatalf("expected 0 requeue delay, got %d", requeue)
			}

			// assert error text matches expectation for the missing-schedule branch
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
				return
			}
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("expected error %q, got %v", tc.wantErr, err)
			}
		})
	}
}
