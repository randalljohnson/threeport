package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// newTestReconciler builds a *controller.Reconciler whose APIClient targets
// the given httptest server. The base URL is stripped of its scheme because
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

// newErrorServer returns a stub API that responds to every request with the
// given status and a valid apiserver Response envelope carrying an error.
func newErrorServer(status int, msg string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := apiserver_lib.Response{Status: apiserver_lib.Status{Error: msg}}
		body, _ := json.Marshal(resp)
		w.WriteHeader(status)
		w.Write(body)
	}))
}

// newJSONResponse writes a StatusOK apiserver Response envelope carrying obj
// as Data[0].
func newJSONResponse(t *testing.T, w http.ResponseWriter, status int, obj interface{}) {
	t.Helper()
	resp := apiserver_lib.Response{Data: []apiserver_lib.Object{obj}}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}
	w.WriteHeader(status)
	w.Write(body)
}

// TestV0ControlPlaneInstanceUpdated_NoOp asserts the update reconciler is a
// stub returning zero requeue and nil error regardless of input state.
func TestV0ControlPlaneInstanceUpdated_NoOp(t *testing.T) {
	// nil reconciler and nil instance are safe because the function does not read them
	requeue, err := v0ControlPlaneInstanceUpdated(nil, nil, newDefinitionTestLogger())

	// assert no error and zero requeue delay
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0ControlPlaneInstanceCreated_AcknowledgeUpdateFails covers the branch
// where the incoming instance has no CreationAcknowledged timestamp and the
// initial UpdateControlPlaneInstance call fails.
func TestV0ControlPlaneInstanceCreated_AcknowledgeUpdateFails(t *testing.T) {
	// stand up a stub API that returns 500 on the PATCH update
	server := newErrorServer(http.StatusInternalServerError, "boom")
	defer server.Close()

	// build an instance that has never been acknowledged
	inst := &v0.ControlPlaneInstance{
		Common: v0.Common{ID: util.Ptr(uint(1))},
	}

	// invoke reconciler; the initial acknowledgement update should surface a wrap error
	requeue, err := v0ControlPlaneInstanceCreated(newTestReconciler(server), inst, newDefinitionTestLogger())

	// assert error wrap prefix and zero requeue
	if err == nil {
		t.Fatalf("expected error from update failure")
	}
	if !strings.HasPrefix(err.Error(), "failed to confirm creation of control plane instance in threeport API") {
		t.Fatalf("expected acknowledgement wrap prefix, got %q", err.Error())
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0ControlPlaneInstanceCreated_GetDefinitionFails covers the branch where
// creation is already acknowledged and the follow-up GetControlPlaneDefinitionByID
// call fails.
func TestV0ControlPlaneInstanceCreated_GetDefinitionFails(t *testing.T) {
	// stand up a stub API that fails every request; the first call after skipping
	// the acknowledgement branch is the definition fetch
	server := newErrorServer(http.StatusInternalServerError, "boom")
	defer server.Close()

	// build an instance whose creation is already acknowledged so the update branch is skipped
	now := time.Now().UTC()
	inst := &v0.ControlPlaneInstance{
		Common: v0.Common{ID: util.Ptr(uint(1))},
		Reconciliation: v0.Reconciliation{
			CreationAcknowledged: &now,
		},
		ControlPlaneDefinitionID:    util.Ptr(uint(2)),
		KubernetesRuntimeInstanceID: util.Ptr(uint(3)),
	}

	// invoke reconciler; the definition fetch should surface a wrap error
	requeue, err := v0ControlPlaneInstanceCreated(newTestReconciler(server), inst, newDefinitionTestLogger())

	// assert error wrap prefix and zero requeue
	if err == nil {
		t.Fatalf("expected error from definition fetch failure")
	}
	if !strings.HasPrefix(err.Error(), "failed to get control plane definition by ID") {
		t.Fatalf("expected definition wrap prefix, got %q", err.Error())
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0ControlPlaneInstanceCreated_DefinitionNotReconciled covers the branch
// where the fetched definition is present but not yet reconciled, surfacing
// the sentinel "controlplane definition not reconciled" error.
func TestV0ControlPlaneInstanceCreated_DefinitionNotReconciled(t *testing.T) {
	// stand up a stub API that returns a definition with Reconciled=false on the GET
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, v0.PathControlPlaneDefinitions) {
			def := v0.ControlPlaneDefinition{
				Reconciliation: v0.Reconciliation{Reconciled: util.Ptr(false)},
			}
			newJSONResponse(t, w, http.StatusOK, def)
			return
		}
		t.Errorf("unexpected request to %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// build an acknowledged instance so the reconciler moves past the update branch
	now := time.Now().UTC()
	inst := &v0.ControlPlaneInstance{
		Common:                      v0.Common{ID: util.Ptr(uint(1))},
		Reconciliation:              v0.Reconciliation{CreationAcknowledged: &now},
		ControlPlaneDefinitionID:    util.Ptr(uint(2)),
		KubernetesRuntimeInstanceID: util.Ptr(uint(3)),
	}

	// invoke reconciler; the not-reconciled definition should surface a sentinel error
	requeue, err := v0ControlPlaneInstanceCreated(newTestReconciler(server), inst, newDefinitionTestLogger())

	// assert exact sentinel error text and zero requeue
	if err == nil || err.Error() != "controlplane definition not reconciled" {
		t.Fatalf("expected 'controlplane definition not reconciled', got %v", err)
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0ControlPlaneInstanceCreated_GetRuntimeInstanceFails covers the branch
// where the definition is reconciled but the follow-up runtime-instance fetch
// fails.
func TestV0ControlPlaneInstanceCreated_GetRuntimeInstanceFails(t *testing.T) {
	// stand up a stub API that returns a reconciled definition, then 500s on the runtime-instance fetch
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, v0.PathControlPlaneDefinitions):
			def := v0.ControlPlaneDefinition{
				Reconciliation: v0.Reconciliation{Reconciled: util.Ptr(true)},
			}
			newJSONResponse(t, w, http.StatusOK, def)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, v0.PathKubernetesRuntimeInstances):
			resp := apiserver_lib.Response{Status: apiserver_lib.Status{Error: "boom"}}
			body, _ := json.Marshal(resp)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write(body)
		default:
			t.Errorf("unexpected request to %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	// build an acknowledged instance so the reconciler moves past the update branch
	now := time.Now().UTC()
	inst := &v0.ControlPlaneInstance{
		Common:                      v0.Common{ID: util.Ptr(uint(1))},
		Reconciliation:              v0.Reconciliation{CreationAcknowledged: &now},
		ControlPlaneDefinitionID:    util.Ptr(uint(2)),
		KubernetesRuntimeInstanceID: util.Ptr(uint(3)),
	}

	// invoke reconciler; the runtime instance fetch should surface a wrap error
	requeue, err := v0ControlPlaneInstanceCreated(newTestReconciler(server), inst, newDefinitionTestLogger())

	// assert error wrap prefix and zero requeue
	if err == nil {
		t.Fatalf("expected error from runtime instance fetch failure")
	}
	if !strings.HasPrefix(err.Error(), "failed to get control plane kubernetesRuntime instance by ID") {
		t.Fatalf("expected runtime instance wrap prefix, got %q", err.Error())
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0ControlPlaneInstanceCreated_RuntimeInstanceNotReconciled covers the
// branch where the runtime instance comes back with Reconciled=false and the
// sentinel error surfaces.
func TestV0ControlPlaneInstanceCreated_RuntimeInstanceNotReconciled(t *testing.T) {
	// stand up a stub API that returns a reconciled definition and a not-reconciled runtime instance
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, v0.PathControlPlaneDefinitions):
			def := v0.ControlPlaneDefinition{
				Reconciliation: v0.Reconciliation{Reconciled: util.Ptr(true)},
			}
			newJSONResponse(t, w, http.StatusOK, def)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, v0.PathKubernetesRuntimeInstances):
			ri := v0.KubernetesRuntimeInstance{
				Reconciliation: v0.Reconciliation{Reconciled: util.Ptr(false)},
			}
			newJSONResponse(t, w, http.StatusOK, ri)
		default:
			t.Errorf("unexpected request to %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	// build an acknowledged instance so the reconciler moves past the update branch
	now := time.Now().UTC()
	inst := &v0.ControlPlaneInstance{
		Common:                      v0.Common{ID: util.Ptr(uint(1))},
		Reconciliation:              v0.Reconciliation{CreationAcknowledged: &now},
		ControlPlaneDefinitionID:    util.Ptr(uint(2)),
		KubernetesRuntimeInstanceID: util.Ptr(uint(3)),
	}

	// invoke reconciler; the not-reconciled runtime instance should surface a sentinel error
	requeue, err := v0ControlPlaneInstanceCreated(newTestReconciler(server), inst, newDefinitionTestLogger())

	// assert exact sentinel error text and zero requeue
	if err == nil || err.Error() != "kubernetes runtime instance not reconciled" {
		t.Fatalf("expected 'kubernetes runtime instance not reconciled', got %v", err)
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0ControlPlaneInstanceCreated_GetRuntimeDefinitionFails covers the branch
// where the runtime instance is reconciled and the follow-up runtime-definition
// fetch fails.
func TestV0ControlPlaneInstanceCreated_GetRuntimeDefinitionFails(t *testing.T) {
	// stand up a stub API that returns a reconciled definition and runtime instance,
	// then 500s on the runtime-definition fetch
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, v0.PathControlPlaneDefinitions):
			def := v0.ControlPlaneDefinition{
				Reconciliation: v0.Reconciliation{Reconciled: util.Ptr(true)},
			}
			newJSONResponse(t, w, http.StatusOK, def)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, v0.PathKubernetesRuntimeInstances):
			ri := v0.KubernetesRuntimeInstance{
				Common:                        v0.Common{ID: util.Ptr(uint(3))},
				Reconciliation:                v0.Reconciliation{Reconciled: util.Ptr(true)},
				KubernetesRuntimeDefinitionID: util.Ptr(uint(9)),
			}
			newJSONResponse(t, w, http.StatusOK, ri)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, v0.PathKubernetesRuntimeDefinitions):
			resp := apiserver_lib.Response{Status: apiserver_lib.Status{Error: "boom"}}
			body, _ := json.Marshal(resp)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write(body)
		default:
			t.Errorf("unexpected request to %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	// build an acknowledged instance so the reconciler moves past the update branch
	now := time.Now().UTC()
	inst := &v0.ControlPlaneInstance{
		Common:                      v0.Common{ID: util.Ptr(uint(1))},
		Reconciliation:              v0.Reconciliation{CreationAcknowledged: &now},
		ControlPlaneDefinitionID:    util.Ptr(uint(2)),
		KubernetesRuntimeInstanceID: util.Ptr(uint(3)),
	}

	// invoke reconciler; the runtime definition fetch should surface a wrap error
	requeue, err := v0ControlPlaneInstanceCreated(newTestReconciler(server), inst, newDefinitionTestLogger())

	// assert error wrap prefix and zero requeue
	if err == nil {
		t.Fatalf("expected error from runtime definition fetch failure")
	}
	if !strings.HasPrefix(err.Error(), "failed to get control plane kubernetesRuntime definition by ID") {
		t.Fatalf("expected runtime definition wrap prefix, got %q", err.Error())
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0ControlPlaneInstanceCreated_GetSelfInstanceFails covers the branch
// where the runtime definition is retrieved and the follow-up self-instance
// lookup fails.
func TestV0ControlPlaneInstanceCreated_GetSelfInstanceFails(t *testing.T) {
	// stand up a stub API that satisfies every prior fetch, then 500s on the self-instance query
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, v0.PathControlPlaneDefinitions):
			def := v0.ControlPlaneDefinition{
				Reconciliation: v0.Reconciliation{Reconciled: util.Ptr(true)},
			}
			newJSONResponse(t, w, http.StatusOK, def)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, v0.PathKubernetesRuntimeInstances):
			ri := v0.KubernetesRuntimeInstance{
				Common:                        v0.Common{ID: util.Ptr(uint(3))},
				Reconciliation:                v0.Reconciliation{Reconciled: util.Ptr(true)},
				KubernetesRuntimeDefinitionID: util.Ptr(uint(9)),
			}
			newJSONResponse(t, w, http.StatusOK, ri)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, v0.PathKubernetesRuntimeDefinitions):
			kd := v0.KubernetesRuntimeDefinition{
				Common:        v0.Common{ID: util.Ptr(uint(9))},
				InfraProvider: util.Ptr(v0.KubernetesRuntimeInfraProviderKind),
			}
			newJSONResponse(t, w, http.StatusOK, kd)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, v0.PathControlPlaneInstances):
			resp := apiserver_lib.Response{Status: apiserver_lib.Status{Error: "boom"}}
			body, _ := json.Marshal(resp)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write(body)
		default:
			t.Errorf("unexpected request to %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	// build an acknowledged instance so the reconciler moves past the update branch
	now := time.Now().UTC()
	inst := &v0.ControlPlaneInstance{
		Common:                      v0.Common{ID: util.Ptr(uint(1))},
		Reconciliation:              v0.Reconciliation{CreationAcknowledged: &now},
		ControlPlaneDefinitionID:    util.Ptr(uint(2)),
		KubernetesRuntimeInstanceID: util.Ptr(uint(3)),
	}

	// invoke reconciler; the self-instance lookup should surface a wrap error
	requeue, err := v0ControlPlaneInstanceCreated(newTestReconciler(server), inst, newDefinitionTestLogger())

	// assert error wrap prefix and zero requeue
	if err == nil {
		t.Fatalf("expected error from self instance fetch failure")
	}
	if !strings.HasPrefix(err.Error(), "failed to get self control plane instance") {
		t.Fatalf("expected self instance wrap prefix, got %q", err.Error())
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0ControlPlaneInstanceDeleted_GetRuntimeInstanceFails covers the delete
// path's first error branch: a failed runtime-instance fetch surfaces a wrap
// error.
func TestV0ControlPlaneInstanceDeleted_GetRuntimeInstanceFails(t *testing.T) {
	// stand up a stub API that returns 500 on the initial runtime-instance fetch
	server := newErrorServer(http.StatusInternalServerError, "boom")
	defer server.Close()

	// build an instance carrying just enough to reach the runtime-instance fetch
	inst := &v0.ControlPlaneInstance{
		Common:                      v0.Common{ID: util.Ptr(uint(1))},
		KubernetesRuntimeInstanceID: util.Ptr(uint(3)),
	}

	// invoke deleted reconciler; the runtime instance fetch should surface a wrap error
	requeue, err := v0ControlPlaneInstanceDeleted(newTestReconciler(server), inst, newDefinitionTestLogger())

	// assert error wrap prefix and zero requeue
	if err == nil {
		t.Fatalf("expected error from runtime instance fetch failure")
	}
	if !strings.HasPrefix(err.Error(), "failed to get control plane kubernetesRuntime instance by ID") {
		t.Fatalf("expected runtime instance wrap prefix, got %q", err.Error())
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}

// TestV0ControlPlaneInstanceDeleted_GetRuntimeDefinitionFails covers the
// delete path's second error branch: the runtime instance is retrieved but
// the follow-up definition fetch fails.
func TestV0ControlPlaneInstanceDeleted_GetRuntimeDefinitionFails(t *testing.T) {
	// stand up a stub API that returns a runtime instance, then 500s on the runtime-definition fetch
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, v0.PathKubernetesRuntimeInstances):
			ri := v0.KubernetesRuntimeInstance{
				Common:                        v0.Common{ID: util.Ptr(uint(3))},
				KubernetesRuntimeDefinitionID: util.Ptr(uint(9)),
			}
			newJSONResponse(t, w, http.StatusOK, ri)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, v0.PathKubernetesRuntimeDefinitions):
			resp := apiserver_lib.Response{Status: apiserver_lib.Status{Error: "boom"}}
			body, _ := json.Marshal(resp)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write(body)
		default:
			t.Errorf("unexpected request to %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	// build an instance carrying just enough to reach the runtime-definition fetch
	inst := &v0.ControlPlaneInstance{
		Common:                      v0.Common{ID: util.Ptr(uint(1))},
		KubernetesRuntimeInstanceID: util.Ptr(uint(3)),
	}

	// invoke deleted reconciler; the runtime definition fetch should surface a wrap error
	requeue, err := v0ControlPlaneInstanceDeleted(newTestReconciler(server), inst, newDefinitionTestLogger())

	// assert error wrap prefix and zero requeue
	if err == nil {
		t.Fatalf("expected error from runtime definition fetch failure")
	}
	if !strings.HasPrefix(err.Error(), "failed to get control plane kubernetesRuntime definition by ID") {
		t.Fatalf("expected runtime definition wrap prefix, got %q", err.Error())
	}
	if requeue != 0 {
		t.Fatalf("expected 0 requeue delay, got %d", requeue)
	}
}
