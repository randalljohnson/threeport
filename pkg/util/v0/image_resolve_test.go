package v0

import (
	"os"
	"os/exec"
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
	got, err := ResolveImageTag("", "v0.1.0-dev")
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
	got, err := ResolveImageTag("", "v0.1.0-dev")
	if err != nil {
		t.Fatalf("ResolveImageTag returned error: %v", err)
	}
	if got != "v0.1.0-dev.3" {
		t.Errorf("ResolveImageTag = %q, want the ref name", got)
	}
}

// gitRedirectVars name the environment variables that point git at a repository
// of their own, overriding both the working directory and an explicit -C path.
// git exports them to hook processes, so a suite invoked from a pre-push hook
// resolves the outer repository unless they are cleared.
var gitRedirectVars = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE"}

// clearGitRedirects unsets the redirect variables for the duration of the test.
// Unsetting rather than emptying matters: git rejects an empty GIT_DIR as an
// invalid path, which fails even a command handed a valid repository elsewhere.
func clearGitRedirects(t *testing.T) {
	t.Helper()
	for _, name := range gitRedirectVars {
		// t.Setenv records the original value and restores it at cleanup; the
		// unset that follows is what the command actually sees
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
}

// isolateFromGit puts the resolver somewhere no repository can be found, so the
// sha read fails and the fallback path runs.
func isolateFromGit(t *testing.T) {
	t.Helper()
	clearGitRedirects(t)
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
	got, err := ResolveImageTag("", "v0.1.0-dev")
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
	got, err := ResolveImageTag("", "v0.1.0-dev")
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
	got, err := ResolveImageTag("", "v0.1.0-dev")
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
	got, err := buildImageTag("", "v0.1.0-dev")
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
	got, err := buildImageTag("", "v0.1.0-dev")
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
	got, err := buildImageTag("", "v0.1.0-dev")
	if err != nil {
		t.Fatalf("buildImageTag returned error: %v", err)
	}
	if got != "v9.9.9" {
		t.Errorf("buildImageTag = %q, want v9.9.9", got)
	}
}

// initRepo creates a git repository holding one empty commit and returns its
// path alongside the seven-character sha of that commit. The redirect variables
// must already be cleared, which clearGitRedirects does.
func initRepo(t *testing.T) (dir, sha string) {
	t.Helper()
	dir = t.TempDir()
	commands := [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"commit", "--allow-empty", "-m", "seed"},
	}
	for _, args := range commands {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v in fixture repo: %s: %v", args, out, err)
		}
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--short=7", "HEAD").Output()
	if err != nil {
		t.Fatalf("failed to read fixture repo sha: %v", err)
	}
	return dir, strings.TrimSpace(string(out))
}

// TestResolveImageTagReadsShaFromRepoDir covers ResolveImageTag reading the
// commit sha from the repository it is handed rather than from the directory the
// process runs in, so a caller holding a repository path tags the code it is
// actually building.
func TestResolveImageTagReadsShaFromRepoDir(t *testing.T) {
	// not in CI, and run from a directory holding no repository so a
	// working-directory read cannot pass
	t.Setenv("IMAGE_TAG", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("ARCH", "")
	isolateFromGit(t)
	// a fixture repository stands in for the caller's tree
	repoDir, want := initRepo(t)
	// the tag carries the fixture repository's sha
	got, err := ResolveImageTag(repoDir, "v0.1.0-dev")
	if err != nil {
		t.Fatalf("ResolveImageTag returned error: %v", err)
	}
	if got != "v0.1.0-dev."+want {
		t.Errorf("ResolveImageTag = %q, want v0.1.0-dev.%s", got, want)
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

// TestImageWithoutTag covers the reference shapes a control plane deployment
// carries. The ported-registry case is the one that decides the function: a
// naive cut at the first colon leaves the registry host alone and points every
// deployment at an image that does not exist.
func TestImageWithoutTag(t *testing.T) {
	tests := []struct {
		name  string
		image string
		want  string
	}{
		{
			name:  "ported registry with a tag",
			image: "localhost:5001/threeport-rest-api:v0.7.0-dev.23",
			want:  "localhost:5001/threeport-rest-api",
		},
		{
			name:  "ported registry with no tag",
			image: "localhost:5001/threeport-rest-api",
			want:  "localhost:5001/threeport-rest-api",
		},
		{
			name:  "hosted registry with a tag",
			image: "ghcr.io/randalljohnson/threeport-rest-api:v0.7.0-dev.23",
			want:  "ghcr.io/randalljohnson/threeport-rest-api",
		},
		{
			name:  "bare name with a tag",
			image: "threeport-rest-api:v0.7.0-dev.23",
			want:  "threeport-rest-api",
		},
		{
			name:  "bare name with no tag",
			image: "threeport-rest-api",
			want:  "threeport-rest-api",
		},
		{
			name:  "digest reference is left alone",
			image: "ghcr.io/randalljohnson/threeport-rest-api@sha256:abc123",
			want:  "ghcr.io/randalljohnson/threeport-rest-api@sha256:abc123",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ImageWithoutTag(test.image); got != test.want {
				t.Errorf("ImageWithoutTag(%q) = %q, want %q", test.image, got, test.want)
			}
		})
	}
}
