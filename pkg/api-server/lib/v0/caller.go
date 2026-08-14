package v0

import (
	"github.com/labstack/echo/v4"

	lib "github.com/threeport/threeport/pkg/api/lib/v0"
	auth "github.com/threeport/threeport/pkg/auth/v0"
)

// CaptureCaller returns middleware that stashes the caller's identity in the
// request context, where the database hooks read it to decide whether a caller
// may change rows another object owns. Pass the API server's auth-enabled flag
// rather than reading the connection, so a request that arrives without a
// client certificate on an auth-enabled server stays untrusted instead of
// inheriting control-plane privileges from a missing certificate.
func CaptureCaller(authEnabled bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if tlsState := c.Request().TLS; tlsState != nil && len(tlsState.PeerCertificates) > 0 {
				// mTLS deployment: the client presented a certificate the server
				// already verified against the control plane CA, so its subject
				// names the caller.
				subject := tlsState.PeerCertificates[0].Subject
				id := lib.CallerIdentity{CommonName: subject.CommonName}
				if len(subject.Organization) > 0 {
					id.Organization = subject.Organization[0]
				}
				if len(subject.OrganizationalUnit) > 0 {
					id.OrganizationalUnit = subject.OrganizationalUnit[0]
				}
				c.SetRequest(c.Request().WithContext(
					lib.WithCaller(c.Request().Context(), id),
				))
				return next(c)
			}

			if !authEnabled {
				// trust-the-network deployment: no client certificate exists to
				// read, so treat every caller as the control plane and let
				// internal reconcilers keep updating the rows they own.
				c.SetRequest(c.Request().WithContext(
					lib.WithCaller(c.Request().Context(), lib.CallerIdentity{
						OrganizationalUnit: auth.OUControlPlane,
					}),
				))
				return next(c)
			}

			// auth is on and this request carried no client certificate, so it
			// reached the server over some path other than the mTLS listener.
			// Leave the identity empty and let the caller be handled as external.
			return next(c)
		}
	}
}
