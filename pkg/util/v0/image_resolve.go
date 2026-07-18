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

// ResolveImageTag returns the image tag for the current build. An explicit
// IMAGE_TAG env var wins. Under GitHub Actions it echoes the pushed ref on a
// tag build, otherwise it joins versionDefault to the short commit sha as
// <version>.<sha>. Outside CI it also joins the short commit sha in a git
// checkout, falling back to versionDefault when the sha cannot be read. A
// single-arch ARCH env var (one arch, no comma) decorates the resolved tag
// with -<arch>; a comma-list ARCH and an unset ARCH both leave the bare tag.
func ResolveImageTag(versionDefault string) (string, error) {
	tag, err := resolveBaseImageTag(versionDefault)
	if err != nil {
		return "", err
	}
	// a single-arch build decorates the tag with -<arch> so each arch pushes a
	// distinct single-arch tag; a comma-list ARCH (used when stitching a
	// manifest) and an unset ARCH both leave the bare tag.
	if arch := strings.TrimSpace(os.Getenv("ARCH")); arch != "" && !strings.Contains(arch, ",") {
		return tag + "-" + arch, nil
	}
	return tag, nil
}

// resolveBaseImageTag resolves the image tag before any per-arch suffix is
// applied: the IMAGE_TAG override, the CI tag-build ref name, the CI
// version-and-sha join, or the same version-and-sha join outside CI in a git
// checkout, falling back to the plain version default when no sha is available.
func resolveBaseImageTag(versionDefault string) (string, error) {
	if tag := strings.TrimSpace(os.Getenv("IMAGE_TAG")); tag != "" {
		return tag, nil
	}
	if os.Getenv("GITHUB_ACTIONS") == "" {
		// local dev: suffix the base with the short commit sha so a local kind
		// deployment's image tag names the exact commit built, instead of the
		// mutable base tag that hides which code is deployed. warn once when the
		// tree carries uncommitted code so the .<sha> tag is not mistaken for a
		// clean-commit build. fall back to the bare version outside a checkout.
		warnIfDirtyOnce()
		sha, err := gitShortSha()
		if err != nil {
			return versionDefault, nil
		}
		return joinImageTag(versionDefault, sha), nil
	}
	if os.Getenv("GITHUB_REF_TYPE") == "tag" {
		return os.Getenv("GITHUB_REF_NAME"), nil
	}
	sha, err := gitShortSha()
	if err != nil {
		return "", err
	}
	return joinImageTag(versionDefault, sha), nil
}

// ResolveImageCoordinates resolves the image repository and tag together. See
// ResolveImageRepo and ResolveImageTag for the resolution order each follows.
func ResolveImageCoordinates(devRepo, versionDefault string) (repo, tag string, err error) {
	tag, err = ResolveImageTag(versionDefault)
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

// gitShortSha returns the seven-character abbreviated commit sha of HEAD.
func gitShortSha() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--short=7", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("failed to read short commit sha: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// dirtyWarnOnce guards the uncommitted-tree warning so a build that resolves the
// tag for many components prints it at most once.
var dirtyWarnOnce sync.Once

// warnIfDirtyOnce prints a single reminder to stderr when the working tree has
// uncommitted changes other than go.mod / go.sum. The module dep-pairing
// replace keeps go.mod / go.sum perpetually modified during local development,
// so they are excluded; any other uncommitted file means the .<sha> dev tag
// names HEAD's commit but not the code actually compiled into the image, so
// committing first keeps the tag truthful.
func warnIfDirtyOnce() {
	dirtyWarnOnce.Do(func() {
		out, err := exec.Command("git", "status", "--porcelain").Output()
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
