package machineruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	logr "github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/threeport/threeport/internal/machinetest"
	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	tp_errors "github.com/threeport/threeport/pkg/errors/v0"
	event "github.com/threeport/threeport/pkg/event/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestMachineRuntimeInstanceCreated_HappyPath drives a full Created
// reconcile against the in-process SSH server. The MRI has HostKey set to
// the server's actual key (no capture path); GetClient succeeds, Ping
// succeeds, and the reachability signal lands as a log statement. On this
// first-reachability pass the reconciler issues a single PATCH that stamps
// creation_confirmed with Reconciled=true, and emits exactly one
// CreateInProgress lifecycle marker at the top of the run; the wrapper's
// SuccessfulCreate event still records the outcome.
func TestMachineRuntimeInstanceCreated_HappyPath(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	// pin the MRI's HostKey to the server's real key so GetClient
	// verifies rather than captures
	mri := machinetest.MRIFromAddr(t, 42, "mri-happy", addr, "u", "p", key)
	mri.HostKey = util.Ptr(hostKeyBase64(signer))

	// mock the PATCH the reconciler issues to stamp creation_confirmed and
	// record every request body so the test can assert exactly one fires
	api := machinetest.NewAPIStub(t)
	var (
		patches   [][]byte
		patchesMu sync.Mutex
		patchPath = fmt.Sprintf("%s/%d", v0.PathMachineRuntimeInstances, 42)
	)
	api.Mux.HandleFunc(patchPath, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		body, _ := io.ReadAll(r.Body)
		patchesMu.Lock()
		patches = append(patches, body)
		patchesMu.Unlock()
		var updated v0.MachineRuntimeInstance
		require.NoError(t, json.Unmarshal(body, &updated))
		updated.ID = util.Ptr(uint(42))
		machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{updated})
	})

	recorder := machinetest.NewFakeRecorder()
	log := logr.Discard()

	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	// drive the Created reconciler against the running SSH server
	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)

	// success path returns (0, nil): no requeue and no error
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)

	// reconciler emits only the CreateInProgress lifecycle marker on the
	// success path; the wrapper covers the outcome and reachability is a
	// log line, so no HostKeyCaptured or SSHReachable events fire
	assert.Equal(t, []string{event.ReasonCreateInProgress}, recorder.GetReasons(), "reconciler emits only the CreateInProgress lifecycle marker on the success path")

	// a single PATCH stamps creation_confirmed with Reconciled=true; the
	// pinned host key needs no capture, so the body carries no HostKey field
	patchesMu.Lock()
	defer patchesMu.Unlock()
	require.Len(t, patches, 1, "expected exactly one PATCH to stamp creation_confirmed")
	assert.Contains(t, string(patches[0]), "CreationConfirmed", "PATCH body should carry the CreationConfirmed field")
	assert.Contains(t, string(patches[0]), `"Reconciled":true`, "PATCH should set Reconciled=true so the resulting update notification does not retrigger reconciliation")
	assert.NotContains(t, string(patches[0]), "HostKey", "pinned host key needs no capture, so the PATCH should not carry HostKey")
}

// TestMachineRuntimeInstanceCreated_NoHostname_RequeuesWithoutDialing covers
// the deferred-dial path: an instance whose hostname is not yet populated
// requeues with the unpopulated delay, returns no error, dials no SSH server,
// records nothing beyond the CreateInProgress lifecycle marker, and persists
// no update, so Reconciled stays unset until the machine is reachable. Both a
// nil and an empty-string hostname take this path.
func TestMachineRuntimeInstanceCreated_NoHostname_RequeuesWithoutDialing(t *testing.T) {
	overrideUnpopulatedRequeueDelay(t, 3)
	// fail loudly if the deferred-dial guard ever lets a reconcile reach the
	// SSH dial: the connect would build a reconcile context from this factory
	overrideReconcileContext(t, func() (context.Context, context.CancelFunc) {
		t.Fatal("reconcile must not dial ssh when the hostname is unpopulated")
		return context.WithCancel(context.Background())
	})

	key := machinetest.NewEncryptionKey(t)

	cases := []struct {
		name     string
		hostname *string
	}{
		{"nil hostname", nil},
		{"empty hostname", util.Ptr("")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// arrange an instance whose hostname is unpopulated so the
			// reconcile must take the deferred-dial path
			mri := &v0.MachineRuntimeInstance{
				Common:      v0.Common{ID: util.Ptr(uint(101))},
				Instance:    v0.Instance{Name: util.Ptr("mri-unpopulated")},
				SSHPassword: util.Ptr("ignored"),
				Hostname:    tc.hostname,
			}

			// count PATCHes so a persisted update would register as nonzero
			api := machinetest.NewAPIStub(t)
			patchCount := registerPatchCounter(t, api, 101)
			recorder := machinetest.NewFakeRecorder()
			log := logr.Discard()
			r := &controller.Reconciler{
				APIClient:      api.Client,
				APIServer:      api.Addr,
				EncryptionKey:  key,
				EventsRecorder: recorder,
			}

			// run the Created hook against the unpopulated instance
			delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)
			// verify the deferred-dial path returns cleanly rather than failing
			require.NoError(t, err, "an unpopulated instance must requeue without erroring")
			// verify it requeues on the unpopulated delay, not the ssh-retry delay
			assert.Equal(t, int64(3), delay, "an unpopulated instance requeues with the unpopulated delay")
			// verify only the lifecycle marker fires; nothing may claim
			// reachability before the machine has been dialed
			assert.Equal(t, []string{event.ReasonCreateInProgress}, recorder.GetReasons(), "no reachability event may be recorded before the machine is reachable")
			// verify nothing is persisted so Reconciled stays unset until reachable
			assert.Equal(t, int64(0), atomic.LoadInt64(patchCount), "no update may be persisted, so Reconciled stays unset")
		})
	}
}

