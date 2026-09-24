package v0

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestValidHLCToken covers the anchored regex used to gate
// AS OF SYSTEM TIME interpolation: accepts a well-formed CRDB HLC
// decimal (digits, one dot, digits) and rejects everything else.
func TestValidHLCToken(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		// happy path: canonical CRDB HLC token
		{"canonical hlc", "1712345678901234567.0000000001", true},
		{"simple decimal", "1.1", true},
		{"multi digit both sides", "1234567890.987654321", true},
		// boundary: empty string is not a valid decimal
		{"empty", "", false},
		// missing dot: pure integer must not pass since regex requires the dot
		{"no dot", "1234567890", false},
		// missing left digits
		{"missing left", ".123", false},
		// missing right digits
		{"missing right", "123.", false},
		// leading sign is not permitted because pattern is anchored to digits
		{"leading plus", "+1.1", false},
		{"leading minus", "-1.1", false},
		// injection attempt: sql fragment appended after decimal
		{"sql suffix injection", "1.1'; DROP TABLE users;--", false},
		// injection attempt: sql fragment prepended
		{"sql prefix injection", "1=1 OR 1.1", false},
		// two dots reject
		{"two dots", "1.2.3", false},
		// whitespace pads reject due to anchoring
		{"leading space", " 1.1", false},
		{"trailing newline", "1.1\n", false},
		// letters reject
		{"letters", "1e10", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// call the pure predicate and assert the boolean matches expectation
			got := ValidHLCToken(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestTranslatePaginationSessionError covers the three arms: nil
// passes through as nil, an error carrying the CRDB GC-threshold
// signature gets wrapped with the restart-pagination guidance while
// preserving the original via errors.Is, and any other error is
// returned unchanged.
func TestTranslatePaginationSessionError(t *testing.T) {
	// nil input yields nil output
	t.Run("nil error returns nil", func(t *testing.T) {
		assert.Nil(t, TranslatePaginationSessionError(nil))
	})

	// CRDB GC-threshold signature triggers wrapping with the restart-pagination
	// guidance; the original error must remain reachable through errors.Is
	t.Run("gc threshold error wrapped with restart guidance", func(t *testing.T) {
		original := errors.New("batch timestamp 123 must be after replica GC threshold 456")

		got := TranslatePaginationSessionError(original)

		// wrapper prepends the user-facing restart message
		assert.Error(t, got)
		assert.Contains(t, got.Error(), "pagination session expired")
		assert.Contains(t, got.Error(), "restart pagination with no queryid")
		// wrapping uses %w so errors.Is still resolves back to the original
		assert.True(t, errors.Is(got, original))
	})

	// arbitrary error is not touched
	t.Run("unrelated error returned unchanged", func(t *testing.T) {
		original := errors.New("some other db failure")

		got := TranslatePaginationSessionError(original)

		// identity is preserved so upstream error handling is unaffected
		assert.Same(t, original, got)
	})

	// only one of the two substrings present does not trigger wrapping;
	// both must appear for the match to fire
	t.Run("only batch timestamp substring not wrapped", func(t *testing.T) {
		original := errors.New("batch timestamp is stale")

		got := TranslatePaginationSessionError(original)

		assert.Same(t, original, got)
	})
	t.Run("only replica gc substring not wrapped", func(t *testing.T) {
		original := errors.New("replica GC threshold advanced")

		got := TranslatePaginationSessionError(original)

		assert.Same(t, original, got)
	})

	// wrapper works over fmt.Errorf-created errors too, not just errors.New
	t.Run("wraps fmt errorf constructed error", func(t *testing.T) {
		original := fmt.Errorf("batch timestamp 1 below replica GC threshold 2")

		got := TranslatePaginationSessionError(original)

		assert.NotSame(t, original, got)
		assert.True(t, errors.Is(got, original))
	})
}

// TestCleanupMaterializedViews covers the smoke path that the
// scheduler launches without panicking. The function has no
// cancellation seam (fire-and-forget ticker goroutine), so this
// test uses a very long interval so the ticker never fires during
// the test and the goroutine sits idle when the test exits.
func TestCleanupMaterializedViews(t *testing.T) {
	logger := zap.NewNop()

	// pass a huge interval so the internal ticker never fires while the
	// test is running; the goal is only to verify the scheduler starts
	// without panicking against a nil db (the ticker branch is guarded
	// so nothing dereferences db until it fires)
	assert.NotPanics(t, func() {
		CleanupMaterializedViews(nil, logger, 3600, 1)
	})
}
