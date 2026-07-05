package cmd

import (
	"strings"
	"testing"
	"time"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestFormatEventObject_HappyPathWithName covers the common case where the
// event row has a fully qualified type, a resolved name, and an id. The name
// wins over the id, and the CamelCase kind is converted to kebab-case.
func TestFormatEventObject_HappyPathWithName(t *testing.T) {
	// arrange an event whose ObjectType is fully qualified and whose
	// ObjectName was resolved by the events-join handler
	e := &v0.Event{
		ObjectType: util.Ptr("example.com/v0.RouterInstance"),
		ObjectID:   util.Ptr[uint](42),
		ObjectName: util.Ptr("some-router"),
	}

	// act: render the OBJECT column value
	got := formatEventObject(e)

	// assert: namespace, kebab-kind, and name are joined with slashes
	want := "example.com/router-instance/some-router"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestFormatEventObject_FallsBackToID covers the branch where the name lookup
// did not resolve so the id is used in its place.
func TestFormatEventObject_FallsBackToID(t *testing.T) {
	// arrange an event with a resolvable type and id but no name
	e := &v0.Event{
		ObjectType: util.Ptr("threeport.io/v0.Widget"),
		ObjectID:   util.Ptr[uint](7),
		ObjectName: nil,
	}

	// act
	got := formatEventObject(e)

	// assert: the id occupies the third segment in place of a name
	want := "threeport.io/widget/7"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestFormatEventObject_FallsBackToIDWhenNameEmpty covers the branch where
// ObjectName is a non-nil empty string; DerefString yields "" and the id path
// is taken.
func TestFormatEventObject_FallsBackToIDWhenNameEmpty(t *testing.T) {
	// arrange: empty-string ObjectName is treated as unresolved
	e := &v0.Event{
		ObjectType: util.Ptr("threeport.io/v0.Widget"),
		ObjectID:   util.Ptr[uint](3),
		ObjectName: util.Ptr(""),
	}

	// act
	got := formatEventObject(e)

	// assert: id fallback used because name is empty
	want := "threeport.io/widget/3"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestFormatEventObject_NoNameNoID covers the branch where neither ObjectName
// nor ObjectID resolves; the row still renders the type segments so the
// column is not blank.
func TestFormatEventObject_NoNameNoID(t *testing.T) {
	// arrange: type present but both id and name absent
	e := &v0.Event{
		ObjectType: util.Ptr("threeport.io/v0.Widget"),
		ObjectID:   nil,
		ObjectName: nil,
	}

	// act
	got := formatEventObject(e)

	// assert: two segments; no trailing id or name
	want := "threeport.io/widget"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestFormatEventObject_EmptyType covers the guard clause where ObjectType is
// nil or empty; formatEventObject returns "" so the OBJECT column stays blank.
func TestFormatEventObject_EmptyType(t *testing.T) {
	cases := []struct {
		name string
		e    *v0.Event
	}{
		// nil pointer case: DerefString collapses to ""
		{"nil type", &v0.Event{ObjectType: nil, ObjectName: util.Ptr("x")}},
		// explicit empty string case: still ""
		{"empty string type", &v0.Event{ObjectType: util.Ptr(""), ObjectName: util.Ptr("x")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke with an empty subject type
			got := formatEventObject(tc.e)
			// verify the guard returns an empty string outright
			if got != "" {
				t.Errorf("expected empty string, got %q", got)
			}
		})
	}
}

// TestFormatEventObject_MalformedTypeReturnsRaw covers the branch where
// ParseQualifiedType rejects the input; the raw type is surfaced so the user
// can still grep for it.
func TestFormatEventObject_MalformedTypeReturnsRaw(t *testing.T) {
	cases := []struct {
		name    string
		rawType string
	}{
		// no slash at all
		{"no slash", "notqualified"},
		// slash at position 0 leaves an empty namespace
		{"leading slash", "/v0.Widget"},
		// trailing slash leaves the version+type empty
		{"trailing slash", "example.com/"},
		// no dot after slash means version can't be split from type
		{"no dot", "example.com/v0Widget"},
		// dot at position 0 leaves an empty version
		{"leading dot", "example.com/.Widget"},
		// dot as last char leaves an empty type name
		{"trailing dot", "example.com/v0."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// arrange: pass through the malformed raw type as-is
			e := &v0.Event{
				ObjectType: util.Ptr(tc.rawType),
				ObjectName: util.Ptr("ignored"),
				ObjectID:   util.Ptr[uint](1),
			}

			// act
			got := formatEventObject(e)

			// assert: malformed input is surfaced unchanged
			if got != tc.rawType {
				t.Errorf("expected raw type %q surfaced, got %q", tc.rawType, got)
			}
		})
	}
}

// TestFormatEventObject_KebabConversion covers the CamelCase to kebab-case
// step so a compound type name renders in the same shape the --for flag uses.
func TestFormatEventObject_KebabConversion(t *testing.T) {
	// arrange: multi-word CamelCase type name to exercise strcase.ToKebab
	e := &v0.Event{
		ObjectType: util.Ptr("threeport.io/v0.MachineRuntimeInstance"),
		ObjectName: util.Ptr("host-a"),
	}

	// act
	got := formatEventObject(e)

	// assert: each capital letter boundary becomes a hyphen
	want := "threeport.io/machine-runtime-instance/host-a"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestOutputEventsTable_RendersHeaderAndRows covers the happy path: header
// present and one row per event, in input order, with age/type/reason/object/note.
func TestOutputEventsTable_RendersHeaderAndRows(t *testing.T) {
	// arrange two events with distinct fields so both order and content
	// are observable in the captured output
	past := time.Now().Add(-5 * time.Minute)
	older := time.Now().Add(-3 * time.Hour)
	events := []v0.Event{
		{
			EventTime:  &past,
			Type:       util.Ptr("Normal"),
			Reason:     util.Ptr("Created"),
			Note:       util.Ptr("first-note"),
			ObjectType: util.Ptr("example.com/v0.Widget"),
			ObjectName: util.Ptr("alpha"),
		},
		{
			EventTime:  &older,
			Type:       util.Ptr("Warning"),
			Reason:     util.Ptr("Failed"),
			Note:       util.Ptr("second-note"),
			ObjectType: util.Ptr("example.com/v0.Widget"),
			ObjectName: util.Ptr("beta"),
		},
	}

	// act: render the table with stdout redirected
	out, err := captureStdout(t, func() error {
		return outputEventsTable(&events)
	})

	// assert: nil error, header present, all row values present, no truncation hint
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, want := range []string{
		"AGE", "TYPE", "REASON", "OBJECT", "NOTE",
		"Normal", "Created", "first-note", "alpha",
		"Warning", "Failed", "second-note", "beta",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got %q", want, out)
		}
	}
	// input order is preserved: first row precedes second
	if strings.Index(out, "first-note") > strings.Index(out, "second-note") {
		t.Errorf("expected first-note row before second-note row, got %q", out)
	}
	// no note was truncated so the -o yaml nudge must not appear
	if strings.Contains(out, "-o yaml") {
		t.Errorf("did not expect truncation hint, got %q", out)
	}
}

// TestOutputEventsTable_EmptySlice covers the boundary where the caller
// hands in an empty slice: only the header should print.
func TestOutputEventsTable_EmptySlice(t *testing.T) {
	// arrange: empty slice so the range body never executes
	events := []v0.Event{}

	// act
	out, err := captureStdout(t, func() error {
		return outputEventsTable(&events)
	})

	// assert: nil error, header present, no truncation hint
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "AGE") || !strings.Contains(out, "NOTE") {
		t.Errorf("expected header row, got %q", out)
	}
	// only the header line should remain
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly one line for empty input, got %d: %q", len(lines), out)
	}
	if strings.Contains(out, "-o yaml") {
		t.Errorf("did not expect truncation hint, got %q", out)
	}
}

// TestOutputEventsTable_CollapsesWhitespaceInNote covers the strings.Fields
// pass that flattens multi-line note content to a single spaced string so the
// row doesn't span multiple lines.
func TestOutputEventsTable_CollapsesWhitespaceInNote(t *testing.T) {
	// arrange: note with newlines and runs of spaces
	now := time.Now()
	events := []v0.Event{
		{
			EventTime: &now,
			Type:      util.Ptr("Normal"),
			Reason:    util.Ptr("Ok"),
			Note:      util.Ptr("line one\n   line two\tline three"),
		},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputEventsTable(&events)
	})

	// assert: whitespace runs collapsed to single spaces; original newlines gone
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "line one line two line three") {
		t.Errorf("expected collapsed note in output, got %q", out)
	}
	// no truncation hint: note is under the max
	if strings.Contains(out, "-o yaml") {
		t.Errorf("did not expect truncation hint, got %q", out)
	}
}

