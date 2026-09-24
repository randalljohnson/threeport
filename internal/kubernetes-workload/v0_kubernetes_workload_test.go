package kubernetesworkload

import (
	"encoding/json"
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

// newTestLogger returns a discard-backed logger that satisfies the *logr.Logger
// signature used by the reconciliation entry points.
func newTestLogger() *logr.Logger {
	l := logr.Discard()
	return &l
}

// writeResponse writes an apiserver_lib.Response envelope carrying the given
// data objects with the specified status code.
func writeResponse(t *testing.T, w http.ResponseWriter, status int, data []apiserver_lib.Object) {
	t.Helper()
	resp := apiserver_lib.Response{Data: data}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response body: %v", err)
	}
	w.WriteHeader(status)
	w.Write(body)
}

// writeErrorResponse writes an apiserver_lib.Response with a non-empty error
// message at the requested status code.
func writeErrorResponse(t *testing.T, w http.ResponseWriter, status int, msg string) {
	t.Helper()
	resp := apiserver_lib.Response{Status: apiserver_lib.Status{Error: msg}}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal error response body: %v", err)
	}
	w.WriteHeader(status)
	w.Write(body)
}

// TestV0KubernetesWorkloadDefinitionCreated_HappyPath covers the branch where
// YAMLDocument parses into one kubernetes resource and the resulting resource
// definition posts successfully.
func TestV0KubernetesWorkloadDefinitionCreated_HappyPath(t *testing.T) {
	// stub API captures the POST body and responds with an empty Created envelope
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		// respond with a Created envelope carrying an empty data set; the
		// caller only iterates the returned list for logging
		writeResponse(t, w, http.StatusCreated, []apiserver_lib.Object{})
	}))
	defer server.Close()

	// build a definition whose YAML holds a single Pod document
	yaml := "apiVersion: v1\nkind: Pod\nmetadata:\n  name: web\n"
	def := &v0.KubernetesWorkloadDefinition{
		Common:       v0.Common{ID: util.Ptr(uint(11))},
		YAMLDocument: util.Ptr(yaml),
	}

	// invoke reconciler against the stub server
	r := newTestReconciler(server)
	requeue, err := v0KubernetesWorkloadDefinitionCreated(r, def, newTestLogger())

	// assert the reconciler returned cleanly and hit the resource-definition-sets endpoint
	if err != nil {
		t.Fatalf("expected nil error on happy path, got %v", err)
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST method, got %s", gotMethod)
	}
	if !strings.HasSuffix(gotPath, v0.PathKubernetesWorkloadResourceDefinitionSets) {
		t.Fatalf("expected path suffix %q, got %q", v0.PathKubernetesWorkloadResourceDefinitionSets, gotPath)
	}
}

