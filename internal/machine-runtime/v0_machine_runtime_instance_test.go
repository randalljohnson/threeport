package machineruntime

import (
	"context"
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

	"github.com/threeport/threeport/internal/machinetest"
	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	encryption "github.com/threeport/threeport/pkg/encryption/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestMachineRuntimeInstanceCreated_HappyPath drives a full Created
// reconcile against the in-process SSH server. The MRI has HostKey set to
// the server's actual key (no capture path); GetClient succeeds, Ping
// succeeds, and a single SSHReachable event is recorded.
func TestMachineRuntimeInstanceCreated_HappyPath(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	mri := machinetest.MRIFromAddr(t, 42, "mri-happy", addr, "u", "p", key)
	mri.HostKey = util.Ptr(machinetest.HostKeyFromSigner(signer))

	api := machinetest.NewAPIStub(t)
	recorder := machinetest.NewFakeRecorder()
	log := logr.Discard()

	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Equal(t, []string{"SSHReachable"}, recorder.GetReasons())
}

// TestMachineRuntimeInstanceCreated_NoHostname_RequeuesWithoutDialing covers
// the deferred-dial path: an instance whose hostname is not yet populated
// requeues with the unpopulated delay, returns no error, dials no SSH server,
// records no event, and persists no update, so Reconciled stays unset until
// the machine is reachable. Both a nil and an empty-string hostname take this
// path.
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
			// verify no event fires before the machine is reachable
			assert.Empty(t, recorder.GetReasons(), "no event may be recorded before the machine is reachable")
			// verify nothing is persisted so Reconciled stays unset until reachable
			assert.Equal(t, int64(0), atomic.LoadInt64(patchCount), "no update may be persisted, so Reconciled stays unset")
		})
	}
}

// TestMachineRuntimeInstanceCreated_HostKeyCaptured covers the first-connect
// path: HostKey is nil, so GetClient captures the server's key, the
// reconciler PATCHes the MRI to persist it, and emits HostKeyCaptured +
// SSHReachable in order.
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

	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Equal(t, []string{"HostKeyCaptured", "SSHReachable"}, recorder.GetReasons())

	patchesMu.Lock()
	defer patchesMu.Unlock()
	require.Len(t, patches, 1, "expected exactly one PATCH to persist the captured host key")
	assert.Contains(t, string(patches[0]), "HostKey", "PATCH body should carry the HostKey field")
	assert.Contains(t, string(patches[0]), `"Reconciled":true`, "PATCH should set Reconciled=true so the resulting update notification does not retrigger reconciliation")
}

// TestMachineRuntimeInstanceCreated_NetworkError points the MRI at an
// unreachable host and asserts the reconciler returns 30s requeue and emits
// SSHConnectFailed.
func TestMachineRuntimeInstanceCreated_NetworkError(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	// 127.0.0.1:1 — port 1 is reserved and never bound; produces
	// "connection refused" (network-class error).
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

	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)
	require.Error(t, err)
	assert.Equal(t, int64(30), delay, "network-class errors should be retried after 30s")
	assert.Equal(t, []string{"SSHConnectFailed"}, recorder.GetReasons())
}

// TestMachineRuntimeInstanceCreated_HostKeyMismatch points the MRI at the
// test server but with a HostKey that doesn't match the server's actual
// host key. SSH client errors (including host key mismatch) always retry
// after 30s, since a misconfigured key may be fixed externally without
// changing the object.
func TestMachineRuntimeInstanceCreated_HostKeyMismatch(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	serverSigner := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, serverSigner, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	// Pin a different host key on the MRI to force a mismatch.
	wrongSigner := machinetest.NewSigner(t)
	mri := machinetest.MRIFromAddr(t, 11, "mri-mismatch", addr, "u", "p", key)
	mri.HostKey = util.Ptr(machinetest.HostKeyFromSigner(wrongSigner))

	api := machinetest.NewAPIStub(t)
	recorder := machinetest.NewFakeRecorder()
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)
	require.Error(t, err)
	assert.Equal(t, int64(30), delay, "ssh-client errors always retry")
	assert.Equal(t, []string{"SSHConnectFailed"}, recorder.GetReasons())
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

