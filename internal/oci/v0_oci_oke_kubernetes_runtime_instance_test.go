package oci

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

// okeAPIStub wraps an httptest.Server that impersonates the threeport API for
// the GET-by-id call the create/delete handlers make before deciding to
// short-circuit or advance the state machine.
type okeAPIStub struct {
	server *httptest.Server
	client *http.Client
	addr   string
}

// newOkeAPIStub stands up an http test server and returns a stub configured so
// the threeport client helpers reach it. The addr has "http://" stripped
// because client_lib.GetResponse prepends a scheme itself; the client has a
// nil transport so GetResponse's scheme heuristic falls through to plain
// http, matching the test server.
func newOkeAPIStub(t *testing.T, handler http.Handler) *okeAPIStub {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &okeAPIStub{
		server: srv,
		client: &http.Client{},
		addr:   strings.TrimPrefix(srv.URL, "http://"),
	}
}

// writeOkeInstance serializes an instance in the apiserver_lib.Response
// envelope the threeport client decodes.
func writeOkeInstance(t *testing.T, w http.ResponseWriter, status int, inst v0.OciOkeKubernetesRuntimeInstance) {
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

// newMinimalOkeInstance returns an OciOkeKubernetesRuntimeInstance with the
// two fields the handlers dereference at entry (ID and Name) set. Callers
// extend it further for state-specific assertions.
func newMinimalOkeInstance(id uint, name string) *v0.OciOkeKubernetesRuntimeInstance {
	return &v0.OciOkeKubernetesRuntimeInstance{
		Common:   v0.Common{ID: util.Ptr(id)},
		Instance: v0.Instance{Name: util.Ptr(name)},
	}
}

// TestV0OciOkeKubernetesRuntimeInstanceUpdated_ReturnsZeroNoError asserts the
// Updated handler is a no-op that returns (0, nil): the reconciliation
// contract for OKE runtime instances has no update-time work.
func TestV0OciOkeKubernetesRuntimeInstanceUpdated_ReturnsZeroNoError(t *testing.T) {
	// use an unconfigured Reconciler; the handler must not touch it
	log := logr.Discard()
	inst := newMinimalOkeInstance(1, "oke-updated")

	// invoke the update handler
	delay, err := v0OciOkeKubernetesRuntimeInstanceUpdated(&controller.Reconciler{}, inst, &log)

	// assert (0, nil) contract: no requeue, no error
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// TestV0OciOkeKubernetesRuntimeInstanceCreated_AlreadyConfirmed drives the
// Created handler against a stub API whose GET returns an instance with
// CreationConfirmed set. HandleInfraCreate must short-circuit to (0, nil)
// without hitting BuildInfra or the notification path.
func TestV0OciOkeKubernetesRuntimeInstanceCreated_AlreadyConfirmed(t *testing.T) {
	// stub the GET-by-id endpoint to return an already-confirmed instance
	now := time.Now().UTC()
	confirmed := newMinimalOkeInstance(42, "oke-confirmed")
	confirmed.CreationConfirmed = util.Ptr(now)

	var getCalls int
	mux := http.NewServeMux()
	path := fmt.Sprintf("%s/42", v0.PathOciOkeKubernetesRuntimeInstances)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		getCalls++
		writeOkeInstance(t, w, http.StatusOK, *confirmed)
	})
	stub := newOkeAPIStub(t, mux)

	log := logr.Discard()
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}

	// invoke the create handler on the caller-supplied minimal instance
	delay, err := v0OciOkeKubernetesRuntimeInstanceCreated(r, newMinimalOkeInstance(42, "oke-confirmed"), &log)

	// assert short-circuit: no error, no requeue, exactly one API GET
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Equal(t, 1, getCalls, "already-confirmed create should GET once and return")
}

