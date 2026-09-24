package v0

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	client_lib "github.com/threeport/threeport/pkg/client/lib/v0"
)

// intPtr and boolPtr build addressable literals inline; keep local so the test
// file doesn't pull util in.
func intPtr(i int) *int    { return &i }
func boolPtr(b bool) *bool { return &b }

// TestGetGatewayHttpPortsByGatewayDefinitionId_HappyPath asserts a single-page
// 200 response decodes into a populated []GatewayHttpPort and the request URL
// carries the gatewaydefinitionid query parameter.
func TestGetGatewayHttpPortsByGatewayDefinitionId_HappyPath(t *testing.T) {
	// setup: server captures the URL, returns one HTTP port on a single page
	var gotPath string
	var callCount int
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		writeOKResponse(t, w, []apiserver_lib.Object{
			v0.GatewayHttpPort{Port: intPtr(80), TLSEnabled: boolPtr(false)},
		})
	})
	defer server.Close()

	// action: call the lookup with a gateway-definition ID
	got, err := GetGatewayHttpPortsByGatewayDefinitionId(&http.Client{}, apiAddr, 42)

	// assert: no error, one port decoded with the expected Port value
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(*got) != 1 {
		t.Fatalf("expected 1 port, got %+v", got)
	}
	if (*got)[0].Port == nil || *(*got)[0].Port != 80 {
		t.Errorf("expected Port=80, got %+v", (*got)[0].Port)
	}

	// assert: URL targets the gateway-http-ports endpoint with the id query
	if !strings.Contains(gotPath, "/v0/gateway-http-ports") {
		t.Errorf("expected gateway-http-ports path, got %q", gotPath)
	}
	if !strings.Contains(gotPath, "gatewaydefinitionid=42") {
		t.Errorf("expected gatewaydefinitionid=42 in query, got %q", gotPath)
	}

	// assert: HasMore=false leaves only one API call
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}
}

// TestGetGatewayHttpPortsByGatewayDefinitionId_MultiPage covers the pagination
// loop: the first response signals HasMore with a queryid and cursor, the
// second response terminates the loop; both pages' ports are concatenated.
func TestGetGatewayHttpPortsByGatewayDefinitionId_MultiPage(t *testing.T) {
	// setup: server serves two pages; page 1 advertises HasMore with queryid
	// and cursor, page 2 terminates; capture raw queries for assertions
	var rawQueries []string
	call := 0
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		rawQueries = append(rawQueries, r.URL.RawQuery)
		call++
		var resp apiserver_lib.Response
		switch call {
		case 1:
			resp = apiserver_lib.Response{
				Data: []apiserver_lib.Object{v0.GatewayHttpPort{Port: intPtr(80)}},
				Meta: apiserver_lib.Meta{
					Pagination: apiserver_lib.Pagination{
						HasMore:    true,
						NextCursor: 9,
						QueryId:    "q-http",
					},
				},
			}
		case 2:
			resp = apiserver_lib.Response{
				Data: []apiserver_lib.Object{v0.GatewayHttpPort{Port: intPtr(8080)}},
				Meta: apiserver_lib.Meta{
					Pagination: apiserver_lib.Pagination{HasMore: false},
				},
			}
		default:
			t.Fatalf("unexpected extra API call: %d", call)
		}
		body, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("failed to marshal response: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(body); err != nil {
			t.Fatalf("failed to write body: %v", err)
		}
	})
	defer server.Close()

	// action: call the lookup
	got, err := GetGatewayHttpPortsByGatewayDefinitionId(&http.Client{}, apiAddr, 42)

	// assert: both pages decoded into the returned slice in order
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(*got) != 2 {
		t.Fatalf("expected 2 ports, got %+v", got)
	}
	if *(*got)[0].Port != 80 || *(*got)[1].Port != 8080 {
		t.Errorf("expected ports [80, 8080], got [%v, %v]", (*got)[0].Port, (*got)[1].Port)
	}

	// assert: exactly two API calls
	if call != 2 {
		t.Errorf("expected 2 API calls, got %d", call)
	}

	// assert: first request carries only gatewaydefinitionid; second appends
	// queryid and cursor from the first response's pagination
	if strings.Contains(rawQueries[0], "queryid=") || strings.Contains(rawQueries[0], "cursor=") {
		t.Errorf("first request should not carry queryid/cursor, got %q", rawQueries[0])
	}
	if !strings.Contains(rawQueries[1], "queryid=q-http") {
		t.Errorf("expected queryid in follow-up query, got %q", rawQueries[1])
	}
	if !strings.Contains(rawQueries[1], "cursor=9") {
		t.Errorf("expected cursor=9 in follow-up query, got %q", rawQueries[1])
	}
	if !strings.Contains(rawQueries[1], "gatewaydefinitionid=42") {
		t.Errorf("expected gatewaydefinitionid preserved in follow-up, got %q", rawQueries[1])
	}
}

