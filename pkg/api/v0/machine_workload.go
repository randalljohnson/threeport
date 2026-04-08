package v0

// MachineWorkloadDefinition is the configuration for a workload that runs on
// a machine runtime.
type MachineWorkloadDefinition struct {
	Common     `swaggerignore:"true" mapstructure:",squash"`
	Definition `mapstructure:",squash"`

	// The shell script that defines the workload to be executed on the machine.
	Script *string `json:"Script,omitempty" gorm:"not null" validate:"required"`

	// The associated machine workload instances that are deployed from this
	// definition.
	MachineWorkloadInstances []*MachineWorkloadInstance `json:"MachineWorkloadInstances,omitempty" validate:"optional,association"`
}

// MachineWorkloadInstance is a deployed instance of a workload running on a
// machine runtime.
type MachineWorkloadInstance struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Instance       `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The machine runtime on which the workload is deployed.
	MachineRuntimeInstanceID *uint `json:"MachineRuntimeInstanceID,omitempty" query:"machineruntimeinstanceid" gorm:"not null" validate:"required"`

	// The definition used to configure the machine workload instance.
	MachineWorkloadDefinitionID *uint `json:"MachineWorkloadDefinitionID,omitempty" query:"machineworkloaddefinitionid" gorm:"not null" validate:"required"`
}
