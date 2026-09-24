package util

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	echo "github.com/labstack/echo/v4"
	version "github.com/threeport/threeport/internal/version"
)

// TestVersionRouteReturnsBinaryVersion covers VersionRoute() registering a GET
// /version handler that responds 200 with a JSON body carrying the binary's
// embedded version string.
func TestVersionRouteReturnsBinaryVersion(t *testing.T) {
	// register the /version route on a fresh echo instance so the handler
	// under test is the only one bound
	e := echo.New()
	VersionRoute(e)

	// drive the route through echo's test server so routing, JSON encoding,
	// and status code all execute end-to-end
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// the handler must return 200 OK, matching the c.JSON status arg
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}

	// the body must decode into RestApiVersion and echo the version string
	// GetVersion() returns
	var got RestApiVersion
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response body %q: %v", rec.Body.String(), err)
	}
	want := version.GetVersion()
	if got.Version != want {
		t.Errorf("Version = %q, want %q", got.Version, want)
	}
	if strings.TrimSpace(got.Version) == "" {
		t.Errorf("Version = %q, want non-empty", got.Version)
	}
}

// TestVersionRouteRejectsWrongMethod covers VersionRoute() binding only GET on
// /version, so a POST to the same path resolves to echo's method-not-allowed
// handling rather than the version handler.
func TestVersionRouteRejectsWrongMethod(t *testing.T) {
	// register the /version route on a fresh echo instance
	e := echo.New()
	VersionRoute(e)

	// hit /version with POST; echo returns 405 when the path exists but the
	// method is not registered
	req := httptest.NewRequest(http.MethodPost, "/version", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// the response must not be 200; the GET-only handler must not fire
	if rec.Code == http.StatusOK {
		t.Errorf("POST /version returned 200; VersionRoute() should bind GET only")
	}
}
