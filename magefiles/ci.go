package main

import (
	"fmt"

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
