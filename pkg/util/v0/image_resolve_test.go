package v0

import (
	"strings"
	"testing"
)

// TestResolveImageRepoPrefersExplicitOverride covers ResolveImageRepo
// returning IMAGE_REPO verbatim when set, ahead of any CI or dev derivation.
func TestResolveImageRepoPrefersExplicitOverride(t *testing.T) {
	// an explicit override plus CI signals that would otherwise derive ghcr
	t.Setenv("IMAGE_REPO", "localhost:5001")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY_OWNER", "AcmeCorp")
	// the override wins over the CI derivation
	if got := ResolveImageRepo("localhost:5001"); got != "localhost:5001" {
		t.Errorf("ResolveImageRepo = %q, want the IMAGE_REPO override", got)
	}
}

// TestResolveImageRepoDerivesGhcrInCI covers ResolveImageRepo building a
// lowercased ghcr namespace from the repository owner under GitHub Actions.
func TestResolveImageRepoDerivesGhcrInCI(t *testing.T) {
	// no override, in CI, mixed-case owner
	t.Setenv("IMAGE_REPO", "")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY_OWNER", "AcmeCorp")
	// the owner lowercases into the ghcr namespace
	if got := ResolveImageRepo("localhost:5001"); got != "ghcr.io/acmecorp" {
		t.Errorf("ResolveImageRepo = %q, want ghcr.io/acmecorp", got)
	}
}

// TestResolveImageRepoFallsBackToDevDefault covers ResolveImageRepo returning
// the dev default outside CI with no override.
func TestResolveImageRepoFallsBackToDevDefault(t *testing.T) {
	// no override, not in CI
	t.Setenv("IMAGE_REPO", "")
	t.Setenv("GITHUB_ACTIONS", "")
	// the local dev registry is the fallback
	if got := ResolveImageRepo("localhost:5001"); got != "localhost:5001" {
		t.Errorf("ResolveImageRepo = %q, want localhost:5001", got)
	}
}

// TestResolveImageTagPrefersExplicitOverride covers ResolveImageTag returning
// IMAGE_TAG verbatim when set.
func TestResolveImageTagPrefersExplicitOverride(t *testing.T) {
	// an explicit tag override under CI
	t.Setenv("IMAGE_TAG", "v9.9.9")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("ARCH", "")
	// the override wins
	got, err := ResolveImageTag("v0.1.0-dev")
	if err != nil {
		t.Fatalf("ResolveImageTag returned error: %v", err)
	}
	if got != "v9.9.9" {
		t.Errorf("ResolveImageTag = %q, want the IMAGE_TAG override", got)
	}
}

// TestResolveImageTagEchoesRefNameOnTagBuild covers ResolveImageTag returning
// the pushed ref name on a CI tag build.
func TestResolveImageTagEchoesRefNameOnTagBuild(t *testing.T) {
	// a CI tag build carries the ref name
	t.Setenv("IMAGE_TAG", "")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REF_TYPE", "tag")
	t.Setenv("GITHUB_REF_NAME", "v0.1.0-dev.3")
	t.Setenv("ARCH", "")
	// the tag build echoes the ref name verbatim
	got, err := ResolveImageTag("v0.1.0-dev")
	if err != nil {
		t.Fatalf("ResolveImageTag returned error: %v", err)
	}
	if got != "v0.1.0-dev.3" {
		t.Errorf("ResolveImageTag = %q, want the ref name", got)
	}
}

// isolateFromGit puts the resolver somewhere no repository can be found, so the
// sha read fails and the fallback path runs. Changing directory alone is not
// enough: git exports GIT_DIR and GIT_WORK_TREE to hook processes, so a suite
// invoked from a pre-push hook still resolves the repository from the
// environment whatever the working directory is, and the fallback assertions
// then see a real sha. Emptying those makes the failure deterministic wherever
// the suite runs.
func isolateFromGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_DIR", "")
	t.Setenv("GIT_WORK_TREE", "")
	t.Setenv("GIT_COMMON_DIR", "")
	t.Chdir(t.TempDir())
}