// TestGetGatewayHttpPortsByGatewayDefinitionId_EmptyResult asserts that a
// single-page 200 with no data returns an empty (non-nil) slice.
func TestGetGatewayHttpPortsByGatewayDefinitionId_EmptyResult(t *testing.T) {
	// setup: server returns an empty page with HasMore=false
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeOKResponse(t, w, []apiserver_lib.Object{})
	})
	defer server.Close()

	// action: call the lookup
	got, err := GetGatewayHttpPortsByGatewayDefinitionId(&http.Client{}, apiAddr, 42)

	// assert: no error, non-nil slice pointer with zero entries
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil slice pointer")
	}
	if len(*got) != 0 {
		t.Errorf("expected 0 ports, got %d", len(*got))
	}
}

// TestGetGatewayHttpPortsByGatewayDefinitionId_APIError asserts that a non-200
// status surfaces the client-lib sentinel through the wrapped error chain and
// pagination stops on the first failure.
func TestGetGatewayHttpPortsByGatewayDefinitionId_APIError(t *testing.T) {
	// setup: server returns 404 with a threeport-shaped error body
	var callCount int
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		writeErrorResponse(t, w, http.StatusNotFound, "no ports")
	})
	defer server.Close()

	// action: call the lookup
	_, err := GetGatewayHttpPortsByGatewayDefinitionId(&http.Client{}, apiAddr, 42)

	// assert: sentinel ErrObjectNotFound reachable via errors.Is
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrObjectNotFound) {
		t.Errorf("expected ErrObjectNotFound in chain, got %v", err)
	}

	// assert: pagination stopped after the first failing call
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}
}

// TestGetGatewayTcpPortsByGatewayDefinitionId_HappyPath asserts a single-page
// 200 response decodes into a populated []GatewayTcpPort and the request URL
// carries the gatewaydefinitionid query parameter.
func TestGetGatewayTcpPortsByGatewayDefinitionId_HappyPath(t *testing.T) {
	// setup: server captures the URL, returns one TCP port on a single page
	var gotPath string
	var callCount int
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		writeOKResponse(t, w, []apiserver_lib.Object{
			v0.GatewayTcpPort{Port: intPtr(6379), TLSEnabled: boolPtr(true)},
		})
	})
	defer server.Close()

	// action: call the lookup
	got, err := GetGatewayTcpPortsByGatewayDefinitionId(&http.Client{}, apiAddr, 7)

	// assert: no error, one port decoded with the expected Port and TLSEnabled
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(*got) != 1 {
		t.Fatalf("expected 1 port, got %+v", got)
	}
	if (*got)[0].Port == nil || *(*got)[0].Port != 6379 {
		t.Errorf("expected Port=6379, got %+v", (*got)[0].Port)
	}
	if (*got)[0].TLSEnabled == nil || !*(*got)[0].TLSEnabled {
		t.Errorf("expected TLSEnabled=true, got %+v", (*got)[0].TLSEnabled)
	}

	// assert: URL targets the gateway-tcp-ports endpoint with the id query
	if !strings.Contains(gotPath, "/v0/gateway-tcp-ports") {
		t.Errorf("expected gateway-tcp-ports path, got %q", gotPath)
	}
	if !strings.Contains(gotPath, "gatewaydefinitionid=7") {
		t.Errorf("expected gatewaydefinitionid=7 in query, got %q", gotPath)
	}

	// assert: HasMore=false leaves only one API call
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}
}

// TestGetGatewayTcpPortsByGatewayDefinitionId_MultiPage covers the pagination
// loop: page 1 advertises HasMore with queryid and cursor, page 2 terminates.
func TestGetGatewayTcpPortsByGatewayDefinitionId_MultiPage(t *testing.T) {
	// setup: two-page server; capture raw queries to assert the follow-up URL
	// carries queryid and cursor
	var rawQueries []string
	call := 0
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		rawQueries = append(rawQueries, r.URL.RawQuery)
		call++
		var resp apiserver_lib.Response
		switch call {
		case 1:
			resp = apiserver_lib.Response{
				Data: []apiserver_lib.Object{v0.GatewayTcpPort{Port: intPtr(5432)}},
				Meta: apiserver_lib.Meta{
					Pagination: apiserver_lib.Pagination{
						HasMore:    true,
						NextCursor: 3,
						QueryId:    "q-tcp",
					},
				},
			}
		case 2:
			resp = apiserver_lib.Response{
				Data: []apiserver_lib.Object{v0.GatewayTcpPort{Port: intPtr(5433)}},
				Meta: apiserver_lib.Meta{
					Pagination: apiserver_lib.Pagination{HasMore: false},
				},
			}
		default:
			t.Fatalf("unexpected extra API call: %d", call)
		}
		body, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("failed to marshal response: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(body); err != nil {
			t.Fatalf("failed to write body: %v", err)
		}
	})
	defer server.Close()

	// action: call the lookup
	got, err := GetGatewayTcpPortsByGatewayDefinitionId(&http.Client{}, apiAddr, 99)

	// assert: both pages decoded in order
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(*got) != 2 {
		t.Fatalf("expected 2 ports, got %+v", got)
	}
	if *(*got)[0].Port != 5432 || *(*got)[1].Port != 5433 {
		t.Errorf("expected ports [5432, 5433], got [%v, %v]", (*got)[0].Port, (*got)[1].Port)
	}

	// assert: follow-up carries queryid and cursor
	if !strings.Contains(rawQueries[1], "queryid=q-tcp") {
		t.Errorf("expected queryid=q-tcp in follow-up query, got %q", rawQueries[1])
	}
	if !strings.Contains(rawQueries[1], "cursor=3") {
		t.Errorf("expected cursor=3 in follow-up query, got %q", rawQueries[1])
	}
}

