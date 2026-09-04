package v0

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
	"gorm.io/gorm/schema"
)

const (
	// uniqueViolationCode is the SQLSTATE both CockroachDB and PostgreSQL
	// return when a unique index rejects a write.
	uniqueViolationCode = "23505"

	// ErrMsgUniqueViolation is the response text for a write a unique index
	// rejected, and the whole of it when no field resolved.
	ErrMsgUniqueViolation = "Object conflicts with an existing object"
)

// conflictColumns matches the column list in a driver's description of a
// rejected key, which takes the form "Key (a, b)=(x, y) already exists." The
// match ends at the closing parenthesis so the values are never captured, since
// an indexed column may hold a credential fingerprint.
var conflictColumns = regexp.MustCompile(`^Key \(([^)]+)\)=`)

// schemaCache holds the model schemas gorm derives, so resolving a column
// costs one parse per type rather than one per rejected write.
var schemaCache = &sync.Map{}

// UniqueConflict describes a write that a unique index rejected.
type UniqueConflict struct {
	// Constraint is the index the database named, empty when the driver
	// supplied none.
	Constraint string

	// Detail is the driver's description of the rejected key, kept for the
	// server log.
	Detail string

	// Fields are the API field names the index covers, in the order the
	// database reported their columns. Empty when no column resolved.
	Fields []string
}

// Message returns the response text for the conflict, naming the fields the
// index covers when they resolved. Field names are safe to publish where an
// index name is not, since a field is already part of the API a client writes
// against.
func (conflict *UniqueConflict) Message() string {
	if len(conflict.Fields) == 0 {
		return ErrMsgUniqueViolation
	}

	return fmt.Sprintf(
		"%s on: %s",
		ErrMsgUniqueViolation,
		strings.Join(conflict.Fields, ", "),
	)
}

// Log records the conflict on the server, at error level when no field
// resolved, since that means this package failed to parse the detail rather
// than the caller doing anything wrong.
func (conflict *UniqueConflict) Log(logger *zap.Logger) {
	if logger == nil {
		return
	}

	if len(conflict.Fields) == 0 {
		logger.Error(
			"unique index rejected a write and the fields behind it did not resolve",
			zap.String("constraint", conflict.Constraint),
			zap.String("detail", conflict.Detail),
		)
		return
	}

	logger.Info(
		"write rejected by unique index",
		zap.String("constraint", conflict.Constraint),
		zap.Strings("fields", conflict.Fields),
	)
}

// UniqueViolation describes the conflict when err is a write a unique index
// rejected, and returns nil for any other outcome including success. The model
// supplies the column-to-field mapping, so passing nil yields a conflict that
// names no fields rather than an error.
func UniqueViolation(err error, model interface{}) *UniqueConflict {
	if err == nil {
		return nil
	}

	// match on the driver's code and never on message text: a wrong guess
	// answers the client 409 for a failure that was not a conflict
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolationCode {
		return nil
	}

	return &UniqueConflict{
		Constraint: pgErr.ConstraintName,
		Detail:     pgErr.Detail,
		Fields:     resolveConflictFields(model, parseConflictColumns(pgErr.Detail)),
	}
}

// parseConflictColumns reads the column names out of a driver's description of
// a rejected key, returning nil when it takes an unrecognized form.
func parseConflictColumns(detail string) []string {
	match := conflictColumns.FindStringSubmatch(detail)
	if match == nil {
		return nil
	}

	columns := strings.Split(match[1], ",")
	for i, column := range columns {
		columns[i] = strings.TrimSpace(column)
	}

	return columns
}

// resolveConflictFields translates database column names into the API field
// names a client sends, dropping any column that matches no field.
func resolveConflictFields(model interface{}, columns []string) []string {
	if model == nil || len(columns) == 0 {
		return nil
	}

	// take the names from the schema rather than converting the column text by
	// case rules, which would not put an acronym back together
	modelSchema, err := schema.Parse(model, schemaCache, schema.NamingStrategy{})
	if err != nil {
		return nil
	}

	var fields []string
	for _, column := range columns {
		field, ok := modelSchema.FieldsByDBName[column]
		if !ok {
			continue
		}
		fields = append(fields, field.Name)
	}

	return fields
}

// RespondWriteError answers a write the database refused: a conflict when a
// unique index rejected it, and a server fault for anything else. The model
// supplies the column-to-field mapping the conflict message names its fields
// from, so a zero value of the object being written is what to pass.
func RespondWriteError(
	c echo.Context,
	logger *zap.Logger,
	err error,
	model interface{},
	fullyQualifiedType string,
) error {
	if conflict := UniqueViolation(err, model); conflict != nil {
		conflict.Log(logger)
		return ResponseStatus409(c, nil, errors.New(conflict.Message()), fullyQualifiedType)
	}

	return ResponseStatus500(c, nil, err, fullyQualifiedType)
}
