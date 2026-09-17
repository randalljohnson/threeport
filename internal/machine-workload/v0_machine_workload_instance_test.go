package machineworkload

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	logr "github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	wlstatus "github.com/threeport/threeport/internal/kubernetes-workload/status"
	"github.com/threeport/threeport/internal/machinetest"
	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	"github.com/threeport/threeport/pkg/encryption/v0"
	tp_errors "github.com/threeport/threeport/pkg/errors/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// dialSSH opens an ssh.Client with password auth and insecure host keys for the in-process SSH fixture.
func dialSSH(t *testing.T, addr, user, password string) *ssh.Client {
	t.Helper()
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         3 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	require.NoError(t, err)
	return client
}

// TestBuildScript covers set -e, optional cd, env export quoting, and trailing newlines.
func TestBuildScript(t *testing.T) {
	cases := []struct {
		name       string
		script     string
		workingDir string
		env        []string
		wantHas    []string
		wantNotHas []string
	}{
		{
			name:    "minimal",
			script:  "echo hi",
			wantHas: []string{"set -e\n", "echo hi\n"},
		},
		{
			name:       "with working dir",
			script:     "ls\n",
			workingDir: "/var/log",
			wantHas:    []string{"cd '/var/log'\n", "ls\n"},
		},
		{
			name:    "exports env entries with value quoting",
			script:  "true",
			env:     []string{"KEY=plain", "QUOTED=a b c"},
			wantHas: []string{"export KEY='plain'\n", "export QUOTED='a b c'\n"},
		},
		{
			name:    "env with single quote in value is shell-safe",
			script:  "true",
			env:     []string{"K=val'ue"},
			wantHas: []string{`export K='val'\''ue'`},
		},
		{
			name:       "empty env entries are skipped",
			script:     "true",
			env:        []string{"", "OK=1"},
			wantHas:    []string{"export OK='1'\n"},
			wantNotHas: []string{"export =", "export ''"},
		},
		{
			name:    "env without = exports bare name",
			script:  "true",
			env:     []string{"NOEQUALS"},
			wantHas: []string{"export NOEQUALS\n"},
		},
		{
			name:    "appends trailing newline when script has none",
			script:  "echo done",
			wantHas: []string{"echo done\n"},
		},
		{
			name:       "preserves existing trailing newline without doubling",
			script:     "echo done\n",
			wantHas:    []string{"echo done\n"},
			wantNotHas: []string{"echo done\n\n"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildScript(c.script, c.workingDir, c.env)
			for _, want := range c.wantHas {
				assert.Contains(t, got, want, "buildScript output should contain %q", want)
			}
			for _, notWant := range c.wantNotHas {
				assert.NotContains(t, got, notWant, "buildScript output should NOT contain %q", notWant)
			}
		})
	}
}

// TestShellQuote covers single-quote wrapping and embedded-quote escaping.
func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"plain":      "'plain'",
		"":           "''",
		"a b c":      "'a b c'",
		"don't":      `'don'\''t'`,
		"''":         `''\'''\'''`,
		"$VAR `cmd`": "'$VAR `cmd`'",
	}
	for in, want := range cases {
		assert.Equal(t, want, shellQuote(in), "shellQuote(%q)", in)
	}
}

// TestTruncateMessage covers pass-through under the limit and the truncation marker over it.
func TestTruncateMessage(t *testing.T) {
	short := strings.Repeat("a", 10)
	assert.Equal(t, short, truncateMessage(short), "short message should pass through unchanged")

	overLimit := strings.Repeat("b", maxEventMessageChars+50)
	got := truncateMessage(overLimit)
	assert.Less(t, len(got), len(overLimit), "overlong message should be shorter after truncation")
	assert.True(t, strings.HasSuffix(got, "...[truncated]"), "truncation marker should be appended")

	// leave an exactly-at-limit message unchanged
	exact := strings.Repeat("c", maxEventMessageChars)
	assert.Equal(t, exact, truncateMessage(exact), "exactly-at-limit message should pass through unchanged")
}

