package handlers

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	sqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_lib "github.com/threeport/threeport/pkg/api/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
)

// rejectingConstraint is the index the simulated driver error names. A test
// asserts it stays out of the response body, so it is a string no handler
// would produce for another reason.
const rejectingConstraint = "idx_test_unique_violation"

// uniqueViolationCase is one create request whose write a unique index
// rejects, and the handler that owes the client a 409 for it.
type uniqueViolationCase struct {
	name string

	// source is the file defining the handler. The generated handlers all come
	// from one template and their output is identical once the type name is
	// substituted, so one object type stands for every generated object. The
	// hand-written handlers are not copies of anything and each carries its
	// own row.
	source string

	// handler is a closure rather than a method value because the secret
	// definition handler is echo middleware and has a different shape from the
	// plain handlers.
	handler func(Handler, echo.Context) error

	route string
	body  string

	// models are the tables the handler reads or writes. They exist so the
	// pre-read each create handler runs first reports no row rather than a
	// missing table, which would answer 500 before the write under test.
	models []any

	// objectType is registered with the tagged-field map the payload check
	// reads, which answers 500 when its lookup misses.
	objectType string
	object     any
}

var uniqueViolationCases = []uniqueViolationCase{
	{
		name:       "generated add handler",
		source:     "kubernetes_workload_gen.go",
		handler:    func(h Handler, c echo.Context) error { return h.AddKubernetesWorkloadDefinition(c) },
		route:      "/v0/kubernetes-workload-definitions",
		body:       `{"Name":"my-app","YAMLDocument":"kind: Namespace"}`,
		models:     []any{&api_v0.KubernetesWorkloadDefinition{}},
		objectType: api_v0.ObjectTypeKubernetesWorkloadDefinition,
		object:     new(api_v0.KubernetesWorkloadDefinition),
	},
	{
		name:   "resource definition set handler",
		source: "kubernetes_workload.go",
		handler: func(h Handler, c echo.Context) error {
			return h.AddKubernetesWorkloadResourceDefinitions(c)
		},
		route:      "/v0/kubernetes-workload-resource-definition-sets",
		body:       `[{"KubernetesWorkloadDefinitionID":1,"JSONDefinition":"e30="}]`,
		models:     []any{&api_v0.KubernetesWorkloadResourceDefinition{}},
		objectType: api_v0.ObjectTypeKubernetesWorkloadResourceDefinition,
		object:     new(api_v0.KubernetesWorkloadResourceDefinition),
	},
	{
		name:   "secret definition middleware",
		source: "secret.go",
		handler: func(h Handler, c echo.Context) error {
			// the middleware wraps the next handler, which never runs because
			// the write it guards fails
			return h.CustomAddSecretDefinition(func(echo.Context) error { return nil })(c)
		},
		route:      "/v0/secret-definitions",
		body:       `{"Name":"my-secret","Data":{"key":"value"}}`,
		models:     []any{&api_v0.SecretDefinition{}},
		objectType: api_v0.ObjectTypeSecretDefinition,
		object:     new(api_v0.SecretDefinition),
	},
	{
		name:   "module api route handler",
		source: "module.go",
		handler: func(h Handler, c echo.Context) error {
			return h.AddModuleApiRouteWithModuleObjectReferences(c)
		},
		route:      "/v0/module-api-routes",
		body:       `{"Path":"/v0/routers","ModuleApiID":1}`,
		models:     []any{&api_v0.ModuleApiRoute{}, &api_v0.ModuleApi{}, &api_v0.ModuleObject{}},
		objectType: api_v0.ObjectTypeModuleApiRoute,
		object:     new(api_v0.ModuleApiRoute),
	},
}

