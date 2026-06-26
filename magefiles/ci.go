package main

import (
	"fmt"

	mg "github.com/magefile/mage/mg"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// Ci provides a type for methods that emit values for CI workflow steps.
type Ci mg.Namespace

// Env prints KEY=value lines for the workflow to append to GITHUB_ENV. It emits
// only the values that non-mage steps consume: GOFLAGS and PARALLEL_GO_BUILD,
// both set to the memory-derived build worker count. The go test steps inherit
// GOFLAGS, and the goreleaser step reads PARALLEL_GO_BUILD for its per-target
// parallelism. Values mage itself consumes (image repo, tag, image-build
// parallelism) are self-derived at use and are not emitted here.
func (Ci) Env() error {
	parallel := util.BuildParallelism()
	fmt.Printf("GOFLAGS=-p=%d\n", parallel)
	fmt.Printf("PARALLEL_GO_BUILD=%d\n", parallel)
	return nil
}
