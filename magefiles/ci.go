package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	mg "github.com/magefile/mage/mg"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// Ci provides a type for methods that emit values for CI workflow steps.
type Ci mg.Namespace

// Env prints KEY=value lines for the workflow to append to GITHUB_ENV. It emits
// only the values that non-mage steps consume: GOFLAGS, the memory-derived
// go-build worker count inherited by the non-mage go test step and by
// goreleaser's per-target compiles, and GORELEASER_PARALLELISM, a quarter of
// that worker count, the number of whole-tree targets goreleaser builds at once
// since each links the full tree. mage's own build targets self-derive their
// parallelism, so no build-worker count is emitted for them.
func (Ci) Env() error {
	fmt.Printf("GOFLAGS=-p=%d\n", util.BuildParallelism())
	fmt.Printf("GORELEASER_PARALLELISM=%d\n", util.ReleaseParallelism())
	return nil
}

// Teardown removes what an integration job leaves behind so the next run on
// the same node starts against an empty dind. Gated on CI=true so a local
// `mage ci:teardown` is a no-op — local developers who want their kind
// clusters, tptctl config, or registry preserved keep them.
//
// Order matters. Graceful control-plane teardown first (tptctl's own state
// files get cleaned up while it still has access). Then force-delete every
// kind cluster: `--all` catches whatever this run created (kind cluster
// names have drifted across releases). Then hand-remove any threeport-*
// control-plane containers with restart-on-failure policies (kind stops
// them via docker stop; docker's own restart-on-failure resurrects them
// when the next dind starts against the shared /opt/dind-storage hostPath,
// which was the observed "cluster already exists" failure mode on repeated
// self-hosted runs). Then reap networks, anonymous volumes, and stopped
// containers. NOT `docker system prune -a` — that wipes the ~13 GB image
// layer cache the shared hostPath is designed to preserve.
func (Ci) Teardown() error {
	if os.Getenv("CI") != "true" {
		fmt.Println("ci:teardown: not running in CI, skipping")
		return nil
	}

	// Best-effort graceful teardown of the tptctl control plane.
	teardownStep("tptctl", "down", "--name", "test")

	// Force-delete every kind cluster the shared dind knows about. Fork
	// (core) creates clusters named threeport-dev-N; modules create
	// threeport-test. `--all` covers both without codegen having to know.
	teardownStep("kind", "delete", "clusters", "--all")

	// Hand-remove any container labelled as a kind-cluster node or named
	// threeport-* that survived the above (kind may have been killed
	// mid-flight, dind may have lost track, or restart-on-failure may have
	// beaten kind to the process). sh -c because we need pipes + xargs and
	// the failing-empty-input case has to no-op cleanly.
	teardownStep("sh", "-c",
		`docker ps -aq --filter "label=io.x-k8s.kind.cluster" | xargs -r docker rm -f`)
	teardownStep("sh", "-c",
		`docker ps -aq --filter "name=threeport-" | xargs -r docker rm -f`)

	// Reap kind networks + anonymous volumes + stopped-container metadata.
	// These are what the "auto-restart" bug latches onto: even after the
	// container is gone, its network and mount point can persist.
	teardownStep("docker", "network", "prune", "-f")
	teardownStep("docker", "volume", "prune", "-f")
	teardownStep("docker", "container", "prune", "-f")

	// Force-remove any leftover tptctl config; a failed run leaves tptctl
	// unable to clear its own control-plane entry, which blocks the next
	// bring-up on this pod with "instance already exists" style errors.
	if home, err := os.UserHomeDir(); err == nil {
		teardownStep("rm", "-f", filepath.Join(home, ".threeport", "config.yaml"))
	}

	// Local registry: bring it down via the dev target so its container +
	// port + network side-effects are all handled in one place.
	if err := (Dev{}).LocalRegistryDown(); err != nil {
		fmt.Printf("ci:teardown: remove local registry: %v\n", err)
	}

	return nil
}

// teardownStep runs a cleanup command best-effort, logging on failure so one
// failed command doesn't abort the rest of teardown.
func teardownStep(cmd string, args ...string) {
	if out, err := exec.Command(cmd, args...).CombinedOutput(); err != nil {
		fmt.Printf("ci:teardown: %s %v failed: %v (%s)\n",
			cmd, args, err, string(out))
	}
}
