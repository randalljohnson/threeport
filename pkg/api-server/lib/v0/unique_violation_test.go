package v0

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

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
			constraint, conflict := UniqueViolation(test.err)
			assert.Equal(t, test.conflict, conflict)
			assert.Equal(t, test.constraint, constraint)
		})
	}
}
