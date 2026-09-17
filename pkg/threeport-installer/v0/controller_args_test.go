package v0

import (
	"testing"

	api "github.com/threeport/threeport/pkg/api/v0"
)

// TestGetControllerArgsPassesMachineWorkloadConcurrency covers the
// machine-workload controller receiving the worker-count flag.
func TestGetControllerArgsPassesMachineWorkloadConcurrency(t *testing.T) {
	cpi := NewInstaller()
	cpi.Opts.AuthEnabled = true
	cpi.Opts.MachineWorkloadInstanceConcurrentReconciles = 4

	args := cpi.getControllerArgs(api.ControlPlaneComponent{Name: ThreeportMachineWorkloadControllerName})

	want := "-machine-workload-instance-concurrent-reconciles=4"
	found := false
	for _, arg := range args {
		if arg == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("got args %v, want %s", args, want)
	}
}

// TestGetControllerArgsOmitsMachineWorkloadConcurrencyOnOthers covers a
// controller that does not accept the machine-workload flag.
func TestGetControllerArgsOmitsMachineWorkloadConcurrencyOnOthers(t *testing.T) {
	cpi := NewInstaller()
	cpi.Opts.AuthEnabled = true

	args := cpi.getControllerArgs(api.ControlPlaneComponent{Name: ThreeportSecretControllerName})

	for _, arg := range args {
		s, ok := arg.(string)
		if ok && len(s) > 40 && s[:40] == "-machine-workload-instance-concurrent-re" {
			t.Fatalf("secret controller received machine-workload flag %s", s)
		}
	}
}
