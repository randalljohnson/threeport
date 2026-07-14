package v0

import "gorm.io/datatypes"

// GcpGceMachineRuntimeInstance is a deployed GCE virtual machine provisioned
// through Threeport's durable infrastructure lifecycle.
type GcpGceMachineRuntimeInstance struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Instance       `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The GCP provider in which the VM is provisioned.
	GcpProviderID *uint `json:",omitempty" validate:"required" gorm:"not null" relationship:"requires"`

	// The GCP region in which the VM is provisioned.
	Region *string `json:",omitempty" validate:"optional"`

	// The GCP zone in which the VM is provisioned.
	Zone *string `json:",omitempty" validate:"optional"`

	// The network the VM attaches to.
	NetworkID *string `json:",omitempty" validate:"optional"`

	// The SSH username provisioned on the VM.
	SSHUser *string `json:",omitempty" validate:"optional"`

	// CIDR ranges allowed to reach the VM over SSH. Empty means the provider
	// default open range.
	SSHSourceRanges *[]string `json:",omitempty" validate:"optional" gorm:"type:jsonb;serializer:json"`

	// IngressRules are additional firewall ingress rules applied to the VM.
	// Complements SSHSourceRanges (which is a legacy shape and will eventually
	// fold in as an equivalent rule). Rules are provider-agnostic; the GCE
	// reconciler translates each to a google_compute_firewall.
	IngressRules *[]IngressRule `json:",omitempty" validate:"optional" gorm:"type:jsonb;serializer:json"`

	// NetworkCIDR is the CIDR block for the VPC network the VM is placed in.
	// Optional; when unset the reconciler falls back to the default network
	// or the network selected by NetworkID.
	NetworkCIDR *string `json:",omitempty" validate:"optional"`

	// SubnetCIDR is the CIDR block for the subnet the VM's primary interface
	// is placed in. Optional; when unset the reconciler falls back to the
	// default subnet for the region.
	SubnetCIDR *string `json:",omitempty" validate:"optional"`

	// AssignPublicIP controls whether the primary network interface gets an
	// external IP address. Defaults false; the reconciler reads back the
	// assigned address into ExternalIP after provisioning.
	AssignPublicIP *bool `json:",omitempty" validate:"optional" gorm:"default:false"`

	// The hostname surfaced after provisioning.
	Hostname *string `json:",omitempty" validate:"optional"`

	// The external IP surfaced after provisioning.
	ExternalIP *string `json:",omitempty" validate:"optional"`

	// The generated SSH private key, surfaced once after provisioning.
	SSHKey *string `json:",omitempty" validate:"optional" encrypt:"true"`

	// GcpSharedNetworkID references the shared network this instance's primary
	// interface attaches to. Managed by the reconciler: users supply
	// NetworkCIDR and SubnetCIDR at the instance level and the reconciler
	// resolves to (or creates) a shared network for the (provider, region)
	// tuple, then wires the FK here so requires-AOR holds the shared network
	// in place while this instance exists.
	GcpSharedNetworkID *uint `json:",omitempty" validate:"optional" relationship:"requires"`

	// The definition that configures this instance.
	GcpGceMachineRuntimeDefinitionID *uint `json:",omitempty" validate:"required" gorm:"not null" relationship:"requires"`

	// The machine runtime instance associated with the GCE machine.
	MachineRuntimeInstanceID *uint `json:",omitempty" validate:"required" gorm:"not null" relationship:"marries"`

	// An inventory of all GCP resources backing this VM.
	ResourceInventory *datatypes.JSON `json:",omitempty" validate:"optional"`
}
