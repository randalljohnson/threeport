package v0

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// uniqueViolationCode is the SQLSTATE both CockroachDB and PostgreSQL
	// return when a unique index rejects a write.
	uniqueViolationCode = "23505"

	// ErrMsgUniqueViolation is the response text for a write a unique index
	// rejected. It says a conflict happened without saying which index found
	// it, because index names are chosen per object and a client that keyed on
	// one would break the first time an index was renamed.
	ErrMsgUniqueViolation = "Object conflicts with an existing object"
)

// UniqueViolation reports whether err is a write a unique index rejected, and
// names the index that rejected it for the server log. The name is empty when
// the driver does not supply one, so a caller logs it rather than depending on
// it.
//
// A rejected write means the request asks for state that already exists, which
// the client can resolve by reading that state or asking for something else.
// That is what separates it from the write failures a client cannot act on.
func UniqueViolation(err error) (string, bool) {
	if err == nil {
		return "", false
	}

	// trust the driver error code when the typed error is present anywhere in
	// the chain. There is no message-text fallback here: unlike a retry, which
	// costs a wasted attempt when the guess is wrong, a wrong guess here
	// answers the client 409 for a failure that was not a conflict.
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolationCode {
		return "", false
	}

	return pgErr.ConstraintName, true
}
