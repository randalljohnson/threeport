package v0

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"

	lib "github.com/threeport/threeport/pkg/api/lib/v0"
)

// newEchoContext builds an Echo context wrapping req and a recorder response.
func newEchoContext(req *http.Request) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

// TestCaptureCaller_ExtractsFullIdentity covers the happy path where a
// peer cert with CommonName, Organization, and OrganizationalUnit is
// stashed on the request context in full.
func TestCaptureCaller_ExtractsFullIdentity(t *testing.T) {
	// build a request bearing a synthesized peer cert with all three fields
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{
			Subject: pkix.Name{
				CommonName:         "alice",
				Organization:       []string{"acme"},
				OrganizationalUnit: []string{"platform"},
			},
		}},
	}

	// capture the identity the middleware writes into the request context
	var got lib.CallerIdentity
	c, _ := newEchoContext(req)
	handler := CaptureCaller(func(c echo.Context) error {
		got = lib.Caller(c.Request().Context())
		return nil
	})

	// invoke the middleware chain
	err := handler(c)

	// assert the chained handler ran without error and saw the full identity
	assert.NoError(t, err)
	assert.Equal(t, "alice", got.CommonName)
	assert.Equal(t, "acme", got.Organization)
	assert.Equal(t, "platform", got.OrganizationalUnit)
}

// TestCaptureCaller_CommonNameOnly covers a peer cert that carries only
// a CommonName; Organization and OrganizationalUnit stay empty.
func TestCaptureCaller_CommonNameOnly(t *testing.T) {
	// peer cert has only CommonName populated
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{
			Subject: pkix.Name{CommonName: "bob"},
		}},
	}

	var got lib.CallerIdentity
	c, _ := newEchoContext(req)
	handler := CaptureCaller(func(c echo.Context) error {
		got = lib.Caller(c.Request().Context())
		return nil
	})

	// invoke the middleware chain
	err := handler(c)

	// assert only CommonName copied, the other two remain zero
	assert.NoError(t, err)
	assert.Equal(t, "bob", got.CommonName)
	assert.Empty(t, got.Organization)
	assert.Empty(t, got.OrganizationalUnit)
}

// TestCaptureCaller_UsesFirstOrganizationEntry covers the boundary that
// only the first Organization and OrganizationalUnit entry is stored.
func TestCaptureCaller_UsesFirstOrganizationEntry(t *testing.T) {
	// peer cert has multiple Organization / OrganizationalUnit entries
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{
			Subject: pkix.Name{
				CommonName:         "carol",
				Organization:       []string{"first-org", "second-org"},
				OrganizationalUnit: []string{"first-ou", "second-ou"},
			},
		}},
	}

	var got lib.CallerIdentity
	c, _ := newEchoContext(req)
	handler := CaptureCaller(func(c echo.Context) error {
		got = lib.Caller(c.Request().Context())
		return nil
	})

	// invoke the middleware chain
	err := handler(c)

	// assert only the first entry of each slice is used
	assert.NoError(t, err)
	assert.Equal(t, "first-org", got.Organization)
	assert.Equal(t, "first-ou", got.OrganizationalUnit)
}

// TestCaptureCaller_NoTLS covers a plain HTTP request: the request
// context stays untouched and the caller identity is the zero value.
func TestCaptureCaller_NoTLS(t *testing.T) {
	// plain HTTP request has no TLS state
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.TLS = nil

	var got lib.CallerIdentity
	var sameRequest bool
	c, _ := newEchoContext(req)
	original := c.Request()
	handler := CaptureCaller(func(c echo.Context) error {
		got = lib.Caller(c.Request().Context())
		sameRequest = c.Request() == original
		return nil
	})

	// invoke the middleware chain
	err := handler(c)

	// assert the middleware left the request alone and the caller stays zero
	assert.NoError(t, err)
	assert.Equal(t, lib.CallerIdentity{}, got)
	assert.True(t, sameRequest, "request should not be replaced when TLS is nil")
}

// TestCaptureCaller_TLSWithoutPeerCerts covers a TLS handshake that
// completed without a client cert: identity stays zero-valued.
func TestCaptureCaller_TLSWithoutPeerCerts(t *testing.T) {
	// TLS state present but no peer certificates
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: nil}

	var got lib.CallerIdentity
	var sameRequest bool
	c, _ := newEchoContext(req)
	original := c.Request()
	handler := CaptureCaller(func(c echo.Context) error {
		got = lib.Caller(c.Request().Context())
		sameRequest = c.Request() == original
		return nil
	})

	// invoke the middleware chain
	err := handler(c)

	// assert no identity extracted and the request object is unchanged
	assert.NoError(t, err)
	assert.Equal(t, lib.CallerIdentity{}, got)
	assert.True(t, sameRequest, "request should not be replaced when peer certs are empty")
}

// TestCaptureCaller_PropagatesHandlerError covers that an error returned
// by the wrapped handler surfaces unchanged.
func TestCaptureCaller_PropagatesHandlerError(t *testing.T) {
	// build a benign request; the wrapped handler will error
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	c, _ := newEchoContext(req)
	wantErr := echo.NewHTTPError(http.StatusTeapot, "nope")
	handler := CaptureCaller(func(c echo.Context) error {
		return wantErr
	})

	// invoke the middleware chain
	err := handler(c)

	// assert the middleware passes the wrapped handler's error through unchanged
	assert.Equal(t, wantErr, err)
}
