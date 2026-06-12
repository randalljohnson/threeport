// STUB: temporary stand-in for the reusable GCE VM infrastructure provider;
// replace with the real implementation when it lands.
//
// The adapter imports this package only to construct the provider and to read
// the surfaced create outputs. The stub exposes exactly the surface the adapter
// calls and nothing more: the input fields the adapter populates and the output
// fields it reads after provisioning.
package machine

import (
	"gorm.io/datatypes"
)

// GceMachineInfra is the reusable GCE VM infrastructure provider. The real
// implementation deploys a Compute Engine instance; the adapter populates these
// input fields and reads zero-value outputs so the integration can compile and
// be tested in isolation.
type GceMachineInfra struct {
	// ProjectID is the GCP project the VM is provisioned in.
	ProjectID string

	// Region is the GCP region the VM is provisioned in.
	Region string

	// Zone is the GCP zone the VM is provisioned in.
	Zone string

	// MachineType is the GCE machine type (e.g. e2-medium).
	MachineType string

	// ImageID is the boot image identifier.
	ImageID string

	// NetworkID is the network the VM attaches to.
	NetworkID string

	// ServiceAccountCredentials is the service account key JSON used to
	// authenticate to GCP from outside GCP.
	ServiceAccountCredentials string

	// SSHUser is the username provisioned on the VM.
	SSHUser string

	// SSHSourceRanges are the CIDR ranges allowed to reach the VM over SSH.
	SSHSourceRanges []string

	// Hostname, ExternalIP, and SSHKey back the surfaced outputs. The real
	// provider populates these during DeployInfra(); the stub exposes them as
	// settable fields so the output-write test can seed known values.
	Hostname   string
	ExternalIP string
	SSHKey     string
}

// DeployInfra creates the VM (blocking). The stub is a no-op.
func (g *GceMachineInfra) DeployInfra() error {
	return nil
}

// DestroyInfra tears down the VM (blocking). The stub is a no-op.
func (g *GceMachineInfra) DestroyInfra() error {
	return nil
}

// SetStackState restores previously persisted state for crash recovery. The
// stub is a no-op.
func (g *GceMachineInfra) SetStackState(state *datatypes.JSON) error {
	return nil
}

// GetStackState returns the current infrastructure state for persistence. The
// stub returns nil state.
func (g *GceMachineInfra) GetStackState() (*datatypes.JSON, error) {
	return nil, nil
}

// CreateOutputs returns the hostname, external IP, and generated SSH private
// key surfaced after provisioning. The create flow reads these and persists
// them onto the API object.
func (g *GceMachineInfra) CreateOutputs() (hostname, externalIP, sshPrivateKey string) {
	return g.Hostname, g.ExternalIP, g.SSHKey
}