// TestMachineRuntimeInstanceCreated_IdempotentOnDoubleCall proves a
// re-reconcile of an MRI whose host key is already persisted does not PATCH
// again. Leg one runs in capture mode (HostKey nil) and persists the key
// with exactly one PATCH; leg two carries the server's real host key, so
// GetClient runs in verification mode, the capture branch is skipped, and
// no second PATCH lands.
func TestMachineRuntimeInstanceCreated_IdempotentOnDoubleCall(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	api := machinetest.NewAPIStub(t)
	patchCount := registerPatchCounter(t, api, 21)
	log := logr.Discard()

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
	assert.Equal(t, []string{"HostKeyCaptured", "SSHReachable"}, firstRecorder.GetReasons())
	require.Equal(t, int64(1), atomic.LoadInt64(patchCount), "first reconcile persists the captured host key with one PATCH")

	second := machinetest.NewMRIWithInfra(t, 21, "mri-idem", addr, "u", "p", key, machinetest.MRIInfraOpts{
		HostKey: machinetest.HostKeyFromSigner(signer),
	})
	secondRecorder := machinetest.NewFakeRecorder()
	r.EventsRecorder = secondRecorder

	delay, err = v0MachineRuntimeInstanceCreated(r, second, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Equal(t, []string{"SSHReachable"}, secondRecorder.GetReasons())
	assert.Equal(t, int64(1), atomic.LoadInt64(patchCount), "second reconcile must not re-PATCH the host key")
}

// TestMachineRuntimeInstanceCreated_SSHPingFails_Retries drives a connect
// that succeeds and a ping that fails (non-zero exit), asserting the
// configurable retry delay is returned, SSHPingFailed is recorded, and no
// update is persisted.
func TestMachineRuntimeInstanceCreated_SSHPingFails_Retries(t *testing.T) {
	overrideRetryDelay(t, 7)
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 1})
	defer stop()

	mri := machinetest.NewMRIWithInfra(t, 31, "mri-pingfail", addr, "u", "p", key, machinetest.MRIInfraOpts{
		HostKey: machinetest.HostKeyFromSigner(signer),
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

	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)
	require.Error(t, err)
	assert.Equal(t, int64(7), delay, "ping failures requeue with the configurable delay")
	assert.Equal(t, []string{"SSHPingFailed"}, recorder.GetReasons())
	assert.Equal(t, int64(0), atomic.LoadInt64(patchCount), "no update may be persisted on ping failure")
}

// TestMachineRuntimeInstanceCreated_HostKeyPatchFails_Retries closes the
// API stub before the reconcile so the host-key PATCH hits a refused
// connection. A transport-level failure must requeue (non-zero delay,
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

	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)
	require.Error(t, err)
	assert.Equal(t, int64(30), delay, "transport failures on the host-key PATCH requeue after 30s")
	assert.Empty(t, recorder.GetReasons(), "no events may be recorded when the capture PATCH fails")
}

// TestMachineRuntimeInstanceCreated_HostKeyPatchHTTP500_TerminalError is
// the sibling of the transport-failure case: an HTTP 500 on the host-key
// PATCH is not a network error, so the delay is 0, but the error must
// still be non-nil so the dispatch requeues instead of marking the object
// reconciled.
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

	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)
	require.Error(t, err)
	assert.Equal(t, int64(0), delay, "an http 500 is not a network error, so no requeue delay")
	assert.Empty(t, recorder.GetReasons())
}