// TestOutputEventsTable_TruncatesLongNoteAndHints covers the branch where a
// note exceeds eventMessageTableMax: it is shortened and the reader is nudged
// toward -o yaml at the bottom of the output.
func TestOutputEventsTable_TruncatesLongNoteAndHints(t *testing.T) {
	// arrange: build a note longer than eventMessageTableMax (80)
	longNote := strings.Repeat("x", eventMessageTableMax+50)
	now := time.Now()
	events := []v0.Event{
		{
			EventTime: &now,
			Type:      util.Ptr("Warning"),
			Reason:    util.Ptr("Overflow"),
			Note:      util.Ptr(longNote),
		},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputEventsTable(&events)
	})

	// assert: nil error, full-length note not present, truncation hint printed
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if strings.Contains(out, longNote) {
		t.Errorf("expected long note to be truncated, got full note in %q", out)
	}
	// hint appears when at least one note got shortened
	if !strings.Contains(out, "-o yaml") {
		t.Errorf("expected truncation hint, got %q", out)
	}
}

// TestOutputEventsTable_NilFieldsRenderBlanks covers events with nil pointer
// fields: DerefString collapses each to an empty string so no panic occurs
// and the row still renders.
func TestOutputEventsTable_NilFieldsRenderBlanks(t *testing.T) {
	// arrange: an event with only EventTime set; every other pointer is nil
	now := time.Now()
	events := []v0.Event{
		{EventTime: &now},
	}

	// act
	out, err := captureStdout(t, func() error {
		return outputEventsTable(&events)
	})

	// assert: no panic, no error, header row still present
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(out, "AGE") {
		t.Errorf("expected header row present, got %q", out)
	}
	// no truncation hint for an empty note
	if strings.Contains(out, "-o yaml") {
		t.Errorf("did not expect truncation hint, got %q", out)
	}
}
