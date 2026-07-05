package v0

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
)

// newTestServer returns an httptest server whose handler runs fn and a client
// that talks to it via a bare http.Client without a scheme prefix on the URL.
func newTestServer(t *testing.T, fn http.HandlerFunc) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(fn)
	// strip the http:// prefix so GetResponse re-prepends "http://".
	url := strings.TrimPrefix(srv.URL, "http://")
	return srv, url
}

// TestGetResponseSuccess covers the happy path: response body decodes into
// the Response struct and no error is returned.
func TestGetResponseSuccess(t *testing.T) {
	// server returns 200 with a valid Response JSON payload.
	srv, url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"Meta":{"ObjectCount":1},"Type":"Foo","Data":[{"ID":1}],"Status":{"code":200,"message":"OK","error":""}}`)
	})
	defer srv.Close()

	// invoke GetResponse against the test server.
	resp, err := GetResponse(srv.Client(), url, http.MethodGet, &bytes.Buffer{}, nil, http.StatusOK)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	// verify the returned Response reflects the payload.
	if resp == nil || resp.Status.Code != http.StatusOK {
		t.Fatalf("expected status 200 in response, got %#v", resp)
	}
	if resp.Type != "Foo" {
		t.Errorf("expected Type Foo, got %q", resp.Type)
	}
	if resp.Meta.ObjectCount != 1 {
		t.Errorf("expected ObjectCount 1, got %d", resp.Meta.ObjectCount)
	}
}

// TestGetResponseRequestHeaders covers that reqHeader entries are added to
// the outbound request.
func TestGetResponseRequestHeaders(t *testing.T) {
	// server asserts custom header made it through, then returns 200.
	srv, url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Custom"); got != "custom-value" {
			t.Errorf("expected X-Custom header to be forwarded, got %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"Status":{"code":200}}`)
	})
	defer srv.Close()

	// pass a reqHeader map so GetResponse forwards it.
	_, err := GetResponse(
		srv.Client(),
		url,
		http.MethodGet,
		&bytes.Buffer{},
		map[string]string{"X-Custom": "custom-value"},
		http.StatusOK,
	)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// TestGetResponseErrorMapping covers each status-code branch: 400/404/401/403/409
// map to typed sentinel errors.
func TestGetResponseErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		sentinel   error
	}{
		{"bad request", http.StatusBadRequest, ErrBadRequest},
		{"not found", http.StatusNotFound, ErrObjectNotFound},
		{"unauthorized", http.StatusUnauthorized, ErrUnauthorized},
		{"forbidden", http.StatusForbidden, ErrForbidden},
		{"conflict", http.StatusConflict, ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// server returns the case-specific error status code and a valid
			// Response envelope carrying a canned error message.
			srv, url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = io.WriteString(w, `{"Status":{"code":0,"message":"nope","error":"boom"}}`)
			})
			defer srv.Close()

			// GetResponse should surface the sentinel via errors.Is.
			_, err := GetResponse(srv.Client(), url, http.MethodGet, &bytes.Buffer{}, nil, http.StatusOK)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, tc.sentinel) {
				t.Errorf("expected err to wrap %v, got %v", tc.sentinel, err)
			}
			if !strings.Contains(err.Error(), "boom") {
				t.Errorf("expected error message to include upstream error, got %q", err.Error())
			}
		})
	}
}

// TestGetResponseObjectOwned covers the 400 branch where the upstream error
// message indicates external update is blocked; sentinel is ErrObjectOwned.
func TestGetResponseObjectOwned(t *testing.T) {
	// server returns 400 with the ExternalUpdateBlocked substring in the error.
	srv, url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		body := `{"Status":{"code":400,"message":"bad","error":"field ` + api_v0.ErrMsgExternalUpdateBlocked + `"}}`
		_, _ = io.WriteString(w, body)
	})
	defer srv.Close()

	_, err := GetResponse(srv.Client(), url, http.MethodGet, &bytes.Buffer{}, nil, http.StatusOK)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	// verify the object-owned sentinel wins over the generic bad-request sentinel.
	if !errors.Is(err, ErrObjectOwned) {
		t.Errorf("expected ErrObjectOwned, got %v", err)
	}
}