// TestMachineRuntimeInstanceCreated_EventRecordingFailure_Continues sets
// the recorder to fail every call and asserts a happy-path reconcile still
// succeeds; event persistence must never block reconciliation.
func TestMachineRuntimeInstanceCreated_EventRecordingFailure_Continues(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	mri := machinetest.NewMRIWithInfra(t, 71, "mri-eventfail", addr, "u", "p", key, machinetest.MRIInfraOpts{
		HostKey: machinetest.HostKeyFromSigner(signer),
	})

	api := machinetest.NewAPIStub(t)
	recorder := machinetest.NewFakeRecorder()
	recorder.RecordErr = errors.New("event store down")
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)
	require.NoError(t, err, "event persistence failures must not block reconciliation")
	assert.Equal(t, int64(0), delay)
	assert.Equal(t, []string{"SSHReachable"}, recorder.GetReasons())
}

// TestMachineRuntimeInstanceCreated_ContextCancellation_AbortsSSH injects
// an already-canceled reconcile context and asserts the handler returns
// promptly with the configurable retry delay, and that the abandoned
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

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int64(5), delay)
	assert.Less(t, elapsed, 5*time.Second, "an already-canceled context must abort the reconcile promptly")
	assert.Equal(t, []string{"SSHConnectFailed"}, recorder.GetReasons())

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
		HostKey: machinetest.HostKeyFromSigner(signer),
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

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int64(9), delay)
	assert.Less(t, elapsed, 5*time.Second, "timeout must fire well before the held session would release")
	assert.Equal(t, []string{"SSHPingFailed"}, recorder.GetReasons())
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

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int64(11), delay)
	assert.Less(t, elapsed, 5*time.Second, "timeout must fire well before the held handshake would release")
	assert.Equal(t, []string{"SSHConnectFailed"}, recorder.GetReasons())
}

// registerDefinitionGet registers a GET handler for the machine runtime
// definition with the given id that replies with a definition carrying the
// supplied infra provider, so the abstract Deleted handler can learn which
// provider backs the machine.
func registerDefinitionGet(t *testing.T, api *machinetest.APIStub, id uint, infraProvider string) {
	t.Helper()
	api.Mux.HandleFunc(fmt.Sprintf("%s/%d", v0.PathMachineRuntimeDefinitions, id), func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		def := v0.MachineRuntimeDefinition{
			Common:        v0.Common{ID: util.Ptr(id)},
			InfraProvider: util.Ptr(infraProvider),
		}
		machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{&def})
	})
}

// registerMarriedDeleteCounter registers a DELETE handler for the married GCE
// instance with the given id that counts calls and replies with a valid
// envelope, so tests can assert the abstract Deleted handler scheduled the
// married teardown exactly once.
func registerMarriedDeleteCounter(t *testing.T, api *machinetest.APIStub, id uint) *int64 {
	t.Helper()
	var count int64
	api.Mux.HandleFunc(fmt.Sprintf("%s/%d", v0.PathGcpGceMachineRuntimeInstances, id), func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		atomic.AddInt64(&count, 1)
		deleted := v0.GcpGceMachineRuntimeInstance{Common: v0.Common{ID: util.Ptr(id)}}
		machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{&deleted})
	})
	return &count
}

// registerAttachedWorkloadQuery registers a collection GET handler for the
// machine workload instances path that returns the supplied attached workloads
// for any query string, so the abstract Deleted handler can find the workloads
// to tear down before deprovisioning the machine.
func registerAttachedWorkloadQuery(t *testing.T, api *machinetest.APIStub, attached []v0.MachineWorkloadInstance) {
	t.Helper()
	api.Mux.HandleFunc(v0.PathMachineWorkloadInstances, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		objs := make([]apiserver_lib.Object, len(attached))
		for i := range attached {
			a := attached[i]
			objs[i] = &a
		}
		machinetest.WriteResponse(t, w, http.StatusOK, objs)
	})
}

// registerAttachedWorkloadDeleteCounter registers a DELETE handler for the
// attached machine workload instance with the given id that counts calls and
// replies with a valid envelope, so tests can assert the handler scheduled the
// attached workload teardown exactly once.
func registerAttachedWorkloadDeleteCounter(t *testing.T, api *machinetest.APIStub, id uint) *int64 {
	t.Helper()
	var count int64
	api.Mux.HandleFunc(fmt.Sprintf("%s/%d", v0.PathMachineWorkloadInstances, id), func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		atomic.AddInt64(&count, 1)
		deleted := v0.MachineWorkloadInstance{Common: v0.Common{ID: util.Ptr(id)}}
		machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{&deleted})
	})
	return &count
}