// TestMachineRuntimeInstanceCreated_HostKeyCaptured covers the first-connect
// path: HostKey is nil, so GetClient captures the server's key and the
// reconciler PATCHes the MRI to persist it with Reconciled=true. The
// captured key and reachability signals land as log statements; the
// reconciler emits exactly one CreateInProgress lifecycle marker and no other
// boot-noise events on the create path.
func TestMachineRuntimeInstanceCreated_HostKeyCaptured(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	mri := machinetest.MRIFromAddr(t, 7, "mri-capture", addr, "u", "p", key)
	mri.HostKey = nil

	api := machinetest.NewAPIStub(t)
	var (
		patches   [][]byte
		patchesMu sync.Mutex
		patchPath = fmt.Sprintf("%s/%d", v0.PathMachineRuntimeInstances, 7)
	)
	api.Mux.HandleFunc(patchPath, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		body, _ := io.ReadAll(r.Body)
		patchesMu.Lock()
		patches = append(patches, body)
		patchesMu.Unlock()
		var updated v0.MachineRuntimeInstance
		require.NoError(t, json.Unmarshal(body, &updated))
		updated.ID = util.Ptr(uint(7))
		machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{updated})
	})

	recorder := machinetest.NewFakeRecorder()
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	// drive the Created reconciler against the running SSH server
	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)

	// success path returns (0, nil): no requeue and no error
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)

	// reconciler emits only the CreateInProgress lifecycle marker on the
	// create path; no HostKeyCaptured or SSHReachable events fire, since
	// the captured key persists via PATCH and both signals land as logs
	assert.Equal(t, []string{event.ReasonCreateInProgress}, recorder.GetReasons(), "reconciler emits only the CreateInProgress lifecycle marker on the create path")

	// PATCH persists the captured host key with Reconciled=true so the
	// resulting update notification does not retrigger reconciliation
	patchesMu.Lock()
	defer patchesMu.Unlock()
	require.Len(t, patches, 1, "expected exactly one PATCH to persist the captured host key")
	assert.Contains(t, string(patches[0]), "HostKey", "PATCH body should carry the HostKey field")
	assert.Contains(t, string(patches[0]), `"Reconciled":true`, "PATCH should set Reconciled=true so the resulting update notification does not retrigger reconciliation")
}

// TestMachineRuntimeInstanceCreated_NetworkError points the MRI at an
// unreachable host and asserts the reconciler returns 30s requeue and a
// carrying ErrWithEvent whose Reason is SSHConnectFailed. The wrapper's
// HandleEventOverride substitutes that event for the generic FailedCreate
// row, so the failure path itself calls RecordEvent only for the
// CreateInProgress lifecycle marker emitted at the top of the run.
func TestMachineRuntimeInstanceCreated_NetworkError(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	// point at 127.0.0.1:1 (reserved, never bound) to force a connection-refused
	// network-class error out of GetClient
	mri := machinetest.MRIFromAddr(t, 9, "mri-unreachable", "127.0.0.1:1", "u", "p", key)

	api := machinetest.NewAPIStub(t)
	recorder := machinetest.NewFakeRecorder()
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	// drive the Created reconciler against the unreachable host
	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)

	// reconciler surfaces the failure with a 30s requeue for retry
	require.Error(t, err)
	assert.Equal(t, int64(30), delay, "network-class errors should be retried after 30s")

	// error carries the specific-reason event the wrapper will substitute
	// for the generic FailedCreate row
	var errWithEvent *tp_errors.ErrWithEvent
	require.ErrorAs(t, err, &errWithEvent, "reconciler should return *tp_errors.ErrWithEvent so the wrapper can substitute the specific reason")
	require.NotNil(t, errWithEvent.Event.Reason)
	assert.Equal(t, "SSHConnectFailed", *errWithEvent.Event.Reason)

	// failure path defers the failure event to the wrapper, so the only
	// direct RecordEvent call is the CreateInProgress lifecycle marker at the
	// top of the run; no SSHConnectFailed event fires here
	assert.Equal(t, []string{event.ReasonCreateInProgress}, recorder.GetReasons(), "failure path should not call RecordEvent directly for the failure; the wrapper substitutes it")
}