// TestV0OciOkeKubernetesRuntimeInstanceCreated_GetReconciliationError covers
// the failure path for the initial state fetch: an API 500 must surface as
// a wrapped error and no requeue.
func TestV0OciOkeKubernetesRuntimeInstanceCreated_GetReconciliationError(t *testing.T) {
	// stub the GET-by-id endpoint to return a 500 error envelope
	mux := http.NewServeMux()
	path := fmt.Sprintf("%s/7", v0.PathOciOkeKubernetesRuntimeInstances)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		writeErrorStatus(t, w, http.StatusInternalServerError, "boom")
	})
	stub := newOkeAPIStub(t, mux)

	log := logr.Discard()
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}

	// invoke the create handler and expect the API error to bubble up
	delay, err := v0OciOkeKubernetesRuntimeInstanceCreated(r, newMinimalOkeInstance(7, "oke-err"), &log)

	// assert the error path returns delay=0 and a wrapped fetch error
	require.Error(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Contains(t, err.Error(), "failed to get reconciliation state")
}

// TestV0OciOkeKubernetesRuntimeInstanceDeleted_NotScheduled asserts the Deleted
// handler rejects a delete-notification for an instance that has no
// DeletionScheduled set: the state machine treats it as a bug and returns an
// error without side effects.
func TestV0OciOkeKubernetesRuntimeInstanceDeleted_NotScheduled(t *testing.T) {
	// stub GET to return an instance with DeletionScheduled left nil
	mux := http.NewServeMux()
	path := fmt.Sprintf("%s/9", v0.PathOciOkeKubernetesRuntimeInstances)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		writeOkeInstance(t, w, http.StatusOK, *newMinimalOkeInstance(9, "oke-not-scheduled"))
	})
	stub := newOkeAPIStub(t, mux)

	log := logr.Discard()
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}

	// invoke the delete handler on an instance whose delete was never scheduled
	delay, err := v0OciOkeKubernetesRuntimeInstanceDeleted(r, newMinimalOkeInstance(9, "oke-not-scheduled"), &log)

	// assert the explicit sentinel error surfaces and no requeue is requested
	require.Error(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Contains(t, err.Error(), "deletion notification received but not scheduled")
}

// TestV0OciOkeKubernetesRuntimeInstanceDeleted_AlreadyConfirmed asserts the
// Deleted handler short-circuits when the delete has already been confirmed
// upstream. The state machine returns (0, nil) after a single API GET.
func TestV0OciOkeKubernetesRuntimeInstanceDeleted_AlreadyConfirmed(t *testing.T) {
	// stub GET to return an instance whose delete is scheduled AND confirmed
	now := time.Now().UTC()
	confirmed := newMinimalOkeInstance(11, "oke-del-confirmed")
	confirmed.DeletionScheduled = util.Ptr(now)
	confirmed.DeletionConfirmed = util.Ptr(now)

	var getCalls int
	mux := http.NewServeMux()
	path := fmt.Sprintf("%s/11", v0.PathOciOkeKubernetesRuntimeInstances)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		getCalls++
		writeOkeInstance(t, w, http.StatusOK, *confirmed)
	})
	stub := newOkeAPIStub(t, mux)

	log := logr.Discard()
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}

	// invoke the delete handler and expect early exit
	delay, err := v0OciOkeKubernetesRuntimeInstanceDeleted(r, newMinimalOkeInstance(11, "oke-del-confirmed"), &log)

	// assert clean short-circuit with exactly one API GET
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Equal(t, 1, getCalls, "already-confirmed delete should GET once and return")
}

// TestV0OciOkeKubernetesRuntimeInstanceDeleted_GetReconciliationError covers
// the failure path for the initial state fetch on delete: an API 500 must
// surface as a wrapped error with no requeue.
func TestV0OciOkeKubernetesRuntimeInstanceDeleted_GetReconciliationError(t *testing.T) {
	// stub the GET-by-id endpoint to return a 500 error envelope
	mux := http.NewServeMux()
	path := fmt.Sprintf("%s/5", v0.PathOciOkeKubernetesRuntimeInstances)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		writeErrorStatus(t, w, http.StatusInternalServerError, "kaboom")
	})
	stub := newOkeAPIStub(t, mux)

	log := logr.Discard()
	r := &controller.Reconciler{APIClient: stub.client, APIServer: stub.addr}

	// invoke the delete handler and expect the API error to bubble up
	delay, err := v0OciOkeKubernetesRuntimeInstanceDeleted(r, newMinimalOkeInstance(5, "oke-del-err"), &log)

	// assert the error path returns delay=0 and a wrapped fetch error
	require.Error(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Contains(t, err.Error(), "failed to get reconciliation state")
}
