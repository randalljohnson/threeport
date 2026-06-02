package machinetest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
)

// APIStub wraps an httptest.Server and exposes the http.Client and base URL
// the threeport client helpers expect. The Mux is exposed so tests can
// register per-path handlers.
type APIStub struct {
	Server *httptest.Server
	Mux    *http.ServeMux
	Client *http.Client
	Addr   string
}

// NewAPIStub returns an APIStub with an empty mux. The Addr field has the
// "http://" scheme stripped because the threeport client helpers prepend a
// scheme themselves (see client_lib.GetResponse). Client is a bare
// *http.Client (Transport == nil) so the scheme check in GetResponse falls
// through to "http://", since srv.Client() returns one whose *http.Transport
// has TLSClientConfig set on Go versions that preconfigure it for mixed
// HTTP and HTTPS test servers, which trips GetResponse into picking
// "https://" against our HTTP server.
func NewAPIStub(t *testing.T) *APIStub {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &APIStub{
		Server: srv,
		Mux:    mux,
		Client: &http.Client{},
		Addr:   strings.TrimPrefix(srv.URL, "http://"),
	}
}

// WriteResponse marshals data into an apiserver_lib.Response envelope and
// writes it with the given status. The threeport client expects this exact
// shape from any endpoint.
func WriteResponse(t *testing.T, w http.ResponseWriter, status int, data []apiserver_lib.Object) {
	t.Helper()
	body, err := json.Marshal(apiserver_lib.Response{Data: data})
	require.NoError(t, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
