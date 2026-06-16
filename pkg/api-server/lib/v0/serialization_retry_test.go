package v0

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// TestIsSerializationFailureClassifies covers detection of retryable
// serialization conflicts: a typed driver error carrying the 40001 code, the
// CockroachDB error text variants, and the non-retryable cases that must not
// be treated as serialization failures.
func TestIsSerializationFailureClassifies(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			// typed driver error with the serialization code is retryable
			name:     "pg error with 40001 code is retryable",
			err:      &pgconn.PgError{Code: "40001"},
			expected: true,
		},
		{
			// the code wrapped in error text still classifies as retryable
			name:     "error text mentioning 40001 is retryable",
			err:      errors.New("TransactionRetryWithProtoRefreshError: ... RETRY_SERIALIZABLE (SQLSTATE 40001)"),
			expected: true,
		},
		{
			// a typed driver error with a different code is not retryable
			name:     "pg error with another code is not retryable",
			err:      &pgconn.PgError{Code: "23505"},
			expected: false,
		},
		{
			// an unrelated error is not retryable
			name:     "unrelated error is not retryable",
			err:      errors.New("record not found"),
			expected: false,
		},
		{
			// no error is not a serialization failure
			name:     "nil error is not retryable",
			err:      nil,
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// classify the error and assert the retryable verdict
			assert.Equal(t, tc.expected, isSerializationFailure(tc.err))
		})
	}
}

// TestRetryOnSerializationFailureReRunsThenSucceeds covers the happy retry
// path: a write that fails once with a serialization conflict is re-run and
// the eventual success is returned.
func TestRetryOnSerializationFailureReRunsThenSucceeds(t *testing.T) {
	// fail the first attempt with a serialization conflict, then succeed
	calls := 0
	result := RetryOnSerializationFailure(func() *gorm.DB {
		calls++
		if calls == 1 {
			return &gorm.DB{Error: &pgconn.PgError{Code: "40001"}}
		}
		return &gorm.DB{Error: nil}
	})

	// the write ran twice and the final result carries no error
	assert.Equal(t, 2, calls)
	assert.NoError(t, result.Error)
}

// TestRetryOnSerializationFailureSkipsNonRetryable covers the fast path: a
// non-retryable error is returned on the first attempt without re-running.
func TestRetryOnSerializationFailureSkipsNonRetryable(t *testing.T) {
	// fail with an error that is not a serialization conflict
	nonRetryable := errors.New("duplicate key value violates unique constraint")
	calls := 0
	result := RetryOnSerializationFailure(func() *gorm.DB {
		calls++
		return &gorm.DB{Error: nonRetryable}
	})

	// the write ran once and the original error is surfaced unchanged
	assert.Equal(t, 1, calls)
	assert.ErrorIs(t, result.Error, nonRetryable)
}

// TestRetryOnSerializationFailureExhaustsBudget covers budget exhaustion: a
// write that keeps failing with a serialization conflict is retried up to the
// bound and the last error is returned.
func TestRetryOnSerializationFailureExhaustsBudget(t *testing.T) {
	// always fail with a serialization conflict
	calls := 0
	result := RetryOnSerializationFailure(func() *gorm.DB {
		calls++
		return &gorm.DB{Error: &pgconn.PgError{Code: "40001"}}
	})

	// the write ran the maximum number of attempts and still reports the error
	assert.Equal(t, serializationRetryMax, calls)
	assert.True(t, isSerializationFailure(result.Error))
}
