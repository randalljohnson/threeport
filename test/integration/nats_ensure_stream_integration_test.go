//go:build integration

package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"
)

// natsURLFromEnv reads the NATS_URL used by the integration cluster; when the
// var is unset the test is skipped so the suite still passes on a laptop
// without a running NATS.
func natsURLFromEnv(t *testing.T) string {
	t.Helper()
	url := os.Getenv("NATS_URL")
	if url == "" {
		t.Skip("skipping NATS integration test: NATS_URL not set")
	}
	return url
}

// connectNATS opens a connection to NATS_URL and skips t if unreachable so a
// stopped NATS during local development does not fail the suite.
func connectNATS(t *testing.T) *nats.Conn {
	t.Helper()
	url := natsURLFromEnv(t)
	nc, err := nats.Connect(url)
	if err != nil {
		t.Skipf("skipping NATS integration test: cannot connect to %s (%v)", url, err)
	}
	return nc
}

// TestEnsureStreamIdempotentAcrossRepeatedCalls verifies that AddStream on
// the same name+subjects returns without error when invoked twice back to
// back. Threeport's stream setup relies on this idempotency at every
// rest-api and controller start-up.
func TestEnsureStreamIdempotentAcrossRepeatedCalls(t *testing.T) {
	nc := connectNATS(t)
	defer nc.Close()

	js, err := nc.JetStream(nats.PublishAsyncMaxPending(256))
	if err != nil {
		t.Fatalf("failed to build JetStream context: %v", err)
	}

	// setup: a unique stream name isolates parallel runs of the suite
	name := "integration-ensure-stream-a"
	subject := "integration.ensure.stream.a"
	defer func() { _ = js.DeleteStream(name) }()
	cfg := &nats.StreamConfig{Name: name, Subjects: []string{subject}}

	// action: two AddStream calls with the exact same config
	if _, err := js.AddStream(cfg); err != nil {
		t.Fatalf("first AddStream failed: %v", err)
	}
	// assert: the second call succeeds without an error
	if _, err := js.AddStream(cfg); err != nil {
		t.Fatalf("second AddStream should be idempotent, got: %v", err)
	}
}

// TestEnsureStreamRejectsConflictingConfig verifies the non-idempotent case:
// re-adding a stream with a different subject list should raise an error,
// so a caller mis-managing config doesn't silently corrupt the stream.
func TestEnsureStreamRejectsConflictingConfig(t *testing.T) {
	nc := connectNATS(t)
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("failed to build JetStream context: %v", err)
	}

	name := "integration-ensure-stream-b"
	defer func() { _ = js.DeleteStream(name) }()

	// setup: create the stream with subject list A
	if _, err := js.AddStream(&nats.StreamConfig{Name: name, Subjects: []string{"integration.ensure.stream.b.one"}}); err != nil {
		t.Fatalf("initial AddStream failed: %v", err)
	}

	// action: re-add with subject list B (conflict)
	_, err = js.AddStream(&nats.StreamConfig{Name: name, Subjects: []string{"integration.ensure.stream.b.two"}})

	// assert: the server rejects the conflicting update or the client
	// translates it into an error mentioning "stream" (defensive substring
	// check since the exact wording changes across nats.go versions)
	if err == nil {
		t.Fatal("expected error when re-adding stream with conflicting subjects")
	}
	if !errors.Is(err, nats.ErrStreamNameAlreadyInUse) && !strings.Contains(strings.ToLower(err.Error()), "stream") {
		t.Fatalf("expected stream-conflict error, got %v", err)
	}
}

// TestEnsureStreamRepeatsWithConsumers extends the idempotency check to the
// consumer surface: adding a consumer to an already-existing stream should
// not fail on the second identical call.
func TestEnsureStreamRepeatsWithConsumers(t *testing.T) {
	nc := connectNATS(t)
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("failed to build JetStream context: %v", err)
	}

	name := "integration-ensure-stream-c"
	subject := "integration.ensure.stream.c"
	defer func() { _ = js.DeleteStream(name) }()

	if _, err := js.AddStream(&nats.StreamConfig{Name: name, Subjects: []string{subject}}); err != nil {
		t.Fatalf("AddStream failed: %v", err)
	}

	// action: add a durable consumer twice with the same config
	durable := "integration-ensure-consumer"
	cfg := &nats.ConsumerConfig{Durable: durable, AckPolicy: nats.AckExplicitPolicy}
	if _, err := js.AddConsumer(name, cfg); err != nil {
		t.Fatalf("first AddConsumer failed: %v", err)
	}
	// assert: repeated identical AddConsumer should not raise an error
	if _, err := js.AddConsumer(name, cfg); err != nil {
		t.Fatalf("second AddConsumer should be idempotent, got: %v", err)
	}
}
