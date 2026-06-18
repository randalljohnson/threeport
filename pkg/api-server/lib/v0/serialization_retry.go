package v0

import (
	"errors"
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
	// serialization conflict before the error is surfaced to the caller.
	serializationRetryMax = 5

	// serializationRetryBaseDelay is the wait before the first retry. The
	// wait grows linearly with each subsequent attempt to spread out
	// contending writers.
	serializationRetryBaseDelay = 10 * time.Millisecond
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
		time.Sleep(serializationRetryBaseDelay * time.Duration(attempt+1))
	}

	return result
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
	// message cannot be mistaken for the code.
	msg := err.Error()
	return strings.Contains(msg, "(SQLSTATE "+serializationFailureCode+")") ||
		strings.Contains(msg, "RETRY_SERIALIZABLE") ||
		strings.Contains(msg, "TransactionRetryWithProtoRefreshError")
}
