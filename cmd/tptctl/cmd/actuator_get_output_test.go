package cmd

import (
	"io"
	"os"
	"strings"
	"testing"

	config_v0 "github.com/threeport/threeport/pkg/config/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written during that window as a string.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	// swap os.Stdout for an in-memory pipe so we can read what was written
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to open pipe: %v", err)
	}
	os.Stdout = w

	// restore stdout on return so a failing fn cannot leak the swap
	defer func() {
		os.Stdout = orig
	}()

	fnErr := fn()
	// close the writer so the reader sees EOF
	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("failed to close writer: %v", closeErr)
	}

	buf, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("failed to read captured stdout: %v", readErr)
	}
	return string(buf), fnErr
}

// TestOutputGetv0ProfilesCmd_RendersHeaderAndRows covers the happy path
// where each profile in the slice contributes a NAME + AGE row under the
// tabwriter header.
func TestOutputGetv0ProfilesCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two profiles so we can assert on both order and per-row content
	profiles := []config_v0.ProfileConfig{
		{Profile: config_v0.ProfileValues{Name: util.Ptr("alice"), Age: util.Ptr("30")}},
		{Profile: config_v0.ProfileValues{Name: util.Ptr("bob"), Age: util.Ptr("42")}},
	}

	// act: invoke the command with stdout redirected so we can inspect output
	out, err := captureStdout(t, func() error {
		return outputGetv0ProfilesCmd(&profiles)
	})

	// assert: nil error, header present, and each row's name and age rendered
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "AGE") {
		t.Errorf("expected header with NAME and AGE, got %q", out)
	}
	for _, want := range []string{"alice", "30", "bob", "42"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	// alice row precedes bob row: preserves iteration order of the input slice
	if strings.Index(out, "alice") > strings.Index(out, "bob") {
		t.Errorf("expected alice row before bob row, got %q", out)
	}
}

// TestOutputGetv0ProfilesCmd_EmptySlice covers the boundary where the caller
// hands in an empty slice: only the header should print and no rows.
func TestOutputGetv0ProfilesCmd_EmptySlice(t *testing.T) {
	// arrange: an empty slice so the range body never executes
	profiles := []config_v0.ProfileConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0ProfilesCmd(&profiles)
	})

	// assert: header renders, but no row separators for values appear
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "AGE") {
		t.Errorf("expected header row, got %q", out)
	}
	// only the header line should be present
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one line for empty input, got %d: %q", len(lines), out)
	}
}

// TestOutputGetv0TiersCmd_RendersHeaderAndRows covers the happy path for the
// tier output: NAME + CRITICALITY + AGE column headers plus a row per tier.
func TestOutputGetv0TiersCmd_RendersHeaderAndRows(t *testing.T) {
	// arrange two tiers with distinct criticality values so column population
	// is observable in the captured output
	tiers := []config_v0.TierConfig{
		{Tier: config_v0.TierValues{
			Name:        util.Ptr("gold"),
			Criticality: util.Ptr(1),
			Age:         util.Ptr("5d"),
		}},
		{Tier: config_v0.TierValues{
			Name:        util.Ptr("silver"),
			Criticality: util.Ptr(2),
			Age:         util.Ptr("10d"),
		}},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0TiersCmd(&tiers)
	})

	// assert: nil error, three-column header, and per-row values present
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{"NAME", "CRITICALITY", "AGE", "gold", "1", "5d", "silver", "2", "10d"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	// gold row precedes silver row: preserves input order
	if strings.Index(out, "gold") > strings.Index(out, "silver") {
		t.Errorf("expected gold row before silver row, got %q", out)
	}
}

// TestOutputGetv0TiersCmd_EmptySlice covers the boundary where the caller
// hands in an empty slice: only the header should print.
func TestOutputGetv0TiersCmd_EmptySlice(t *testing.T) {
	// arrange
	tiers := []config_v0.TierConfig{}

	// act
	out, err := captureStdout(t, func() error {
		return outputGetv0TiersCmd(&tiers)
	})

	// assert: only the header line remains
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "CRITICALITY") {
		t.Errorf("expected header row with CRITICALITY, got %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one line for empty input, got %d: %q", len(lines), out)
	}
}
