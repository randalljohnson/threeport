package v0

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ResolveThreeportPin resolves the pinned threeport repo, ghcr namespace, and
// version from the module's go.mod, following a replace directive when one
// overrides the module's own path and version.
func ResolveThreeportPin() (repo, namespace, ver string, err error) {
	out, err := exec.Command("go", "list", "-m", "-json", "github.com/threeport/threeport").Output()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to list threeport module: %w", err)
	}

	var mod struct {
		Path    string
		Version string
		Replace *struct {
			Path    string
			Version string
		}
	}
	if err := json.Unmarshal(out, &mod); err != nil {
		return "", "", "", fmt.Errorf("failed to parse module json: %w", err)
	}

	// a replace directive overrides the module's own path and version
	path, version := mod.Path, mod.Version
	if mod.Replace != nil {
		path, version = mod.Replace.Path, mod.Replace.Version
	}

	return imageCoords(path, version)
}

// imageCoords derives the owner/name repo, the ghcr registry namespace, and
// the version from a github.com module path and version. It strips the
// github.com prefix, splits owner from name, and lowercases the owner for
// the registry, which ghcr requires.
func imageCoords(modPath, version string) (repo, registry, ver string, err error) {
	const host = "github.com/"
	if !strings.HasPrefix(modPath, host) {
		// a local filesystem replace (testing against a local threeport
		// checkout) has no published registry to derive; name it plainly
		if strings.HasPrefix(modPath, "/") || strings.HasPrefix(modPath, ".") {
			return "", "", "", fmt.Errorf("threeport is replaced with the local path %q; no published registry to resolve, expected for local dev where CI uses the committed github pin", modPath)
		}
		return "", "", "", fmt.Errorf("module path %q is not a github.com path", modPath)
	}

	repo = strings.TrimPrefix(modPath, host)
	owner, _, ok := strings.Cut(repo, "/")
	if !ok || owner == "" {
		return "", "", "", fmt.Errorf("module path %q has no owner/name", modPath)
	}

	registry = "ghcr.io/" + strings.ToLower(owner)
	return repo, registry, version, nil
}
