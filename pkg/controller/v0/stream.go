package controller

import (
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/nats-io/nats.go"
)

const (
	// streamRetryInterval is the sleep duration between stream lookup
	// attempts.
	streamRetryInterval = 2 * time.Second

	// streamMaxRetries caps the wait at ~4 minutes total, matching
	// WaitForAPI. Each retry is one StreamNames() iteration so the
	// total cost is dominated by the wait, not the lookups.
	streamMaxRetries = 60
)

// WaitForStream blocks until the named JetStream stream exists,
// retrying every 2 seconds for up to ~4 minutes. Exits with code 1
// after the retry budget so a stuck pod can restart.
func WaitForStream(js nats.JetStreamContext, streamName string, log logr.Logger) {
	log.Info("waiting for stream", "streamName", streamName)

	for i := 0; i < streamMaxRetries; i++ {
		for s := range js.StreamNames() {
			if s == streamName {
				log.Info("stream is available", "streamName", streamName)
				return
			}
		}
		time.Sleep(streamRetryInterval)
	}

	log.Error(
		fmt.Errorf("stream not found after %d retries", streamMaxRetries),
		"failed to find stream",
		"streamName", streamName,
	)
	os.Exit(1)
}
