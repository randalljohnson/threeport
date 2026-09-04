package v0

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"
)

// conflictTestModel supplies the column-to-field mapping the resolution tests
// parse. It carries an acronym field so resolution is exercised on the case
// that converting column text back to a field name by case rules alone gets
// wrong.
type conflictTestModel struct {
	Name      *string
	IPAddress *string
}

// TestUniqueViolationClassifies covers detection of a write a unique index
// rejected: a typed driver error carrying the 23505 code, the same error
// reached through a wrapper, and the failures that must keep answering as
// server faults rather than conflicts.
func TestUniqueViolationClassifies(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		constraint string
		conflict   bool
	}{
		{
			// typed driver error with the unique violation code is a conflict
			// and names the index that rejected the write
			name:       "pg error with 23505 code is a conflict",
			err:        &pgconn.PgError{Code: "23505", ConstraintName: "idx_machine_instance_ip_address"},
			constraint: "idx_machine_instance_ip_address",
			conflict:   true,
		},
		{
			// the driver leaves the name out on some servers, which changes
			// what there is to log and not whether it is a conflict
			name:       "pg error with 23505 code and no constraint name is a conflict",
			err:        &pgconn.PgError{Code: "23505"},
			constraint: "",
			conflict:   true,
		},
		{
			// the handler receives the driver error through gorm, so the code
			// has to be found through a wrapper rather than only at the top
			name:       "wrapped pg error with 23505 code is a conflict",
			err:        fmt.Errorf("persist object: %w", &pgconn.PgError{Code: "23505", ConstraintName: "idx_events_dedup"}),
			constraint: "idx_events_dedup",
			conflict:   true,
		},
		{
			// a serialization conflict is retried rather than answered, so it
			// must not reach the client as a 409
			name:     "pg error with 40001 code is not a conflict",
			err:      &pgconn.PgError{Code: "40001"},
			conflict: false,
		},
		{
			// a foreign key violation is a different fault with a different
			// answer, and shares only the constraint vocabulary
			name:     "pg error with 23503 code is not a conflict",
			err:      &pgconn.PgError{Code: "23503", ConstraintName: "fk_machine_instance_runtime"},
			conflict: false,
		},
		{
			// there is no message-text fallback, so a value that happens to
			// carry the digits cannot be mistaken for the code
			name:     "untyped error carrying 23505 as data is not a conflict",
			err:      errors.New("failed to provision host-23505"),
			conflict: false,
		},
		{
			// gorm's own translated error carries no driver code, so it does
			// not classify here
			name:     "gorm record not found is not a conflict",
			err:      gorm.ErrRecordNotFound,
			conflict: false,
		},
		{
			// a successful write reports no conflict
			name:     "nil error is not a conflict",
			err:      nil,
			conflict: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conflict := UniqueViolation(test.err, &conflictTestModel{})

			// classification decides whether the client sees a 409
			assert.Equal(t, test.conflict, conflict != nil)
			if !test.conflict {
				assert.Nil(t, conflict)
				return
			}

			// the index name reaches the server log rather than the client
			assert.Equal(t, test.constraint, conflict.Constraint)
		})
	}
}

