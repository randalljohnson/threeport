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
// serialization conflict, which is retryable. It checks the driver error code
// first and falls back to the error text so the detection still holds when the
// code is wrapped or the underlying type is not surfaced.
func isSerializationFailure(err error) bool {
	if err == nil {
		return false
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == serializationFailureCode {
		return true
	}

	msg := err.Error()
	return strings.Contains(msg, serializationFailureCode) ||
		strings.Contains(msg, "RETRY_SERIALIZABLE") ||
		strings.Contains(msg, "TransactionRetryWithProtoRefreshError")
}
