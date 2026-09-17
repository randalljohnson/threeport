package v0

import (
	"testing"
)

// TestGetControllerArgsPassesConcurrentReconciles covers every
// controller receiving the shared worker-count flag.
func TestGetControllerArgsPassesConcurrentReconciles(t *testing.T) {
	cpi := NewInstaller()
	cpi.Opts.AuthEnabled = true
	cpi.Opts.ConcurrentReconciles = 4

	if len(ThreeportControllerList) == 0 {
		t.Fatal("ThreeportControllerList is empty")
	}
	want := "-concurrent-reconciles=4"
	for _, controller := range ThreeportControllerList {
		args := cpi.getControllerArgs(*controller)
		found := false
		for _, arg := range args {
			if arg == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s got args %v, want %s", controller.Name, args, want)
		}
	}
}
