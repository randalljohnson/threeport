package cockroach

import (
	"errors"
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
	"gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	handlers "github.com/threeport/threeport/pkg/api-server/v0/handlers"
	api_lib "github.com/threeport/threeport/pkg/api/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestUniqueViolationCarriesTheSqlstateAndConstraint covers the fact the
// handler's conflict answer rests on: CockroachDB rejects a write a unique
// index refused with the postgres unique-violation SQLSTATE, on a typed driver
// error, naming the index that refused it. Neither sqlite nor a dry-run handle
// can produce that error, so nothing else in the tree observes it.
func TestUniqueViolationCarriesTheSqlstateAndConstraint(t *testing.T) {
	reference := newReference("Workload", 1, "Gateway", 1, api_v0.RelationshipDescribes)
	require.NoError(t, testDb.Create(&reference).Error, "the first reference is accepted")

	// the same pair again collides on the full-table index across both sides
	duplicate := newReference("Workload", 1, "Gateway", 1, api_v0.RelationshipDescribes)
	err := testDb.Create(&duplicate).Error
	require.Error(t, err, "the duplicate pair is rejected")

	// the typed driver error survives to the caller, so the handler can tell a
	// conflict apart from a fault it cannot classify
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "the rejection arrives as a typed driver error: %v", err)
	assert.Equal(t, "23505", pgErr.Code, "the rejection carries the unique violation sqlstate")
	assert.NotEmpty(t, pgErr.ConstraintName, "the rejection names the index that refused the write")

	// the classifier reads the same error the handler receives
	conflict := apiserver_lib.UniqueViolation(err, new(api_v0.AttachedObjectReference))
	require.NotNil(t, conflict, "the classifier reports a conflict")
	assert.NotEmpty(t, conflict.Constraint, "the classifier recovers the index name for the log")
}

// TestPartialUniqueIndexReleasesOnSoftDelete covers the property every partial
// index in the schema is written for: a soft-deleted row leaves the index, so
// the value it held can be used again right away. Without the predicate the
// row keeps its slot until the database hard-deletes it, and a client that
// deletes and recreates the same object is refused for as long as that takes.
func TestPartialUniqueIndexReleasesOnSoftDelete(t *testing.T) {
	// a marriage is guarded by a partial index carrying deleted_at IS NULL
	married := newReference("Workload", 20, "Gateway", 20, api_v0.RelationshipMarries)
	require.NoError(t, testDb.Create(&married).Error, "the first marriage is accepted")

	// a second live marriage on the same base is refused while the first lives
	second := newReference("Workload", 20, "Gateway", 21, api_v0.RelationshipMarries)
	require.Error(t, testDb.Create(&second).Error, "a second live marriage on the same base is refused")

	// soft delete leaves the row in the table with deleted_at set
	require.NoError(t, testDb.Delete(&married).Error, "the marriage is soft deleted")
	var remaining int64
	require.NoError(t,
		testDb.Unscoped().Model(&api_v0.AttachedObjectReference{}).
			Where("id = ?", *married.ID).Count(&remaining).Error,
	)
	require.Equal(t, int64(1), remaining, "the soft-deleted row is still in the table")

	// with the row out of the index the base can be married again
	remarried := newReference("Workload", 20, "Gateway", 22, api_v0.RelationshipMarries)
	assert.NoError(t, testDb.Create(&remarried).Error,
		"the base is married again once the first marriage is soft deleted")
}

// TestRecreatingASoftDeletedReferenceIsAccepted covers the case a client hits
// most often: an object is deleted and the same one is created again. Every
// unique index on the table carries the deleted_at predicate, so the pair the
// deleted row held is free the moment it is deleted rather than when the
// database eventually hard-deletes the row.
func TestRecreatingASoftDeletedReferenceIsAccepted(t *testing.T) {
	reference := newReference("Workload", 50, "Gateway", 50, api_v0.RelationshipDescribes)
	require.NoError(t, testDb.Create(&reference).Error, "the first reference is accepted")
	require.NoError(t, testDb.Delete(&reference).Error, "the reference is soft deleted")

	// the soft-deleted row is still in the table, so an index without the
	// predicate would still be holding the pair
	var remaining int64
	require.NoError(t,
		testDb.Unscoped().Model(&api_v0.AttachedObjectReference{}).
			Where("id = ?", *reference.ID).Count(&remaining).Error,
	)
	require.Equal(t, int64(1), remaining, "the soft-deleted row is still in the table")

	recreated := newReference("Workload", 50, "Gateway", 50, api_v0.RelationshipDescribes)
	assert.NoError(t, testDb.Create(&recreated).Error,
		"the same pair is accepted again once the first reference is soft deleted")
}

// fullTableUnique is a model whose unique index carries no predicate. It is
// declared here rather than borrowed from the api types because the property
// under test is what the missing predicate costs, and every api type is free to
// gain one.
type fullTableUnique struct {
	gorm.Model
	Slot *string `gorm:"uniqueIndex:idx_full_table_unique"`
}

