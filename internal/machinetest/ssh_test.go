package machinetest

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// dialClient dials addr as user with password, using signer's public key as
// the pinned HostKey. Returns a connected *ssh.Client the caller closes.
func dialClient(t *testing.T, addr, user, password string, hostKey ssh.Signer) *ssh.Client {
	t.Helper()
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.FixedHostKey(hostKey.PublicKey()),
		Timeout:         2 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	require.NoError(t, err)
	return client
}

// TestNewSigner covers that NewSigner returns a usable signer whose public
// key marshals to a non-empty wire encoding.
func TestNewSigner(t *testing.T) {
	// generate a fresh signer
	signer := NewSigner(t)
	// signer's public key must marshal to a non-empty blob so it can be
	// used as a HostKey pinned by clients
	require.NotEmpty(t, signer.PublicKey().Marshal())
}

// TestStartSSHServer_ExecReturnsExitCode covers the exec branch of
// handleSession: the server drains stdin, then sends the configured
// ExitCode as exit-status, and the client observes it as an ExitError.
func TestStartSSHServer_ExecReturnsExitCode(t *testing.T) {
	// start the server with a non-zero exit code so we can assert the
	// exact value routed through the exit-status reply
	hostKey := NewSigner(t)
	addr, stop := StartSSHServer(t, hostKey, "u", "p", SSHOpts{ExitCode: 7})
	defer stop()

	// dial the server with matching credentials and pinned host key
	client := dialClient(t, addr, "u", "p", hostKey)
	defer client.Close()

	// open a session and run a script through stdin so the server has
	// something to drain before it replies with exit-status
	session, err := client.NewSession()
	require.NoError(t, err)
	defer session.Close()
	session.Stdin = bytes.NewBufferString("echo hi\n")

	// Run reports an *ssh.ExitError whose ExitStatus mirrors opts.ExitCode
	runErr := session.Run("whatever")
	exitErr, ok := runErr.(*ssh.ExitError)
	require.True(t, ok, "expected *ssh.ExitError, got %T: %v", runErr, runErr)
	require.Equal(t, 7, exitErr.ExitStatus())
}

// TestStartSSHServer_ExecZeroExit covers the exec branch when ExitCode is
// zero: session.Run returns nil.
func TestStartSSHServer_ExecZeroExit(t *testing.T) {
	// start with default ExitCode 0
	hostKey := NewSigner(t)
	addr, stop := StartSSHServer(t, hostKey, "u", "p", SSHOpts{})
	defer stop()

	// dial and run to hit the zero-exit path
	client := dialClient(t, addr, "u", "p", hostKey)
	defer client.Close()

	session, err := client.NewSession()
	require.NoError(t, err)
	defer session.Close()
	session.Stdin = bytes.NewBufferString("")

	// a zero exit-status is reported as a nil error
	require.NoError(t, session.Run("noop"))
}

// TestStartSSHServer_HoldSession covers the HoldSession branch of
// handleSession: the server accepts exec but never sends exit-status;
// closing the server via stop unblocks the goroutine.
func TestStartSSHServer_HoldSession(t *testing.T) {
	// hold longer than the deadline we impose below so the client sees
	// the hang, not a natural exit
	hostKey := NewSigner(t)
	addr, stop := StartSSHServer(t, hostKey, "u", "p", SSHOpts{HoldSession: 5 * time.Second})

	client := dialClient(t, addr, "u", "p", hostKey)

	session, err := client.NewSession()
	require.NoError(t, err)

	// run the exec in a goroutine and assert it hangs until stop() fires
	done := make(chan error, 1)
	go func() {
		session.Stdin = bytes.NewBufferString("")
		done <- session.Run("hang")
	}()

	// give the server enough time to accept the exec request
	select {
	case <-done:
		t.Fatal("session.Run returned before stop; HoldSession did not hold")
	case <-time.After(200 * time.Millisecond):
	}

	// close the client first so serveSSHConn's channel range unblocks,
	// then stop() unblocks the held session and drains goroutines
	_ = client.Close()
	stop()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("session.Run did not return after stop unblocked the server")
	}
}

// TestStartSSHServer_BadPassword covers the PasswordCallback rejection
// path: dialing with a wrong password fails the handshake.
func TestStartSSHServer_BadPassword(t *testing.T) {
	// register credentials that the client will fail to match
	hostKey := NewSigner(t)
	addr, stop := StartSSHServer(t, hostKey, "u", "correct", SSHOpts{})
	defer stop()

	// try to dial with the wrong password and expect a handshake error
	cfg := &ssh.ClientConfig{
		User:            "u",
		Auth:            []ssh.AuthMethod{ssh.Password("wrong")},
		HostKeyCallback: ssh.FixedHostKey(hostKey.PublicKey()),
		Timeout:         2 * time.Second,
	}
	_, err := ssh.Dial("tcp", addr, cfg)
	require.Error(t, err)
}

// TestStartSSHServer_UnknownChannelRejected covers serveSSHConn's
// non-"session" channel branch: OpenChannel returns a Rejection error.
func TestStartSSHServer_UnknownChannelRejected(t *testing.T) {
	// stand up the server and complete the handshake
	hostKey := NewSigner(t)
	addr, stop := StartSSHServer(t, hostKey, "u", "p", SSHOpts{})
	defer stop()

	client := dialClient(t, addr, "u", "p", hostKey)
	defer client.Close()

	// open a channel of an unrecognized type; server should reject it
	// via the UnknownChannelType path in serveSSHConn
	_, _, err := client.OpenChannel("direct-tcpip", nil)
	require.Error(t, err)
	rej, ok := err.(*ssh.OpenChannelError)
	require.True(t, ok, "expected *ssh.OpenChannelError, got %T: %v", err, err)
	require.Equal(t, ssh.UnknownChannelType, rej.Reason)
}

// TestStartSSHServer_UnknownRequestReplied covers handleSession's default
// branch: an unrecognized request with WantReply=true gets a false reply.
func TestStartSSHServer_UnknownRequestReplied(t *testing.T) {
	// stand up the server
	hostKey := NewSigner(t)
	addr, stop := StartSSHServer(t, hostKey, "u", "p", SSHOpts{})
	defer stop()

	client := dialClient(t, addr, "u", "p", hostKey)
	defer client.Close()

	// open a session and send a request the handler doesn't know about;
	// the default branch replies false when WantReply is set
	session, err := client.NewSession()
	require.NoError(t, err)
	defer session.Close()

	ok, err := session.SendRequest("no-such-request", true, nil)
	require.NoError(t, err)
	require.False(t, ok)
}

// TestStartSSHServer_HandshakeFailureDropsConn covers serveSSHConn's early
// return when the SSH handshake fails: a raw TCP dial that doesn't speak
// SSH is accepted, and the server closes without panicking.
func TestStartSSHServer_HandshakeFailureDropsConn(t *testing.T) {
	// stand up the server so we can dial it
	hostKey := NewSigner(t)
	addr, stop := StartSSHServer(t, hostKey, "u", "p", SSHOpts{})
	defer stop()

	// dial as a plain TCP client with no SSH handshake; the server's
	// ssh.NewServerConn returns an error and serveSSHConn returns cleanly
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	// send some non-SSH bytes and close; the server-side handler must
	// not panic when the handshake fails
	_, _ = conn.Write([]byte("not-ssh\n"))
	_ = conn.Close()

	// give the server goroutine a moment to observe the failure so that
	// stop's wg.Wait() drains cleanly when the test ends
	time.Sleep(50 * time.Millisecond)
}
