package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_lib "github.com/threeport/threeport/pkg/api/lib/v0"
	api "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// secretVersionsOnce guards the one-time registration of the
// SecretDefinition ObjectTaggedFields entry that PayloadCheck reads
// through when translating a v0 payload.
var secretVersionsOnce sync.Once

// registerSecretVersions primes the shared apiserver_lib registry with
// SecretDefinition's v0 schema without importing the versions package
// (which would introduce an import cycle via routes -> handlers).
// This inlines what versions.AddSecretDefinitionVersions() does.
func registerSecretVersions(t *testing.T) {
	t.Helper()
	secretVersionsOnce.Do(func() {
		fields := &apiserver_lib.FieldsByTag{
			Optional:             []string{},
			OptionalAssociations: []string{},
			Required:             []string{},
			TagName:              string(api_lib.ValidateTag),
		}
		tf := map[string]*apiserver_lib.FieldsByTag{
			string(api_lib.ValidateTag): fields,
		}
		apiserver_lib.ParseStruct(
			string(api_lib.ValidateTag),
			reflect.ValueOf(new(api.SecretDefinition)),
			"",
			apiserver_lib.Translate,
			tf,
		)
		versionObj := apiserver_lib.VersionObject{
			Object:  string(api.ObjectTypeSecretDefinition),
			Version: "v0",
		}
		apiserver_lib.ObjectTaggedFields[versionObj] = fields
		apiserver_lib.AddObjectVersion(versionObj)
	})
}

// setupSecretDB returns an in-memory sqlite DB with the
// SecretDefinition table migrated. Handlers that read or write the
// secret_definitions table can be exercised end-to-end against it.
func setupSecretDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&api.SecretDefinition{}))
	return db
}

// newSecretContext builds an echo request context targeting the
// secret-definitions POST route with the given JSON body. The path is
// set so PayloadCheck's versionFromPath extracts "v0".
func newSecretContext(t *testing.T, body string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	// wire the CustomValidator with the same custom tags the api-server
	// registers at startup so ValidateBoundData exercises the real path
	v := validator.New()
	require.NoError(t, v.RegisterValidation("optional", apiserver_lib.IsOptional))
	require.NoError(t, v.RegisterValidation("association", apiserver_lib.IsAssociation))
	require.NoError(t, v.RegisterValidation("ISO8601date", apiserver_lib.IsISO8601Date))
	e.Validator = &apiserver_lib.CustomValidator{Validator: v}

	req := httptest.NewRequest(
		http.MethodPost,
		"/v0/secret-definitions",
		strings.NewReader(body),
	)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/v0/secret-definitions")
	return c, rec
}

// newSecretHandler returns a Handler wired to the given DB and a
// silent logger. NC and JS stay nil since the tested paths short-
// circuit before reaching NATS.
func newSecretHandler(db *gorm.DB) Handler {
	return Handler{
		DB:     db,
		Logger: zap.NewNop(),
	}
}

// TestAddSecretDefinitionMiddleware asserts the add path wires
// CustomAddSecretDefinition in as its sole middleware.
func TestAddSecretDefinitionMiddleware(t *testing.T) {
	h := newSecretHandler(nil)

	// resolve the middleware list once
	mws := h.AddSecretDefinitionMiddleware()

	// one entry: the custom-add middleware
	require.Len(t, mws, 1, "add path exposes a single middleware")
	assert.NotNil(t, mws[0], "the sole entry is a non-nil middleware")
}

// TestGetSecretDefinitionMiddleware asserts the read path exposes an
// empty (but non-nil) middleware chain.
func TestGetSecretDefinitionMiddleware(t *testing.T) {
	h := newSecretHandler(nil)

	// resolve the empty middleware list
	mws := h.GetSecretDefinitionMiddleware()

	// no middleware attached to reads
	assert.NotNil(t, mws, "middleware slice is initialized, not nil")
	assert.Empty(t, mws, "no read-path middleware registered")
}

