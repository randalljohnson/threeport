package machineruntime

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestIsNetworkErr covers the substring matches that classify an error as
// network-class (retryable) vs terminal (not retryable).
func TestIsNetworkErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"connection refused", errors.New("dial tcp 127.0.0.1:22: connection refused"), true},
		{"no route to host", errors.New("dial tcp: no route to host"), true},
		{"io timeout", errors.New("dial tcp: i/o timeout"), true},
		{"network unreachable", errors.New("network is unreachable"), true},
		{"host unreachable", errors.New("host is unreachable"), true},
		{"EOF", errors.New("EOF"), true},
		{"auth failure (terminal)", errors.New("ssh: unable to authenticate"), false},
		{"host key mismatch (terminal)", errors.New("host key mismatch for x: expected y, got z"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, isNetworkErr(c.err))
		})
	}
}

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

	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Equal(t, []string{"SSHReachable"}, recorder.GetReasons())
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

// TestMachineRuntimeInstanceCreated_TerminalError points the MRI at the
// test server but with a HostKey that doesn't match the server's actual
// host key. The resulting host-key-mismatch error is terminal, not
// network-class, so the reconciler returns 0s requeue.
func TestMachineRuntimeInstanceCreated_TerminalError(t *testing.T) {
	key := machinetest.NewEncryptionKey(t)
	serverSigner := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, serverSigner, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	// Pin a different host key on the MRI to force a mismatch.
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

	delay, err := v0MachineRuntimeInstanceCreated(r, mri, &log)
	require.Error(t, err)
	assert.Equal(t, int64(0), delay, "terminal errors must not be retried")
	assert.Equal(t, []string{"SSHConnectFailed"}, recorder.GetReasons())
	assert.Contains(t, strings.ToLower(err.Error()), "host key mismatch")
}

// TestMachineRuntimeInstanceUpdated_NoOp confirms Updated returns (0, nil)
// for the current stub implementation.
func TestMachineRuntimeInstanceUpdated_NoOp(t *testing.T) {
	log := logr.Discard()
	delay, err := v0MachineRuntimeInstanceUpdated(&controller.Reconciler{}, &v0.MachineRuntimeInstance{}, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// TestMachineRuntimeInstanceDeleted_NoOp confirms Deleted returns (0, nil)
// for the current stub implementation.
func TestMachineRuntimeInstanceDeleted_NoOp(t *testing.T) {
	log := logr.Discard()
	delay, err := v0MachineRuntimeInstanceDeleted(&controller.Reconciler{}, &v0.MachineRuntimeInstance{}, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}

// hostKeyBase64 returns the base64-encoded marshalled public key matching
// buildHostKeyCallback's verification-mode encoding.
func hostKeyBase64(signer interface{ PublicKey() ssh.PublicKey }) string {
	return base64.StdEncoding.EncodeToString(signer.PublicKey().Marshal())
}