// TestSanitizeScriptOutput covers ANSI stripping and carriage-return progress collapse.
func TestSanitizeScriptOutput(t *testing.T) {
	assert.Equal(t, "", sanitizeScriptOutput(""))

	withAnsi := "before \x1b[31mred text\x1b[0m after"
	assert.Equal(t, "before red text after", sanitizeScriptOutput(withAnsi))

	progressLine := "10%\r50%\r100% done\nnext line"
	got := sanitizeScriptOutput(progressLine)
	assert.Contains(t, got, "100% done", "the final state of a carriage-return-redrawn line should survive")
	assert.NotContains(t, got, "10%", "intermediate progress states should be collapsed")
	assert.NotContains(t, got, "50%", "intermediate progress states should be collapsed")
	assert.Contains(t, got, "next line")
}

// TestRunRemoteScript_ConnectionError covers NewSession failing on a closed client.
func TestRunRemoteScript_ConnectionError(t *testing.T) {
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	client := dialSSH(t, addr, "u", "p")
	// close the client so NewSession fails
	require.NoError(t, client.Close(), "manual close to invalidate the client before runRemoteScript")

	stdout, stderr, exitCode, timedOut, err := runRemoteScript(
		client,
		"true",
		"sh",
		"",
		nil,
		nil,
	)
	assert.Error(t, err, "runRemoteScript should surface the NewSession failure as a connection error")
	assert.Equal(t, -1, exitCode)
	assert.False(t, timedOut)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

// TestRunRemoteScript_HappyPath covers a zero-exit script with no transport error.
func TestRunRemoteScript_HappyPath(t *testing.T) {
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 0})
	defer stop()

	client := dialSSH(t, addr, "u", "p")
	defer client.Close()

	_, _, exitCode, timedOut, err := runRemoteScript(client, "echo ok\n", "sh", "", nil, nil)
	require.NoError(t, err, "happy-path script must return no transport error")
	assert.Equal(t, 0, exitCode)
	assert.False(t, timedOut)
}

// TestRunRemoteScript_ScriptFailed covers a non-zero exit without a transport error.
func TestRunRemoteScript_ScriptFailed(t *testing.T) {
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{ExitCode: 7})
	defer stop()

	client := dialSSH(t, addr, "u", "p")
	defer client.Close()

	_, _, exitCode, timedOut, err := runRemoteScript(client, "false\n", "sh", "", nil, nil)
	require.NoError(t, err, "non-zero exit is not a transport error")
	assert.Equal(t, 7, exitCode)
	assert.False(t, timedOut)
}

// TestRunRemoteScript_Timeout covers killing a long-running script and setting timedOut.
func TestRunRemoteScript_Timeout(t *testing.T) {
	signer := machinetest.NewSigner(t)
	addr, stop := machinetest.StartSSHServer(t, signer, "u", "p", machinetest.SSHOpts{HoldSession: 2 * time.Second})
	defer stop()

	client := dialSSH(t, addr, "u", "p")
	defer client.Close()

	one := 1
	start := time.Now()
	_, _, exitCode, timedOut, err := runRemoteScript(client, "sleep 30\n", "sh", "", nil, &one)
	elapsed := time.Since(start)

	assert.True(t, timedOut, "expected timedOut=true when session exceeds timeout")
	assert.Equal(t, -1, exitCode)
	assert.Less(t, elapsed, 5*time.Second, "runRemoteScript should return shortly after the 1s deadline, not wait the full 30s")
	// discard err; timedOut is the signal
	_ = err
}

// fixture is the API stub, SSH server, MWD/MWI objects, and Reconciler one test uses.
// Tests mutate the objects before calling the handler.
type fixture struct {
	t         *testing.T
	api       *machinetest.APIStub
	recorder  *machinetest.FakeRecorder
	r         *controller.Reconciler
	mri       *v0.MachineRuntimeInstance
	mwd       *v0.MachineWorkloadDefinition
	mwi       *v0.MachineWorkloadInstance
	patches   [][]byte
	patchesMu sync.Mutex
	stopSSH   func()
}

