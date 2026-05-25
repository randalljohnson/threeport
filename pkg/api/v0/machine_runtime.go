package v0

// MachineRuntimeDefinition is the configuration for a machine runtime.  It
// serves as a template for provisioning machine runtime instances.
type MachineRuntimeDefinition struct {
	Common     `swaggerignore:"true" mapstructure:",squash"`
	Definition `mapstructure:",squash"`

	// The associated machine runtime instances that are deployed from this
	// definition.
	MachineRuntimeInstances []*MachineRuntimeInstance `json:"MachineRuntimeInstances,omitempty" validate:"optional,association"`
}

// MachineRuntimeInstance is a machine that serves as a runtime for workloads.
type MachineRuntimeInstance struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Instance       `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The hostname or IP address used to reach the machine.
	Hostname *string `json:"Hostname,omitempty" query:"hostname" gorm:"not null" validate:"required"`

	// The SSH username for authenticating to the machine.
	SSHUser *string `json:"SSHUser,omitempty" query:"sshuser" gorm:"not null" validate:"required"`

	// The SSH private key for authenticating to the machine.
	SSHKey *string `json:"SSHKey,omitempty" validate:"optional" encrypt:"true"`

	// The SSH password for authenticating to the machine.
	SSHPassword *string `json:"SSHPassword,omitempty" validate:"optional" encrypt:"true"`

	// The SSH port on the machine.
	Port *int `json:"Port,omitempty" query:"port" gorm:"default:22" validate:"optional"`

	// The remote machine's SSH public host key, used to verify identity on
	// connection. If not provided, captured on first connection.
	HostKey *string `json:"HostKey,omitempty" validate:"optional"`

	// The machine runtime definition for this instance.  Optional because
	// imported machines may not have an associated definition.
	MachineRuntimeDefinitionID *uint `json:"MachineRuntimeDefinitionID,omitempty" query:"machineruntimedefinitionid" validate:"optional"`

	// The associated machine workload instances running on this machine runtime.
	MachineWorkloadInstances []*MachineWorkloadInstance `json:"MachineWorkloadInstances,omitempty" validate:"optional,association"`
}