// TestV0KubernetesWorkloadDefinitionCreated_YAMLParseError covers the branch
// where GetJsonResourcesFromYamlDoc surfaces a parse error and the wrap prefix
// bubbles up.
func TestV0KubernetesWorkloadDefinitionCreated_YAMLParseError(t *testing.T) {
	// broken YAML: unclosed flow mapping trips the yaml.v3 decoder
	def := &v0.KubernetesWorkloadDefinition{
		Common:       v0.Common{ID: util.Ptr(uint(1))},
		YAMLDocument: util.Ptr("{foo: bar"),
	}

	// invoke reconciler; no API is dialed because parsing fails first
	requeue, err := v0KubernetesWorkloadDefinitionCreated(&controller.Reconciler{}, def, newTestLogger())

	// assert the outer wrap prefix and zero requeue
	if err == nil {
		t.Fatalf("expected parse error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "failed to get JSON kube objects from YAML document") {
		t.Fatalf("expected parse wrap prefix, got %q", err.Error())
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0KubernetesWorkloadDefinitionCreated_APIError covers the branch where
// the POST to the resource-definition-sets endpoint fails and the wrap error
// surfaces to the caller.
func TestV0KubernetesWorkloadDefinitionCreated_APIError(t *testing.T) {
	// stub API returns a 500 with a valid Response envelope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusInternalServerError, "boom")
	}))
	defer server.Close()

	// build a valid single-document definition so the API call happens
	def := &v0.KubernetesWorkloadDefinition{
		Common:       v0.Common{ID: util.Ptr(uint(1))},
		YAMLDocument: util.Ptr("apiVersion: v1\nkind: Pod\nmetadata:\n  name: p\n"),
	}

	// invoke reconciler against the failing stub
	r := newTestReconciler(server)
	requeue, err := v0KubernetesWorkloadDefinitionCreated(r, def, newTestLogger())

	// assert the outer wrap prefix and zero requeue
	if err == nil {
		t.Fatalf("expected api error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "failed to create kubernetes workload resource definitions in API") {
		t.Fatalf("expected api wrap prefix, got %q", err.Error())
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0KubernetesWorkloadDefinitionUpdated_AlwaysNoOp asserts the update
// reconciler is a stub returning zero requeue and nil error regardless of
// input state.
func TestV0KubernetesWorkloadDefinitionUpdated_AlwaysNoOp(t *testing.T) {
	// nil reconciler and nil definition are safe because the function does not read them
	requeue, err := v0KubernetesWorkloadDefinitionUpdated(nil, nil, newTestLogger())

	// assert the stub returns cleanly with a zero requeue
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0KubernetesWorkloadDefinitionDeleted covers the three delete branches:
// missing DeletionScheduled surfaces an error; a scheduled deletion with an
// empty resource-definition list returns cleanly; and a deletion already
// confirmed returns cleanly without any API traffic.
func TestV0KubernetesWorkloadDefinitionDeleted(t *testing.T) {
	now := time.Now()

	// stub API returns an empty list for the GET path so the delete loop is a no-op
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the caller only issues a GET for the resource-definition list
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		writeResponse(t, w, http.StatusOK, []apiserver_lib.Object{})
	}))
	defer server.Close()

	cases := []struct {
		name    string
		def     *v0.KubernetesWorkloadDefinition
		wantErr string
	}{
		{
			// missing DeletionScheduled aborts before touching the API
			name: "missing schedule returns error",
			def: &v0.KubernetesWorkloadDefinition{
				Common:         v0.Common{ID: util.Ptr(uint(1))},
				Reconciliation: v0.Reconciliation{DeletionScheduled: nil},
			},
			wantErr: "deletion notification receieved but not scheduled",
		},
		{
			// scheduled deletion drives the GET+loop path, empty list ends cleanly
			name: "scheduled deletion returns cleanly",
			def: &v0.KubernetesWorkloadDefinition{
				Common:         v0.Common{ID: util.Ptr(uint(1))},
				Reconciliation: v0.Reconciliation{DeletionScheduled: &now},
			},
		},
		{
			// already-confirmed deletion short-circuits before any API traffic
			name: "already-confirmed deletion returns cleanly",
			def: &v0.KubernetesWorkloadDefinition{
				Common: v0.Common{ID: util.Ptr(uint(1))},
				Reconciliation: v0.Reconciliation{
					DeletionScheduled: &now,
					DeletionConfirmed: &now,
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke reconciler; the missing-schedule case never dials the stub
			r := newTestReconciler(server)
			requeue, err := v0KubernetesWorkloadDefinitionDeleted(r, tc.def, newTestLogger())

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

// TestV0KubernetesWorkloadDefinitionDeleted_GetError covers the branch where
// the GET call for related resource definitions fails and the wrap error
// surfaces to the caller.
func TestV0KubernetesWorkloadDefinitionDeleted_GetError(t *testing.T) {
	now := time.Now()

	// stub API returns 500 on the GET path so the outer wrap prefix triggers
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusInternalServerError, "boom")
	}))
	defer server.Close()

	// build a scheduled-deletion definition so the GET actually happens
	def := &v0.KubernetesWorkloadDefinition{
		Common:         v0.Common{ID: util.Ptr(uint(1))},
		Reconciliation: v0.Reconciliation{DeletionScheduled: &now},
	}

	// invoke reconciler against the failing stub
	r := newTestReconciler(server)
	requeue, err := v0KubernetesWorkloadDefinitionDeleted(r, def, newTestLogger())

	// assert the outer wrap prefix and zero requeue
	if err == nil {
		t.Fatalf("expected error from api failure")
	}
	if !strings.HasPrefix(err.Error(), "failed to get kubernetes workload resource definitions") {
		t.Fatalf("expected get wrap prefix, got %q", err.Error())
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0KubernetesWorkloadInstanceCreated_DefNotReconciled covers the branch
// where the referenced kubernetes workload definition is not yet reconciled
// and the instance reconciler backs off with an error.
func TestV0KubernetesWorkloadInstanceCreated_DefNotReconciled(t *testing.T) {
	// stub API returns a definition whose Reconciled flag is explicitly false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		def := v0.KubernetesWorkloadDefinition{
			Common:         v0.Common{ID: util.Ptr(uint(9))},
			Reconciliation: v0.Reconciliation{Reconciled: util.Ptr(false)},
		}
		writeResponse(t, w, http.StatusOK, []apiserver_lib.Object{def})
	}))
	defer server.Close()

	// build an instance whose definition ID drives the GET-by-ID call
	inst := &v0.KubernetesWorkloadInstance{
		Common:                         v0.Common{ID: util.Ptr(uint(1))},
		KubernetesWorkloadDefinitionID: util.Ptr(uint(9)),
	}

	// invoke reconciler; the definition-not-reconciled branch produces the sentinel error
	r := newTestReconciler(server)
	requeue, err := v0KubernetesWorkloadInstanceCreated(r, inst, newTestLogger())

	// assert the sentinel error surfaces and zero requeue
	if err == nil || err.Error() != "kubernetes workload definition not reconciled" {
		t.Fatalf("expected definition-not-reconciled error, got %v", err)
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0KubernetesWorkloadInstanceCreated_DefFetchError covers the branch where
// the initial GetKubernetesWorkloadDefinitionByID call inside
// confirmK8sWorkloadDefReconciled fails and the wrap error surfaces.
func TestV0KubernetesWorkloadInstanceCreated_DefFetchError(t *testing.T) {
	// stub API returns 500 for every request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusInternalServerError, "boom")
	}))
	defer server.Close()

	// build a minimal instance so the definition-by-ID call is dispatched
	inst := &v0.KubernetesWorkloadInstance{
		Common:                         v0.Common{ID: util.Ptr(uint(1))},
		KubernetesWorkloadDefinitionID: util.Ptr(uint(9)),
	}

	// invoke reconciler against the failing stub
	r := newTestReconciler(server)
	requeue, err := v0KubernetesWorkloadInstanceCreated(r, inst, newTestLogger())

	// assert the outer confirm-reconciled wrap prefix and zero requeue
	if err == nil {
		t.Fatalf("expected error from api failure")
	}
	if !strings.HasPrefix(err.Error(), "failed to determine if kubernetes workload definition is reconciled") {
		t.Fatalf("expected confirm wrap prefix, got %q", err.Error())
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0KubernetesWorkloadInstanceUpdated_RuntimeInstanceFetchError covers the
// branch where the first GET (kubernetes runtime instance by ID) fails and the
// wrap error surfaces to the caller.
func TestV0KubernetesWorkloadInstanceUpdated_RuntimeInstanceFetchError(t *testing.T) {
	// stub API returns 500 for the runtime-instance-by-ID call
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusInternalServerError, "boom")
	}))
	defer server.Close()

	// build an instance whose runtime-instance ID drives the first GET
	inst := &v0.KubernetesWorkloadInstance{
		Common:                      v0.Common{ID: util.Ptr(uint(1))},
		KubernetesRuntimeInstanceID: util.Ptr(uint(3)),
	}

	// invoke reconciler against the failing stub
	r := newTestReconciler(server)
	requeue, err := v0KubernetesWorkloadInstanceUpdated(r, inst, newTestLogger())

	// assert the outer runtime-instance wrap prefix and zero requeue
	if err == nil {
		t.Fatalf("expected error from api failure")
	}
	if !strings.HasPrefix(err.Error(), "failed to get kubernetes runtime instance by ID") {
		t.Fatalf("expected runtime-instance wrap prefix, got %q", err.Error())
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0KubernetesWorkloadInstanceDeleted covers the three delete branches:
// missing DeletionScheduled surfaces an error; a scheduled deletion whose
// resource-instance list is empty returns cleanly; and a deletion already
// confirmed returns cleanly without touching the API.
func TestV0KubernetesWorkloadInstanceDeleted(t *testing.T) {
	now := time.Now()

	// stub API returns an empty resource-instance list so the caller returns before dialing kube
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		writeResponse(t, w, http.StatusOK, []apiserver_lib.Object{})
	}))
	defer server.Close()

	cases := []struct {
		name    string
		inst    *v0.KubernetesWorkloadInstance
		wantErr string
	}{
		{
			// missing DeletionScheduled aborts before touching the API
			name: "missing schedule returns error",
			inst: &v0.KubernetesWorkloadInstance{
				Common:         v0.Common{ID: util.Ptr(uint(1))},
				Reconciliation: v0.Reconciliation{DeletionScheduled: nil},
			},
			wantErr: "deletion notification receieved but not scheduled",
		},
		{
			// scheduled deletion drives the GET path; empty list ends cleanly
			name: "scheduled deletion with empty resources returns cleanly",
			inst: &v0.KubernetesWorkloadInstance{
				Common:         v0.Common{ID: util.Ptr(uint(1))},
				Reconciliation: v0.Reconciliation{DeletionScheduled: &now},
			},
		},
		{
			// already-confirmed deletion short-circuits before any API traffic
			name: "already-confirmed deletion returns cleanly",
			inst: &v0.KubernetesWorkloadInstance{
				Common: v0.Common{ID: util.Ptr(uint(1))},
				Reconciliation: v0.Reconciliation{
					DeletionScheduled: &now,
					DeletionConfirmed: &now,
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke reconciler; the missing-schedule case never dials the stub
			r := newTestReconciler(server)
			requeue, err := v0KubernetesWorkloadInstanceDeleted(r, tc.inst, newTestLogger())

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