// TestMachineRuntimeInstanceCreated_HostKeyMismatch points the MRI at the
// test server but with a HostKey that doesn't match the server's actual
// host key. SSH client errors (including host key mismatch) always retry
// after 30s, since a misconfigured key may be fixed externally without
// changing the object. The failure surfaces as an ErrWithEvent whose Reason
// is SSHConnectFailed, which the wrapper substitutes for the generic
// FailedCreate event; the reconciler itself records only the
// CreateInProgress lifecycle marker emitted at the top of the run.
func TestMachineRuntimeInstanceCreated_HostKeyMismatch(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	serverSigner := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, serverSigner, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	// pin a different host key on the MRI to force a mismatch against the
	// server's actual key
	wrongSigner := machinetest.NewSigner(t)
	mri := machinetest.MRIFromAddr(t, 11, "mri-mismatch", addr, "u", "p", key)
	mri.HostKey = util.Ptr(hostKeyBase64(wrongSigner))

	api := machinetest.NewAPIStub(t)
	recorder := machinetest.NewFakeRecorder()
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	// drive the Created reconciler against the mismatched host key
	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)

	// ssh-client failures always retry after 30s
	require.Error(t, err)
	assert.Equal(t, int64(30), delay, "ssh-client errors always retry")

	// error carries the specific-reason event the wrapper will substitute
	// for the generic FailedCreate row
	var errWithEvent *tp_errors.ErrWithEvent
	require.ErrorAs(t, err, &errWithEvent, "reconciler should return *tp_errors.ErrWithEvent so the wrapper can substitute the specific reason")
	require.NotNil(t, errWithEvent.Event.Reason)
	assert.Equal(t, "SSHConnectFailed", *errWithEvent.Event.Reason)

	// failure path defers the failure event to the wrapper, so the only
	// direct RecordEvent call is the CreateInProgress lifecycle marker at the
	// top of the run; no SSHConnectFailed event fires here
	assert.Equal(t, []string{event.ReasonCreateInProgress}, recorder.GetReasons(), "failure path should not call RecordEvent directly for the failure; the wrapper substitutes it")
}

// overrideRetryDelay shrinks the package retry delay for one test and
// restores it on cleanup. Tests in this package run sequentially, so
// mutating the package var is safe.
func overrideRetryDelay(t *testing.T, seconds int64) {
	t.Helper()
	prev := sshRetryDelaySeconds
	sshRetryDelaySeconds = seconds
	t.Cleanup(func() { sshRetryDelaySeconds = prev })
}

// overrideUnpopulatedRequeueDelay sets the unpopulated-instance requeue
// delay for one test and restores it on cleanup.
func overrideUnpopulatedRequeueDelay(t *testing.T, seconds int64) {
	t.Helper()
	prev := unpopulatedRequeueDelaySeconds
	unpopulatedRequeueDelaySeconds = seconds
	t.Cleanup(func() { unpopulatedRequeueDelaySeconds = prev })
}

// overrideSSHTimeout shrinks the package SSH operation timeout for one test
// and restores it on cleanup.
func overrideSSHTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := sshOperationTimeout
	sshOperationTimeout = d
	t.Cleanup(func() { sshOperationTimeout = prev })
}

// overrideReconcileContext swaps the package reconcile-context factory for
// one test and restores it on cleanup.
func overrideReconcileContext(t *testing.T, fn func() (context.Context, context.CancelFunc)) {
	t.Helper()
	prev := newReconcileContext
	newReconcileContext = fn
	t.Cleanup(func() { newReconcileContext = prev })
}

// registerPatchCounter registers a PATCH handler for the MRI with the given
// id that counts calls and replies with a valid envelope, so tests can
// assert exactly how many updates the reconciler persisted.
func registerPatchCounter(t *testing.T, api *machinetest.APIStub, id uint) *int64 {
	t.Helper()
	var count int64
	api.Mux.HandleFunc(fmt.Sprintf("%s/%d", v0.PathMachineRuntimeInstances, id), func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		atomic.AddInt64(&count, 1)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var updated v0.MachineRuntimeInstance
		require.NoError(t, json.Unmarshal(body, &updated))
		updated.ID = util.Ptr(id)
		machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{updated})
	})
	return &count
}

// registerMachineRuntimeDefinition registers a GET handler for the machine
// runtime definition with the given id that answers with a GCE-provisioned
// definition, so a reconcile that loads the parent to name the married
// provider kind finds one instead of a missing object.
func registerMachineRuntimeDefinition(t *testing.T, api *machinetest.APIStub, id uint) {
	t.Helper()
	api.Mux.HandleFunc(fmt.Sprintf("%s/%d", v0.PathMachineRuntimeDefinitions, id), func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{
			&v0.MachineRuntimeDefinition{
				Common:        v0.Common{ID: util.Ptr(id)},
				Definition:    v0.Definition{Name: util.Ptr("mrd-married")},
				InfraProvider: util.Ptr(v0.MachineRuntimeInfraProviderGCE),
			},
		})
	})
}

