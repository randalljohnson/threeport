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

	// The hostname surfaced after provisioning.
	Hostname *string `json:",omitempty" validate:"optional"`

	// The external IP surfaced after provisioning.
	ExternalIP *string `json:",omitempty" validate:"optional"`

	// The generated SSH private key, surfaced once after provisioning.
	SSHKey *string `json:",omitempty" validate:"optional" encrypt:"true"`

	// The definition that configures this instance.
	GcpGceMachineRuntimeDefinitionID *uint `json:",omitempty" validate:"required" gorm:"not null" relationship:"requires"`

	// The machine runtime instance associated with the GCE machine.
	MachineRuntimeInstanceID *uint `json:",omitempty" validate:"required" gorm:"not null" relationship:"marries"`

	// An inventory of all GCP resources backing this VM.
	ResourceInventory *datatypes.JSON `json:",omitempty" validate:"optional"`
}
