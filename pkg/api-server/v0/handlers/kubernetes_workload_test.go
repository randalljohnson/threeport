package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_lib "github.com/threeport/threeport/pkg/api/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// kwrdVersionsOnce guards process-wide registration of the
// KubernetesWorkloadResourceDefinition tagged fields. PayloadCheck resolves
// the entry from the ObjectTaggedFields global via versionFromPath(c.Path()),
// so tests must seed the same registration the api-server does at startup.
// The registration is inlined here rather than delegated to the versions
// package because importing that package would create an import cycle
// (versions -> api-server/v0 -> routes -> handlers).
var kwrdVersionsOnce sync.Once

// registerKWRDVersion mirrors versions.AddKubernetesWorkloadResourceDefinitionVersions
// but keys directly into apiserver_lib globals so the handlers package's tests
// avoid importing the versions package.
func registerKWRDVersion() {
	tagName := string(api_lib.ValidateTag)
	tf := map[string]*apiserver_lib.FieldsByTag{
		tagName: {
			TagName:              tagName,
			Optional:             []string{},
			OptionalAssociations: []string{},
			Required:             []string{},
		},
	}
	apiserver_lib.ParseStruct(
		tagName,
		reflect.ValueOf(new(v0.KubernetesWorkloadResourceDefinition)),
		"",
		apiserver_lib.Translate,
		tf,
	)
	versionObj := apiserver_lib.VersionObject{
		Object:  v0.ObjectTypeKubernetesWorkloadResourceDefinition,
		Version: "v0",
	}
	apiserver_lib.ObjectTaggedFields[versionObj] = tf[tagName]
	apiserver_lib.AddObjectVersion(versionObj)
}

// setupKWRDHandler returns a Handler wired to an in-memory sqlite DB with
// the KubernetesWorkloadResourceDefinition table migrated, plus a no-op
// logger. Also registers the ObjectTaggedFields entry the handler's
// PayloadCheck reads through.
func setupKWRDHandler(t *testing.T) (Handler, *gorm.DB) {
	t.Helper()
	// use file::memory: with a per-test cache name plus shared cache so
	// AutoMigrate and the handler's subsequent Create share the same
	// in-memory database across whichever connection GORM hands out from
	// its pool. Bare ":memory:" would silently give the handler a fresh
	// empty DB.
	dsn := fmt.Sprintf("file:kwrdtest_%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&v0.KubernetesWorkloadResourceDefinition{}))
	kwrdVersionsOnce.Do(registerKWRDVersion)
	return Handler{DB: db, Logger: zap.NewNop()}, db
}

// newKWRDContext builds an echo.Context targeting the resource-definition-sets
// path with a JSON body. The CustomValidator is wired with the same custom
// tags the api-server registers so ValidateBoundData exercises the real
// validation path rather than falling back to unknown-tag errors.
func newKWRDContext(t *testing.T, body []byte) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	v := validator.New()
	require.NoError(t, v.RegisterValidation("optional", apiserver_lib.IsOptional))
	require.NoError(t, v.RegisterValidation("association", apiserver_lib.IsAssociation))
	require.NoError(t, v.RegisterValidation("ISO8601date", apiserver_lib.IsISO8601Date))
	e.Validator = &apiserver_lib.CustomValidator{Validator: v}

	req := httptest.NewRequest(
		http.MethodPost,
		v0.PathKubernetesWorkloadResourceDefinitionSets,
		bytes.NewReader(body),
	)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// SetPath drives versionFromPath("/v0/...") so PayloadCheck's
	// ObjectTaggedFields lookup keys on the "v0" API version.
	c.SetPath(v0.PathKubernetesWorkloadResourceDefinitionSets)
	return c, rec
}