// newFixture starts the SSH server and registers GET MWD, GET MRI, and PATCH MWI stubs.
func newFixture(t *testing.T, opts machinetest.SSHOpts) *fixture {
	t.Helper()
	key := machinetest.NewEncryptionKey(t)
	signer := machinetest.NewSigner(t)
	addr, stopSSH := machinetest.StartSSHServer(t, signer, "u", "p", opts)

	mri := machinetest.MRIFromAddr(t, 100, "mri-fixture", addr, "u", "p", key)
	mwd := &v0.MachineWorkloadDefinition{
		Common:       v0.Common{ID: util.Ptr(uint(200))},
		Definition:   v0.Definition{Name: util.Ptr("mwd-fixture")},
		CreateScript: util.Ptr("echo create"),
		UpdateScript: util.Ptr("echo update"),
		DeleteScript: util.Ptr("echo delete"),
	}
	mwi := &v0.MachineWorkloadInstance{
		Common:                      v0.Common{ID: util.Ptr(uint(300))},
		Instance:                    v0.Instance{Name: util.Ptr("mwi-fixture")},
		MachineRuntimeInstanceID:    util.Ptr(uint(100)),
		MachineWorkloadDefinitionID: util.Ptr(uint(200)),
	}

	f := &fixture{
		t:        t,
		api:      machinetest.NewAPIStub(t),
		recorder: machinetest.NewFakeRecorder(),
		mri:      mri,
		mwd:      mwd,
		mwi:      mwi,
		stopSSH:  stopSSH,
	}
	t.Cleanup(stopSSH)

	// register GET MWD
	f.api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathMachineWorkloadDefinitions, *mwd.ID),
		func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodGet, r.Method)
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{*f.mwd})
		},
	)

	// register GET MRI
	f.api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathMachineRuntimeInstances, *mri.ID),
		func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodGet, r.Method)
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{*f.mri})
		},
	)

	// register PATCH MWI
	f.api.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathMachineWorkloadInstances, *mwi.ID),
		func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodPatch, r.Method)
			body, _ := io.ReadAll(r.Body)
			f.patchesMu.Lock()
			f.patches = append(f.patches, body)
			f.patchesMu.Unlock()
			var updated v0.MachineWorkloadInstance
			require.NoError(t, json.Unmarshal(body, &updated))
			updated.ID = util.Ptr(uint(*f.mwi.ID))
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{updated})
		},
	)

	f.r = &controller.Reconciler{
		APIClient:      f.api.Client,
		APIServer:      f.api.Addr,
		EncryptionKey:  key,
		EventsRecorder: f.recorder,
	}
	return f
}

// patchedStatuses returns each PATCH body's Status in the order received.
func (f *fixture) patchedStatuses() []string {
	f.patchesMu.Lock()
	defer f.patchesMu.Unlock()
	out := make([]string, 0, len(f.patches))
	for _, body := range f.patches {
		var mwi v0.MachineWorkloadInstance
		require.NoError(f.t, json.Unmarshal(body, &mwi))
		if mwi.Status != nil {
			out = append(out, *mwi.Status)
		} else {
			out = append(out, "")
		}
	}
	return out
}

// patchedReconciled returns each PATCH body's Reconciled pointer in the order received.
func (f *fixture) patchedReconciled() []*bool {
	f.patchesMu.Lock()
	defer f.patchesMu.Unlock()
	out := make([]*bool, 0, len(f.patches))
	for _, body := range f.patches {
		var mwi v0.MachineWorkloadInstance
		require.NoError(f.t, json.Unmarshal(body, &mwi))
		out = append(out, mwi.Reconciled)
	}
	return out
}

// patchedCreationFailed returns each PATCH body's CreationFailed pointer.
func (f *fixture) patchedCreationFailed() []*bool {
	f.patchesMu.Lock()
	defer f.patchesMu.Unlock()
	out := make([]*bool, 0, len(f.patches))
	for _, body := range f.patches {
		var mwi v0.MachineWorkloadInstance
		require.NoError(f.t, json.Unmarshal(body, &mwi))
		out = append(out, mwi.CreationFailed)
	}
	return out
}

