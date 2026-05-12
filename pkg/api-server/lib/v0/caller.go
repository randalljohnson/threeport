package v0

import (
	"github.com/labstack/echo/v4"

	api "github.com/threeport/threeport/pkg/api/v0"
)

// CaptureCallerCN is an Echo middleware that reads the request's mTLS peer
// common name when present and stashes it in the request context.
func CaptureCallerCN(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if tlsState := c.Request().TLS; tlsState != nil && len(tlsState.PeerCertificates) > 0 {
			if cn := tlsState.PeerCertificates[0].Subject.CommonName; cn != "" {
				c.SetRequest(c.Request().WithContext(
					api.WithCallerCN(c.Request().Context(), cn),
				))
			}
		}
		return next(c)
	}
}
