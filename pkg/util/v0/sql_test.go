package v0

import (
	"testing"
)

// TestSqlNullInt64 covers nil, zero, and non-zero uint inputs to confirm
// the returned sql.NullInt64 mirrors the input value and validity.
func TestSqlNullInt64(t *testing.T) {
	zero := uint(0)
	positive := uint(42)
	maxVal := ^uint(0) >> 1 // stay within int64 positive range

	tests := []struct {
		name      string
		input     *uint
		wantValid bool
		wantInt64 int64
	}{
		{
			name:      "nil input returns invalid NullInt64",
			input:     nil,
			wantValid: false,
			wantInt64: 0,
		},
		{
			name:      "zero value is still valid",
			input:     &zero,
			wantValid: true,
			wantInt64: 0,
		},
		{
			name:      "positive value is preserved",
			input:     &positive,
			wantValid: true,
			wantInt64: 42,
		},
		{
			name:      "large value fits int64",
			input:     &maxVal,
			wantValid: true,
			wantInt64: int64(maxVal),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// act: invoke conversion helper
			got := SqlNullInt64(tt.input)

			// assert: non-nil pointer returned regardless of input
			if got == nil {
				t.Fatalf("SqlNullInt64 returned nil pointer")
			}

			// assert: Valid reflects whether the input pointer was non-nil
			if got.Valid != tt.wantValid {
				t.Errorf("Valid = %v, want %v", got.Valid, tt.wantValid)
			}

			// assert: Int64 carries the dereferenced value when valid
			if got.Int64 != tt.wantInt64 {
				t.Errorf("Int64 = %d, want %d", got.Int64, tt.wantInt64)
			}
		})
	}
}

// TestSqlNullInt64ReturnsDistinctPointers confirms each call returns a
// fresh sql.NullInt64 so callers can mutate results independently.
func TestSqlNullInt64ReturnsDistinctPointers(t *testing.T) {
	// act: call twice with the same nil input
	a := SqlNullInt64(nil)
	b := SqlNullInt64(nil)

	// assert: the two returned pointers are distinct allocations
	if a == b {
		t.Errorf("expected distinct pointers, got same address %p", a)
	}
}