// TestMachineRuntimeInstanceCreated_IdempotentOnDoubleCall asserts a
// re-reconcile of an instance whose first-reachability write already landed
// does not write again. Leg one runs in capture mode with creation_confirmed
// unset, so exactly one PATCH persists the captured host key and the
// confirmation stamp together; leg two carries the server's real host key and
// a stamped creation_confirmed, so GetClient runs in verification mode, the
// write guard finds nothing new to record, and no second PATCH lands.
func TestMachineRuntimeInstanceCreated_IdempotentOnDoubleCall(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	api := machinetest.NewAPIStub(t)
	patchCount := registerPatchCounter(t, api, 21)
	log := logr.Discard()

	// leg one: no host key and no confirmation stamp, so the reachable pass
	// has both to record and issues the single combined PATCH
	first := machinetest.MRIFromAddr(t, 21, "mri-idem", addr, "u", "p", key)
	firstRecorder := machinetest.NewFakeRecorder()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: firstRecorder,
	}

	delay, err := v0MachineRuntimeInstanceCreated(r, first, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Equal(t, []string{event.ReasonCreateInProgress}, firstRecorder.GetReasons())
	require.Equal(t, int64(1), atomic.LoadInt64(patchCount), "first reconcile persists the captured host key and the confirmation stamp with one PATCH")

	// leg two: the state leg one persisted, so the write guard has nothing
	// left to record
	second := machinetest.NewMRIWithInfra(t, 21, "mri-idem", addr, "u", "p", key, machinetest.MRIInfraOpts{
		HostKey: hostKeyBase64(signer),
	})
	second.CreationConfirmed = util.Ptr(time.Now().UTC())
	secondRecorder := machinetest.NewFakeRecorder()
	r.EventsRecorder = secondRecorder

	delay, err = v0MachineRuntimeInstanceCreated(r, second, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Equal(t, []string{event.ReasonCreateInProgress}, secondRecorder.GetReasons())
	assert.Equal(t, int64(1), atomic.LoadInt64(patchCount), "a reconcile with nothing new to record must not PATCH again")
}

// TestMachineRuntimeInstanceCreated_SSHPingFails_Retries drives a connect
// that succeeds and a ping that fails (non-zero exit), asserting the
// configurable retry delay is returned, the error carries the SSHPingFailed
// event the wrapper substitutes for the generic FailedCreate row, and no
// update is persisted.
func TestMachineRuntimeInstanceCreated_SSHPingFails_Retries(t *testing.T) {
	overrideRetryDelay(t, 7)
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 1})
	defer stop()

	mri := machinetest.NewMRIWithInfra(t, 31, "mri-pingfail", addr, "u", "p", key, machinetest.MRIInfraOpts{
		HostKey: hostKeyBase64(signer),
	})

	api := machinetest.NewAPIStub(t)
	patchCount := registerPatchCounter(t, api, 31)
	recorder := machinetest.NewFakeRecorder()
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	// drive the Created reconciler against a host whose ping fails
	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)

	// the first failure on this instance requeues at the base delay, before
	// the backoff has had a chance to double it
	require.Error(t, err)
	assert.Equal(t, int64(7), delay, "the first ping failure requeues with the configurable base delay")

	// error carries the specific-reason event the wrapper will substitute
	// for the generic FailedCreate row
	var errWithEvent *tp_errors.ErrWithEvent
	require.ErrorAs(t, err, &errWithEvent, "reconciler should return *tp_errors.ErrWithEvent so the wrapper can substitute the specific reason")
	require.NotNil(t, errWithEvent.Event.Reason)
	assert.Equal(t, "SSHPingFailed", *errWithEvent.Event.Reason)

	// failure path defers the failure event to the wrapper, and nothing is
	// persisted for a machine that never answered
	assert.Equal(t, []string{event.ReasonCreateInProgress}, recorder.GetReasons(), "failure path should not call RecordEvent directly for the failure; the wrapper substitutes it")
	assert.Equal(t, int64(0), atomic.LoadInt64(patchCount), "no update may be persisted on ping failure")
}

// TestMachineRuntimeInstanceCreated_HostKeyPatchFails_Retries closes the
// API stub before the reconcile so the single first-reachability PATCH,
// which persists the captured host key and stamps creation_confirmed, hits a
// refused connection. A transport-level failure must requeue (non-zero delay,
// non-nil error) so the failed persist is retried rather than the object
// being silently marked reconciled.
func TestMachineRuntimeInstanceCreated_HostKeyPatchFails_Retries(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	mri := machinetest.MRIFromAddr(t, 41, "mri-patchfail", addr, "u", "p", key)

	api := machinetest.NewAPIStub(t)
	api.Server.Close()

	recorder := machinetest.NewFakeRecorder()
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	// drive the Created reconciler with the API unreachable
	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)

	// a refused connection is a network-class error, so the persist retries
	require.Error(t, err)
	assert.Equal(t, int64(30), delay, "transport failures on the first-reachability PATCH requeue after 30s")

	// the only event is the lifecycle marker at the top of the run; a
	// reconcile whose persist failed records nothing about reachability
	assert.Equal(t, []string{event.ReasonCreateInProgress}, recorder.GetReasons(), "a failed persist records nothing beyond the lifecycle marker")
}

