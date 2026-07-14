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

	// The hostname or IP address used to reach the machine. Optional at
	// create so the abstract instance can exist before the machine is
	// provisioned; populated once the machine is reachable.
	Hostname *string `json:",omitempty" validate:"optional"`

	// The SSH username for authenticating to the machine. Optional at create
	// for the same reason as the hostname; populated once the machine is
	// provisioned.
	SSHUser *string `json:",omitempty" validate:"optional"`

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

	// The provider-specific subnet identifier the VM should attach to.
	// Required in custom-mode shared VPCs where multiple subnets share a
	// region.
	SubnetID *string `json:",omitempty" gorm:"type:text" validate:"optional"`

	// IngressRules are additional firewall ingress rules applied to the
	// machine. Rules are provider-agnostic; each provider reconciler
	// translates them to its native firewall shape. When SSHSourceRanges
	// is set on the provider-specific instance, the reconciler prepends an
	// equivalent SSH rule to this list so a single ingress pipeline
	// produces every firewall.
	IngressRules *[]IngressRule `json:",omitempty" validate:"optional" gorm:"type:jsonb;serializer:json"`

	// NetworkCIDR is the CIDR block for the VPC network the machine is
	// placed in. Optional; when unset the reconciler falls back to a
	// provider-specific default.
	NetworkCIDR *string `json:",omitempty" validate:"optional"`

	// SubnetCIDR is the CIDR block for the subnet the machine's primary
	// interface is placed in. Optional; when unset the reconciler falls
	// back to a provider-specific default.
	SubnetCIDR *string `json:",omitempty" validate:"optional"`

	// AssignPublicIP controls whether the primary network interface gets
	// an external IP address. Defaults false; the reconciler reads back
	// the assigned address into Hostname after provisioning when true.
	AssignPublicIP *bool `json:",omitempty" validate:"optional" gorm:"default:false"`

	// An inventory of all provider resources backing this machine, used for
	// crash recovery and deprovisioning.
	ResourceInventory *datatypes.JSON `json:",omitempty" validate:"optional"`

	// The machine runtime definition for this instance.  Optional because
	// imported machines may not have an associated definition.
	MachineRuntimeDefinitionID *uint `json:",omitempty" validate:"optional" relationship:"requires"`

	// The associated machine workload instances running on this machine runtime.
	MachineWorkloadInstances []*MachineWorkloadInstance `json:",omitempty" validate:"optional,association"`
}
