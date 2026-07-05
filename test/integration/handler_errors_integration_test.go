//go:build integration

package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

// buildURL joins the API address with a path, tolerating a trailing slash on
// the base URL so callers can pass either form.
func buildURL(base, path string) string {
	return strings.TrimRight(base, "/") + path
}

// TestHandler404OnUnknownResource asserts that a GET against an ID that does
// not exist returns a 404, so callers can key off the status code to
// distinguish absence from failure.
func TestHandler404OnUnknownResource(t *testing.T) {
	requireKube(t)
	apiAddr, apiClient := getAPIServerURL(t)

	// action: request a KubernetesWorkloadInstance ID that will not exist
	url := buildURL(apiAddr, "/v0/kubernetes-workload-instances/999999999")
	resp, err := apiClient.Get(url)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// assert: the API returns 404 (the client library maps this into
	// ErrObjectNotFound; we assert directly at the transport layer here)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown resource; got %d body=%s", resp.StatusCode, string(body))
	}
}

// TestHandler400OnMalformedJSON asserts that a POST with an invalid JSON body
// returns a 400, so the router does not defer syntax errors to the handler.
func TestHandler400OnMalformedJSON(t *testing.T) {
	requireKube(t)
	apiAddr, apiClient := getAPIServerURL(t)

	// action: send an unbalanced JSON payload to the create endpoint
	url := buildURL(apiAddr, "/v0/kubernetes-workload-definitions")
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString("{not-json"))
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := apiClient.Do(req)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// assert: malformed JSON must be rejected at the syntax layer as 400
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON; got %d body=%s", resp.StatusCode, string(body))
	}
}

// TestHandler400OnMissingRequiredField asserts that a POST missing a
// validate:"required" field returns 400 so the validator runs before any
// database insert.
func TestHandler400OnMissingRequiredField(t *testing.T) {
	requireKube(t)
	apiAddr, apiClient := getAPIServerURL(t)

	// action: POST an empty KubernetesWorkloadDefinition body; Name and
	// YAMLDocument are validate:"required" so validation must trip
	url := buildURL(apiAddr, "/v0/kubernetes-workload-definitions")
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString("{}"))
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := apiClient.Do(req)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// assert: missing required fields should be surfaced as 400
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing required field; got %d body=%s", resp.StatusCode, string(body))
	}
}

// TestHandler404OnUnknownPath asserts an unmatched URL yields 404 rather
// than routing into a handler that would panic on unexpected input.
func TestHandler404OnUnknownPath(t *testing.T) {
	requireKube(t)
	apiAddr, apiClient := getAPIServerURL(t)

	url := buildURL(apiAddr, "/v0/no-such-endpoint")
	resp, err := apiClient.Get(url)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown path; got %d", resp.StatusCode)
	}
}
