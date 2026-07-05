package gcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	logr "github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// gkeAPIStub wraps an httptest.Server that impersonates the threeport API for
// the exactly-one endpoint the create/delete handlers hit before deciding to
// short-circuit: the GET-by-id call for GcpGkeKubernetesRuntimeInstances.
type gkeAPIStub struct {
	server *httptest.Server
	client *http.Client
	addr   string
}

// newGkeAPIStub stands up an http test server and returns a stub configured
// so the threeport client helpers reach it. The addr has "http://" stripped
// because client_lib.GetResponse prepends a scheme itself; the client has a
// nil transport so GetResponse's scheme heuristic falls through to plain
// http, matching the test server.
func newGkeAPIStub(t *testing.T, handler http.Handler) *gkeAPIStub {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &gkeAPIStub{
		server: srv,
		client: &http.Client{},
		addr:   strings.TrimPrefix(srv.URL, "http://"),
	}
}

// writeGkeInstance serializes an instance in the apiserver_lib.Response
// envelope the threeport client decodes.
func writeGkeInstance(t *testing.T, w http.ResponseWriter, status int, inst v0.GcpGkeKubernetesRuntimeInstance) {
	t.Helper()
	body, err := json.Marshal(apiserver_lib.Response{Data: []apiserver_lib.Object{inst}})
	require.NoError(t, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeErrorStatus emits an apiserver_lib.Response with a non-2xx status,
// forcing the client's error-decoding branch.
func writeErrorStatus(t *testing.T, w http.ResponseWriter, code int, msg string) {
	t.Helper()
	body, err := json.Marshal(apiserver_lib.Response{
		Status: apiserver_lib.Status{Code: code, Message: http.StatusText(code), Error: msg},
	})
	require.NoError(t, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

// newMinimalInstance returns a GcpGkeKubernetesRuntimeInstance with the two
// fields the handlers dereference at entry (ID and Name) set. Callers extend
// it further for reconciliation-state-specific assertions.
func newMinimalInstance(id uint, name string) *v0.GcpGkeKubernetesRuntimeInstance {
	return &v0.GcpGkeKubernetesRuntimeInstance{
		Common:   v0.Common{ID: util.Ptr(id)},
		Instance: v0.Instance{Name: util.Ptr(name)},
	}
}

// TestV0GcpGkeKubernetesRuntimeInstanceUpdated_ReturnsZeroNoError asserts the
// Updated handler is a no-op that returns (0, nil): the reconciliation
// contract for GKE runtime instances has no update-time work.
func TestV0GcpGkeKubernetesRuntimeInstanceUpdated_ReturnsZeroNoError(t *testing.T) {
	// use an unconfigured Reconciler; the handler must not touch it
	log := logr.Discard()
	inst := newMinimalInstance(1, "gke-updated")

	// invoke the update handler
	delay, err := v0GcpGkeKubernetesRuntimeInstanceUpdated(&controller.Reconciler{}, inst, &log)

	// assert (0, nil) contract: no requeue, no error
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// TestV0GcpGkeKubernetesRuntimeInstanceCreated_AlreadyConfirmed drives the
// Created handler against a stub API whose GET returns an instance with
// CreationConfirmed set. HandleInfraCreate must short-circuit to (0, nil)
// without hitting BuildInfra or the notification path.
func TestV0GcpGkeKubernetesRuntimeInstanceCreated_AlreadyConfirmed(t *testing.T) {
	// stub the GET-by-id endpoint to return an already-confirmed instance
	now := time.Now().UTC()
	confirmed := newMinimalInstance(42, "gke-confirmed")
	confirmed.CreationConfirmed = util.Ptr(now)

	var getCalls int
	mux := http.NewServeMux()
	path := fmt.Sprintf("%s/42", v0.PathGcpGkeKubernetesRuntimeInstances)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		getCalls++
		writeGkeInstance(t, w, http.StatusOK, *confirmed)
	})
	stub := newGkeAPIStub(t, mux)

	log := logr.Discard()
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}

	// invoke the create handler on the caller-supplied minimal instance
	delay, err := v0GcpGkeKubernetesRuntimeInstanceCreated(r, newMinimalInstance(42, "gke-confirmed"), &log)

	// assert short-circuit: no error, no requeue, exactly one API GET
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Equal(t, 1, getCalls, "already-confirmed create should GET once and return")
}

// TestV0GcpGkeKubernetesRuntimeInstanceCreated_GetReconciliationError covers
// the failure path for the initial state fetch: an API 500 must surface as
// a wrapped error and no requeue.
func TestV0GcpGkeKubernetesRuntimeInstanceCreated_GetReconciliationError(t *testing.T) {
	// stub the GET-by-id endpoint to return a 500 error envelope
	mux := http.NewServeMux()
	path := fmt.Sprintf("%s/7", v0.PathGcpGkeKubernetesRuntimeInstances)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		writeErrorStatus(t, w, http.StatusInternalServerError, "boom")
	})
	stub := newGkeAPIStub(t, mux)

	log := logr.Discard()
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}

	// invoke the create handler and expect the API error to bubble up
	delay, err := v0GcpGkeKubernetesRuntimeInstanceCreated(r, newMinimalInstance(7, "gke-err"), &log)

	// assert the error path returns delay=0 and a wrapped fetch error
	require.Error(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Contains(t, err.Error(), "failed to get reconciliation state")
}