// TestHandlersAnswerUniqueViolationWith409 asserts the layer that sets the
// status. A write a unique index rejected means the request conflicts with a
// row that already exists, which the client can act on, so answering 500
// instead tells a controller the server is at fault and it backs off and
// retries a request that can never succeed.
func TestHandlersAnswerUniqueViolationWith409(t *testing.T) {
	for _, test := range uniqueViolationCases {
		t.Run(test.name, func(t *testing.T) {
			registerValidateTags(test.objectType, test.object)

			c, rec := newUniqueViolationRequest(test.route, test.body)
			h := newRejectingHandler(t, test.models)

			require.NoError(t, test.handler(h, c))

			assert.Equal(t, http.StatusConflict, rec.Code, "handler defined in %s", test.source)

			// the index that rejected the write is logged and not returned:
			// index names are chosen per object and get renamed, so a client
			// that read one would be reading something the server never
			// promised to keep
			assert.NotContains(t, rec.Body.String(), rejectingConstraint,
				"the response does not name the index that rejected the write")
		})
	}
}

// newRejectingHandler returns a handler whose every create fails the way
// CockroachDB fails one a unique index rejected. The driver error is injected
// rather than provoked because sqlite reports its own error type, and the
// classifier the handlers call reads the postgres SQLSTATE off a typed pgx
// error. What that leaves unproven is that CockroachDB really answers that
// SQLSTATE for a partial unique index, which needs a real server and belongs
// in test/integration.
func newRejectingHandler(t *testing.T, models []any) Handler {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(models...))

	require.NoError(t, db.Callback().Create().After("gorm:create").Register(
		"test:reject_with_unique_violation",
		func(tx *gorm.DB) {
			tx.Statement.DB.Error = &pgconn.PgError{
				Code:           "23505",
				Severity:       "ERROR",
				Message:        `duplicate key value violates unique constraint "` + rejectingConstraint + `"`,
				ConstraintName: rejectingConstraint,
			}
		},
	))

	return Handler{DB: db, Logger: zap.NewNop()}
}

// registerValidateTags populates the tagged-field map the payload check reads,
// for one object type. The versions package does this for every object at
// server start, and this test cannot call it: versions reaches handlers
// through pkg/api-server/v0 and pkg/api-server/v0/routes, so importing it from
// an in-package test is an import cycle.
func registerValidateTags(objectType string, obj any) {
	taggedFields := map[string]*apiserver_lib.FieldsByTag{
		string(api_lib.ValidateTag): {
			TagName:              string(api_lib.ValidateTag),
			Required:             []string{},
			Optional:             []string{},
			OptionalAssociations: []string{},
		},
	}
	apiserver_lib.ParseStruct(
		string(api_lib.ValidateTag),
		reflect.ValueOf(obj),
		"",
		apiserver_lib.Translate,
		taggedFields,
	)
	apiserver_lib.ObjectTaggedFields[apiserver_lib.VersionObject{
		Version: "v0",
		Object:  objectType,
	}] = taggedFields[string(api_lib.ValidateTag)]
}

// newUniqueViolationRequest drives a create handler the way the router does:
// the strict query binder and the validator registered on the echo instance,
// the route pattern set so the payload check can read the api version, and the
// context wrapped so a handler's assertion to the custom context succeeds.
func newUniqueViolationRequest(route, body string) (*apiserver_lib.CustomContext, *httptest.ResponseRecorder) {
	e := echo.New()
	e.Binder = apiserver_lib.NewQueryBinder()

	// the create handlers validate the bound object against the custom tags
	// the api types carry; without a validator echo reports that none is
	// registered and the validation path panics on the error type
	validate := validator.New()
	validate.RegisterValidation("optional", apiserver_lib.IsOptional)
	validate.RegisterValidation("association", apiserver_lib.IsAssociation)
	validate.RegisterValidation("ISO8601date", apiserver_lib.IsISO8601Date)
	e.Validator = &apiserver_lib.CustomValidator{Validator: validate}

	req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)

	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath(route)

	return &apiserver_lib.CustomContext{Context: c}, rec
}