// TestPatchSecretDefinitionMiddleware asserts the patch path exposes
// an empty (but non-nil) middleware chain.
func TestPatchSecretDefinitionMiddleware(t *testing.T) {
	h := newSecretHandler(nil)

	// resolve the empty middleware list
	mws := h.PatchSecretDefinitionMiddleware()

	// no middleware attached to patches
	assert.NotNil(t, mws, "middleware slice is initialized, not nil")
	assert.Empty(t, mws, "no patch-path middleware registered")
}

// TestPutSecretDefinitionMiddleware asserts the put path exposes an
// empty (but non-nil) middleware chain.
func TestPutSecretDefinitionMiddleware(t *testing.T) {
	h := newSecretHandler(nil)

	// resolve the empty middleware list
	mws := h.PutSecretDefinitionMiddleware()

	// no middleware attached to puts
	assert.NotNil(t, mws, "middleware slice is initialized, not nil")
	assert.Empty(t, mws, "no put-path middleware registered")
}

// TestDeleteSecretDefinitionMiddleware asserts the delete path
// exposes an empty (but non-nil) middleware chain.
func TestDeleteSecretDefinitionMiddleware(t *testing.T) {
	h := newSecretHandler(nil)

	// resolve the empty middleware list
	mws := h.DeleteSecretDefinitionMiddleware()

	// no middleware attached to deletes
	assert.NotNil(t, mws, "middleware slice is initialized, not nil")
	assert.Empty(t, mws, "no delete-path middleware registered")
}

// TestCustomAddSecretDefinition_EmptyPayload asserts the handler
// rejects a POST with an empty JSON object body: PayloadCheck's empty
// branch returns 400 and the handler propagates it through
// ResponseStatusErr.
func TestCustomAddSecretDefinition_EmptyPayload(t *testing.T) {
	registerSecretVersions(t)
	db := setupSecretDB(t)
	h := newSecretHandler(db)

	// send a valid but empty JSON object; PayloadCheck rejects it
	c, rec := newSecretContext(t, "{}")

	// invoke the handler with a nil next since it short-circuits
	err := h.CustomAddSecretDefinition(nil)(c)

	// no unhandled error escapes; the response is a plain 400
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "empty payload yields 400")
}

// TestCustomAddSecretDefinition_MissingRequiredFields asserts that a
// payload with no Name or Data reaches ValidateBoundData and is
// rejected with 400 for the missing required fields.
func TestCustomAddSecretDefinition_MissingRequiredFields(t *testing.T) {
	registerSecretVersions(t)
	db := setupSecretDB(t)
	h := newSecretHandler(db)

	// payload names an optional field only; both required fields are absent
	c, rec := newSecretContext(t, `{"Reconciled":true}`)

	// invoke the handler with a nil next
	err := h.CustomAddSecretDefinition(nil)(c)

	// validator returns 400 for missing required fields
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "missing required fields yields 400")

	// error text names the missing-required-fields message
	var envelope apiserver_lib.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	assert.Contains(
		t,
		strings.ToLower(envelope.Status.Error),
		"required",
		"validator error text mentions required fields",
	)
}

// TestCustomAddSecretDefinition_DuplicateName asserts that submitting
// a SecretDefinition whose Name matches an existing row surfaces a
// 409 Conflict with the standard duplicate-name error text.
func TestCustomAddSecretDefinition_DuplicateName(t *testing.T) {
	registerSecretVersions(t)
	db := setupSecretDB(t)
	h := newSecretHandler(db)

	// pre-seed the table with the name the request will try to reuse
	existing := &api.SecretDefinition{
		Definition: api.Definition{Name: util.Ptr("dup-secret")},
	}
	require.NoError(t, db.Create(existing).Error)

	// request carries the same name so the duplicate check fires
	body := `{"Name":"dup-secret","Data":{"password":"s3cret"}}`
	c, rec := newSecretContext(t, body)

	// invoke the handler with a nil next since it short-circuits at 409
	err := h.CustomAddSecretDefinition(nil)(c)

	// no unhandled error escapes; the response is a plain 409
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, rec.Code, "duplicate name yields 409")

	// error text names the duplicate-name condition
	var envelope apiserver_lib.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	assert.Contains(
		t,
		envelope.Status.Error,
		"already exists",
		"conflict error text mentions the duplicate name",
	)
}