// TestFullTableUniqueIndexHoldsAfterSoftDelete covers what a unique index costs
// when it carries no deleted_at predicate, which is the reason every index in
// the schema that guards a recreatable object carries one. The row is gone as
// far as every read is concerned and still occupies its slot, so recreating the
// object it described is refused until the database hard-deletes it.
func TestFullTableUniqueIndexHoldsAfterSoftDelete(t *testing.T) {
	require.NoError(t, testDb.AutoMigrate(&fullTableUnique{}), "the table is built")

	slot := "one-per-slot"
	row := fullTableUnique{Slot: &slot}
	require.NoError(t, testDb.Create(&row).Error, "the first row is accepted")
	require.NoError(t, testDb.Delete(&row).Error, "the row is soft deleted")

	// the soft-deleted row reads as absent
	var found fullTableUnique
	assert.ErrorIs(t, testDb.Where("id = ?", row.ID).First(&found).Error, gorm.ErrRecordNotFound,
		"the soft-deleted row reads as absent")

	// and still holds its slot, because an index with no predicate indexes it
	recreated := fullTableUnique{Slot: &slot}
	err := testDb.Create(&recreated).Error
	require.Error(t, err, "the slot is still held after the soft delete")

	conflict := apiserver_lib.UniqueViolation(err, new(fullTableUnique))
	assert.NotNil(t, conflict,
		"the refusal is a unique violation, so a client is answered 409 it cannot clear")
}

// TestGeneratedHandlerAnswers409OnUniqueViolation drives a generated create
// handler against the real database, which is the only place the whole path is
// exercised: the index refuses the write, the driver reports it, the classifier
// reads it, and the handler turns it into a status. The handler tests prove the
// last step alone, against an injected error.
func TestGeneratedHandlerAnswers409OnUniqueViolation(t *testing.T) {
	registerValidateTags(api_v0.ObjectTypeAttachedObjectReference, new(api_v0.AttachedObjectReference))
	handler := handlers.Handler{DB: testDb, Logger: zap.NewNop()}

	body := `{"ObjectType":"Workload","ObjectID":40,"AttachedObjectType":"Gateway","AttachedObjectID":40}`

	created, _ := newCreateRequest(body)
	require.NoError(t, handler.AddAttachedObjectReference(created))

	conflicted, recorder := newCreateRequest(body)
	require.NoError(t, handler.AddAttachedObjectReference(conflicted))

	assert.Equal(t, http.StatusConflict, recorder.Code,
		"a write the index refused is answered as a conflict the client can act on")
	assert.NotContains(t, recorder.Body.String(), "idx_",
		"the response does not name the index that refused the write")
}

// nullableSlot is a model whose unique index guards a nullable column under
// the same deleted_at predicate the schema's partial indexes carry. It is
// declared here rather than borrowed from the api types because the property
// under test is what an unset column does to the index, and every api type is
// free to make its guarded column required.
type nullableSlot struct {
	gorm.Model
	Slot *string `gorm:"uniqueIndex:idx_nullable_slot,where:deleted_at IS NULL"`
}

// TestUniqueIndexTreatsEveryNullAsDistinct covers the half of a unique index on
// an optional column that decides whether the column can stay optional: a row
// leaving the column unset takes no slot, so any number of rows may leave it
// unset while the same index still refuses two rows carrying one value. Without
// it a guarded optional column becomes required in practice, because the second
// row omitting it is refused.
func TestUniqueIndexTreatsEveryNullAsDistinct(t *testing.T) {
	require.NoError(t, testDb.AutoMigrate(&nullableSlot{}), "the table is built")

	// three rows leaving the guarded column unset are all accepted
	for range 3 {
		assert.NoError(t, testDb.Create(&nullableSlot{}).Error,
			"a row leaving the guarded column unset takes no slot in the index")
	}

	// and the same index still guards the rows that do carry a value
	slot := "one-per-slot"
	require.NoError(t, testDb.Create(&nullableSlot{Slot: &slot}).Error,
		"the first row carrying a value is accepted")

	err := testDb.Create(&nullableSlot{Slot: &slot}).Error
	require.Error(t, err, "a second row carrying the same value is refused")

	conflict := apiserver_lib.UniqueViolation(err, new(nullableSlot))
	assert.NotNil(t, conflict, "the refusal is a unique violation")
}

// newReference returns an attached object reference with the four indexed
// columns set, which is everything the indexes under test read.
func newReference(
	objectType string,
	objectID uint,
	attachedType string,
	attachedID uint,
	relationship api_v0.Relationship,
) api_v0.AttachedObjectReference {
	return api_v0.AttachedObjectReference{
		ObjectType:         util.Ptr(objectType),
		ObjectID:           util.Ptr(objectID),
		AttachedObjectType: util.Ptr(attachedType),
		AttachedObjectID:   util.Ptr(attachedID),
		Relationship:       util.Ptr(relationship),
	}
}

// newCreateRequest drives a create handler the way the router does: the strict
// query binder and the validator registered on the echo instance, the route
// pattern set so the payload check can read the api version, and the context
// wrapped so a handler's assertion to the custom context succeeds.
func newCreateRequest(body string) (*apiserver_lib.CustomContext, *httptest.ResponseRecorder) {
	const route = "/v0/attached-object-references"

	e := echo.New()
	e.Binder = apiserver_lib.NewQueryBinder()

	validate := validator.New()
	validate.RegisterValidation("optional", apiserver_lib.IsOptional)
	validate.RegisterValidation("association", apiserver_lib.IsAssociation)
	validate.RegisterValidation("ISO8601date", apiserver_lib.IsISO8601Date)
	e.Validator = &apiserver_lib.CustomValidator{Validator: validate}

	req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)

	recorder := httptest.NewRecorder()
	c := e.NewContext(req, recorder)
	c.SetPath(route)

	return &apiserver_lib.CustomContext{Context: c}, recorder
}

// registerValidateTags populates the tagged-field map the payload check reads,
// for one object type. The versions package does this for every object at
// server start, and reaching it from here would pull the whole route table in.
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
