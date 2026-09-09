package root

import (
	"testing"

	. "github.com/dave/jennifer/jen"
	"github.com/stretchr/testify/assert"
)

// TestEmitCiTeardownPrunesNamedVolumes covers named-volume prune in generated Ci.Teardown.
func TestEmitCiTeardownPrunesNamedVolumes(t *testing.T) {
	f := NewFile("main")
	emitCiTeardownFunc(f)
	src := f.GoString()

	assert.Contains(t, src, `name=buildx_buildkit_`)
	assert.Contains(t, src, `teardownStep("docker", "volume", "prune", "-af")`)
	assert.NotContains(t, src, `teardownStep("docker", "volume", "prune", "-f")`)
}
