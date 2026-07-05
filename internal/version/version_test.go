package version

import (
	"strings"
	"testing"
)

// TestGetVersionStripsTrailingNewline asserts GetVersion() returns the embedded
// version with any trailing newline removed.
func TestGetVersionStripsTrailingNewline(t *testing.T) {
	// invoke the function under test
	got := GetVersion()

	// assert no trailing newline remains
	if strings.HasSuffix(got, "\n") {
		t.Errorf("GetVersion() = %q, must not end with a newline", got)
	}

	// assert the returned value matches the embedded version with newline trimmed
	want := strings.TrimSuffix(Version, "\n")
	if got != want {
		t.Errorf("GetVersion() = %q, want %q", got, want)
	}
}

// TestGetVersionNonEmpty asserts GetVersion() returns a non-empty string, so
// the embedded version.txt is populated at build time.
func TestGetVersionNonEmpty(t *testing.T) {
	// invoke the function under test
	got := GetVersion()

	// assert the result is not empty
	if got == "" {
		t.Fatal("GetVersion() returned empty string; version.txt must be embedded and non-empty")
	}
}

// TestVersionEmbedded asserts the Version variable is populated from the
// embedded version.txt file.
func TestVersionEmbedded(t *testing.T) {
	// assert the embedded variable is non-empty
	if Version == "" {
		t.Fatal("Version is empty; //go:embed directive failed to populate it")
	}
}
