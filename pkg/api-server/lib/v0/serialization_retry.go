package v0

import (
	"errors"
	"math/rand"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const (
	// serializationFailureCode is the SQLSTATE returned by CockroachDB and
	// PostgreSQL when a transaction is aborted due to a serialization
	// conflict. Such failures are safe to retry from the start.
	serializationFailureCode = "40001"

	// serializationRetryMax bounds how many times a write is re-run after a
	// serialization conflict before the error is surfaced to the caller. Sized
	// for the observed workload: bursts of concurrent PATCHes on the same row
	// exhaust a 5-attempt budget for a small fraction of writers, so the cap
	// sits high enough to absorb that burst without stranding the caller.
	serializationRetryMax = 12

	// serializationRetryBaseDelay seeds the exponential backoff. Doubling and
	// jitter spread contending writers across a wide enough window that a
	// stampede resolves within the retry budget.
	serializationRetryBaseDelay = 10 * time.Millisecond

	// serializationRetryMaxDelay caps the per-attempt wait so a long tail on
	// exponential backoff does not stall the request context.
	serializationRetryMaxDelay = 500 * time.Millisecond
)

// RetryOnSerializationFailure runs a database write and re-runs it from the
// start when the write fails with a serialization conflict, up to a bounded
// number of attempts. It returns the result of the last attempt so callers
// inspect result.Error as usual. Non-retryable errors are returned on the
// first attempt without delay.
func RetryOnSerializationFailure(write func() *gorm.DB) *gorm.DB {
	var result *gorm.DB
	for attempt := 0; attempt < serializationRetryMax; attempt++ {
		result = write()
		if result.Error == nil || !isSerializationFailure(result.Error) {
			return result
		}
		time.Sleep(serializationRetryBackoff(attempt))
	}

	return result
}

// serializationRetryBackoff returns the wait before the next attempt: base *
// 2^attempt capped at the max, then multiplied by a random factor in [0.5, 1.5)
// so contending writers do not retry in lockstep and re-collide.
func serializationRetryBackoff(attempt int) time.Duration {
	delay := serializationRetryBaseDelay << attempt
	if delay <= 0 || delay > serializationRetryMaxDelay {
		delay = serializationRetryMaxDelay
	}
	jitter := 0.5 + rand.Float64()
	return time.Duration(float64(delay) * jitter)
}

// isSerializationFailure reports whether err is a CockroachDB or PostgreSQL
// serialization conflict, which is retryable. It trusts the typed driver error
// code when present and otherwise falls back to the error text, so detection
// still holds when the underlying driver type is not surfaced.
func isSerializationFailure(err error) bool {
	if err == nil {
		return false
	}

	// trust the driver error code when the typed error is present anywhere in
	// the chain; a different code is not a serialization conflict even if the
	// message text happens to carry the digits 40001.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == serializationFailureCode
	}

	// fall back to the error text only when no typed driver error is present,
	// matching the SQLSTATE on a bordered token so a value carried in the
	// message cannot be mistaken for the code. WriteTooOldError is a specific
	// CockroachDB variant of the same class, retryable for the same reason.
	msg := err.Error()
	return strings.Contains(msg, "(SQLSTATE "+serializationFailureCode+")") ||
		strings.Contains(msg, "RETRY_SERIALIZABLE") ||
		strings.Contains(msg, "TransactionRetryWithProtoRefreshError") ||
		strings.Contains(msg, "WriteTooOldError")
}