// TestMachineWorkloadInstanceCreated_HappyPath covers a zero-exit create that patches Healthy and Reconciled.
func TestMachineWorkloadInstanceCreated_HappyPath(t *testing.T) {
	f := newFixture(t, machinetest.SSHOpts{ExitCode: 0})
	log := logr.Discard()

	// run Created
	delay, err := v0MachineWorkloadInstanceCreated(f.r, f.mwi, &log)

	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)

	assert.Equal(t, []string{string(wlstatus.WorkloadInstanceStatusHealthy)}, f.patchedStatuses())

	reconciled := f.patchedReconciled()
	require.Len(t, reconciled, 1)
	require.NotNil(t, reconciled[0])
	assert.True(t, *reconciled[0], "successful create should mark Reconciled=true")

	failed := f.patchedCreationFailed()
	require.Len(t, failed, 1)
	require.NotNil(t, failed[0])
	assert.False(t, *failed[0], "successful create must set CreationFailed=false")

	assert.Empty(t, f.recorder.GetReasons(), "successful script emits no event; the wrapper's SuccessfulCreate event carries the outcome and the log line covers the diagnostic detail")
}

// TestMachineWorkloadInstanceCreated_RuntimeNotReconciled covers a 30s requeue when the MRI is unreconciled.
func TestMachineWorkloadInstanceCreated_RuntimeNotReconciled(t *testing.T) {
	f := newFixture(t, machinetest.SSHOpts{ExitCode: 0})
	f.mri.Reconciled = util.Ptr(false)
	log := logr.Discard()

	delay, err := v0MachineWorkloadInstanceCreated(f.r, f.mwi, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(30), delay, "should requeue after 30s while runtime is unreconciled")
	assert.Empty(t, f.patchedStatuses(), "should not have PATCHed any status")
	assert.Empty(t, f.recorder.GetReasons(), "should not have recorded any events")
}

// TestMachineWorkloadInstanceCreated_ScriptFails covers a non-zero create that patches Unhealthy and returns ErrWithEvent.
func TestMachineWorkloadInstanceCreated_ScriptFails(t *testing.T) {
	f := newFixture(t, machinetest.SSHOpts{ExitCode: 1})
	log := logr.Discard()

	// run Created
	delay, err := v0MachineWorkloadInstanceCreated(f.r, f.mwi, &log)

	require.Error(t, err)
	assert.Equal(t, int64(30), delay)

	assert.Equal(t, []string{string(wlstatus.WorkloadInstanceStatusUnhealthy)}, f.patchedStatuses())

	reconciled := f.patchedReconciled()
	require.Len(t, reconciled, 1)
	assert.False(t, *reconciled[0], "failed create must patch Reconciled=false")

	failed := f.patchedCreationFailed()
	require.Len(t, failed, 1)
	require.NotNil(t, failed[0])
	assert.True(t, *failed[0], "script failure must set CreationFailed=true")

	var errWithEvent *tp_errors.ErrWithEvent
	require.ErrorAs(t, err, &errWithEvent, "reconciler should return *tp_errors.ErrWithEvent so the wrapper can substitute the specific reason")
	require.NotNil(t, errWithEvent.Event.Reason)
	assert.Equal(t, "ScriptFailed", *errWithEvent.Event.Reason)

	assert.Empty(t, f.recorder.GetReasons(), "failure path should not call RecordEvent directly; the wrapper substitutes the event")
}

// TestMachineWorkloadInstanceCreated_GetDefinitionFails covers an MWD lookup failure with no SSH, PATCH, or events.
func TestMachineWorkloadInstanceCreated_GetDefinitionFails(t *testing.T) {
	f := newFixture(t, machinetest.SSHOpts{ExitCode: 0})
	// return 500 for every MWD GET
	f.api.Mux.HandleFunc(
		fmt.Sprintf("%s/", v0.PathMachineWorkloadDefinitions),
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		},
	)
	// point the MWI at an unregistered MWD id
	f.mwi.MachineWorkloadDefinitionID = util.Ptr(uint(999))
	log := logr.Discard()

	delay, err := v0MachineWorkloadInstanceCreated(f.r, f.mwi, &log)
	require.Error(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Empty(t, f.patchedStatuses())
	assert.Empty(t, f.recorder.GetReasons())
}