// overrideDeprovisionRequeueDelay sets the deprovision requeue delay for one
// test and restores it on cleanup.
func overrideDeprovisionRequeueDelay(t *testing.T, seconds int64) {
	t.Helper()
	prev := deprovisionRequeueDelaySeconds
	deprovisionRequeueDelaySeconds = seconds
	t.Cleanup(func() { deprovisionRequeueDelaySeconds = prev })
}

// TestMachineRuntimeInstanceDeleted_DeprovisionsAttachedAndProvider covers the
// Deleted hook teardown ordering: an attached workload still present is deleted
// and the handler requeues before touching the provider; once the attached
// workloads are gone the married provider instance is deleted and the handler
// requeues without confirming; a married instance already scheduled for
// deletion is requeued without a second delete; once the married instance is
// gone the abstract deletion is confirmed; and an imported machine is a clean
// no-op. The invariant under test is that deletion is never confirmed while an
// attached workload or a provider VM may still be live.
func TestMachineRuntimeInstanceDeleted_DeprovisionsAttachedAndProvider(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	log := logr.Discard()

	t.Run("attached workload present deletes it and requeues before provider", func(t *testing.T) {
		overrideDeprovisionRequeueDelay(t, 5)
		// arrange a provider-provisioned mri with one attached workload still
		// present and not yet scheduled for deletion
		mri := machinetest.NewMRIWithInfra(t, 60, "mri-del-attached", "127.0.0.1:22", "u", "p", key, machinetest.MRIInfraOpts{
			MachineRuntimeDefinitionID: 7,
			Region:                     "us-central1",
		})
		workload := v0.MachineWorkloadInstance{
			Common:                   v0.Common{ID: util.Ptr(uint(401))},
			MachineRuntimeInstanceID: util.Ptr(uint(60)),
		}

		api := machinetest.NewAPIStub(t)
		registerAttachedWorkloadQuery(t, api, []v0.MachineWorkloadInstance{workload})
		workloadDeleteCount := registerAttachedWorkloadDeleteCounter(t, api, 401)
		recorder := machinetest.NewFakeRecorder()
		r := &controller.Reconciler{
			APIClient:      api.Client,
			APIServer:      api.Addr,
			EncryptionKey:  key,
			EventsRecorder: recorder,
		}

		// run the Deleted hook while an attached workload is still present
		delay, err := v0MachineRuntimeInstanceDeleted(r, mri, &log)
		require.NoError(t, err)
		// verify it requeues without reaching the provider while a workload lives
		assert.Equal(t, int64(5), delay, "delete must requeue while an attached workload is still present")
		// verify the attached workload was deleted exactly once to run its delete script
		assert.Equal(t, int64(1), atomic.LoadInt64(workloadDeleteCount), "the attached workload must be deleted to run its uninstall script")
		// verify the attached-workload-deleting event is recorded
		assert.Equal(t, []string{"AttachedWorkloadDeleting"}, recorder.GetReasons())
	})

	t.Run("married present deletes it and requeues without confirming", func(t *testing.T) {
		overrideDeprovisionRequeueDelay(t, 5)
		// arrange a provider-provisioned mri whose attached workloads are gone and
		// whose married GCE instance is still present and not yet scheduled
		mri := machinetest.NewMRIWithInfra(t, 61, "mri-del-provisioned", "127.0.0.1:22", "u", "p", key, machinetest.MRIInfraOpts{
			MachineRuntimeDefinitionID: 7,
			Region:                     "us-central1",
		})
		married := v0.GcpGceMachineRuntimeInstance{
			Common:                   v0.Common{ID: util.Ptr(uint(301))},
			Instance:                 v0.Instance{Name: util.Ptr("mri-del-provisioned")},
			MachineRuntimeInstanceID: util.Ptr(uint(61)),
		}

		api := machinetest.NewAPIStub(t)
		registerAttachedWorkloadQuery(t, api, nil)
		registerDefinitionGet(t, api, 7, v0.MachineRuntimeInfraProviderGCE)
		registerMarriedQuery(t, api, []v0.GcpGceMachineRuntimeInstance{married})
		deleteCount := registerMarriedDeleteCounter(t, api, 301)
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
		// verify the handler requeues rather than confirming while the VM is live
		assert.Equal(t, int64(5), delay, "delete must requeue, not confirm, while the married instance is present")
		// verify the married instance was deleted exactly once to drive the destroy
		assert.Equal(t, int64(1), atomic.LoadInt64(deleteCount), "the married instance must be deleted to trigger deprovision")
		// verify the deprovision-in-progress event is recorded
		assert.Equal(t, []string{"ProviderResourcesDeprovisioning"}, recorder.GetReasons())
	})

	t.Run("married already scheduled requeues without a second delete", func(t *testing.T) {
		overrideDeprovisionRequeueDelay(t, 5)
		// arrange a married instance whose deletion is already underway so the
		// handler must not delete it again
		mri := machinetest.NewMRIWithInfra(t, 65, "mri-del-inflight", "127.0.0.1:22", "u", "p", key, machinetest.MRIInfraOpts{
			MachineRuntimeDefinitionID: 7,
			Region:                     "us-central1",
		})
		scheduled := time.Now().UTC()
		married := v0.GcpGceMachineRuntimeInstance{
			Common:                   v0.Common{ID: util.Ptr(uint(305))},
			Instance:                 v0.Instance{Name: util.Ptr("mri-del-inflight")},
			MachineRuntimeInstanceID: util.Ptr(uint(65)),
			Reconciliation:           v0.Reconciliation{DeletionScheduled: &scheduled},
		}

		api := machinetest.NewAPIStub(t)
		registerAttachedWorkloadQuery(t, api, nil)
		registerDefinitionGet(t, api, 7, v0.MachineRuntimeInfraProviderGCE)
		registerMarriedQuery(t, api, []v0.GcpGceMachineRuntimeInstance{married})
		deleteCount := registerMarriedDeleteCounter(t, api, 305)
		recorder := machinetest.NewFakeRecorder()
		r := &controller.Reconciler{
			APIClient:      api.Client,
			APIServer:      api.Addr,
			EncryptionKey:  key,
			EventsRecorder: recorder,
		}

		// run the Deleted hook while the teardown is in flight
		delay, err := v0MachineRuntimeInstanceDeleted(r, mri, &log)
		require.NoError(t, err)
		// verify it requeues without confirming
		assert.Equal(t, int64(5), delay, "an in-flight teardown must requeue, not confirm")
		// verify no second delete is issued against the already-scheduled instance
		assert.Equal(t, int64(0), atomic.LoadInt64(deleteCount), "an already-scheduled married instance must not be re-deleted")
		// verify no event fires when nothing new happened this pass
		assert.Empty(t, recorder.GetReasons(), "no event may fire while waiting on an in-flight teardown")
	})

	t.Run("married gone confirms deletion", func(t *testing.T) {
		// arrange a provisioned mri whose attached workloads and married instance
		// have been fully torn down, so both queries return empty sets
		mri := machinetest.NewMRIWithInfra(t, 64, "mri-del-gone", "127.0.0.1:22", "u", "p", key, machinetest.MRIInfraOpts{
			MachineRuntimeDefinitionID: 7,
			Region:                     "us-central1",
		})

		api := machinetest.NewAPIStub(t)
		registerAttachedWorkloadQuery(t, api, nil)
		registerDefinitionGet(t, api, 7, v0.MachineRuntimeInfraProviderGCE)
		registerMarriedQuery(t, api, nil)
		recorder := machinetest.NewFakeRecorder()
		r := &controller.Reconciler{
			APIClient:      api.Client,
			APIServer:      api.Addr,
			EncryptionKey:  key,
			EventsRecorder: recorder,
		}

		// run the Deleted hook once the attached workloads and married instance are gone
		delay, err := v0MachineRuntimeInstanceDeleted(r, mri, &log)
		require.NoError(t, err)
		// verify the abstract deletion is now confirmed
		assert.Equal(t, int64(0), delay, "deletion must confirm once the provider VM is gone")
		// verify no event fires on the confirming pass
		assert.Empty(t, recorder.GetReasons())
	})

	t.Run("imported machine is a no-op", func(t *testing.T) {
		// arrange an imported machine with no definition, so there is nothing to
		// deprovision
		mri := machinetest.MRIFromAddr(t, 62, "mri-del-imported", "127.0.0.1:22", "u", "p", key)

		api := machinetest.NewAPIStub(t)
		recorder := machinetest.NewFakeRecorder()
		r := &controller.Reconciler{
			APIClient:      api.Client,
			APIServer:      api.Addr,
			EncryptionKey:  key,
			EventsRecorder: recorder,
		}

		// run the Deleted hook against the imported instance
		delay, err := v0MachineRuntimeInstanceDeleted(r, mri, &log)
		require.NoError(t, err)
		// verify deletion confirms immediately with no deprovision work
		assert.Equal(t, int64(0), delay)
		assert.Empty(t, recorder.GetReasons(), "imported machines have nothing to deprovision")
	})
}

