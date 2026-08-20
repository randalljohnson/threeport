package v0

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
	"gorm.io/gorm/schema"
)

const (
	// uniqueViolationCode is the SQLSTATE both CockroachDB and PostgreSQL
	// return when a unique index rejects a write.
	uniqueViolationCode = "23505"

	// ErrMsgUniqueViolation is the response text for a write a unique index
	// rejected, and the whole of that text when the fields behind the index
	// cannot be resolved.
	ErrMsgUniqueViolation = "Object conflicts with an existing object"
)

// conflictColumns matches the column list in a driver's description of a
// rejected key, which takes the form "Key (a, b)=(x, y) already exists." The
// match ends at the closing parenthesis so the values are never captured and
// cannot reach a client, which matters because an indexed column may hold a
// credential fingerprint or another value that is deliberately not published.
var conflictColumns = regexp.MustCompile(`^Key \(([^)]+)\)=`)

// schemaCache holds the model schemas gorm derives, so resolving a column
// costs one parse per type rather than one per rejected write.
var schemaCache = &sync.Map{}

// UniqueConflict describes a write that a unique index rejected.
type UniqueConflict struct {
	// Constraint is the index the database named, empty when the driver
	// supplied none.
	Constraint string

	// Detail is the driver's description of the rejected key. It is kept for
	// the server log, where an unrecognized form is what a reader needs to see
	// to fix the parse.
	Detail string

	// Fields are the API field names the index covers, in the order the
	// database reported their columns. Empty when the detail did not parse or
	// no column resolved to a field.
	Fields []string
}

// Message returns the response text for the conflict, naming the fields the
// index covers when they resolved. Field names are safe to publish where an
// index name is not: a field is part of the API a client already writes
// against, so it cannot be renamed without the API changing with it.
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

// Log records the conflict on the server. A conflict whose fields did not
// resolve logs at error level, because the client was answered with a message
// naming nothing and the cause is a parse this package owns rather than
// anything the caller did. That makes an unrecognized detail findable by
// searching the logs instead of waiting for a report of a vague 409.
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
//
// A rejected write means the request asks for state that already exists, which
// the client can resolve by reading that state or asking for something else.
// That separates it from the write failures a client cannot act on.
func UniqueViolation(err error, model interface{}) *UniqueConflict {
	if err == nil {
		return nil
	}

	// trust the driver error code when the typed error is present anywhere in
	// the chain. There is no message-text fallback here: unlike a retry, which
	// costs a wasted attempt when the guess is wrong, a wrong guess here
	// answers the client 409 for a failure that was not a conflict.
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
// a rejected key. It returns nil when the description does not take the form
// this recognizes, which leaves the conflict without fields rather than
// guessing at which ones the index covers.
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
// names a client sends, using the schema gorm derives from the model. Deriving
// it keeps an acronym intact, where converting the column text back to a field
// name by case rules alone would not. A column matching no field is dropped,
// since naming something absent from the API sends the reader looking for a
// field they cannot set.
func resolveConflictFields(model interface{}, columns []string) []string {
	if model == nil || len(columns) == 0 {
		return nil
	}

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