// TestMachineWorkloadInstanceUpdated_HappyPath covers a zero-exit update that patches Healthy and Reconciled.
func TestMachineWorkloadInstanceUpdated_HappyPath(t *testing.T) {
	f := newFixture(t, machinetest.SSHOpts{ExitCode: 0})
	log := logr.Discard()

	// run Updated
	delay, err := v0MachineWorkloadInstanceUpdated(f.r, f.mwi, &log)

	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)

	assert.Equal(t, []string{string(wlstatus.WorkloadInstanceStatusHealthy)}, f.patchedStatuses())

	reconciled := f.patchedReconciled()
	require.Len(t, reconciled, 1)
	require.NotNil(t, reconciled[0])
	assert.True(t, *reconciled[0], "successful update should mark Reconciled=true")

	assert.Empty(t, f.recorder.GetReasons(), "successful script emits no event; the wrapper's SuccessfulUpdate event carries the outcome and the log line covers the diagnostic detail")
}

// TestMachineWorkloadInstanceUpdated_ScriptFails covers a non-zero update that patches Unhealthy and returns ErrWithEvent.
func TestMachineWorkloadInstanceUpdated_ScriptFails(t *testing.T) {
	f := newFixture(t, machinetest.SSHOpts{ExitCode: 1})
	log := logr.Discard()

	// run Updated
	delay, err := v0MachineWorkloadInstanceUpdated(f.r, f.mwi, &log)

	require.Error(t, err)
	assert.Equal(t, int64(30), delay)

	assert.Equal(t, []string{string(wlstatus.WorkloadInstanceStatusUnhealthy)}, f.patchedStatuses())

	reconciled := f.patchedReconciled()
	require.Len(t, reconciled, 1)
	assert.False(t, *reconciled[0], "failed update must patch Reconciled=false")

	var errWithEvent *tp_errors.ErrWithEvent
	require.ErrorAs(t, err, &errWithEvent, "reconciler should return *tp_errors.ErrWithEvent so the wrapper can substitute the specific reason")
	require.NotNil(t, errWithEvent.Event.Reason)
	assert.Equal(t, "ScriptFailed", *errWithEvent.Event.Reason)

	assert.Empty(t, f.recorder.GetReasons(), "failure path should not call RecordEvent directly; the wrapper substitutes the event")
}

// TestMachineWorkloadInstanceUpdated_NoUpdateScript covers a nil UpdateScript returning (0, nil) with no SSH or PATCH.
func TestMachineWorkloadInstanceUpdated_NoUpdateScript(t *testing.T) {
	f := newFixture(t, machinetest.SSHOpts{ExitCode: 0})
	f.mwd.UpdateScript = nil
	log := logr.Discard()

	delay, err := v0MachineWorkloadInstanceUpdated(f.r, f.mwi, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Empty(t, f.patchedStatuses())
	assert.Empty(t, f.recorder.GetReasons())
}

// TestMachineWorkloadInstanceDeleted_HappyPath covers a zero-exit delete that returns (0, nil) without PATCHing the MWI.
func TestMachineWorkloadInstanceDeleted_HappyPath(t *testing.T) {
	f := newFixture(t, machinetest.SSHOpts{ExitCode: 0})
	log := logr.Discard()

	// run Deleted
	delay, err := v0MachineWorkloadInstanceDeleted(f.r, f.mwi, &log)

	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)

	assert.Empty(t, f.patchedStatuses(), "Deleted should not PATCH the MWI; the generated reconciler handles removal")

	assert.Empty(t, f.recorder.GetReasons(), "successful script emits no event; the wrapper's SuccessfulDelete event carries the outcome and the log line covers the diagnostic detail")
}

