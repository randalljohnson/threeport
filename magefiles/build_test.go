package main

import (
	"go/build"
	"path/filepath"
	"testing"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestParallelFromEnvDefaultsToDoubleBuildParallelism covers parallelFromEnv
// self-computing twice the build worker count when PARALLEL_IMAGE_BUILD is
// unset or empty, since packaging images is lighter than compiling.
func TestParallelFromEnvDefaultsToDoubleBuildParallelism(t *testing.T) {
	// an empty value triggers the self-compute path
	t.Setenv("PARALLEL_IMAGE_BUILD", "")
	// the action under test: derive the image-build parallelism
	want := util.BuildParallelism() * 2
	if got := parallelFromEnv(); got != want {
		t.Errorf("parallelFromEnv() = %d, want %d (2x BuildParallelism)", got, want)
	}
}

// TestParallelFromEnvParsesValidCount covers parallelFromEnv returning the
// parsed integer when PARALLEL_IMAGE_BUILD holds a positive number.
func TestParallelFromEnvParsesValidCount(t *testing.T) {
	// an explicit positive count overrides the self-compute
	t.Setenv("PARALLEL_IMAGE_BUILD", "4")
	// the action under test: parse the explicit count
	if got := parallelFromEnv(); got != 4 {
		t.Errorf("parallelFromEnv() = %d, want 4", got)
	}
}

// TestParallelFromEnvFloorsInvalidAndNonPositive covers parallelFromEnv
// returning one for a non-numeric value and for zero or negative counts.
func TestParallelFromEnvFloorsInvalidAndNonPositive(t *testing.T) {
	// each case is a value that cannot serve as a worker count
	for _, in := range []string{"abc", "0", "-3"} {
		t.Run(in, func(t *testing.T) {
			t.Setenv("PARALLEL_IMAGE_BUILD", in)
			// the action under test: a bad or non-positive value floors to one
			if got := parallelFromEnv(); got != 1 {
				t.Errorf("parallelFromEnv(%q) = %d, want 1", in, got)
			}
		})
	}
}

// TestInstallDirPrefersGobin covers installDir returning GOBIN verbatim when it
// is set, ahead of the GOPATH-derived fallback.
func TestInstallDirPrefersGobin(t *testing.T) {
	// an explicit GOBIN wins over any GOPATH derivation
	t.Setenv("GOBIN", "/custom/gobin")
	// the action under test: resolve the install directory
	if got := installDir(); got != "/custom/gobin" {
		t.Errorf("installDir() = %q, want /custom/gobin", got)
	}
}

// TestInstallDirFallsBackToGopathBin covers installDir deriving GOPATH/bin when
// GOBIN is empty, yielding a non-empty directory since build.Default.GOPATH
// falls back to the home go directory.
func TestInstallDirFallsBackToGopathBin(t *testing.T) {
	// no GOBIN forces the GOPATH-derived fallback
	t.Setenv("GOBIN", "")
	// the action under test: resolve the install directory
	got := installDir()
	// the fallback is GOPATH/bin and is never empty
	want := filepath.Join(build.Default.GOPATH, "bin")
	if got != want {
		t.Errorf("installDir() = %q, want %q", got, want)
	}
	if got == "" {
		t.Errorf("installDir() returned empty, want a non-empty GOPATH/bin")
	}
}

// TestEnvOrReturnsSetValue covers envOr returning the trimmed value of a set
// env var ahead of the default.
func TestEnvOrReturnsSetValue(t *testing.T) {
	// a set value with surrounding whitespace is returned trimmed
	t.Setenv("TPT_TEST_ENVOR", "  hello  ")
	// the action under test: read the env var with a default
	if got := envOr("TPT_TEST_ENVOR", "fallback"); got != "hello" {
		t.Errorf("envOr() = %q, want %q", got, "hello")
	}
}

// TestEnvOrFallsBackOnEmptyOrWhitespace covers envOr returning the default when
// the env var is unset, empty, or whitespace-only.
func TestEnvOrFallsBackOnEmptyOrWhitespace(t *testing.T) {
	// each case is an env value that should yield the default
	cases := []struct {
		name string
		set  bool
		val  string
	}{
		// the var is never set
		{"unset", false, ""},
		// the var is set but empty
		{"empty", true, ""},
		// the var holds only whitespace
		{"whitespace only", true, "   "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.set {
				t.Setenv("TPT_TEST_ENVOR", c.val)
			}
			// the action under test: an unusable value yields the default
			if got := envOr("TPT_TEST_ENVOR", "fallback"); got != "fallback" {
				t.Errorf("envOr() = %q, want %q", got, "fallback")
			}
		})
	}
}
