package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	mg "github.com/magefile/mage/mg"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// Ci provides a type for methods that emit CI env and tear down leftovers.
type Ci mg.Namespace

// Env prints GOFLAGS and GORELEASER_PARALLELISM as KEY=value lines for
// non-mage steps. Mage targets self-derive their own parallelism.
func (Ci) Env() error {
	fmt.Printf("GOFLAGS=-p=%d\n", util.BuildParallelism())
	fmt.Printf("GORELEASER_PARALLELISM=%d\n", util.ReleaseParallelism())
	return nil
}

// Teardown removes leftover kind clusters, containers, networks, and volumes
// so the next integration job on the same node does not inherit this run's
// dind state. It is a no-op unless CI=true, so a local run keeps the
// developer's clusters, tptctl config, and registry. Leftover kind node
// containers persist in the shared dind hostPath and come back when the next
// dind starts, so they are removed by hand. It does not run docker system
// prune -a, which would wipe the image layers the hostPath exists to preserve.
func (Ci) Teardown() error {
	// skip unless CI so prune and kind --all cannot hit a local environment
	if os.Getenv("CI") != "true" {
		fmt.Println("ci:teardown: not running in CI, skipping")
		return nil
	}

	// take down the test control plane
	teardownStep("./bin/tptctl", "down", "-n", testControlPlaneName)

	// delete every kind cluster; --all covers every name the tests use
	teardownStep("kind", "delete", "clusters", "--all")

	// force-remove leftover kind and threeport containers
	teardownStep("sh", "-c",
		`docker ps -aq --filter "label=io.x-k8s.kind.cluster" | xargs -r docker rm -f`)
	teardownStep("sh", "-c",
		`docker ps -aq --filter "name=threeport-" | xargs -r docker rm -f`)

	// remove leftover buildkit containers
	teardownStep("sh", "-c",
		`docker ps -aq --filter "name=buildx_buildkit_" | xargs -r docker rm -f`)

	// prune unused networks and stopped containers, not the image cache.
	// leftover networks outlive the containers that owned them
	teardownStep("docker", "network", "prune", "-f")
	teardownStep("docker", "container", "prune", "-f")

	// remove leftover client config that would block the next bring-up
	if home, err := os.UserHomeDir(); err == nil {
		teardownStep("rm", "-f", filepath.Join(home, ".threeport", "config.yaml"))
	}

	// remove the local image registry
	if err := (Dev{}).LocalRegistryDown(); err != nil {
		fmt.Fprintf(os.Stderr, "ci:teardown: remove local registry: %v\n", err)
	}

	// prune volumes last, including named volumes buildx leaves.
	// prune without --all leaves those. run after container removals
	// so leftover registry storage does not accumulate
	teardownStep("docker", "volume", "prune", "-af")

	return nil
}

// teardownStep runs a cleanup command, logging a failure instead of
// returning it so later steps still run.
func teardownStep(cmd string, args ...string) {
	if out, err := exec.Command(cmd, args...).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "ci:teardown: %s %v failed: %v (%s)\n",
			cmd, args, err, string(out))
	}
}
