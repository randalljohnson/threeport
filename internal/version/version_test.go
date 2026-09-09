package version

import (
	"strings"
	"testing"
)

// TestGetVersionPrefersReleaseVersion covers the link-time release version
// winning over the embedded one. A release build stamps the tag it publishes
// under, and that is the only place the tag is known, so losing this precedence
// makes every released binary report the development base instead.
func TestGetVersionPrefersReleaseVersion(t *testing.T) {
	// stand in for a release build's -X stamp
	t.Cleanup(func() { ReleaseVersion = "" })
	ReleaseVersion = "v9.9.9-rc.1"
	// the stamped version wins over the embedded one
	if got := GetVersion(); got != "v9.9.9-rc.1" {
		t.Errorf("GetVersion = %q, want the stamped v9.9.9-rc.1", got)
	}
}

// TestGetVersionFallsBackToEmbedded covers an unstamped development build
// reporting the embedded version with the version file's trailing newline
// trimmed, since the value is interpolated into image tags.
func TestGetVersionFallsBackToEmbedded(t *testing.T) {
	// no release build stamped a version
	ReleaseVersion = ""
	// the embedded version stands, trimmed
	got := GetVersion()
	if got == "" {
		t.Fatal("GetVersion is empty, want the embedded version")
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("GetVersion = %q, want no line break", got)
	}
	if want := strings.TrimSuffix(Version, "\n"); got != want {
		t.Errorf("GetVersion = %q, want the embedded %q", got, want)
	}
}
