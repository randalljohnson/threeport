package v0

// GcpGceMachineRuntimeDefinition provides the provisioning template for GCE
// machine runtime instances. It mirrors GcpGkeKubernetesRuntimeDefinition: the
// machine-shaping directives live here so every instance derived from the
// definition is provisioned identically.
type GcpGceMachineRuntimeDefinition struct {
	Common     `swaggerignore:"true" mapstructure:",squash"`
	Definition `mapstructure:",squash"`

	// The GCE machine type (e.g. e2-medium).
	MachineType *string `json:",omitempty" validate:"optional"`

	// The boot image identifier.
	ImageID *string `json:",omitempty" validate:"optional"`

	// The GCP GCE machine runtime instances derived from this definition.
	GcpGceMachineRuntimeInstances []*GcpGceMachineRuntimeInstance `json:",omitempty" validate:"optional,association"`

	// The machine runtime definition for a GCE machine in GCP. Optional because
	// imported machines may not have an associated definition.
	MachineRuntimeDefinitionID *uint `json:",omitempty" validate:"optional" relationship:"marries"`
}