// TestUniqueViolationResolvesFields covers translation of the columns a driver
// reports into the API field names a client sends, across the detail forms the
// database produces and the forms this cannot read.
func TestUniqueViolationResolvesFields(t *testing.T) {
	tests := []struct {
		name   string
		detail string
		model  interface{}
		fields []string
	}{
		{
			// the single-column form, produced by most indexes
			name:   "single column resolves to its field",
			detail: "Key (name)=('demo-a') already exists.",
			model:  &conflictTestModel{},
			fields: []string{"Name"},
		},
		{
			// a composite index reports every column it covers, in order
			name:   "composite columns resolve in the order reported",
			detail: "Key (name, ip_address)=('demo-a', '10.0.0.42') already exists.",
			model:  &conflictTestModel{},
			fields: []string{"Name", "IPAddress"},
		},
		{
			// an acronym survives only because the mapping comes from the
			// schema rather than from re-casing the column text
			name:   "acronym column resolves to the field's own casing",
			detail: "Key (ip_address)=('10.0.0.42') already exists.",
			model:  &conflictTestModel{},
			fields: []string{"IPAddress"},
		},
		{
			// a column with no matching field is dropped rather than named,
			// since a client cannot act on a field the API does not carry
			name:   "column matching no field is dropped",
			detail: "Key (name, deleted_at)=('demo-a', NULL) already exists.",
			model:  &conflictTestModel{},
			fields: []string{"Name"},
		},
		{
			// an unrecognized detail leaves the conflict without fields, which
			// is the state that logs at error level
			name:   "unrecognized detail resolves no fields",
			detail: "conflicting key detected",
			model:  &conflictTestModel{},
			fields: nil,
		},
		{
			// some servers supply no detail at all
			name:   "empty detail resolves no fields",
			detail: "",
			model:  &conflictTestModel{},
			fields: nil,
		},
		{
			// a caller with no model in hand still gets a well-formed conflict
			name:   "nil model resolves no fields",
			detail: "Key (name)=('demo-a') already exists.",
			model:  nil,
			fields: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := &pgconn.PgError{Code: "23505", Detail: test.detail}

			conflict := UniqueViolation(err, test.model)

			// resolution never changes whether the write was rejected
			assert.NotNil(t, conflict)
			assert.Equal(t, test.fields, conflict.Fields)

			// the detail is carried through for the log whatever it held
			assert.Equal(t, test.detail, conflict.Detail)
		})
	}
}

// TestUniqueConflictMessageNamesFields covers the response text, which names
// the fields when they resolved and stands alone when they did not.
func TestUniqueConflictMessageNamesFields(t *testing.T) {
	tests := []struct {
		name     string
		conflict UniqueConflict
		message  string
	}{
		{
			name:     "one field is named",
			conflict: UniqueConflict{Fields: []string{"Hostname"}},
			message:  "Object conflicts with an existing object on: Hostname",
		},
		{
			name:     "several fields are named in order",
			conflict: UniqueConflict{Fields: []string{"Name", "ApiNamespace"}},
			message:  "Object conflicts with an existing object on: Name, ApiNamespace",
		},
		{
			name:     "no fields falls back to the unqualified text",
			conflict: UniqueConflict{},
			message:  "Object conflicts with an existing object",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.message, test.conflict.Message())
		})
	}
}

// TestUniqueConflictLogRaisesUnresolvedToError covers the log level, which
// separates a conflict answered with field names from one answered with
// nothing, so a detail this cannot read is findable by searching the logs.
func TestUniqueConflictLogRaisesUnresolvedToError(t *testing.T) {
	tests := []struct {
		name     string
		conflict UniqueConflict
		level    zapcore.Level
		message  string
	}{
		{
			// resolved fields mean the client was answered usefully, which is
			// ordinary traffic rather than a fault
			name: "resolved fields log at info",
			conflict: UniqueConflict{
				Constraint: "idx_machine_runtime_instance_hostname",
				Detail:     "Key (hostname)=('10.0.0.42') already exists.",
				Fields:     []string{"Hostname"},
			},
			level:   zapcore.InfoLevel,
			message: "write rejected by unique index",
		},
		{
			// unresolved fields mean the client was answered with a message
			// naming nothing, so the parse needs fixing
			name: "unresolved fields log at error",
			conflict: UniqueConflict{
				Constraint: "idx_machine_runtime_instance_hostname",
				Detail:     "conflicting key detected",
			},
			level:   zapcore.ErrorLevel,
			message: "unique index rejected a write and the fields behind it did not resolve",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// capture what the handler's logger would have written
			core, recorded := observer.New(zapcore.DebugLevel)

			test.conflict.Log(zap.New(core))

			// one entry, at the level that decides whether anyone looks at it
			entries := recorded.All()
			assert.Len(t, entries, 1)
			assert.Equal(t, test.level, entries[0].Level)
			assert.Equal(t, test.message, entries[0].Message)
		})
	}
}

// TestUniqueConflictLogAcceptsNilLogger asserts a conflict logs nothing rather
// than panicking when the caller holds no logger.
func TestUniqueConflictLogAcceptsNilLogger(t *testing.T) {
	conflict := UniqueConflict{Fields: []string{"Hostname"}}

	assert.NotPanics(t, func() { conflict.Log(nil) })
}
