package v0

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidMachineSize covers that a known node size is accepted and an unknown
// one is rejected.
func TestValidMachineSize(t *testing.T) {
	// accepts a node size present in the machine type map
	assert.True(t, ValidMachineSize("Small"))
	// rejects a node size absent from the machine type map
	assert.False(t, ValidMachineSize("NotARealSize"))
	// accepts a size carried by only one profile, which is what makes this
	// check too permissive for a caller that knows its profile
	assert.True(t, ValidMachineSize("2XSmall"))
}

// TestValidMachineSizeForProfile covers that a size is only accepted for a
// profile that actually carries it, including the sizes the profile-agnostic
// check lets through.
func TestValidMachineSizeForProfile(t *testing.T) {
	// size carried by the requested profile
	assert.True(t, ValidMachineSizeForProfile("Balanced", "2XSmall"))
	assert.True(t, ValidMachineSizeForProfile("MemoryOptimized", "Medium"))

	// size carried by another profile but not this one; ValidMachineSize
	// accepts both of these, which is the gap this check closes
	assert.False(t, ValidMachineSizeForProfile("MemoryOptimized", "2XSmall"))
	assert.False(t, ValidMachineSizeForProfile("ComputeOptimized", "9XLarge"))

	// unknown profile and unknown size are both rejected
	assert.False(t, ValidMachineSizeForProfile("NotARealProfile", "Medium"))
	assert.False(t, ValidMachineSizeForProfile("Balanced", "NotARealSize"))
}