// TestGetResponseDefaultErrorBranch covers the default branch when status
// isn't one of the mapped codes: a 500 returns a formatted error not wrapping
// any sentinel.
func TestGetResponseDefaultErrorBranch(t *testing.T) {
	// server returns 500 with a full Response envelope.
	srv, url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"Status":{"code":500,"message":"Internal Server Error","error":"kaboom"}}`)
	})
	defer srv.Close()

	_, err := GetResponse(srv.Client(), url, http.MethodGet, &bytes.Buffer{}, nil, http.StatusOK)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	// verify the formatted message includes the API status text.
	if !strings.Contains(err.Error(), "API returned status") {
		t.Errorf("expected 'API returned status' framing, got %q", err.Error())
	}
}

// TestGetResponseErrorMessageFromBodyWhenStatusErrorEmpty covers the branch
// where the upstream returns an unstructured error body without a
// Status.Error field: the raw body becomes the error message.
func TestGetResponseErrorMessageFromBodyWhenStatusErrorEmpty(t *testing.T) {
	// server returns 404 with no Status.Error populated but a body string.
	srv, url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		// Status.Error empty; GetResponse should fall back to raw body text.
		_, _ = io.WriteString(w, `{"Status":{"code":404,"message":"Not Found","error":""}}`)
	})
	defer srv.Close()

	_, err := GetResponse(srv.Client(), url, http.MethodGet, &bytes.Buffer{}, nil, http.StatusOK)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	// the raw JSON body should appear in the wrapped message.
	if !strings.Contains(err.Error(), `"Not Found"`) {
		t.Errorf("expected raw body substring in error, got %q", err.Error())
	}
}

// TestGetResponseInvalidJSONErrorBody covers the branch where the API returns
// a non-success status with a body that isn't valid Response JSON: unmarshal
// fails and GetResponse surfaces a decode error.
func TestGetResponseInvalidJSONErrorBody(t *testing.T) {
	// server returns 500 with unparseable body.
	srv, url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `not json`)
	})
	defer srv.Close()

	_, err := GetResponse(srv.Client(), url, http.MethodGet, &bytes.Buffer{}, nil, http.StatusOK)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	// error should describe the unmarshal failure and include the raw body.
	if !strings.Contains(err.Error(), "failed to unmarshal") {
		t.Errorf("expected unmarshal error, got %q", err.Error())
	}
}

// TestGetResponseInvalidSuccessBody covers the branch where a 200 response
// body isn't valid Response JSON: decode fails on the success path.
func TestGetResponseInvalidSuccessBody(t *testing.T) {
	// server returns 200 with garbage.
	srv, url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `not json`)
	})
	defer srv.Close()

	_, err := GetResponse(srv.Client(), url, http.MethodGet, &bytes.Buffer{}, nil, http.StatusOK)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	// error should describe the decode failure on the success branch.
	if !strings.Contains(err.Error(), "failed to decode response body") {
		t.Errorf("expected decode error, got %q", err.Error())
	}
}

// TestGetResponseRequestBuildFailure covers the branch where http.NewRequest
// rejects the method: an invalid method returns a build error.
func TestGetResponseRequestBuildFailure(t *testing.T) {
	// invalid http method triggers NewRequest to fail.
	_, err := GetResponse(&http.Client{}, "example.test", "BAD METHOD", &bytes.Buffer{}, nil, http.StatusOK)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to build request") {
		t.Errorf("expected build error, got %q", err.Error())
	}
}

// TestGetResponseNetworkFailure covers the branch where client.Do fails,
// e.g. when the server rejects the connection.
func TestGetResponseNetworkFailure(t *testing.T) {
	// point at an address with no listener to force a Do() failure.
	_, err := GetResponse(&http.Client{}, "127.0.0.1:1", http.MethodGet, &bytes.Buffer{}, nil, http.StatusOK)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to execute call") {
		t.Errorf("expected execute-call error, got %q", err.Error())
	}
}

// TestGetResponseCustomTransportTLSPrefix covers the branch where the client
// carries a *CustomTransport with IsTlsEnabled true: the URL is prefixed with
// https:// instead of http://.
func TestGetResponseCustomTransportTLSPrefix(t *testing.T) {
	// custom transport whose RoundTripper records the URL scheme actually used.
	var seenURL string
	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		seenURL = req.URL.String()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"Status":{"code":200}}`)),
			Header:     make(http.Header),
		}, nil
	})
	client := &http.Client{Transport: &CustomTransport{CustomRoundTripper: rt, IsTlsEnabled: true}}

	_, err := GetResponse(client, "example.test", http.MethodGet, &bytes.Buffer{}, nil, http.StatusOK)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	// verify https scheme was chosen because IsTlsEnabled is true.
	if !strings.HasPrefix(seenURL, "https://") {
		t.Errorf("expected https scheme, got %q", seenURL)
	}
}

