package v0

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// CockroachDB reports a serialization conflict when overlapping transactions
// cannot be placed in a valid one-at-a-time ordering, and in a handful of
// adjacent situations such as clock uncertainty between nodes. Rather than
// record an answer no serial execution could produce, it aborts one of the
// transactions, rolls it back whole, and returns SQLSTATE 40001, which asks the
// client to run the whole transaction again.
//
// That class reaches a client here because CockroachDB runs at the SERIALIZABLE
// isolation level by default, which hands the conflict back rather than
// absorbing it. PostgreSQL defaults to the weaker READ COMMITTED, where
// contending writes block and re-evaluate instead of aborting, so with that
// default in mind SQLSTATE 40001 is easy to treat as a case that never happens.
// On CockroachDB it happens during routine contention.
const (
	// serializationFailureCode is the SQLSTATE both CockroachDB and PostgreSQL
	// return on a serialization conflict.
	serializationFailureCode = "40001"

	// serializationRetryMax bounds how many times a write is re-run after a
	// serialization conflict before the error is surfaced to the caller. A
	// burst of concurrent updates to one row outlasts a budget of a few
	// attempts, so the cap sits high enough to absorb a burst and low enough
	// to bound how long a write that keeps conflicting holds its connection.
	serializationRetryMax = 12

	// serializationRetryBaseDelay seeds the exponential backoff. Doubling and
	// jitter spread contending writers across a wide enough window that a
	// stampede resolves within the retry budget.
	serializationRetryBaseDelay = 10 * time.Millisecond

	// serializationRetryMaxDelay caps the per-attempt wait so a long tail on
	// exponential backoff does not stall the request context.
	serializationRetryMaxDelay = 500 * time.Millisecond
)

// RetryWrite runs a database write and re-runs it from the start when the write
// fails with a serialization conflict, up to a bounded number of attempts. It
// returns the result of the last attempt so callers inspect result.Error as
// usual. Non-retryable errors are returned on the first attempt without delay.
// Retrying stops as soon as ctx is done, so a caller that hung up mid-request
// does not hold a goroutine and a database connection through the backoff.
//
// write carries out one transaction and nothing else. A conflict rolls that
// transaction back whole, so the re-run starts against a database no part of
// the failed attempt reached, and it usually succeeds once the conflicting
// writer commits. A closure that writes twice, or that also changes state
// outside the database, re-applies whatever already landed and must not be
// passed here.
func RetryWrite(ctx context.Context, write func() *gorm.DB) *gorm.DB {
	var result *gorm.DB
	for attempt := 0; attempt < serializationRetryMax; attempt++ {
		result = write()
		if result.Error == nil || !isSerializationFailure(result.Error) {
			return result
		}

		// the budget is spent, so hand the conflict back now rather than
		// waiting out a backoff no further attempt follows
		if attempt == serializationRetryMax-1 {
			break
		}

		// wait out the backoff, but give up on the whole retry budget the
		// moment the request is cancelled. Stop the timer on that path so the
		// runtime reclaims it now rather than at its deadline.
		timer := time.NewTimer(serializationRetryBackoff(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return result
		case <-timer.C:
		}
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