// TestMachineRuntimeInstanceCreated_HostKeyPatchHTTP500_TerminalError is
// the sibling of the transport-failure case: an HTTP 500 on the
// first-reachability PATCH is not a network error, so the delay is 0, but the
// error must still be non-nil so the dispatch requeues instead of marking the
// object reconciled.
func TestMachineRuntimeInstanceCreated_HostKeyPatchHTTP500_TerminalError(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	mri := machinetest.MRIFromAddr(t, 51, "mri-patch500", addr, "u", "p", key)

	api := machinetest.NewAPIStub(t)
	api.Mux.HandleFunc(fmt.Sprintf("%s/%d", v0.PathMachineRuntimeInstances, 51), func(w http.ResponseWriter, r *http.Request) {
		machinetest.WriteResponse(t, w, http.StatusInternalServerError, nil)
	})

	recorder := machinetest.NewFakeRecorder()
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	// drive the Created reconciler against an API that rejects the persist
	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)

	// a server-side rejection is terminal for this pass, but still an error
	require.Error(t, err)
	assert.Equal(t, int64(0), delay, "an http 500 is not a network error, so no requeue delay")
	assert.Equal(t, []string{event.ReasonCreateInProgress}, recorder.GetReasons(), "a rejected persist records nothing beyond the lifecycle marker")
}

// TestMachineRuntimeInstanceCreated_EventRecordingFailure_Continues sets
// the recorder to fail every call and asserts a happy-path reconcile still
// succeeds and still persists its first-reachability write; event persistence
// must never block reconciliation.
func TestMachineRuntimeInstanceCreated_EventRecordingFailure_Continues(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	mri := machinetest.NewMRIWithInfra(t, 71, "mri-eventfail", addr, "u", "p", key, machinetest.MRIInfraOpts{
		HostKey: hostKeyBase64(signer),
	})

	api := machinetest.NewAPIStub(t)
	patchCount := registerPatchCounter(t, api, 71)
	recorder := machinetest.NewFakeRecorder()
	recorder.RecordErr = errors.New("event store down")
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	// drive the Created reconciler with every RecordEvent call failing
	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)

	// the reconcile still completes and still records the confirmation stamp
	require.NoError(t, err, "event persistence failures must not block reconciliation")
	assert.Equal(t, int64(0), delay)
	assert.Equal(t, []string{event.ReasonCreateInProgress}, recorder.GetReasons())
	assert.Equal(t, int64(1), atomic.LoadInt64(patchCount), "the first-reachability write still lands when the event store is down")
}

// TestMachineRuntimeInstanceCreated_ContextCancellation_AbortsSSH injects
// an already-canceled reconcile context and asserts the handler returns
// promptly with the configurable retry delay, that the failure names the
// cancellation and carries the SSHConnectFailed event, and that the abandoned
// connect's client is closed behind it so no connection or goroutine is
// left hanging.
func TestMachineRuntimeInstanceCreated_ContextCancellation_AbortsSSH(t *testing.T) {
	overrideRetryDelay(t, 5)
	overrideReconcileContext(t, func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, cancel
	})

	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	var openConns atomic.Int64
	addr, stopServer := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{
		ExitCode:  0,
		OpenConns: &openConns,
	})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(stopServer) }
	defer stop()

	mri := machinetest.MRIFromAddr(t, 81, "mri-canceled", addr, "u", "p", key)

	api := machinetest.NewAPIStub(t)
	recorder := machinetest.NewFakeRecorder()
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	start := time.Now()
	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)
	elapsed := time.Since(start)

	// the canceled context aborts the connect instead of waiting it out
	require.Error(t, err)
	assert.Contains(t, err.Error(), context.Canceled.Error(), "the aborted connect must name the cancellation in its message")
	assert.Equal(t, int64(5), delay)
	assert.Less(t, elapsed, 5*time.Second, "an already-canceled context must abort the reconcile promptly")

	// the abort surfaces as a connect failure the wrapper can substitute for
	// the generic FailedCreate row
	var errWithEvent *tp_errors.ErrWithEvent
	require.ErrorAs(t, err, &errWithEvent, "reconciler should return *tp_errors.ErrWithEvent so the wrapper can substitute the specific reason")
	require.NotNil(t, errWithEvent.Event.Reason)
	assert.Equal(t, "SSHConnectFailed", *errWithEvent.Event.Reason)
	assert.Equal(t, []string{event.ReasonCreateInProgress}, recorder.GetReasons(), "failure path should not call RecordEvent directly for the failure; the wrapper substitutes it")

	// stopping the server waits for every accepted connection to finish
	// serving, which only happens once the abandoned connect's client has
	// been closed; a leaked client would hang the stop and fail the run
	stop()
	assert.Equal(t, int64(0), openConns.Load(), "abandoned ssh connection must be closed")
}

