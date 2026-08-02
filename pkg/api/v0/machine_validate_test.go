package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidLocation covers that a known threeport location is accepted and an
// unknown one is rejected.
func TestValidLocation(t *testing.T) {
	// accepts a location present in the region map
	assert.True(t, ValidLocation("Local"))
	// rejects a location absent from the region map
	assert.False(t, ValidLocation("NotARealLocation"))
}

// TestValidMachineSize covers that a known node size is accepted and an unknown
// one is rejected.
func TestValidMachineSize(t *testing.T) {
	// accepts a node size present in the machine type map
	assert.True(t, ValidMachineSize("Small"))
	// rejects a node size absent from the machine type map
	assert.False(t, ValidMachineSize("NotARealSize"))
}