// TestMachineWorkloadInstanceDeleted_ScriptFails covers a non-zero delete that returns ErrWithEvent and a 30s requeue.
func TestMachineWorkloadInstanceDeleted_ScriptFails(t *testing.T) {
	f := newFixture(t, machinetest.SSHOpts{ExitCode: 1})
	log := logr.Discard()

	// run Deleted
	delay, err := v0MachineWorkloadInstanceDeleted(f.r, f.mwi, &log)

	require.Error(t, err)
	assert.Equal(t, int64(30), delay)

	var errWithEvent *tp_errors.ErrWithEvent
	require.ErrorAs(t, err, &errWithEvent, "reconciler should return *tp_errors.ErrWithEvent so the wrapper can substitute the specific reason")
	require.NotNil(t, errWithEvent.Event.Reason)
	assert.Equal(t, "ScriptFailed", *errWithEvent.Event.Reason)

	assert.Empty(t, f.recorder.GetReasons(), "failure path should not call RecordEvent directly; the wrapper substitutes the event")
}

// TestMachineWorkloadInstanceDeleted_ScriptFailsPastGracePeriod covers confirming delete after the grace period on a failing script.
func TestMachineWorkloadInstanceDeleted_ScriptFailsPastGracePeriod(t *testing.T) {
	f := newFixture(t, machinetest.SSHOpts{ExitCode: 1})
	// set DeletionScheduled past the grace period
	f.mwi.DeletionScheduled = util.Ptr(time.Now().Add(-2 * unreachableDeleteGracePeriod))
	log := logr.Discard()

	delay, err := v0MachineWorkloadInstanceDeleted(f.r, f.mwi, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
	assert.Equal(t, []string{"ScriptFailed", "DeleteScriptFailedGraceExceeded"}, f.recorder.GetReasons())
}

// TestSshDialFailed covers TCP-level connect failures versus auth and
// decrypt errors that must not trip the unreachable grace path.
func TestSshDialFailed(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	timeout := &net.OpError{Op: "dial", Net: "tcp", Err: timeoutErr{}}
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "decrypt", err: errors.New("failed to decrypt ssh password"), want: false},
		{name: "auth", err: fmt.Errorf("failed to dial ssh 127.0.0.1:22: %w", errors.New("unable to authenticate")), want: false},
		{name: "host key", err: fmt.Errorf("failed to dial ssh 127.0.0.1:22: %w", errors.New("host key mismatch")), want: false},
		{name: "refused", err: fmt.Errorf("failed to dial ssh 127.0.0.1:22: %w", refused), want: true},
		{name: "timeout", err: fmt.Errorf("failed to dial ssh 192.0.2.1:22: %w", timeout), want: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, sshDialFailed(c.err))
		})
	}
}

// timeoutErr is a net.Error that reports Timeout.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// TestMachineWorkloadInstanceDeleted_AuthFailPastGracePeriod covers a
// reachable host with a wrong password after the grace period: sshDialFailed
// is false, so delete does not confirm.
func TestMachineWorkloadInstanceDeleted_AuthFailPastGracePeriod(t *testing.T) {
	f := newFixture(t, machinetest.SSHOpts{ExitCode: 0})
	wrong, err := encryption.Encrypt(f.r.EncryptionKey, "wrong")
	require.NoError(t, err)
	f.mri.SSHPassword = util.Ptr(wrong)
	f.mwi.DeletionScheduled = util.Ptr(time.Now().Add(-2 * unreachableDeleteGracePeriod))
	log := logr.Discard()

	delay, err := v0MachineWorkloadInstanceDeleted(f.r, f.mwi, &log)
	require.Error(t, err)
	assert.Equal(t, int64(30), delay)
	assert.NotContains(t, f.recorder.GetReasons(), "DeleteScriptSkipped")
}

// TestMachineWorkloadInstanceDeleted_EventRecordFailsPastGrace covers a
// RecordEvent failure after grace: the handler still returns (0, nil) so
// a failed event write cannot pin the delete.
func TestMachineWorkloadInstanceDeleted_EventRecordFailsPastGrace(t *testing.T) {
	f := newFixture(t, machinetest.SSHOpts{ExitCode: 1})
	f.recorder.RecordErr = errors.New("nats down")
	f.mwi.DeletionScheduled = util.Ptr(time.Now().Add(-2 * unreachableDeleteGracePeriod))
	log := logr.Discard()

	delay, err := v0MachineWorkloadInstanceDeleted(f.r, f.mwi, &log)
	require.NoError(t, err)
	assert.Equal(t, int64(0), delay)
}