// registerMarriedQuery registers a collection GET handler for the GCE machine
// runtime instances path that returns the supplied married instances for any
// query string, so the abstract Updated handler can resolve the child it diffs
// against.
func registerMarriedQuery(t *testing.T, api *machinetest.APIStub, married []v0.GcpGceMachineRuntimeInstance) {
	t.Helper()
	api.Mux.HandleFunc(v0.PathGcpGceMachineRuntimeInstances, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		objs := make([]apiserver_lib.Object, len(married))
		for i := range married {
			m := married[i]
			objs[i] = &m
		}
		machinetest.WriteResponse(t, w, http.StatusOK, objs)
	})
}

// registerMarriedPatchCounter registers a PATCH handler for the married GCE
// instance with the given id that records every PATCH body and replies with a
// valid envelope, so tests can assert what the abstract Updated handler
// propagated to the child.
func registerMarriedPatchCounter(t *testing.T, api *machinetest.APIStub, id uint) *[][]byte {
	t.Helper()
	var (
		bodies [][]byte
		mu     sync.Mutex
	)
	out := &bodies
	api.Mux.HandleFunc(fmt.Sprintf("%s/%d", v0.PathGcpGceMachineRuntimeInstances, id), func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		mu.Lock()
		bodies = append(bodies, body)
		out = &bodies
		mu.Unlock()
		var updated v0.GcpGceMachineRuntimeInstance
		require.NoError(t, json.Unmarshal(body, &updated))
		updated.ID = util.Ptr(id)
		machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{&updated})
	})
	return out
}

