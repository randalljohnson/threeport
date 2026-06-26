package v0

import "testing"

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

// TestResolveImageTagReturnsVersionOutsideCI covers ResolveImageTag returning
// the version default unchanged outside CI.
func TestResolveImageTagReturnsVersionOutsideCI(t *testing.T) {
	// no override, not in CI
	t.Setenv("IMAGE_TAG", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("ARCH", "")
	// the version default passes through untouched
	got, err := ResolveImageTag("v0.1.0-dev")
	if err != nil {
		t.Fatalf("ResolveImageTag returned error: %v", err)
	}
	if got != "v0.1.0-dev" {
		t.Errorf("ResolveImageTag = %q, want v0.1.0-dev", got)
	}
}

// TestResolveImageTagDecoratesSingleArch covers ResolveImageTag decorating the
// resolved tag with -<arch> when ARCH names a single arch.
func TestResolveImageTagDecoratesSingleArch(t *testing.T) {
	// an explicit override plus a single-arch ARCH
	t.Setenv("IMAGE_TAG", "v9.9.9")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("ARCH", "arm64")
	// the arch decorates the resolved tag
	got, err := ResolveImageTag("v0.1.0-dev")
	if err != nil {
		t.Fatalf("ResolveImageTag returned error: %v", err)
	}
	if got != "v9.9.9-arm64" {
		t.Errorf("ResolveImageTag = %q, want v9.9.9-arm64", got)
	}
}

// TestResolveImageTagSingleArchAppliesToVersionDefault covers the -<arch>
// decoration landing on the plain version default outside CI.
func TestResolveImageTagSingleArchAppliesToVersionDefault(t *testing.T) {
	// no override, not in CI, with a single-arch ARCH set
	t.Setenv("IMAGE_TAG", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("ARCH", "amd64")
	// the version default carries the arch decoration through
	got, err := ResolveImageTag("v0.1.0-dev")
	if err != nil {
		t.Fatalf("ResolveImageTag returned error: %v", err)
	}
	if got != "v0.1.0-dev-amd64" {
		t.Errorf("ResolveImageTag = %q, want v0.1.0-dev-amd64", got)
	}
}

// TestResolveImageTagCommaListArchIsBare covers a comma-list ARCH leaving the
// resolved tag undecorated, the bare-tag path the manifest job consumes.
func TestResolveImageTagCommaListArchIsBare(t *testing.T) {
	// an override with a comma-list ARCH, as the manifest stitch sets
	t.Setenv("IMAGE_TAG", "v9.9.9")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("ARCH", "amd64,arm64")
	// the bare tag passes through undecorated
	got, err := ResolveImageTag("v0.1.0-dev")
	if err != nil {
		t.Fatalf("ResolveImageTag returned error: %v", err)
	}
	if got != "v9.9.9" {
		t.Errorf("ResolveImageTag = %q, want v9.9.9", got)
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
