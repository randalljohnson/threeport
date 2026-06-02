package controller

import (
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
)

// IsNetworkErr returns true when err looks like a transient network failure
// the caller should retry on, false for terminal errors (auth, validation,
// host key mismatch, schema errors).
//
// Detection uses public sentinels rather than string matching, which is
// brittle across Go versions and platforms.
//
// errors.Is(err, io.EOF) catches a peer closing the connection mid
// handshake, where io.EOF is the public sentinel in the io package.
//
// errors.As(err, &net.Error).Timeout() catches deadline expiry on dial
// or read, where the net.Error interface in package net exposes Timeout.
//
// errors.Is against syscall sentinels catches dial and session level
// failures: ECONNREFUSED, EHOSTUNREACH, ENETUNREACH for dial against an
// unreachable port, host, or network; ECONNRESET when a peer drops an
// established connection; EPIPE when we write to a connection the peer
// already closed. These errno values live in the syscall package.
//
// errors.As(err, &*net.DNSError) with IsTemporary or IsTimeout catches a
// transient DNS resolution failure (resolver briefly unavailable). A
// permanent NXDOMAIN does not classify as retryable.
//
// Higher level libraries (golang.org/x/crypto/ssh, net/http) wrap their
// underlying net.Dial errors, so errors.Is and errors.As walk the wrap
// chain back to these sentinels.
func IsNetworkErr(err error) bool {
	if err == nil {
		return false
	}

	// catch a peer closing the connection mid handshake
	if errors.Is(err, io.EOF) {
		return true
	}

	// catch deadline expiry on dial or read via the net.Error interface
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// catch kernel level dial and session failures, in order: connection
	// refused against a closed port, no route to the host, network not
	// reachable, peer dropped an established connection, write to a
	// connection the peer already closed
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) {
		return true
	}

	// catch transient dns resolution failure, permanent NXDOMAIN stays
	// terminal so we do not retry a hostname that genuinely does not exist
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && (dnsErr.IsTemporary || dnsErr.IsTimeout) {
		return true
	}

	return false
}

// RetryOnNetworkErr maps a reconciler internal error onto the (requeue, err)
// pair returned by reconciler step functions. 30s requeue when the error
// looks like a transient network failure, 0s (terminal) otherwise. Both
// paths wrap the error with the provided message prefix using %w.
//
// Use this at API client and similar call sites where the controller
// should retry on transient transport failure but stop reconciling on
// terminal errors (validation, auth, schema mismatch).
func RetryOnNetworkErr(err error, msgPrefix string) (int64, error) {
	if IsNetworkErr(err) {
		return 30, fmt.Errorf("%s: %w", msgPrefix, err)
	}
	return 0, fmt.Errorf("%s: %w", msgPrefix, err)
}