// TestMachineRuntimeInstanceCreated_SSHOperationTimeout_ReturnsErrorWithDelay
// points the reconcile at a server that holds the ping session open far
// past the operation timeout and asserts the handler returns within the
// timeout window with the configurable retry delay, instead of hanging for
// the full hold.
func TestMachineRuntimeInstanceCreated_SSHOperationTimeout_ReturnsErrorWithDelay(t *testing.T) {
	overrideRetryDelay(t, 9)
	overrideSSHTimeout(t, 100*time.Millisecond)

	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{HoldSession: 30 * time.Second})
	defer stop()

	mri := machinetest.NewMRIWithInfra(t, 91, "mri-timeout", addr, "u", "p", key, machinetest.MRIInfraOpts{
		HostKey: hostKeyBase64(signer),
	})

	api := machinetest.NewAPIStub(t)
	recorder := machinetest.NewFakeRecorder()
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	start := time.Now()
	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)
	elapsed := time.Since(start)

	// the operation timeout fires and reports the expired deadline as the cause
	require.Error(t, err)
	assert.Contains(t, err.Error(), context.DeadlineExceeded.Error(), "the aborted ping must name the expired deadline in its message")
	assert.Equal(t, int64(9), delay)
	assert.Less(t, elapsed, 5*time.Second, "timeout must fire well before the held session would release")

	// the abort surfaces as a ping failure the wrapper can substitute for the
	// generic FailedCreate row
	var errWithEvent *tp_errors.ErrWithEvent
	require.ErrorAs(t, err, &errWithEvent, "reconciler should return *tp_errors.ErrWithEvent so the wrapper can substitute the specific reason")
	require.NotNil(t, errWithEvent.Event.Reason)
	assert.Equal(t, "SSHPingFailed", *errWithEvent.Event.Reason)
	assert.Equal(t, []string{event.ReasonCreateInProgress}, recorder.GetReasons(), "failure path should not call RecordEvent directly for the failure; the wrapper substitutes it")
}

// TestMachineRuntimeInstanceCreated_SSHConnectTimeout_ReturnsErrorWithDelay
// is the connect-phase sibling of the ping timeout test: the server holds
// each accepted connection before any protocol bytes, so the dial blocks
// as if the host were not responding, and the handler must return within
// the operation timeout with the configurable retry delay.
func TestMachineRuntimeInstanceCreated_SSHConnectTimeout_ReturnsErrorWithDelay(t *testing.T) {
	overrideRetryDelay(t, 11)
	overrideSSHTimeout(t, 100*time.Millisecond)

	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{HoldHandshake: 30 * time.Second})
	defer stop()

	mri := machinetest.MRIFromAddr(t, 95, "mri-connect-timeout", addr, "u", "p", key)

	api := machinetest.NewAPIStub(t)
	recorder := machinetest.NewFakeRecorder()
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	start := time.Now()
	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)
	elapsed := time.Since(start)

	// the operation timeout fires and reports the expired deadline as the cause
	require.Error(t, err)
	assert.Contains(t, err.Error(), context.DeadlineExceeded.Error(), "the aborted connect must name the expired deadline in its message")
	assert.Equal(t, int64(11), delay)
	assert.Less(t, elapsed, 5*time.Second, "timeout must fire well before the held handshake would release")

	// the abort surfaces as a connect failure the wrapper can substitute for
	// the generic FailedCreate row
	var errWithEvent *tp_errors.ErrWithEvent
	require.ErrorAs(t, err, &errWithEvent, "reconciler should return *tp_errors.ErrWithEvent so the wrapper can substitute the specific reason")
	require.NotNil(t, errWithEvent.Event.Reason)
	assert.Equal(t, "SSHConnectFailed", *errWithEvent.Event.Reason)
	assert.Equal(t, []string{event.ReasonCreateInProgress}, recorder.GetReasons(), "failure path should not call RecordEvent directly for the failure; the wrapper substitutes it")
}

