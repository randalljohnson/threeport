package v0

import (
	"testing"

	api "github.com/threeport/threeport/pkg/api/v0"
)

// TestGetControllerArgsPassesConcurrentReconciles covers every
// controller receiving the shared worker-count flag.
func TestGetControllerArgsPassesConcurrentReconciles(t *testing.T) {
	cpi := NewInstaller()
	cpi.Opts.AuthEnabled = true
	cpi.Opts.ConcurrentReconciles = 4

	for _, name := range []string{
		ThreeportMachineWorkloadControllerName,
		ThreeportSecretControllerName,
	} {
		args := cpi.getControllerArgs(api.ControlPlaneComponent{Name: name})
		want := "-concurrent-reconciles=4"
		found := false
		for _, arg := range args {
			if arg == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s got args %v, want %s", name, args, want)
		}
	}
}
