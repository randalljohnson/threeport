package v0

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAPIContext builds an echo context and recorder pair for exercising
// the ResponseStatus* helpers end-to-end (they call c.JSON, so we need
// a real ResponseWriter to capture the encoded body and status).
func newAPIContext() (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

// decodeResponse pulls the JSON body written by a helper back into a
// Response so assertions can inspect Status fields directly.
func decodeResponse(t *testing.T, rec *httptest.ResponseRecorder) Response {
	t.Helper()
	var got Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	return got
}

// TestResponseStatus200 asserts the 200 helper writes a 200 status
// code, sets the standard OK message, and clears any inbound error.
func TestResponseStatus200(t *testing.T) {
	c, rec := newAPIContext()
	// pre-populate an error to prove the helper resets it
	in := Response{Status: Status{Error: "stale"}}

	require.NoError(t, ResponseStatus200(c, in))

	// response envelope carries 200 with OK and no error
	assert.Equal(t, http.StatusOK, rec.Code)
	got := decodeResponse(t, rec)
	assert.Equal(t, http.StatusOK, got.Status.Code)
	assert.Equal(t, http.StatusText(http.StatusOK), got.Status.Message)
	assert.Equal(t, "", got.Status.Error)
}

// TestResponseStatus201 asserts the 201 helper writes a Created status
// on both the envelope and the HTTP response.
func TestResponseStatus201(t *testing.T) {
	c, rec := newAPIContext()

	require.NoError(t, ResponseStatus201(c, Response{}))

	// response envelope carries 201 Created
	assert.Equal(t, http.StatusCreated, rec.Code)
	got := decodeResponse(t, rec)
	assert.Equal(t, http.StatusCreated, got.Status.Code)
	assert.Equal(t, http.StatusText(http.StatusCreated), got.Status.Message)
}

// TestResponseStatus202 asserts the 202 helper writes an Accepted
// status on both the envelope and the HTTP response.
func TestResponseStatus202(t *testing.T) {
	c, rec := newAPIContext()

	require.NoError(t, ResponseStatus202(c, Response{}))

	// response envelope carries 202 Accepted
	assert.Equal(t, http.StatusAccepted, rec.Code)
	got := decodeResponse(t, rec)
	assert.Equal(t, http.StatusAccepted, got.Status.Code)
	assert.Equal(t, http.StatusText(http.StatusAccepted), got.Status.Message)
}

// TestResponseStatusExpected routes success codes to the matching
// helper and falls through to a 500 for anything unrecognized.
func TestResponseStatusExpected(t *testing.T) {
	tests := []struct {
		name         string
		id           int
		wantHTTP     int
		wantEnvelope int
	}{
		// 200 route delegates to the 200 helper
		{"routes-200", 200, http.StatusOK, http.StatusOK},
		// 201 route delegates to the 201 helper
		{"routes-201", 201, http.StatusCreated, http.StatusCreated},
		// unknown id falls through to a bare 500 JSON string
		{"unknown-falls-through-to-500", 999, http.StatusInternalServerError, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := newAPIContext()

			// dispatch through the switch
			require.NoError(t, ResponseStatusExpected(tt.id, c, Response{}))

			// http status matches the mapped bucket
			assert.Equal(t, tt.wantHTTP, rec.Code)

			// the fallback branch writes a bare string, not a Response,
			// so only decode when we routed to a real helper
			if tt.wantEnvelope != 0 {
				got := decodeResponse(t, rec)
				assert.Equal(t, tt.wantEnvelope, got.Status.Code)
			}
		})
	}
}

// TestResponseStatus400 asserts the 400 helper writes a BadRequest
// status and carries the wrapped error text through to the envelope.
func TestResponseStatus400(t *testing.T) {
	c, rec := newAPIContext()
	inErr := errors.New("bad input")

	require.NoError(t, ResponseStatus400(c, nil, inErr, "Widget"))

	// http status matches 400
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	got := decodeResponse(t, rec)
	// envelope status mirrors http status and carries error text
	assert.Equal(t, http.StatusBadRequest, got.Status.Code)
	assert.Equal(t, http.StatusText(http.StatusBadRequest), got.Status.Message)
	assert.Equal(t, "bad input", got.Status.Error)
	// object type propagates
	assert.Equal(t, "Widget", got.Type)
}