// TestMachineRuntimeInstanceDeleted_ReclaimsProviderResources covers the
// Deleted hook's cascade onto the married provider instance that holds the
// live machine: a provisioned instance whose married row still exists issues a
// delete for it and requeues until the provider reconciler clears the row, a
// married row already scheduled for deletion is not deleted a second time, an
// instance whose married row has cleared completes the delete, and an imported
// machine has nothing to reclaim. Every leg records the DeleteInProgress
// lifecycle marker.
func TestMachineRuntimeInstanceDeleted_ReclaimsProviderResources(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	log := logr.Discard()

	// registerMarriedInstances answers the married-instance query with the
	// given rows and records the request path of every per-row delete the
	// cascade issues, so a leg can assert exactly which rows were reclaimed
	registerMarriedInstances := func(
		t *testing.T,
		api *machinetest.APIStub,
		married []v0.GcpGceMachineRuntimeInstance,
	) *[]string {
		t.Helper()
		rows := make([]apiserver_lib.Object, 0, len(married))
		for _, marriedInstance := range married {
			row := marriedInstance
			rows = append(rows, &row)
		}
		api.Mux.HandleFunc(v0.PathGcpGceMachineRuntimeInstances, func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodGet, r.Method)
			machinetest.WriteResponse(t, w, http.StatusOK, rows)
		})
		var deletedPaths []string
		api.Mux.HandleFunc(v0.PathGcpGceMachineRuntimeInstances+"/", func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodDelete, r.Method)
			deletedPaths = append(deletedPaths, r.URL.Path)
			machinetest.WriteResponse(t, w, http.StatusOK, rows)
		})
		return &deletedPaths
	}

	t.Run("live married instance is deleted and the cascade requeues", func(t *testing.T) {
		// arrange a provisioned instance whose married provider row still
		// holds the live machine
		mri := machinetest.NewMRIWithInfra(t, 61, "mri-del-provisioned", "127.0.0.1:22", "u", "p", key, machinetest.MRIInfraOpts{
			MachineRuntimeDefinitionID: 7,
			Region:                     "us-central1",
		})

		api := machinetest.NewAPIStub(t)
		registerMachineRuntimeDefinition(t, api, 7)
		deletedPaths := registerMarriedInstances(t, api, []v0.GcpGceMachineRuntimeInstance{{
			Common:                   v0.Common{ID: util.Ptr(uint(601))},
			Instance:                 v0.Instance{Name: util.Ptr("mri-del-provisioned")},
			MachineRuntimeInstanceID: util.Ptr(uint(61)),
		}})
		recorder := machinetest.NewFakeRecorder()
		r := &controller.Reconciler{
			APIClient:      api.Client,
			APIServer:      api.Addr,
			EncryptionKey:  key,
			EventsRecorder: recorder,
		}

		// run the Deleted hook against the provisioned instance
		delay, err := v0MachineRuntimeInstanceDeleted(r, mri, &log)
		require.NoError(t, err)
		// verify the hook waits for the provider reconciler to clear the row
		assert.Equal(t, controller.Requeue30s, delay, "the delete requeues until every married provider instance clears")
		// verify the cascade issued the delete that reclaims the live machine
		assert.Equal(t, []string{fmt.Sprintf("%s/%d", v0.PathGcpGceMachineRuntimeInstances, 601)}, *deletedPaths, "the cascade must delete the married provider instance")
		assert.Equal(t, []string{event.ReasonDeleteInProgress}, recorder.GetReasons())
	})

	t.Run("married instance already scheduled for deletion is not deleted again", func(t *testing.T) {
		// arrange a married row a prior pass already scheduled for deletion
		mri := machinetest.NewMRIWithInfra(t, 63, "mri-del-scheduled", "127.0.0.1:22", "u", "p", key, machinetest.MRIInfraOpts{
			MachineRuntimeDefinitionID: 7,
			Region:                     "us-central1",
		})

		api := machinetest.NewAPIStub(t)
		registerMachineRuntimeDefinition(t, api, 7)
		deletedPaths := registerMarriedInstances(t, api, []v0.GcpGceMachineRuntimeInstance{{
			Common:                   v0.Common{ID: util.Ptr(uint(603))},
			Instance:                 v0.Instance{Name: util.Ptr("mri-del-scheduled")},
			Reconciliation:           v0.Reconciliation{DeletionScheduled: util.Ptr(time.Now().UTC())},
			MachineRuntimeInstanceID: util.Ptr(uint(63)),
		}})
		recorder := machinetest.NewFakeRecorder()
		r := &controller.Reconciler{
			APIClient:      api.Client,
			APIServer:      api.Addr,
			EncryptionKey:  key,
			EventsRecorder: recorder,
		}

		// run the Deleted hook against the already-scheduled married row
		delay, err := v0MachineRuntimeInstanceDeleted(r, mri, &log)
		require.NoError(t, err)
		// verify the hook keeps waiting rather than declaring the delete done
		assert.Equal(t, controller.Requeue30s, delay, "the delete requeues while the scheduled row is still present")
		// verify a requeue does not re-issue a delete already in flight
		assert.Empty(t, *deletedPaths, "a married row already scheduled for deletion must not be deleted again")
		assert.Equal(t, []string{event.ReasonDeleteInProgress}, recorder.GetReasons())
	})

	t.Run("cleared married instance completes the delete", func(t *testing.T) {
		// arrange a provisioned instance whose married row the provider
		// reconciler has already removed
		mri := machinetest.NewMRIWithInfra(t, 65, "mri-del-cleared", "127.0.0.1:22", "u", "p", key, machinetest.MRIInfraOpts{
			MachineRuntimeDefinitionID: 7,
			Region:                     "us-central1",
		})

		api := machinetest.NewAPIStub(t)
		registerMachineRuntimeDefinition(t, api, 7)
		deletedPaths := registerMarriedInstances(t, api, nil)
		recorder := machinetest.NewFakeRecorder()
		r := &controller.Reconciler{
			APIClient:      api.Client,
			APIServer:      api.Addr,
			EncryptionKey:  key,
			EventsRecorder: recorder,
		}

		// run the Deleted hook once the cascade has nothing left to reclaim
		delay, err := v0MachineRuntimeInstanceDeleted(r, mri, &log)
		require.NoError(t, err)
		// verify the delete finishes instead of requeueing forever
		assert.Equal(t, controller.Done, delay, "the delete completes once every married provider instance has cleared")
		assert.Empty(t, *deletedPaths, "there is nothing left to delete")
		assert.Equal(t, []string{event.ReasonDeleteInProgress}, recorder.GetReasons())
	})

	t.Run("imported machine has nothing to reclaim", func(t *testing.T) {
		// arrange an imported machine, which has no parent definition and so
		// no married provider instance behind it
		mri := machinetest.MRIFromAddr(t, 62, "mri-del-imported", "127.0.0.1:22", "u", "p", key)

		api := machinetest.NewAPIStub(t)
		deletedPaths := registerMarriedInstances(t, api, nil)
		recorder := machinetest.NewFakeRecorder()
		r := &controller.Reconciler{
			APIClient:      api.Client,
			APIServer:      api.Addr,
			EncryptionKey:  key,
			EventsRecorder: recorder,
		}

		// run the Deleted hook against the imported machine
		delay, err := v0MachineRuntimeInstanceDeleted(r, mri, &log)
		require.NoError(t, err)
		assert.Equal(t, controller.Done, delay)
		assert.Empty(t, *deletedPaths, "imported machines have no provider resources to reclaim")
		assert.Equal(t, []string{event.ReasonDeleteInProgress}, recorder.GetReasons())
	})
}

