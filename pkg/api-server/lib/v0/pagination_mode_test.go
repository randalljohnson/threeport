package v0

import "testing"

// TestValidPaginationMode_AcceptsSupportedModesAndRejectsOthers covers the
// supported constants, the empty string, and arbitrary unknown input.
func TestValidPaginationMode_AcceptsSupportedModesAndRejectsOthers(t *testing.T) {
	// each case pairs an input with the expected accept/reject verdict
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "accepts as-of-system-time",
			input: string(PaginationModeAsOfSystemTime),
			want:  true,
		},
		{
			name:  "accepts materialized-view",
			input: string(PaginationModeMaterializedView),
			want:  true,
		},
		{
			name:  "rejects empty string",
			input: "",
			want:  false,
		},
		{
			name:  "rejects unknown mode",
			input: "snapshot",
			want:  false,
		},
		{
			name:  "rejects case-mismatched supported mode",
			input: "As-Of-System-Time",
			want:  false,
		},
		{
			name:  "rejects value with surrounding whitespace",
			input: " as-of-system-time ",
			want:  false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// invoke the validator with the case's input
			got := ValidPaginationMode(tc.input)
			// verify the verdict matches the expected accept/reject decision
			if got != tc.want {
				t.Fatalf("ValidPaginationMode(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestPaginationModeConstants_HaveExpectedStringValues asserts the wire values
// of the exported PaginationMode constants so consumers reading configuration
// or query parameters can rely on them.
func TestPaginationModeConstants_HaveExpectedStringValues(t *testing.T) {
	// verify the as-of-system-time constant's underlying string
	if got := string(PaginationModeAsOfSystemTime); got != "as-of-system-time" {
		t.Errorf("PaginationModeAsOfSystemTime = %q, want %q", got, "as-of-system-time")
	}
	// verify the materialized-view constant's underlying string
	if got := string(PaginationModeMaterializedView); got != "materialized-view" {
		t.Errorf("PaginationModeMaterializedView = %q, want %q", got, "materialized-view")
	}
}
