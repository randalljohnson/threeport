package v0

import "gorm.io/datatypes"

// MachineRuntimeDefinition is the configuration for a machine runtime.  It
// serves as a template for provisioning machine runtime instances.
type MachineRuntimeDefinition struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Definition     `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The infrastructure provider that provisions machines from this
	// definition. Empty for imported machines that already exist.
	InfraProvider *string `json:",omitempty" validate:"optional"`

	// The provider account name that selects which account the machine is
	// provisioned on. Empty falls back to the default provider account.
	InfraProviderAccountName *string `json:",omitempty" validate:"optional"`

	// The compute capacity of the machine. Resolved server-side together with
	// the machine profile to a provider machine type.
	MachineSize *string `json:",omitempty" validate:"optional" gorm:"default:Medium"`

	// The CPU-to-memory ratio of the machine. Resolved server-side together
	// with the machine size to a provider machine type.
	MachineProfile *string `json:",omitempty" validate:"optional" gorm:"default:Balanced"`

	// The provider-specific machine type. Populated by the controller from the
	// machine size and profile; not supplied for provider-provisioned machines.
	MachineType *string `json:",omitempty" validate:"optional"`

	// The provider image identifier used to boot the machine.
	ImageID *string `json:",omitempty" validate:"optional"`

	// The associated machine runtime instances that are deployed from this
	// definition.
	MachineRuntimeInstances []*MachineRuntimeInstance `json:",omitempty" validate:"optional,association"`
}

// MachineRuntimeInstance is a machine that serves as a runtime for workloads.
type MachineRuntimeInstance struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Instance       `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The hostname or IP address used to reach the machine.
	Hostname *string `json:",omitempty" validate:"required" gorm:"not null"`

	// The SSH username for authenticating to the machine.
	SSHUser *string `json:",omitempty" validate:"required" gorm:"not null"`

	// The SSH private key for authenticating to the machine.
	SSHKey *string `json:",omitempty" validate:"optional" encrypt:"true"`

	// The SSH password for authenticating to the machine.
	SSHPassword *string `json:",omitempty" validate:"optional" encrypt:"true"`

	// The SSH port on the machine.
	Port *int `json:",omitempty" validate:"optional" gorm:"default:22"`

	// The remote machine's SSH public host key, used to verify identity on
	// connection. If not provided, captured on first connection.
	HostKey *string `json:",omitempty" validate:"optional"`

	// The provider region in which the machine is provisioned.
	Region *string `json:",omitempty" validate:"optional"`

	// The abstract threeport location for the machine. Mapped server-side to a
	// provider region and zone. Optional so imported machines that supply a
	// concrete region directly still validate.
	Location *string `json:",omitempty" validate:"optional"`

	// The provider network identifier the machine attaches to.
	NetworkID *string `json:",omitempty" validate:"optional"`

	// An inventory of all provider resources backing this machine, used for
	// crash recovery and deprovisioning.
	ResourceInventory *datatypes.JSON `json:",omitempty" validate:"optional"`

	// The machine runtime definition for this instance.  Optional because
	// imported machines may not have an associated definition.
	MachineRuntimeDefinitionID *uint `json:",omitempty" validate:"optional" relationship:"requires"`

	// The associated machine workload instances running on this machine runtime.
	MachineWorkloadInstances []*MachineWorkloadInstance `json:",omitempty" validate:"optional,association"`
}