// TestGetGatewayTcpPortsByGatewayDefinitionId_APIError asserts a non-200
// surfaces the client-lib sentinel through the wrapped error chain.
func TestGetGatewayTcpPortsByGatewayDefinitionId_APIError(t *testing.T) {
	// setup: server returns 401 with a threeport-shaped error body
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusUnauthorized, "no token")
	})
	defer server.Close()

	// action: call the lookup
	_, err := GetGatewayTcpPortsByGatewayDefinitionId(&http.Client{}, apiAddr, 1)

	// assert: sentinel ErrUnauthorized reachable via errors.Is
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, client_lib.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized in chain, got %v", err)
	}
}

// TestGetGatewayHttpAndTcpPortsByGatewayDefinitionId_HappyPath asserts that
// both underlying calls succeed and their results are returned in order.
func TestGetGatewayHttpAndTcpPortsByGatewayDefinitionId_HappyPath(t *testing.T) {
	// setup: server dispatches on path so the same handler serves both
	// endpoint URLs with distinct payloads
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, v0.PathGatewayHttpPorts):
			writeOKResponse(t, w, []apiserver_lib.Object{
				v0.GatewayHttpPort{Port: intPtr(443), TLSEnabled: boolPtr(true)},
			})
		case strings.HasSuffix(r.URL.Path, v0.PathGatewayTcpPorts):
			writeOKResponse(t, w, []apiserver_lib.Object{
				v0.GatewayTcpPort{Port: intPtr(5432), TLSEnabled: boolPtr(false)},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})
	defer server.Close()

	// action: fetch both port sets in one call
	httpPorts, tcpPorts, err := GetGatewayHttpAndTcpPortsByGatewayDefinitionId(&http.Client{}, apiAddr, 42)

	// assert: no error, both slices populated with expected values
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if httpPorts == nil || len(*httpPorts) != 1 {
		t.Fatalf("expected 1 http port, got %+v", httpPorts)
	}
	if tcpPorts == nil || len(*tcpPorts) != 1 {
		t.Fatalf("expected 1 tcp port, got %+v", tcpPorts)
	}
	if *(*httpPorts)[0].Port != 443 {
		t.Errorf("expected http Port=443, got %v", (*httpPorts)[0].Port)
	}
	if *(*tcpPorts)[0].Port != 5432 {
		t.Errorf("expected tcp Port=5432, got %v", (*tcpPorts)[0].Port)
	}
}

// TestGetGatewayHttpAndTcpPortsByGatewayDefinitionId_HttpError asserts that
// an HTTP-port fetch failure surfaces with nil, nil, err and does NOT then
// call the TCP endpoint.
func TestGetGatewayHttpAndTcpPortsByGatewayDefinitionId_HttpError(t *testing.T) {
	// setup: server 500s on the http-ports path; tcp path should never be hit
	var tcpCalled bool
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, v0.PathGatewayTcpPorts) {
			tcpCalled = true
		}
		writeErrorResponse(t, w, http.StatusInternalServerError, "boom")
	})
	defer server.Close()

	// action: call the combined lookup
	httpPorts, tcpPorts, err := GetGatewayHttpAndTcpPortsByGatewayDefinitionId(&http.Client{}, apiAddr, 42)

	// assert: both returned slices are nil and the error is surfaced
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if httpPorts != nil || tcpPorts != nil {
		t.Errorf("expected nil,nil slices on error, got httpPorts=%v tcpPorts=%v", httpPorts, tcpPorts)
	}

	// assert: TCP endpoint was not called after HTTP failure
	if tcpCalled {
		t.Error("expected TCP endpoint to be skipped after HTTP failure")
	}
}

