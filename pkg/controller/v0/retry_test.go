package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// timeoutNetErr is a minimal net.Error that reports Timeout() == true.
// Used in place of a real net.OpError, which has many unexported fields.
type timeoutNetErr struct{}

func (timeoutNetErr) Error() string   { return "timeout test error" }
func (timeoutNetErr) Timeout() bool   { return true }
func (timeoutNetErr) Temporary() bool { return false }

// TestIsNetworkErr covers each public sentinel IsNetworkErr recognizes,
// plus a few terminal errors that must return false.
func TestIsNetworkErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"io.EOF (direct)", io.EOF, true},
		{"io.EOF (wrapped)", fmt.Errorf("ssh handshake: %w", io.EOF), true},
		{"net.Error timeout", timeoutNetErr{}, true},
		{"net.Error timeout wrapped", fmt.Errorf("dial: %w", timeoutNetErr{}), true},
		{"ECONNREFUSED (direct)", syscall.ECONNREFUSED, true},
		{"ECONNREFUSED wrapped", fmt.Errorf("dial tcp 127.0.0.1:1: %w", syscall.ECONNREFUSED), true},
		{"EHOSTUNREACH wrapped", fmt.Errorf("dial: %w", syscall.EHOSTUNREACH), true},
		{"ENETUNREACH wrapped", fmt.Errorf("dial: %w", syscall.ENETUNREACH), true},
		{"ECONNRESET wrapped", fmt.Errorf("read: %w", syscall.ECONNRESET), true},
		{"EPIPE wrapped", fmt.Errorf("write: %w", syscall.EPIPE), true},
		{"DNS error transient", &net.DNSError{Err: "no such host", Name: "host.example", IsTemporary: true}, true},
		{"DNS error timeout", &net.DNSError{Err: "dns query timed out", Name: "host.example", IsTimeout: true}, true},
		{"DNS error permanent (NXDOMAIN)", &net.DNSError{Err: "no such host", Name: "host.example", IsNotFound: true}, false},
		{"auth failure", errors.New("ssh: unable to authenticate"), false},
		{"host key mismatch", errors.New("host key mismatch for x"), false},
		{"validation error", errors.New("bad request: invalid field"), false},
		{"context.Canceled", context.Canceled, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, IsNetworkErr(c.err))
		})
	}
}

// TestIsNetworkErr_RealDial confirms a live failed dial against a closed
// port produces an error IsNetworkErr correctly classifies. This guards
// against the wrap chain in net.Dial changing in a way that breaks
// errors.Is detection.
func TestIsNetworkErr_RealDial(t *testing.T) {
	// 127.0.0.1:1 is reserved and never bound, dial should refuse
	conn, err := net.DialTimeout("tcp", "127.0.0.1:1", 2*time.Second)
	if err == nil {
		_ = conn.Close()
		t.Skip("unexpected: port 1 was reachable")
	}
	assert.True(t, IsNetworkErr(err), "live ECONNREFUSED from net.Dial should classify as network err, got %v", err)
}

// TestRetryOnNetworkErr confirms the (30, err) and (0, err) split and that
// the message prefix wraps the original error via %w.
func TestRetryOnNetworkErr(t *testing.T) {
	t.Run("network error returns 30s requeue", func(t *testing.T) {
		original := fmt.Errorf("dial tcp: %w", syscall.ECONNREFUSED)
		delay, err := RetryOnNetworkErr(original, "failed to call api")
		assert.Equal(t, int64(30), delay)
		assert.ErrorIs(t, err, original)
		assert.Contains(t, err.Error(), "failed to call api")
	})

	t.Run("terminal error returns 0s (no retry)", func(t *testing.T) {
		original := errors.New("ssh: unable to authenticate")
		delay, err := RetryOnNetworkErr(original, "failed to call api")
		assert.Equal(t, int64(0), delay)
		assert.ErrorIs(t, err, original)
		assert.Contains(t, err.Error(), "failed to call api")
	})
}