// TestMachineRuntimeInstanceUpdated_ImportedSSHKeyChange_VerifiesReachable
// covers the imported-machine update path: an instance with no definition has
// no married provider instance, so a credential change is validated by
// re-running the ssh connect and ping, which records SSHReachable when the
// machine answers.
func TestMachineRuntimeInstanceUpdated_ImportedSSHKeyChange_VerifiesReachable(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	// arrange an imported instance (no definition) whose host key is pinned so
	// the connect runs in verification mode
	mri := machinetest.NewMRIWithInfra(t, 201, "mri-imported-update", addr, "u", "p", key, machinetest.MRIInfraOpts{
		HostKey: machinetest.HostKeyFromSigner(signer),
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

	// run the Updated hook against the imported instance
	delay, err := v0MachineRuntimeInstanceUpdated(r, mri, &log)
	// verify the connectivity re-check succeeds without error or requeue
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	// verify the re-check records reachability rather than a married-instance patch
	assert.Equal(t, []string{"SSHReachable"}, recorder.GetReasons())
}

// TestMachineRuntimeInstanceUpdated_SSHUserChange_PropagatesToMarried covers
// the provisioned-machine update path: when the fetched ssh user differs from
// the value last propagated to the married provider instance, the handler
// patches the married instance's ssh user with Reconciled=false so the child
// reconciler runs a pulumi up that applies the new metadata in place.
func TestMachineRuntimeInstanceUpdated_SSHUserChange_PropagatesToMarried(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)

	// arrange a provisioned instance whose ssh user was changed to "newuser";
	// no hostname so the post-propagation key check would defer rather than dial
	mri := &v0.MachineRuntimeInstance{
		Common:                     v0.Common{ID: util.Ptr(uint(202))},
		Instance:                   v0.Instance{Name: util.Ptr("mri-user-update")},
		MachineRuntimeDefinitionID: util.Ptr(uint(7)),
		SSHUser:                    util.Ptr("newuser"),
	}

	// the married child still carries the old ssh user, so the users differ
	married := v0.GcpGceMachineRuntimeInstance{
		Common:                   v0.Common{ID: util.Ptr(uint(303))},
		Instance:                 v0.Instance{Name: util.Ptr("mri-user-update")},
		MachineRuntimeInstanceID: util.Ptr(uint(202)),
		SSHUser:                  util.Ptr("olduser"),
	}

	api := machinetest.NewAPIStub(t)
	registerMarriedQuery(t, api, []v0.GcpGceMachineRuntimeInstance{married})
	patches := registerMarriedPatchCounter(t, api, 303)
	recorder := machinetest.NewFakeRecorder()
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	// run the Updated hook against the user-changed instance
	delay, err := v0MachineRuntimeInstanceUpdated(r, mri, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	// verify the handler records the ssh user update
	assert.Equal(t, []string{"SSHUserUpdated"}, recorder.GetReasons())
	// verify exactly one PATCH carried the new ssh user and cleared Reconciled
	require.Len(t, *patches, 1, "expected one PATCH to propagate the ssh user")
	body := string((*patches)[0])
	assert.Contains(t, body, "newuser", "PATCH must carry the new ssh user")
	assert.Contains(t, body, `"Reconciled":false`, "PATCH must clear Reconciled so the married update notification fires")
}

// TestMachineRuntimeInstanceUpdated_NoChange_NoMarriedPatch covers the
// no-op update: when the fetched ssh user and ssh key match the married
// instance, the handler patches nothing and records no event.
func TestMachineRuntimeInstanceUpdated_NoChange_NoMarriedPatch(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	sameKey := machinetest.NewEncryptionKey(t)

	// arrange a provisioned instance whose ssh user and key already match the
	// married child, so nothing has changed
	mri := &v0.MachineRuntimeInstance{
		Common:                     v0.Common{ID: util.Ptr(uint(204))},
		Instance:                   v0.Instance{Name: util.Ptr("mri-nochange")},
		MachineRuntimeDefinitionID: util.Ptr(uint(7)),
		SSHUser:                    util.Ptr("u"),
		SSHKey:                     util.Ptr(encryptKeyForTest(t, key, sameKey)),
	}
	married := v0.GcpGceMachineRuntimeInstance{
		Common:                   v0.Common{ID: util.Ptr(uint(305))},
		Instance:                 v0.Instance{Name: util.Ptr("mri-nochange")},
		MachineRuntimeInstanceID: util.Ptr(uint(204)),
		SSHUser:                  util.Ptr("u"),
		SSHKey:                   util.Ptr(encryptKeyForTest(t, key, sameKey)),
	}

	api := machinetest.NewAPIStub(t)
	registerMarriedQuery(t, api, []v0.GcpGceMachineRuntimeInstance{married})
	patches := registerMarriedPatchCounter(t, api, 305)
	recorder := machinetest.NewFakeRecorder()
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	// run the Updated hook against the unchanged instance
	delay, err := v0MachineRuntimeInstanceUpdated(r, mri, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	// verify no event and no PATCH when nothing changed
	assert.Empty(t, recorder.GetReasons(), "no event may fire when ssh user and key are unchanged")
	assert.Empty(t, *patches, "no PATCH may land when nothing changed")
}

// TestMachineRuntimeInstanceUpdated_NoMarriedInstance_Requeues covers the race
// where the abstract create path has not yet created the married child: the
// query returns nothing, so the handler requeues on the ssh-retry delay rather
// than erroring.
func TestMachineRuntimeInstanceUpdated_NoMarriedInstance_Requeues(t *testing.T) {
	overrideRetryDelay(t, 4)
	key := machinetest.NewEncryptionKey(t)

	mri := &v0.MachineRuntimeInstance{
		Common:                     v0.Common{ID: util.Ptr(uint(206))},
		Instance:                   v0.Instance{Name: util.Ptr("mri-no-married")},
		MachineRuntimeDefinitionID: util.Ptr(uint(7)),
		SSHUser:                    util.Ptr("u"),
	}

	api := machinetest.NewAPIStub(t)
	// the married query returns an empty set
	registerMarriedQuery(t, api, nil)
	recorder := machinetest.NewFakeRecorder()
	log := logr.Discard()
	r := &controller.Reconciler{
		APIClient:      api.Client,
		APIServer:      api.Addr,
		EncryptionKey:  key,
		EventsRecorder: recorder,
	}

	// run the Updated hook before the married child exists
	delay, err := v0MachineRuntimeInstanceUpdated(r, mri, &log)
	require.NoError(t, err)
	// verify it requeues on the ssh-retry delay rather than erroring
	assert.Equal(t, int64(4), delay)
	assert.Empty(t, recorder.GetReasons())
}

// encryptKeyForTest encrypts plaintext with the given encryption key so a test
// fixture carries a ciphertext the handler can decrypt. The plaintext argument
// is itself an encryption key string only to give each fixture distinct,
// deterministic content; any string would do.
func encryptKeyForTest(t *testing.T, encryptionKey, plaintext string) string {
	t.Helper()
	ct, err := encryption.Encrypt(encryptionKey, plaintext)
	require.NoError(t, err)
	return ct
}

// TestMachineRuntimeInstanceCreated_ConcurrentReconciles_NoRace runs many
// Created reconciles for distinct MRIs concurrently against one SSH
// server. Every reconcile must succeed and every recorder must hold
// exactly its own SSHReachable event, proving no shared state bleeds
// between concurrent reconciles. Run under -race.
func TestMachineRuntimeInstanceCreated_ConcurrentReconciles_NoRace(t *testing.T) {
	const n = 50
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	api := machinetest.NewAPIStub(t)
	log := logr.Discard()
	hostKey := machinetest.HostKeyFromSigner(signer)

	// build all inputs on the test goroutine; the require-based helpers
	// are not safe to call from spawned goroutines
	mris := make([]*v0.MachineRuntimeInstance, n)
	recorders := make([]*machinetest.FakeRecorder, n)
	for i := 0; i < n; i++ {
		mris[i] = machinetest.NewMRIWithInfra(t, uint(1000+i), fmt.Sprintf("mri-conc-%d", i), addr, "u", "p", key, machinetest.MRIInfraOpts{
			HostKey: hostKey,
		})
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
		assert.Equal(t, []string{"SSHReachable"}, recorders[i].GetReasons(), "reconcile %d", i)
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
	hostKey := machinetest.HostKeyFromSigner(signer)

	mris := make([]*v0.MachineRuntimeInstance, n)
	recorders := make([]*machinetest.FakeRecorder, n)
	for i := 0; i < n; i++ {
		mris[i] = machinetest.NewMRIWithInfra(t, uint(2000+i), fmt.Sprintf("mri-leak-%d", i), addr, "u", "p", key, machinetest.MRIInfraOpts{
			HostKey: hostKey,
		})
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
