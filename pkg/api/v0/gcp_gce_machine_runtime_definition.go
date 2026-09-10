package v0

// GcpGceMachineRuntimeDefinition is the configuration for GCE machine
// runtime instances.
type GcpGceMachineRuntimeDefinition struct {
	Common     `swaggerignore:"true" mapstructure:",squash"`
	Definition `mapstructure:",squash"`

	// The GCE machine type, e.g. e2-medium
	MachineType *string `json:",omitempty" validate:"optional"`

	// The boot image identifier
	ImageID *string `json:",omitempty" validate:"optional"`

	// The GCP GCE machine runtime instances derived from this definition
	GcpGceMachineRuntimeInstances []*GcpGceMachineRuntimeInstance `json:",omitempty" validate:"optional,association"`

	// The machine runtime definition for a GCE machine in GCP
	MachineRuntimeDefinitionID *uint `json:",omitempty" validate:"optional" relationship:"marries"`
}
