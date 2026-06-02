package v0

// MachineWorkloadDefinition is the configuration for a workload that runs on
// a machine runtime.
type MachineWorkloadDefinition struct {
	Common     `swaggerignore:"true" mapstructure:",squash"`
	Definition `mapstructure:",squash"`

	// The shell script to run when a machine workload instance is created.
	CreateScript *string `json:"CreateScript,omitempty" gorm:"not null" validate:"required"`

	// The shell script to run when a machine workload instance is updated.
	UpdateScript *string `json:"UpdateScript,omitempty" validate:"optional"`

	// The shell script to run when a machine workload instance is deleted.
	DeleteScript *string `json:"DeleteScript,omitempty" gorm:"not null" validate:"required"`

	// The shell to use for script execution.
	Shell *string `json:"Shell,omitempty" gorm:"default:/bin/bash" validate:"optional"`

	// The working directory for script execution.
	WorkingDir *string `json:"WorkingDir,omitempty" validate:"optional"`

	// The timeout in seconds for script execution.
	Timeout *int `json:"Timeout,omitempty" validate:"optional"`

	// The environment variables to set for the workload as KEY=VALUE entries.
	// Nil distinguishes "unset" from "empty list".  Values are encrypted at
	// rest; keys stay queryable.
	Env *[]string `json:"Env,omitempty" gorm:"type:jsonb;serializer:json" validate:"optional" encrypt:"true"`

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
	MachineRuntimeInstanceID *uint `json:"MachineRuntimeInstanceID,omitempty" query:"machineruntimeinstanceid" gorm:"not null" validate:"required" relationship:"requires"`

	// The definition used to configure the machine workload instance.
	MachineWorkloadDefinitionID *uint `json:"MachineWorkloadDefinitionID,omitempty" query:"machineworkloaddefinitionid" gorm:"not null" validate:"required"`

	// The latest status of the workload instance as observed by the reconciler
	// when the most recent script execution completed.
	Status *string `json:"Status,omitempty" query:"status" validate:"optional"`

	// All events generated for the machine workload instance.
	Events []*WorkloadEvent `json:"Events,omitempty" query:"events" validate:"optional"`

	// The environment variables set for the workload as KEY=VALUE entries.
	// Nil falls back to the definition's Env at reconcile time, distinguishing
	// "unset" from "empty list".  Values are encrypted at rest; keys stay
	// queryable.
	Env *[]string `json:"Env,omitempty" gorm:"type:jsonb;serializer:json" validate:"optional" encrypt:"true"`
}