// TestV0GcpGkeKubernetesRuntimeInstanceDeleted_NotScheduled asserts the Deleted
// handler rejects a delete-notification for an instance that has no
// DeletionScheduled set: the state machine treats it as a bug and returns
// an error without side effects.
func TestV0GcpGkeKubernetesRuntimeInstanceDeleted_NotScheduled(t *testing.T) {
	// stub GET to return an instance with DeletionScheduled left nil
	mux := http.NewServeMux()
	path := fmt.Sprintf("%s/9", v0.PathGcpGkeKubernetesRuntimeInstances)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		writeGkeInstance(t, w, http.StatusOK, *newMinimalInstance(9, "gke-not-scheduled"))
	})
	stub := newGkeAPIStub(t, mux)

	log := logr.Discard()
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}

	// invoke the delete handler on an instance whose delete was never scheduled
	delay, err := v0GcpGkeKubernetesRuntimeInstanceDeleted(r, newMinimalInstance(9, "gke-not-scheduled"), &log)

	// assert the explicit sentinel error surfaces and no requeue is requested
	require.Error(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Contains(t, err.Error(), "deletion notification received but not scheduled")
}

// TestV0GcpGkeKubernetesRuntimeInstanceDeleted_AlreadyConfirmed asserts the
// Deleted handler short-circuits when the delete has already been confirmed
// upstream. The state machine returns (0, nil) after a single API GET.
func TestV0GcpGkeKubernetesRuntimeInstanceDeleted_AlreadyConfirmed(t *testing.T) {
	// stub GET to return an instance whose delete is scheduled AND confirmed
	now := time.Now().UTC()
	confirmed := newMinimalInstance(11, "gke-del-confirmed")
	confirmed.DeletionScheduled = util.Ptr(now)
	confirmed.DeletionConfirmed = util.Ptr(now)

	var getCalls int
	mux := http.NewServeMux()
	path := fmt.Sprintf("%s/11", v0.PathGcpGkeKubernetesRuntimeInstances)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		getCalls++
		writeGkeInstance(t, w, http.StatusOK, *confirmed)
	})
	stub := newGkeAPIStub(t, mux)

	log := logr.Discard()
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}

	// invoke the delete handler and expect early exit
	delay, err := v0GcpGkeKubernetesRuntimeInstanceDeleted(r, newMinimalInstance(11, "gke-del-confirmed"), &log)

	// assert clean short-circuit with exactly one API GET
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Equal(t, 1, getCalls, "already-confirmed delete should GET once and return")
}

// TestV0GcpGkeKubernetesRuntimeInstanceDeleted_GetReconciliationError covers
// the failure path for the initial state fetch on delete: an API 500 must
// surface as a wrapped error with no requeue.
func TestV0GcpGkeKubernetesRuntimeInstanceDeleted_GetReconciliationError(t *testing.T) {
	// stub the GET-by-id endpoint to return a 500 error envelope
	mux := http.NewServeMux()
	path := fmt.Sprintf("%s/5", v0.PathGcpGkeKubernetesRuntimeInstances)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		writeErrorStatus(t, w, http.StatusInternalServerError, "kaboom")
	})
	stub := newGkeAPIStub(t, mux)

	log := logr.Discard()
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}

	// invoke the delete handler and expect the API error to bubble up
	delay, err := v0GcpGkeKubernetesRuntimeInstanceDeleted(r, newMinimalInstance(5, "gke-del-err"), &log)

	// assert the error path returns delay=0 and a wrapped fetch error
	require.Error(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Contains(t, err.Error(), "failed to get reconciliation state")
}