// TestMachineRuntimeInstanceCreated_ConcurrentReconciles_NoRace runs many
// Created reconciles for distinct MRIs concurrently against one SSH
// server. Every reconcile must succeed and every recorder must hold
// exactly its own CreateInProgress event, proving no shared state bleeds
// between concurrent reconciles. Run under -race.
func TestMachineRuntimeInstanceCreated_ConcurrentReconciles_NoRace(t *testing.T) {
	const n = 50
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	api := machinetest.NewAPIStub(t)
	log := logr.Discard()
	hostKey := hostKeyBase64(signer)
	confirmed := time.Now().UTC()

	// build all inputs on the test goroutine; the require-based helpers
	// are not safe to call from spawned goroutines
	mris := make([]*v0.MachineRuntimeInstance, n)
	recorders := make([]*machinetest.FakeRecorder, n)
	for i := 0; i < n; i++ {
		mris[i] = machinetest.NewMRIWithInfra(t, uint(1000+i), fmt.Sprintf("mri-conc-%d", i), addr, "u", "p", key, machinetest.MRIInfraOpts{
			HostKey: hostKey,
		})
		// pre-stamp the confirmation so a reachable pass has nothing new to
		// record, keeping the burst on the ssh path with no api traffic
		mris[i].CreationConfirmed = util.Ptr(confirmed)
		recorders[i] = machinetest.NewFakeRecorder()
	}

	var wg sync.WaitGroup
	delays := make([]int64, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := &controller.Reconciler{
				APIClient:      api.Client,
				APIServer:      api.Addr,
				EncryptionKey:  key,
				EventsRecorder: recorders[i],
			}
			delays[i], errs[i] = v0MachineRuntimeInstanceCreated(r, mris[i], &log)
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoError(t, errs[i], "reconcile %d", i)
		assert.Equal(t, int64(0), delays[i], "reconcile %d", i)
		assert.Equal(t, []string{event.ReasonCreateInProgress}, recorders[i].GetReasons(), "reconcile %d", i)
	}
}

// TestMachineRuntimeInstanceCreated_ManyConcurrent_NoConnLeak runs a wide
// burst of concurrent reconciles against a connection-counting SSH server
// and asserts every SSH connection is closed and goroutines settle back to
// the pre-burst baseline after the burst drains. This proves the SSH path
// closes connections and leaks no goroutines under width; it makes no
// claim about provider-level concurrency limits. Run under -race.
func TestMachineRuntimeInstanceCreated_ManyConcurrent_NoConnLeak(t *testing.T) {
	const n = 200
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	var openConns atomic.Int64
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{
		ExitCode:  0,
		OpenConns: &openConns,
	})
	defer stop()

	api := machinetest.NewAPIStub(t)
	log := logr.Discard()
	hostKey := hostKeyBase64(signer)
	confirmed := time.Now().UTC()

	mris := make([]*v0.MachineRuntimeInstance, n)
	recorders := make([]*machinetest.FakeRecorder, n)
	for i := 0; i < n; i++ {
		mris[i] = machinetest.NewMRIWithInfra(t, uint(2000+i), fmt.Sprintf("mri-leak-%d", i), addr, "u", "p", key, machinetest.MRIInfraOpts{
			HostKey: hostKey,
		})
		// pre-stamp the confirmation so the burst issues no api calls, whose
		// pooled connections and goroutines would blur the leak assertion
		mris[i].CreationConfirmed = util.Ptr(confirmed)
		recorders[i] = machinetest.NewFakeRecorder()
	}

	baseline := runtime.NumGoroutine()

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := &controller.Reconciler{
				APIClient:      api.Client,
				APIServer:      api.Addr,
				EncryptionKey:  key,
				EventsRecorder: recorders[i],
			}
			_, errs[i] = v0MachineRuntimeInstanceCreated(r, mris[i], &log)
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoError(t, errs[i], "reconcile %d", i)
	}

	// every ssh client must be closed after the burst drains
	require.Eventually(t, func() bool {
		return openConns.Load() == 0
	}, 10*time.Second, 20*time.Millisecond, "open ssh connections must drain to zero")

	// goroutines must settle back near the pre-burst baseline
	require.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= baseline+10
	}, 10*time.Second, 20*time.Millisecond, "goroutines must return to baseline after the burst")
}

// hostKeyBase64 returns the base64-encoded marshalled public key matching
// buildHostKeyCallback's verification-mode encoding.
func hostKeyBase64(signer interface{ PublicKey() ssh.PublicKey }) string {
	return base64.StdEncoding.EncodeToString(signer.PublicKey().Marshal())
}