// TestResponseStatus401 asserts the 401 helper writes an Unauthorized
// status and carries the inbound error text.
func TestResponseStatus401(t *testing.T) {
	c, rec := newAPIContext()

	require.NoError(t, ResponseStatus401(c, nil, errors.New("no auth"), "Widget"))

	// http status matches 401
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	got := decodeResponse(t, rec)
	// envelope status carries the mapped code, message, and error
	assert.Equal(t, http.StatusUnauthorized, got.Status.Code)
	assert.Equal(t, "no auth", got.Status.Error)
}

// TestResponseStatus403 asserts the 403 helper writes a Forbidden
// status and carries the inbound error text.
func TestResponseStatus403(t *testing.T) {
	c, rec := newAPIContext()

	require.NoError(t, ResponseStatus403(c, nil, errors.New("denied"), "Widget"))

	// http status matches 403
	assert.Equal(t, http.StatusForbidden, rec.Code)
	got := decodeResponse(t, rec)
	// envelope status carries the mapped code and error text
	assert.Equal(t, http.StatusForbidden, got.Status.Code)
	assert.Equal(t, "denied", got.Status.Error)
}

// TestResponseStatus404 asserts the 404 helper writes a NotFound
// status and carries the inbound error text.
func TestResponseStatus404(t *testing.T) {
	c, rec := newAPIContext()

	require.NoError(t, ResponseStatus404(c, nil, errors.New("missing"), "Widget"))

	// http status matches 404
	assert.Equal(t, http.StatusNotFound, rec.Code)
	got := decodeResponse(t, rec)
	// envelope status carries the mapped code and error text
	assert.Equal(t, http.StatusNotFound, got.Status.Code)
	assert.Equal(t, "missing", got.Status.Error)
}

// TestResponseStatus409 asserts the 409 helper writes a Conflict
// status and carries the inbound error text.
func TestResponseStatus409(t *testing.T) {
	c, rec := newAPIContext()

	require.NoError(t, ResponseStatus409(c, nil, errors.New("conflict"), "Widget"))

	// http status matches 409
	assert.Equal(t, http.StatusConflict, rec.Code)
	got := decodeResponse(t, rec)
	// envelope status carries the mapped code and error text
	assert.Equal(t, http.StatusConflict, got.Status.Code)
	assert.Equal(t, "conflict", got.Status.Error)
}

// TestResponseStatus500 asserts the 500 helper writes an
// InternalServerError status and carries the inbound error text.
func TestResponseStatus500(t *testing.T) {
	c, rec := newAPIContext()

	require.NoError(t, ResponseStatus500(c, nil, errors.New("boom"), "Widget"))

	// http status matches 500
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	got := decodeResponse(t, rec)
	// envelope status carries the mapped code and error text
	assert.Equal(t, http.StatusInternalServerError, got.Status.Code)
	assert.Equal(t, "boom", got.Status.Error)
}

// TestResponseStatusErr routes each supported error code to the
// matching helper and falls through to a bare 500 for anything else.
func TestResponseStatusErr(t *testing.T) {
	tests := []struct {
		name         string
		id           int
		wantHTTP     int
		wantEnvelope int
	}{
		// each mapped code lands on its dedicated helper
		{"routes-400", 400, http.StatusBadRequest, http.StatusBadRequest},
		{"routes-401", 401, http.StatusUnauthorized, http.StatusUnauthorized},
		{"routes-403", 403, http.StatusForbidden, http.StatusForbidden},
		{"routes-404", 404, http.StatusNotFound, http.StatusNotFound},
		{"routes-409", 409, http.StatusConflict, http.StatusConflict},
		{"routes-500", 500, http.StatusInternalServerError, http.StatusInternalServerError},
		// unknown ids fall through to a bare 500 JSON string with no envelope
		{"unknown-falls-through-to-500", 418, http.StatusInternalServerError, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := newAPIContext()

			// dispatch through the switch
			require.NoError(t, ResponseStatusErr(tt.id, c, nil, errors.New("x"), "Widget"))

			// http status matches the mapped bucket
			assert.Equal(t, tt.wantHTTP, rec.Code)

			// fallback writes a bare string; skip envelope decode for it
			if tt.wantEnvelope != 0 {
				got := decodeResponse(t, rec)
				assert.Equal(t, tt.wantEnvelope, got.Status.Code)
				assert.Equal(t, "x", got.Status.Error)
			}
		})
	}
}

// TestVersionsExported verifies the package-level Versions map is
// initialized (not nil) so consumers can register versions on it.
func TestVersionsExported(t *testing.T) {
	// package var is initialized to an empty writable map at load time
	require.NotNil(t, Versions)
	Versions[1] = "v0"
	assert.Equal(t, "v0", Versions[1])
	delete(Versions, 1)
}
