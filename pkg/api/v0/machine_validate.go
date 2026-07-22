package v0

import (
	"github.com/threeport/threeport/internal/kubernetes-runtime/mapping"
)

// ValidLocation reports whether the location is a supported threeport location.
func ValidLocation(location string) bool {
	return mapping.ValidLocation(location)
}

// ValidMachineSize reports whether the machine size matches a supported node
// size in the machine type map.
func ValidMachineSize(machineSize string) bool {
	for _, machineType := range *mapping.GetMachineTypeMap() {
		if machineSize == machineType.NodeSize {
			return true
		}
	}
	return false
}
