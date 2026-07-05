package v0

import "testing"

// TestApiVersionAssertsExpectedValue covers the ApiVersion constant.
func TestApiVersionAssertsExpectedValue(t *testing.T) {
	// assert the exported constant matches the v0 client contract
	if ApiVersion != "v0" {
		t.Fatalf("ApiVersion = %q, want %q", ApiVersion, "v0")
	}
}
