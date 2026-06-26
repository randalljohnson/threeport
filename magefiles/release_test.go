package main

import "testing"

// TestValidateBaseAcceptsCleanVersions covers validateBase() accepting a
// bare X.Y.Z and a v-prefixed one, returning the version without the v.
func TestValidateBaseAcceptsCleanVersions(t *testing.T) {
	// each input is a well-formed base, with or without the leading v
	cases := []struct{ in, want string }{
		{"0.7.0", "0.7.0"},
		{"v0.7.0", "0.7.0"},
		{"10.20.30", "10.20.30"},
	}
	for _, c := range cases {
		// the action under test: normalize and validate the base
		got, err := validateBase(c.in)
		// a clean version validates and comes back without the leading v
		if err != nil {
			t.Errorf("validateBase(%q) returned error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("validateBase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestValidateBaseRejectsMalformed covers validateBase() rejecting anything
// that is not a bare X.Y.Z core, including prerelease and partial versions.
func TestValidateBaseRejectsMalformed(t *testing.T) {
	// each input is not a bare three-part numeric version
	for _, in := range []string{"0.7", "0.7.0-dev.1", "1.2.3.4", "x.y.z", ""} {
		// the action under test: validate a malformed base
		if _, err := validateBase(in); err == nil {
			// a malformed base must surface an error, not pass silently
			t.Errorf("validateBase(%q) accepted a malformed version", in)
		}
	}
}

// TestHighestCounterOrdersNumerically covers highestCounter() selecting the
// max counter by numeric value, the case that catches the dev.10 > dev.9
// lexical-sort footgun.
func TestHighestCounterOrdersNumerically(t *testing.T) {
	// tags span single- and double-digit counters under the dev prefix
	tags := []string{"v0.7.0-dev.1", "v0.7.0-dev.2", "v0.7.0-dev.9", "v0.7.0-dev.10"}
	// the action under test: pick the highest counter under the prefix
	got := highestCounter(tags, "v0.7.0-dev.")
	// 10 must win over 9, which a lexical sort would get wrong
	if got != 10 {
		t.Errorf("highestCounter = %d, want 10", got)
	}
}

// TestHighestCounterEmptyIsZero covers highestCounter() returning 0 for no
// matching tags, so the first cut falls through to counter 1.
func TestHighestCounterEmptyIsZero(t *testing.T) {
	// no tags at all, the first-cut bootstrap case
	if got := highestCounter(nil, "v0.7.0-dev."); got != 0 {
		t.Errorf("highestCounter(nil) = %d, want 0", got)
	}
}

// TestHighestCounterIgnoresOtherChannels covers highestCounter() counting
// only tags under the requested prefix, keeping the dev and rc counters
// independent.
func TestHighestCounterIgnoresOtherChannels(t *testing.T) {
	// a mix of dev and rc tags under the same base
	tags := []string{"v0.7.0-dev.3", "v0.7.0-rc.7", "v0.7.0-rc.8"}
	// counting the dev prefix must ignore the rc tags
	if got := highestCounter(tags, "v0.7.0-dev."); got != 3 {
		t.Errorf("highestCounter(dev) = %d, want 3", got)
	}
	// and counting rc must ignore dev
	if got := highestCounter(tags, "v0.7.0-rc."); got != 8 {
		t.Errorf("highestCounter(rc) = %d, want 8", got)
	}
}

// TestFormatVersionChannelAndGa covers formatVersion() producing a
// counter-suffixed tag for a channel build and a bare base for ga.
func TestFormatVersionChannelAndGa(t *testing.T) {
	// a dev channel build carries the channel and counter
	if got := formatVersion("0.7.0", "dev", false, 3); got != "v0.7.0-dev.3" {
		t.Errorf("formatVersion dev = %q, want v0.7.0-dev.3", got)
	}
	// an rc build carries the rc channel and counter
	if got := formatVersion("0.7.0", "rc", false, 1); got != "v0.7.0-rc.1" {
		t.Errorf("formatVersion rc = %q, want v0.7.0-rc.1", got)
	}
	// a ga build is the bare v-prefixed base, ignoring channel and counter
	if got := formatVersion("0.7.0", "", true, 0); got != "v0.7.0" {
		t.Errorf("formatVersion ga = %q, want v0.7.0", got)
	}
}
