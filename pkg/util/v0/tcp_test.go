package v0

import (
	"net"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

// TestWaitForAPIAcceptsReachableEndpoint asserts that WaitForAPI() returns
// promptly once the endpoint on port 443 accepts a TCP connection. The
// function hardcodes port 443, which is privileged on Linux, so the test
// skips when the process cannot bind to it.
func TestWaitForAPIAcceptsReachableEndpoint(t *testing.T) {
	// stand up a real TCP listener on the hardcoded API port
	ln, err := net.Listen("tcp", "127.0.0.1:443")
	if err != nil {
		t.Skipf("cannot bind 127.0.0.1:443 without privilege: %v", err)
	}
	defer ln.Close()

	// accept and immediately close incoming connections in the background
	// so WaitForAPI's dial succeeds
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	// invoke WaitForAPI and enforce it returns quickly on a reachable endpoint
	done := make(chan struct{})
	go func() {
		WaitForAPI("127.0.0.1", logr.Discard())
		close(done)
	}()

	select {
	case <-done:
		// success: WaitForAPI returned before the retry loop could exhaust
	case <-time.After(5 * time.Second):
		t.Fatal("WaitForAPI did not return within 5s despite reachable endpoint")
	}
}
