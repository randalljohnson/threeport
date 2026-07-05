package util

import (
	"bufio"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	nats "github.com/nats-io/nats.go"
)

// startMockNATSServer stands up a minimal in-process TCP listener that speaks
// just enough of the NATS wire protocol for the client's Connect() handshake
// to complete: the server sends INFO on accept, absorbs CONNECT, and replies
// PONG to any PING it sees. It does not implement JetStream, so any AddStream
// request will time out or fail once the underlying Conn is closed.
func startMockNATSServer(t *testing.T) string {
	t.Helper()

	// bind to a random localhost port so parallel tests don't collide
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open mock NATS listener: %v", err)
	}
	// closing the listener stops the accept loop after the test completes
	t.Cleanup(func() { _ = ln.Close() })

	// accept loop runs until the listener is closed at cleanup
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveMockNATS(conn)
		}
	}()

	return "nats://" + ln.Addr().String()
}

// serveMockNATS handles one NATS client connection: emits the INFO greeting,
// reads incoming lines, and replies PONG to PING so the client considers the
// connection healthy. All other client protocol messages are silently ignored.
func serveMockNATS(conn net.Conn) {
	defer conn.Close()

	// send the INFO greeting the client parses to complete Connect()
	info := `INFO {"server_id":"mock","server_name":"mock","version":"2.9.0","proto":1,"go":"go1.20","host":"127.0.0.1","port":4222,"headers":true,"max_payload":1048576,"jetstream":true,"client_id":1}` + "\r\n"
	if _, err := io.WriteString(conn, info); err != nil {
		return
	}

	// read client-side protocol lines and reply PONG to PING; everything
	// else (CONNECT, SUB, PUB) is absorbed but not acted on
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		if strings.HasPrefix(line, "PING") {
			if _, err := io.WriteString(conn, "PONG\r\n"); err != nil {
				return
			}
		}
	}
}

// TestInitJetStreamReturnsErrorOnClosedConn covers InitJetStream() surfacing
// an error when the underlying NATS connection is already closed: the first
// AddStream call fails with ErrConnectionClosed and the wrapped error is
// returned instead of a stream context.
func TestInitJetStreamReturnsErrorOnClosedConn(t *testing.T) {
	// stand up a mock NATS listener so nats.Connect() succeeds without a
	// real broker; then close the resulting Conn so AddStream() will fail
	// immediately when InitJetStream calls it
	url := startMockNATSServer(t)
	nc, err := nats.Connect(
		url,
		nats.Timeout(2*time.Second),
		nats.RetryOnFailedConnect(false),
	)
	if err != nil {
		t.Fatalf("failed to connect to mock NATS: %v", err)
	}

	// closing the conn before InitJetStream runs makes each js.AddStream
	// call return nats.ErrConnectionClosed on its first request
	nc.Close()

	// invoke the function under test; expect a non-nil error and a nil
	// stream-context pointer since the very first AddStream call fails
	js, err := InitJetStream(nc)
	if err == nil {
		t.Fatalf("InitJetStream on closed conn: err = nil, want non-nil")
	}
	if js != nil {
		t.Errorf("InitJetStream on closed conn: js = %v, want nil", js)
	}

	// the error message must name the failed operation and wrap the
	// underlying cause so a caller can trace it back to AddStream
	if !strings.Contains(err.Error(), "could not add stream") {
		t.Errorf("err = %q, want mention of \"could not add stream\"", err.Error())
	}
}

// TestInitJetStreamErrorNamesFirstStream covers InitJetStream() failing on the
// first stream in its ordered AddStream sequence, so the returned error names
// that stream rather than a later one: this pins the ordering the generated
// code establishes.
func TestInitJetStreamErrorNamesFirstStream(t *testing.T) {
	// spin up a mock NATS listener, connect, then close the conn so every
	// AddStream call fails immediately in the order they run
	url := startMockNATSServer(t)
	nc, err := nats.Connect(
		url,
		nats.Timeout(2*time.Second),
		nats.RetryOnFailedConnect(false),
	)
	if err != nil {
		t.Fatalf("failed to connect to mock NATS: %v", err)
	}
	nc.Close()

	// InitJetStream stops at the first failing AddStream; the error text
	// therefore names the first stream (Secret) even though later stream
	// names would also fail in isolation
	_, err = InitJetStream(nc)
	if err == nil {
		t.Fatalf("err = nil, want non-nil on closed conn")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "secret") {
		t.Errorf("err = %q, want first-stream name \"secret\" in message", err.Error())
	}
}
