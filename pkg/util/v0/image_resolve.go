package v0

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
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
// <version>.<sha>. Outside CI it returns versionDefault unchanged. A
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
// version-and-sha join, or the plain version default outside CI.
func resolveBaseImageTag(versionDefault string) (string, error) {
	if tag := strings.TrimSpace(os.Getenv("IMAGE_TAG")); tag != "" {
		return tag, nil
	}
	if os.Getenv("GITHUB_ACTIONS") == "" {
		return versionDefault, nil
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
