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
