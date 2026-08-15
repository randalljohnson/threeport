package v0

import (
	"context"
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
			// a non-40001 driver error stays non-retryable even when its
			// message text embeds the serialization digits
			name:     "pg error with another code and 40001 in text is not retryable",
			err:      &pgconn.PgError{Code: "23505", Message: "value host-40001 already exists"},
			expected: false,
		},
		{
			// a bare error carrying 40001 as data is not a serialization failure
			name:     "untyped error with 40001 in data is not retryable",
			err:      errors.New("failed to provision host-40001"),
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

// TestRetryWriteReRunsThenSucceeds covers the happy retry path: a write that
// fails once with a serialization conflict is re-run and the eventual success
// is returned.
func TestRetryWriteReRunsThenSucceeds(t *testing.T) {
	// fail the first attempt with a serialization conflict, then succeed
	calls := 0
	result := RetryWrite(context.Background(), func() *gorm.DB {
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

// TestRetryWriteSkipsNonRetryable covers the fast path: a non-retryable error
// is returned on the first attempt without re-running.
func TestRetryWriteSkipsNonRetryable(t *testing.T) {
	// fail with an error that is not a serialization conflict
	nonRetryable := errors.New("duplicate key value violates unique constraint")
	calls := 0
	result := RetryWrite(context.Background(), func() *gorm.DB {
		calls++
		return &gorm.DB{Error: nonRetryable}
	})

	// the write ran once and the original error is surfaced unchanged
	assert.Equal(t, 1, calls)
	assert.ErrorIs(t, result.Error, nonRetryable)
}

// TestRetryWriteExhaustsBudget covers budget exhaustion: a write that keeps
// failing with a serialization conflict is retried up to the bound and the last
// error is returned.
func TestRetryWriteExhaustsBudget(t *testing.T) {
	// always fail with a serialization conflict
	calls := 0
	result := RetryWrite(context.Background(), func() *gorm.DB {
		calls++
		return &gorm.DB{Error: &pgconn.PgError{Code: "40001"}}
	})

	// the write ran the maximum number of attempts and still reports the error
	assert.Equal(t, serializationRetryMax, calls)
	assert.True(t, isSerializationFailure(result.Error))
}

// TestRetryWriteStopsOnCancelledContext covers the abandoned request: once the
// caller disconnects, the retry loop gives up its remaining budget instead of
// sleeping out the backoff, and still hands back the last failure.
func TestRetryWriteStopsOnCancelledContext(t *testing.T) {
	// cancel the request while the first attempt is in flight, so the loop
	// reaches the backoff with a context that is already done
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	result := RetryWrite(ctx, func() *gorm.DB {
		calls++
		cancel()
		return &gorm.DB{Error: &pgconn.PgError{Code: "40001"}}
	})

	// the write ran once, well short of the budget, and the conflict is returned
	assert.Equal(t, 1, calls)
	assert.True(t, isSerializationFailure(result.Error))
}
