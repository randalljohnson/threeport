package v0

import (
	"strconv"
	"strings"
)

// threeportModulePath is the canonical module path of the core threeport
// repository, used to recognize a threeport dependency in a consumer's go.mod.
const threeportModulePath = "github.com/threeport/threeport"

// ParseThreeportDependency reads go.mod text and reports the threeport release
// source a consumer depends on. A `replace github.com/threeport/threeport =>
// <owner/name> <version>` directive wins when present, since a fork override
// names the repository the release binaries come from; otherwise a `require
// github.com/threeport/threeport <version>` line yields the upstream repository
// at that version. found is false when the go.mod declares no threeport
// dependency, or when the only directive is a replace to a local filesystem
// path (which carries no release version to download from).
func ParseThreeportDependency(gomod string) (repo, version string, found bool, err error) {
	repo, version, kind := parseThreeportReplace(gomod)
	switch kind {
	case replaceVersioned:
		return repo, version, true, nil
	case replaceLocal:
		// a local checkout replaces the dependency, so there is no release to
		// download; report no dependency rather than falling through to the
		// require placeholder version.
		return "", "", false, nil
	}
	if version, ok := parseThreeportRequire(gomod); ok {
		return shortModulePath(threeportModulePath), version, true, nil
	}
	return "", "", false, nil
}

// threeportReplaceKind classifies how a go.mod replaces the threeport module.
type threeportReplaceKind int

const (
	// replaceNone means no replace directive targets the threeport module.
	replaceNone threeportReplaceKind = iota
	// replaceVersioned means the threeport module is replaced with another
	// module path at a specific version, naming a downloadable release.
	replaceVersioned
	// replaceLocal means the threeport module is replaced with a local
	// filesystem path, which carries no downloadable release version.
	replaceLocal
)

// parseThreeportReplace scans go.mod lines for a replace directive whose left
// side is the threeport module path, returning the replacement repository as an
// "owner/name" path and its version for a versioned replace, or signaling a
// local-path replace so the caller can decline to download a release.
func parseThreeportReplace(gomod string) (repo, version string, kind threeportReplaceKind) {
	for _, line := range strings.Split(gomod, "\n") {
		fields := strings.Fields(stripComment(line))
		// a replace reads: replace <old> => <new> [<version>]
		if len(fields) < 4 || fields[0] != "replace" || fields[2] != "=>" {
			continue
		}
		if fields[1] != threeportModulePath {
			continue
		}
		newPath := fields[3]
		// a local path replacement carries no module version to download from
		if isLocalPath(newPath) {
			return "", "", replaceLocal
		}
		// a versioned replace reads: replace <old> => <new> <version>
		if len(fields) < 5 {
			continue
		}
		return shortModulePath(newPath), fields[4], replaceVersioned
	}
	return "", "", replaceNone
}

// parseThreeportRequire scans go.mod lines for a require directive on the
// threeport module path and returns its version, handling both the single-line
// `require <path> <version>` form and an entry inside a `require ( ... )` block.
func parseThreeportRequire(gomod string) (version string, ok bool) {
	inBlock := false
	for _, line := range strings.Split(gomod, "\n") {
		fields := strings.Fields(stripComment(line))
		if len(fields) == 0 {
			continue
		}
		// track entry into and out of a grouped require block
		if !inBlock && fields[0] == "require" && len(fields) == 2 && fields[1] == "(" {
			inBlock = true
			continue
		}
		if inBlock {
			if fields[0] == ")" {
				inBlock = false
				continue
			}
			if len(fields) >= 2 && fields[0] == threeportModulePath {
				return fields[1], true
			}
			continue
		}
		// single-line require: require <path> <version>
		if fields[0] == "require" && len(fields) >= 3 && fields[1] == threeportModulePath {
			return fields[2], true
		}
	}
	return "", false
}

// stripComment removes a trailing go.mod line comment so a directive followed
// by `// indirect` or similar parses on its leading fields alone.
func stripComment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// isLocalPath reports whether a replace target is a local filesystem path
// rather than a module path, identified by a leading `.` or `/` or a Windows
// drive prefix.
func isLocalPath(path string) bool {
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, ".") || strings.HasPrefix(path, "/") {
		return true
	}
	if strings.HasPrefix(path, "..") {
		return true
	}
	// a Windows-style drive path such as C:\ has a colon in its second byte
	if len(path) >= 2 && path[1] == ':' {
		return true
	}
	return false
}

// shortModulePath reduces a module path to the "owner/name" GitHub repository
// path the release API addresses, dropping a leading host segment such as
// github.com and any deeper subdirectory segments.
func shortModulePath(modulePath string) string {
	parts := strings.Split(modulePath, "/")
	// drop the host segment (e.g. github.com) when present
	if len(parts) >= 1 && strings.Contains(parts[0], ".") {
		parts = parts[1:]
	}
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return strings.Join(parts, "/")
}

// LatestMatchingTag returns the highest-numbered tag shaped `<base>.<N>` among
// tags, comparing N numerically so `<base>.10` outranks `<base>.9`. It reports
// false when no tag matches. A base that is a prefix of another base does not
// cross-contaminate: the dot delimiter and a fully numeric suffix are both
// required, so `v0.7.0` never matches a `v0.7.0-dev.N` tag.
func LatestMatchingTag(tags []string, base string) (string, bool) {
	prefix := base + "."
	highest := -1
	var best string
	for _, tag := range tags {
		suffix := strings.TrimPrefix(tag, prefix)
		// require the prefix to have actually matched
		if suffix == tag {
			continue
		}
		n, err := strconv.Atoi(suffix)
		if err != nil {
			continue
		}
		if n > highest {
			highest = n
			best = tag
		}
	}
	if highest < 0 {
		return "", false
	}
	return best, true
}
