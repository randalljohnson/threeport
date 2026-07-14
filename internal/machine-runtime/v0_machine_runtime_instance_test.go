package machineruntime

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"

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
// succeeds, and the reachability signal lands as a log statement. The
// reconciler emits exactly one CreateInProgress lifecycle marker at the top of
// the run; the wrapper's SuccessfulCreate event still records the outcome.
func TestMachineRuntimeInstanceCreated_HappyPath(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	// pin the MRI's HostKey to the server's real key so GetClient
	// verifies rather than captures
	mri := machinetest.MRIFromAddr(t, 42, "mri-happy", addr, "u", "p", key)
	mri.HostKey = util.Ptr(hostKeyBase64(signer))

	api := machinetest.NewAPIStub(t)
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
		patches    [][]byte
		patchesMu  sync.Mutex
		patchPath  = fmt.Sprintf("%s/%d", v0.PathMachineRuntimeInstances, 7)
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

// hostKeyBase64 returns the base64-encoded marshalled public key matching
// buildHostKeyCallback's verification-mode encoding.
func hostKeyBase64(signer interface{ PublicKey() ssh.PublicKey }) string {
	return base64.StdEncoding.EncodeToString(signer.PublicKey().Marshal())
}
