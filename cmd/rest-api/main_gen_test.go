package main

import (
	"io"
	"net/http"
	"testing"
	"time"
)

// TestConfigureHealthCheckEndpointServesReadyz covers
// configureHealthCheckEndpoint() registering a /readyz handler that returns
// 200 OK with an "OK" body, and starting a listener on :8081 in a background
// goroutine.
func TestConfigureHealthCheckEndpointServesReadyz(t *testing.T) {
	// register the /readyz handler on the default mux and start the :8081
	// listener; the function returns immediately while the goroutine boots
	configureHealthCheckEndpoint()

	// poll the /readyz endpoint until the goroutine's listener is accepting
	// connections; a fresh process rarely needs more than a few ms
	var resp *http.Response
	var err error
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://127.0.0.1:8081/readyz")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("failed to reach /readyz on :8081: %v", err)
	}
	defer resp.Body.Close()

	// the handler must respond 200 OK
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// the handler must write the literal "OK" body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if string(body) != "OK" {
		t.Errorf("body = %q, want %q", string(body), "OK")
	}
}
