package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	mg "github.com/magefile/mage/mg"
)

// Release provides a type for methods that cut tagged releases.
type Release mg.Namespace

// baseVersionPattern matches a bare X.Y.Z release version.
var baseVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// Dev cuts the next dev build under the given base release, tagging the
// latest dev commit as v<base>-dev.<next N> and pushing the tag.
func (Release) Dev(base string) error {
	return cutRelease(base, "dev", false)
}

// Rc cuts the next release candidate under the given base release, tagging
// the latest dev commit as v<base>-rc.<next N> and pushing the tag.
func (Release) Rc(base string) error {
	return cutRelease(base, "rc", false)
}

// Ga cuts the general-availability release for the given base, tagging the
// latest dev commit as v<base> and pushing the tag.
func (Release) Ga(base string) error {
	return cutRelease(base, "", true)
}

// cutRelease tags the latest pushed dev commit as a release and pushes the
// tag, which triggers the release and image workflows. A channel build
// auto-increments the per-channel counter under the base; a ga build tags
// the bare base. The push remote defaults to origin, overridable with
// RELEASE_REMOTE.
func cutRelease(base, channel string, ga bool) error {
	base, err := validateBase(base)
	if err != nil {
		return err
	}

	remote := os.Getenv("RELEASE_REMOTE")
	if remote == "" {
		remote = "origin"
	}

	// fetch first so the counter and the tag target see the latest pushed dev
	if err := git("fetch", remote, "dev", "--tags"); err != nil {
		return fmt.Errorf("failed to fetch %s: %w", remote, err)
	}

	// a channel build bumps its per-channel counter; a ga build needs none
	next := 0
	if !ga {
		next, err = nextCounter(base, channel)
		if err != nil {
			return fmt.Errorf("failed to compute next %s counter: %w", channel, err)
		}
	}
	version := formatVersion(base, channel, ga, next)

	// refuse to reuse an existing tag
	if exec.Command("git", "rev-parse", "-q", "--verify", "refs/tags/"+version).Run() == nil {
		return fmt.Errorf("tag %s already exists", version)
	}

	// tag the latest pushed dev head; the tag push is the release event
	if err := git("tag", "-a", version, remote+"/dev", "-m", "release "+version); err != nil {
		return fmt.Errorf("failed to tag %s: %w", version, err)
	}
	if err := git("push", remote, version); err != nil {
		return fmt.Errorf("failed to push %s: %w", version, err)
	}

	fmt.Printf("pushed %s via %s\n", version, remote)
	fmt.Println("watch: gh run list")
	return nil
}

// validateBase strips a leading v and reports whether the remainder is a
// bare X.Y.Z release version, returning the cleaned base.
func validateBase(base string) (string, error) {
	base = strings.TrimPrefix(base, "v")
	if !baseVersionPattern.MatchString(base) {
		return "", fmt.Errorf("base must be X.Y.Z, got %q", base)
	}
	return base, nil
}

// formatVersion builds the tag string: the bare v<base> for a ga release,
// or v<base>-<channel>.<next> for a channel build.
func formatVersion(base, channel string, ga bool, next int) string {
	if ga {
		return "v" + base
	}
	return fmt.Sprintf("v%s-%s.%d", base, channel, next)
}

// nextCounter returns one more than the highest counter among the existing
// v<base>-<channel>.N tags, or 1 when none exist.
func nextCounter(base, channel string) (int, error) {
	out, err := gitOutput("tag", "--list", fmt.Sprintf("v%s-%s.*", base, channel))
	if err != nil {
		return 0, err
	}
	return highestCounter(strings.Fields(out), fmt.Sprintf("v%s-%s.", base, channel)) + 1, nil
}

// highestCounter returns the largest numeric N among tags shaped <prefix>N,
// or 0 when none match. It parses N numerically so dev.10 ranks above dev.2.
func highestCounter(tags []string, prefix string) int {
	highest := 0
	for _, tag := range tags {
		n, err := strconv.Atoi(strings.TrimPrefix(tag, prefix))
		if err != nil {
			continue
		}
		if n > highest {
			highest = n
		}
	}
	return highest
}

// git runs a git command and surfaces its combined output on failure.
func git(args ...string) error {
	if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(output)), err)
	}
	return nil
}

// gitOutput runs a git command and returns its trimmed standard output.
func gitOutput(args ...string) (string, error) {
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
