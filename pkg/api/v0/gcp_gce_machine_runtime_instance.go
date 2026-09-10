package v0

import "gorm.io/datatypes"

// GcpGceMachineRuntimeInstance is a deployed instance of a GCE virtual machine.
type GcpGceMachineRuntimeInstance struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Instance       `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The GCP provider in which the VM is provisioned
	GcpProviderID *uint `json:",omitempty" validate:"required" gorm:"not null" relationship:"requires"`

	// The GCP region in which the VM is provisioned
	Region *string `json:",omitempty" validate:"optional"`

	// The GCP zone in which the VM is provisioned
	Zone *string `json:",omitempty" validate:"optional"`

	// The network the VM attaches to
	NetworkID *string `json:",omitempty" validate:"optional"`

	// The SSH username provisioned on the VM
	SSHUser *string `json:",omitempty" validate:"optional"`

	// The CIDR ranges allowed to reach the VM over SSH, empty for the world-open default 0.0.0.0/0
	SSHSourceRanges *[]string `json:",omitempty" validate:"optional" gorm:"type:jsonb;serializer:json"`

	// The hostname populated after provisioning
	Hostname *string `json:",omitempty" validate:"optional"`

	// The external IP populated after provisioning
	ExternalIP *string `json:",omitempty" validate:"optional"`

	// The SSH private key for the VM
	SSHKey *string `json:",omitempty" validate:"optional" encrypt:"true"`

	// The definition that configures this instance
	GcpGceMachineRuntimeDefinitionID *uint `json:",omitempty" validate:"required" gorm:"not null" relationship:"requires"`

	// The machine runtime instance associated with the GCE machine
	MachineRuntimeInstanceID *uint `json:",omitempty" validate:"required" gorm:"not null" relationship:"marries"`

	// An inventory of all GCP resources backing this VM
	ResourceInventory *datatypes.JSON `validate:"optional"`
}
