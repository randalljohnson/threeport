package v0

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidHLCToken verifies only a bare CockroachDB HLC decimal is accepted.
// The token is interpolated into an AS OF SYSTEM TIME clause, so anything that
// carries SQL, whitespace, or a newline has to be rejected.
func TestValidHLCToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
		valid bool
	}{
		{"plain decimal", "1712345678901234567.0000000001", true},
		{"single digit either side", "1.0", true},
		{"long logical counter", "1712345678901234567.9999999999", true},

		{"empty", "", false},
		{"integer with no fraction", "1712345678901234567", false},
		{"fraction with no integer", ".0000000001", false},
		{"integer with trailing dot", "1712345678901234567.", false},
		{"two dots", "1712.345.678", false},
		{"negative", "-1712345678901234567.0000000001", false},
		{"exponent notation", "1.7e18", false},
		{"leading space", " 1712345678901234567.0000000001", false},
		{"trailing space", "1712345678901234567.0000000001 ", false},

		// the reason the guard exists: a token reaches the SQL string
		// directly, so each of these has to fail closed
		{"quote escape", "1712345678901234567.0000000001'", false},
		{"statement terminator", "1712345678901234567.0000000001; DROP TABLE v0_events", false},
		{"trailing newline", "1712345678901234567.0000000001\n", false},
		{"newline then sql", "1712345678901234567.0000000001\nDROP TABLE v0_events", false},
		{"leading newline", "\n1712345678901234567.0000000001", false},
		{"comment suffix", "1712345678901234567.0000000001 -- ", false},
		{"subquery", "(SELECT 1)", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.valid, ValidHLCToken(test.token))
		})
	}
}

// TestValidPaginationQueryId verifies only a queryid of the shape the server
// issues is accepted. The value names a materialized view in a LIKE pattern, so
// anything carrying a quote, a wildcard, or SQL has to be rejected.
func TestValidPaginationQueryId(t *testing.T) {
	tests := []struct {
		name    string
		queryId string
		valid   bool
	}{
		{"issued id", "a1b2c3d4e5f6g7h8", true},
		{"all digits", "1234567890123456", true},
		{"all letters", "abcdefghijklmnop", true},

		{"empty", "", false},
		{"too short", "a1b2c3d4e5f6g7h", false},
		{"too long", "a1b2c3d4e5f6g7h8i", false},
		{"uppercase", "A1B2C3D4E5F6G7H8", false},
		{"hyphen", "a1b2c3d4-e5f6g7h", false},
		{"leading space", " 1b2c3d4e5f6g7h8", false},

		// the reason the guard exists: the value reaches a LIKE pattern, so
		// each of these has to fail closed
		{"quote escape", "a1b2c3d4e5f6g7h8'", false},
		{"quote then always true", "' OR '1'='1", false},
		{"comment suffix", "a1b2c3d4e5f6g7h8' -- ", false},
		{"statement terminator", "a1b2c3d4e5f6g7h8'; DROP TABLE v0_events; --", false},
		{"union select", "' UNION SELECT table_name FROM information_schema.tables --", false},
		{"bare wildcard", "%", false},
		{"trailing newline", "a1b2c3d4e5f6g7h8\n", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.valid, ValidPaginationQueryId(test.queryId))
		})
	}
}

// TestTranslatePaginationSessionError verifies a CockroachDB garbage-collection
// error is rewritten into the restart hint and every other error is handed back
// untouched, with the original still reachable through errors.Is.
func TestTranslatePaginationSessionError(t *testing.T) {
	t.Run("nil stays nil", func(t *testing.T) {
		assert.NoError(t, TranslatePaginationSessionError(nil))
	})

	t.Run("gc threshold error gains the restart hint", func(t *testing.T) {
		crdbErr := errors.New(
			"batch timestamp 1712345678901234567.0000000001 must be after replica GC threshold 1712345679901234567.0000000000",
		)

		translated := TranslatePaginationSessionError(crdbErr)

		require.Error(t, translated)
		assert.Contains(t, translated.Error(), "pagination session expired")
		assert.Contains(t, translated.Error(), "restart pagination with no queryid")
		assert.ErrorIs(t, translated, crdbErr, "the CRDB error stays wrapped for the logs")
	})

	t.Run("wrapped gc threshold error is still recognized", func(t *testing.T) {
		crdbErr := fmt.Errorf(
			"handler error: error finding records: %w",
			errors.New("batch timestamp 1.0 must be after replica GC threshold 2.0"),
		)

		translated := TranslatePaginationSessionError(crdbErr)

		assert.Contains(t, translated.Error(), "pagination session expired")
	})

	t.Run("unrelated error is returned unchanged", func(t *testing.T) {
		other := errors.New("connection refused")

		translated := TranslatePaginationSessionError(other)

		assert.Same(t, other, translated)
	})

	t.Run("half a match is not a match", func(t *testing.T) {
		// both substrings are required; either one alone is a different
		// failure and must not be reported as an expired session
		for _, msg := range []string{
			"batch timestamp 1.0 is malformed",
			"replica GC threshold exceeded for range 7",
		} {
			other := errors.New(msg)
			assert.Same(t, other, TranslatePaginationSessionError(other), msg)
		}
	})
}