// TestGetResponseHTTPTransportTLSPrefix covers the branch where the client's
// Transport is *http.Transport with a TLSClientConfig: the URL is prefixed
// with https://.
func TestGetResponseHTTPTransportTLSPrefix(t *testing.T) {
	// stand up a TLS test server so we can use its client, which returns an
	// *http.Transport with TLSClientConfig populated.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Scheme != "" && r.TLS == nil {
			t.Errorf("expected TLS handshake")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"Status":{"code":200}}`)
	}))
	defer srv.Close()

	// the TLS server's Client() returns an http.Client whose Transport is an
	// *http.Transport with a TLSClientConfig; that's exactly what the branch
	// under test detects.
	client := srv.Client()
	if _, ok := client.Transport.(*http.Transport); !ok {
		// belt guard so a future stdlib change doesn't silently skip the branch.
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	url := strings.TrimPrefix(srv.URL, "https://")

	_, err := GetResponse(client, url, http.MethodGet, &bytes.Buffer{}, nil, http.StatusOK)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// TestGetResponseDebugPrint covers the branch where ThreeportGoClientDebug is
// truthy: GetResponse marshals and prints the decoded response.
func TestGetResponseDebugPrint(t *testing.T) {
	// enable debug mode for the duration of the test.
	t.Setenv(GoClientDebug, "true")

	srv, url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"Status":{"code":200}}`)
	})
	defer srv.Close()

	// capture stdout so the debug print doesn't leak into test output; also
	// verifies the debug branch actually wrote something.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	_, err := GetResponse(srv.Client(), url, http.MethodGet, &bytes.Buffer{}, nil, http.StatusOK)
	_ = w.Close()

	// drain the captured stdout regardless of the error state.
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(string(out), "Status") {
		t.Errorf("expected debug print to include response fields, got %q", string(out))
	}
}

// TestGetResponseUsesResponseType asserts that GetResponse round-trips a
// non-empty apiserver_lib.Response so a caller can rely on Data + Status.
func TestGetResponseUsesResponseType(t *testing.T) {
	// return a Response with populated Data + Meta so we can verify the
	// returned struct matches what came off the wire.
	srv, url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"Meta":{"ObjectCount":2},"Type":"T","Data":[{"a":1},{"b":2}],"Status":{"code":200}}`)
	})
	defer srv.Close()

	resp, err := GetResponse(srv.Client(), url, http.MethodGet, &bytes.Buffer{}, nil, http.StatusOK)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	// verify Data length and type field survived decoding.
	if resp.Type != "T" || len(resp.Data) != 2 {
		t.Errorf("unexpected decoded response: %#v", resp)
	}
	var _ apiserver_lib.Response = *resp
}

// roundTripperFunc adapts a function into an http.RoundTripper for tests.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