// decodeResponse pulls the apiserver_lib.Response envelope back out of the
// recorder body so tests can assert on Status.Code and Data length together.
func decodeResponse(t *testing.T, rec *httptest.ResponseRecorder) apiserver_lib.Response {
	t.Helper()
	var resp apiserver_lib.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

// makeValidPayload constructs a request body array of n valid resource
// definitions, each carrying a distinct JSON manifest so DB rows are
// distinguishable in the round-trip assertions.
func makeValidPayload(t *testing.T, n int) []byte {
	t.Helper()
	defs := make([]v0.KubernetesWorkloadResourceDefinition, 0, n)
	for i := 0; i < n; i++ {
		manifest := datatypes.JSON([]byte(`{"kind":"ConfigMap","metadata":{"name":"example"}}`))
		defs = append(defs, v0.KubernetesWorkloadResourceDefinition{
			JSONDefinition:                 &manifest,
			KubernetesWorkloadDefinitionID: util.Ptr(uint(i + 1)),
		})
	}
	body, err := json.Marshal(defs)
	require.NoError(t, err)
	return body
}

// TestAddKubernetesWorkloadResourceDefinitions_HappyPath covers the
// end-to-end success path: a valid multi-element array persists every
// element inside a transaction and the 201 response includes the created
// rows with generated IDs.
func TestAddKubernetesWorkloadResourceDefinitions_HappyPath(t *testing.T) {
	h, db := setupKWRDHandler(t)

	// send a two-element payload through the handler
	body := makeValidPayload(t, 2)
	c, rec := newKWRDContext(t, body)
	require.NoError(t, h.AddKubernetesWorkloadResourceDefinitions(c))

	// verify the HTTP status and envelope report Created
	assert.Equal(t, http.StatusCreated, rec.Code)
	resp := decodeResponse(t, rec)
	assert.Equal(t, http.StatusCreated, resp.Status.Code)
	assert.Equal(t, v0.ObjectTypeKubernetesWorkloadResourceDefinition, resp.Type)

	// verify both rows landed in the DB, each with a non-zero ID assigned
	var rows []v0.KubernetesWorkloadResourceDefinition
	require.NoError(t, db.Find(&rows).Error)
	assert.Len(t, rows, 2, "both payload elements are persisted atomically")
	for _, r := range rows {
		require.NotNil(t, r.ID)
		assert.NotZero(t, *r.ID)
	}
}

// TestAddKubernetesWorkloadResourceDefinitions_EmptyPayload covers the
// PayloadCheck-empty-array branch: an empty JSON array short-circuits to a
// 400 without ever touching the DB.
func TestAddKubernetesWorkloadResourceDefinitions_EmptyPayload(t *testing.T) {
	h, db := setupKWRDHandler(t)

	// invoke the handler with an explicit empty array
	c, rec := newKWRDContext(t, []byte(`[]`))
	require.NoError(t, h.AddKubernetesWorkloadResourceDefinitions(c))

	// verify a 400 with the empty-payload error message
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	resp := decodeResponse(t, rec)
	assert.Equal(t, http.StatusBadRequest, resp.Status.Code)
	assert.Contains(t, resp.Status.Error, apiserver_lib.ErrMsgJSONPayloadEmpty)

	// verify no DB rows were created despite the 400 being handled cleanly
	var count int64
	require.NoError(t, db.Model(&v0.KubernetesWorkloadResourceDefinition{}).Count(&count).Error)
	assert.Zero(t, count, "empty payload never reaches DB write")
}

// TestAddKubernetesWorkloadResourceDefinitions_GORMModelFieldRejected
// covers the PayloadCheck GORM-model-field gate: a client that tries to
// include ID gets a 400 and no rows written.
func TestAddKubernetesWorkloadResourceDefinitions_GORMModelFieldRejected(t *testing.T) {
	h, db := setupKWRDHandler(t)

	// build a payload whose only defect is a client-supplied ID field
	body := []byte(`[{"ID":1,"JSONDefinition":{"kind":"ConfigMap"},"KubernetesWorkloadDefinitionID":1}]`)
	c, rec := newKWRDContext(t, body)
	require.NoError(t, h.AddKubernetesWorkloadResourceDefinitions(c))

	// verify the 400 identifies the GORM Model field violation
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	resp := decodeResponse(t, rec)
	assert.Contains(t, resp.Status.Error, apiserver_lib.ErrMsgGORMModelFieldsUpdateNotAllowed)

	// verify no DB rows were created
	var count int64
	require.NoError(t, db.Model(&v0.KubernetesWorkloadResourceDefinition{}).Count(&count).Error)
	assert.Zero(t, count)
}

// TestAddKubernetesWorkloadResourceDefinitions_MissingRequiredField
// covers the ValidateBoundData branch: a payload missing a required field
// returns 400 with the field name in the error and never writes to the DB.
func TestAddKubernetesWorkloadResourceDefinitions_MissingRequiredField(t *testing.T) {
	h, db := setupKWRDHandler(t)

	// element omits both required fields (JSONDefinition and
	// KubernetesWorkloadDefinitionID) so the validator collects both.
	body := []byte(`[{}]`)
	c, rec := newKWRDContext(t, body)
	require.NoError(t, h.AddKubernetesWorkloadResourceDefinitions(c))

	// verify the 400 identifies the missing-required-fields class of error
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	resp := decodeResponse(t, rec)
	assert.Contains(t, resp.Status.Error, apiserver_lib.ErrMsgMissingRequiredFields)
	// verify each missing field name shows up in the error
	assert.Contains(t, resp.Status.Error, "JSONDefinition")
	assert.Contains(t, resp.Status.Error, "KubernetesWorkloadDefinitionID")

	// verify no DB rows were created
	var count int64
	require.NoError(t, db.Model(&v0.KubernetesWorkloadResourceDefinition{}).Count(&count).Error)
	assert.Zero(t, count)
}

// TestAddKubernetesWorkloadResourceDefinitions_InvalidJSONBody covers the
// c.Bind error path: a body that parses as a single JSON object rather than
// an array is caught at PayloadCheck (empty per-array match falls through
// to the object branch and passes checkPayloadObject), but a body that is
// not JSON at all trips PayloadCheck's Unmarshal to 500.
func TestAddKubernetesWorkloadResourceDefinitions_InvalidJSONBody(t *testing.T) {
	h, _ := setupKWRDHandler(t)

	// send a body that is not valid JSON at all so both single-object and
	// array Unmarshal attempts in PayloadCheck fail
	c, rec := newKWRDContext(t, []byte(`not-json`))
	require.NoError(t, h.AddKubernetesWorkloadResourceDefinitions(c))

	// verify the resulting status is the 500 path PayloadCheck returns
	// when the body isn't JSON at all
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	resp := decodeResponse(t, rec)
	assert.Equal(t, http.StatusInternalServerError, resp.Status.Code)
}

// TestAddKubernetesWorkloadResourceDefinitions_DBError covers the
// transactional Create-error branch: when the underlying DB rejects the
// insert (here by pointing the handler at a DB whose target table was
// dropped just before the request), the handler surfaces a 500 and no
// row lands.
func TestAddKubernetesWorkloadResourceDefinitions_DBError(t *testing.T) {
	h, db := setupKWRDHandler(t)
	// remove the table so Create returns a "no such table" error
	require.NoError(t, db.Migrator().DropTable(&v0.KubernetesWorkloadResourceDefinition{}))

	body := makeValidPayload(t, 1)
	c, rec := newKWRDContext(t, body)
	require.NoError(t, h.AddKubernetesWorkloadResourceDefinitions(c))

	// verify the failure surfaces as 500 and the transaction rolled back
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	resp := decodeResponse(t, rec)
	assert.Equal(t, http.StatusInternalServerError, resp.Status.Code)
}