// TestGetGatewayHttpAndTcpPortsByGatewayDefinitionId_TcpError asserts that
// HTTP success followed by TCP failure returns nil, nil, err.
func TestGetGatewayHttpAndTcpPortsByGatewayDefinitionId_TcpError(t *testing.T) {
	// setup: server serves the http-ports path OK, 404s the tcp-ports path
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, v0.PathGatewayHttpPorts):
			writeOKResponse(t, w, []apiserver_lib.Object{
				v0.GatewayHttpPort{Port: intPtr(80)},
			})
		case strings.HasSuffix(r.URL.Path, v0.PathGatewayTcpPorts):
			writeErrorResponse(t, w, http.StatusNotFound, "no tcp")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})
	defer server.Close()

	// action: call the combined lookup
	httpPorts, tcpPorts, err := GetGatewayHttpAndTcpPortsByGatewayDefinitionId(&http.Client{}, apiAddr, 42)

	// assert: error surfaced and returned slices are nil
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if httpPorts != nil || tcpPorts != nil {
		t.Errorf("expected nil,nil slices on tcp error, got httpPorts=%v tcpPorts=%v", httpPorts, tcpPorts)
	}
}

// TestGetGatewayPortsAsString covers the HTTP/TCP protocol-selection branches:
// http-with-nil-tls => "http", http-with-tls-true => "https", tcp-with-nil-tls
// => "tcp", tcp-with-tls-true => "tls"; the returned string is a comma-joined
// list in http-first order.
func TestGetGatewayPortsAsString(t *testing.T) {
	// setup: server returns two http ports (one plain, one TLS) and two tcp
	// ports (one plain, one TLS)
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, v0.PathGatewayHttpPorts):
			writeOKResponse(t, w, []apiserver_lib.Object{
				v0.GatewayHttpPort{Port: intPtr(80)},
				v0.GatewayHttpPort{Port: intPtr(443), TLSEnabled: boolPtr(true)},
			})
		case strings.HasSuffix(r.URL.Path, v0.PathGatewayTcpPorts):
			writeOKResponse(t, w, []apiserver_lib.Object{
				v0.GatewayTcpPort{Port: intPtr(5432)},
				v0.GatewayTcpPort{Port: intPtr(6379), TLSEnabled: boolPtr(true)},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})
	defer server.Close()

	// action: format the ports as a string
	got, err := GetGatewayPortsAsString(&http.Client{}, apiAddr, 42)

	// assert: no error, string is a comma-joined list in http-first order with
	// the expected protocol tags per port
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "http/80,https/443,tcp/5432,tls/6379"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestGetGatewayPortsAsString_TLSFalseIsPlain asserts that a TLSEnabled pointer
// set to false (distinct from nil) still selects the plain protocol.
func TestGetGatewayPortsAsString_TLSFalseIsPlain(t *testing.T) {
	// setup: server returns one http and one tcp port, both with TLSEnabled=false
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, v0.PathGatewayHttpPorts):
			writeOKResponse(t, w, []apiserver_lib.Object{
				v0.GatewayHttpPort{Port: intPtr(8080), TLSEnabled: boolPtr(false)},
			})
		case strings.HasSuffix(r.URL.Path, v0.PathGatewayTcpPorts):
			writeOKResponse(t, w, []apiserver_lib.Object{
				v0.GatewayTcpPort{Port: intPtr(9000), TLSEnabled: boolPtr(false)},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})
	defer server.Close()

	// action: format the ports as a string
	got, err := GetGatewayPortsAsString(&http.Client{}, apiAddr, 1)

	// assert: TLSEnabled=false yields the plain protocol tag on both slices
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "http/8080,tcp/9000"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestGetGatewayPortsAsString_Empty asserts that no ports on either side
// returns an empty string without error.
func TestGetGatewayPortsAsString_Empty(t *testing.T) {
	// setup: server returns empty data on both endpoints
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeOKResponse(t, w, []apiserver_lib.Object{})
	})
	defer server.Close()

	// action: format the ports as a string
	got, err := GetGatewayPortsAsString(&http.Client{}, apiAddr, 42)

	// assert: no error and the joined string is empty
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// TestGetGatewayPortsAsString_Error asserts that an underlying HTTP-ports
// failure surfaces as an empty string plus a non-nil error.
func TestGetGatewayPortsAsString_Error(t *testing.T) {
	// setup: server returns 500 on every path so the http fetch fails
	server, apiAddr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeErrorResponse(t, w, http.StatusInternalServerError, "boom")
	})
	defer server.Close()

	// action: format the ports as a string
	got, err := GetGatewayPortsAsString(&http.Client{}, apiAddr, 42)

	// assert: empty string and error surfaced
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got != "" {
		t.Errorf("expected empty string on error, got %q", got)
	}
}
