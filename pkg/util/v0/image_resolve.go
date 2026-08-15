package v0

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// ResolveImageRepo returns the image repository to build and push to. An
// explicit IMAGE_REPO env var wins, so a caller can target any registry.
// Under GitHub Actions it derives ghcr.io/<owner> from the repository owner,
// lowercased as ghcr requires. Otherwise it returns devDefault, the local
// development registry.
func ResolveImageRepo(devDefault string) string {
	if repo := strings.TrimSpace(os.Getenv("IMAGE_REPO")); repo != "" {
		return repo
	}
	if os.Getenv("GITHUB_ACTIONS") != "" {
		return "ghcr.io/" + strings.ToLower(os.Getenv("GITHUB_REPOSITORY_OWNER"))
	}
	return devDefault
}

// ResolveImageTag returns the canonical image tag naming the code in repoDir:
// the IMAGE_TAG override, the CI tag-build ref name, the CI version-and-sha
// join, or the same version-and-sha join outside CI in a git checkout, falling
// back to versionDefault when no sha is available. An empty repoDir reads the
// process working directory.
//
// The tag carries no architecture suffix. An install resolves the tag it pulls
// through here rather than through buildImageTag, so an exported ARCH in the
// caller's shell cannot point an install at a single-arch image.
func ResolveImageTag(repoDir, versionDefault string) (string, error) {
	if tag := strings.TrimSpace(os.Getenv("IMAGE_TAG")); tag != "" {
		return tag, nil
	}
	if os.Getenv("GITHUB_ACTIONS") == "" {
		// local dev: suffix the base with the short commit sha so a local kind
		// deployment's image tag names the exact commit built, instead of the
		// mutable base tag that hides which code is deployed. warn once when the
		// tree carries uncommitted code so the .<sha> tag is not mistaken for a
		// clean-commit build. fall back to the bare version outside a checkout.
		warnIfDirtyOnce(repoDir)
		sha, err := gitShortSha(repoDir)
		if err != nil {
			return versionDefault, nil
		}
		return joinImageTag(versionDefault, sha), nil
	}
	if os.Getenv("GITHUB_REF_TYPE") == "tag" {
		return os.Getenv("GITHUB_REF_NAME"), nil
	}
	sha, err := gitShortSha(repoDir)
	if err != nil {
		return "", err
	}
	return joinImageTag(versionDefault, sha), nil
}

// buildImageTag returns the tag a build publishes under: the canonical tag from
// ResolveImageTag, decorated with -<arch> when ARCH names a single architecture
// so each arch pushes a distinct tag. A comma-list ARCH (used when stitching a
// manifest) and an unset ARCH both leave the canonical tag alone.
func buildImageTag(repoDir, versionDefault string) (string, error) {
	tag, err := ResolveImageTag(repoDir, versionDefault)
	if err != nil {
		return "", err
	}
	if arch := strings.TrimSpace(os.Getenv("ARCH")); arch != "" && !strings.Contains(arch, ",") {
		return tag + "-" + arch, nil
	}
	return tag, nil
}

// ResolveImageCoordinates resolves the image repository and the tag a build in
// repoDir publishes under, which carries a -<arch> suffix when ARCH names a
// single architecture. An install resolves its tag with ResolveImageTag
// instead, which never reads ARCH. See ResolveImageRepo for how the repository
// resolves.
func ResolveImageCoordinates(repoDir, devRepo, versionDefault string) (repo, tag string, err error) {
	tag, err = buildImageTag(repoDir, versionDefault)
	if err != nil {
		return "", "", err
	}
	return ResolveImageRepo(devRepo), tag, nil
}

// joinImageTag joins a version and a short commit sha into a dev image tag of
// the form <version>.<sha>.
func joinImageTag(version, sha string) string {
	return version + "." + sha
}

// gitShortSha returns the seven-character abbreviated commit sha of HEAD in
// repoDir.
func gitShortSha(repoDir string) (string, error) {
	out, err := gitCommand(repoDir, "rev-parse", "--short=7", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("failed to read short commit sha: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// gitCommand builds a git invocation scoped to repoDir with -C, so a caller
// holding a repository path reads that repository rather than whichever
// directory the process happens to be running in. An empty repoDir omits -C and
// reads the process working directory, since `git -C ""` is an error.
func gitCommand(repoDir string, args ...string) *exec.Cmd {
	if repoDir != "" {
		args = append([]string{"-C", repoDir}, args...)
	}
	return exec.Command("git", args...)
}

// dirtyWarnOnce guards the uncommitted-tree warning so a build that resolves the
// tag for many components prints it at most once.
var dirtyWarnOnce sync.Once

// warnIfDirtyOnce prints a single reminder to stderr when the tree in repoDir
// has uncommitted changes other than go.mod / go.sum. The module dep-pairing
// replace keeps go.mod / go.sum perpetually modified during local development,
// so they are excluded; any other uncommitted file means the .<sha> dev tag
// names HEAD's commit but not the code actually compiled into the image, so
// committing first keeps the tag truthful.
func warnIfDirtyOnce(repoDir string) {
	dirtyWarnOnce.Do(func() {
		out, err := gitCommand(repoDir, "status", "--porcelain").Output()
		if err != nil {
			return
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			path := fields[len(fields)-1]
			if path == "go.mod" || path == "go.sum" {
				continue
			}
			fmt.Fprintln(os.Stderr, "warning: building a dev image from a tree with uncommitted code; commit first so the .<sha> tag names the exact code (go.mod / go.sum excluded)")
			return
		}
	})
}
