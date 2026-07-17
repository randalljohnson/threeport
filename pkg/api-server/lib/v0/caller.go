package v0

import (
	"github.com/labstack/echo/v4"

	lib "github.com/threeport/threeport/pkg/api/lib/v0"
	auth "github.com/threeport/threeport/pkg/auth/v0"
)

// CaptureCaller is an Echo middleware that reads the request's mTLS peer
// identity (CommonName, Organization, OrganizationalUnit) and stashes it
// in the request context. When no mTLS peer cert is present (auth-disabled
// HTTP mode), populate a synthetic control-plane identity so the ownership
// hook lets internal reconcilers update owned rows in the trust-the-network
// deployment.
func CaptureCaller(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if tlsState := c.Request().TLS; tlsState != nil && len(tlsState.PeerCertificates) > 0 {
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
		c.SetRequest(c.Request().WithContext(
			lib.WithCaller(c.Request().Context(), lib.CallerIdentity{
				OrganizationalUnit: auth.OUControlPlane,
			}),
		))
		return next(c)
	}
}