// TestResolveImageTagFallsBackToVersionOutsideCheckout covers ResolveImageTag
// returning the bare version default when it runs outside CI and outside a git
// checkout, where no short commit sha is available to suffix.
func TestResolveImageTagFallsBackToVersionOutsideCheckout(t *testing.T) {
	// not in CI, and run from outside any git checkout so the sha read fails
	// and the resolver falls back to the bare version default
	t.Setenv("IMAGE_TAG", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("ARCH", "")
	isolateFromGit(t)
	// the bare version default passes through when no sha is available
	got, err := ResolveImageTag("v0.1.0-dev")
	if err != nil {
		t.Fatalf("ResolveImageTag returned error: %v", err)
	}
	if got != "v0.1.0-dev" {
		t.Errorf("ResolveImageTag = %q, want v0.1.0-dev", got)
	}
}

// TestResolveImageTagSuffixesShaInCheckout covers ResolveImageTag suffixing the
// version default with the short commit sha when it runs outside CI inside a git
// checkout, so the tag names the exact commit built.
func TestResolveImageTagSuffixesShaInCheckout(t *testing.T) {
	// not in CI; the test runs inside the repo checkout, so a short sha is read
	t.Setenv("IMAGE_TAG", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("ARCH", "")
	// the resolved tag is <version>.<sha> with a seven-character sha
	got, err := ResolveImageTag("v0.1.0-dev")
	if err != nil {
		t.Fatalf("ResolveImageTag returned error: %v", err)
	}
	prefix := "v0.1.0-dev."
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("ResolveImageTag = %q, want prefix %q", got, prefix)
	}
	if sha := strings.TrimPrefix(got, prefix); len(sha) != 7 {
		t.Errorf("sha suffix = %q, want seven characters", sha)
	}
}

// TestResolveImageTagIgnoresArch covers ResolveImageTag leaving the canonical
// tag undecorated whatever ARCH holds. tptctl up and the module install command
// resolve the tag they pull through here, so an ARCH exported in the caller's
// shell must not point an install at a single-arch image.
func TestResolveImageTagIgnoresArch(t *testing.T) {
	// an explicit override plus a single-arch ARCH that a build would decorate with
	t.Setenv("IMAGE_TAG", "v9.9.9")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("ARCH", "arm64")
	// the canonical tag passes through undecorated
	got, err := ResolveImageTag("v0.1.0-dev")
	if err != nil {
		t.Fatalf("ResolveImageTag returned error: %v", err)
	}
	if got != "v9.9.9" {
		t.Errorf("ResolveImageTag = %q, want the undecorated v9.9.9", got)
	}
}

// TestBuildImageTagDecoratesSingleArch covers buildImageTag decorating the
// canonical tag with -<arch> when ARCH names a single arch.
func TestBuildImageTagDecoratesSingleArch(t *testing.T) {
	// an explicit override plus a single-arch ARCH
	t.Setenv("IMAGE_TAG", "v9.9.9")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("ARCH", "arm64")
	// the arch decorates the resolved tag
	got, err := buildImageTag("v0.1.0-dev")
	if err != nil {
		t.Fatalf("buildImageTag returned error: %v", err)
	}
	if got != "v9.9.9-arm64" {
		t.Errorf("buildImageTag = %q, want v9.9.9-arm64", got)
	}
}

// TestBuildImageTagSingleArchDecoratesFallbackVersion covers the -<arch>
// decoration landing on the bare version default when the resolver falls back
// outside CI and outside a git checkout.
func TestBuildImageTagSingleArchDecoratesFallbackVersion(t *testing.T) {
	// not in CI, single-arch ARCH set, and run from outside any git checkout so
	// the resolver falls back to the bare version default before decorating
	t.Setenv("IMAGE_TAG", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("ARCH", "amd64")
	isolateFromGit(t)
	// the arch decorates the bare fallback version
	got, err := buildImageTag("v0.1.0-dev")
	if err != nil {
		t.Fatalf("buildImageTag returned error: %v", err)
	}
	if got != "v0.1.0-dev-amd64" {
		t.Errorf("buildImageTag = %q, want v0.1.0-dev-amd64", got)
	}
}

// TestBuildImageTagCommaListArchIsBare covers a comma-list ARCH leaving the
// resolved tag undecorated, the bare-tag path the manifest job consumes.
func TestBuildImageTagCommaListArchIsBare(t *testing.T) {
	// an override with a comma-list ARCH, as the manifest stitch sets
	t.Setenv("IMAGE_TAG", "v9.9.9")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("ARCH", "amd64,arm64")
	// the bare tag passes through undecorated
	got, err := buildImageTag("v0.1.0-dev")
	if err != nil {
		t.Fatalf("buildImageTag returned error: %v", err)
	}
	if got != "v9.9.9" {
		t.Errorf("buildImageTag = %q, want v9.9.9", got)
	}
}

// TestJoinImageTagJoinsVersionAndSha covers joinImageTag joining a version
// and short sha with a dot, the dev-path tag the workflows push.
func TestJoinImageTagJoinsVersionAndSha(t *testing.T) {
	// the version carries the v prefix and dev suffix as version.txt holds it
	if got := joinImageTag("v0.1.0-dev", "abc1234"); got != "v0.1.0-dev.abc1234" {
		t.Errorf("joinImageTag = %q, want v0.1.0-dev.abc1234", got)
	}
}
