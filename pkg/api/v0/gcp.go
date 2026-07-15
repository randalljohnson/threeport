package v0

import "gorm.io/datatypes"

// GcpProvider represents a Google Cloud Platform (GCP) project in an account with the GCP service provider.
type GcpProvider struct {
	Common `swaggerignore:"true" mapstructure:",squash"`

	// The unique name of a GCP provider.
	Name *string `json:",omitempty" validate:"required" gorm:"not null"`

	// The GCP project ID for the Google Cloud account.
	ProjectID *string `json:",omitempty" validate:"required" gorm:"not null"`

	// If true, is the GCP provider used when none specified for an instance.
	DefaultProvider *bool `json:",omitempty" validate:"optional" gorm:"default:false"`

	// The region to use for GCP managed services if not specified.
	DefaultRegion *string `json:",omitempty" validate:"required" gorm:"not null"`

	// The service account key JSON for authenticating to GCP from outside GCP.
	// This is the contents of a service account key file exported from GCP Console.
	// Used when the gcp-controller runs outside GCP (e.g., in AWS or on-prem).
	ServiceAccountCredentials *string `json:",omitempty" validate:"optional" encrypt:"true"`

	// The cluster instances deployed with this GCP provider.
	GcpGkeKubernetesRuntimeInstances []*GcpGkeKubernetesRuntimeInstance `json:",omitempty" validate:"optional,association"`
}

// GcpGkeKubernetesRuntimeDefinition provides the configuration for GKE cluster instances.
type GcpGkeKubernetesRuntimeDefinition struct {
	Common     `swaggerignore:"true" mapstructure:",squash"`
	Definition `mapstructure:",squash"`

	// TODO: add fields for region limitations
	// RegionsAllowed
	// RegionsForbidden

	// The number of zones the cluster should span for availability.
	ZoneCount *int `json:",omitempty" validate:"required" gorm:"not null"`

	// The GCP instance type for the default initial node group.
	DefaultNodeGroupInstanceType *string `json:",omitempty" validate:"required" gorm:"not null"`

	// The number of nodes in the default initial node group.
	DefaultNodeGroupInitialSize *int `json:",omitempty" validate:"required" gorm:"not null"`

	// The minimum number of nodes the default initial node group should have.
	DefaultNodeGroupMinimumSize *int `json:",omitempty" validate:"required" gorm:"not null"`

	// The maximum number of nodes the default initial node group should have.
	DefaultNodeGroupMaximumSize *int `json:",omitempty" validate:"required" gorm:"not null"`

	// The GCP GKE kubernetes runtime instances derived from this definition.
	GcpGkeKubernetesRuntimeInstances []*GcpGkeKubernetesRuntimeInstance `json:",omitempty" validate:"optional,association"`

	// The kubernetes runtime definition for a GKE cluster in GCP.
	KubernetesRuntimeDefinitionID *uint `json:",omitempty" validate:"required" gorm:"not null" relationship:"marries"`
}

// GcpGkeKubernetesRuntimeInstance is a deployed instance of a GKE cluster.
type GcpGkeKubernetesRuntimeInstance struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Instance       `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The GCP provider in which the GKE cluster is provisioned.
	GcpProviderID *uint `json:",omitempty" validate:"required" gorm:"not null" relationship:"requires"`

	// The GCP region in which the cluster is provisioned.
	Region *string `json:",omitempty" validate:"optional"`

	// The definition that configures this instance.
	GcpGkeKubernetesRuntimeDefinitionID *uint `json:",omitempty" validate:"required" gorm:"not null" relationship:"requires"`

	// The kubernetes runtime instance associated with the GKE cluster.
	KubernetesRuntimeInstanceID *uint `json:",omitempty" validate:"required" gorm:"not null" relationship:"marries"`

	// An inventory of all GCP resources for the GKE cluster.
	ResourceInventory *datatypes.JSON `json:",omitempty" validate:"optional"`
}

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

// GcpGceMachineRuntimeInstance is a deployed GCE virtual machine provisioned
// through Threeport's durable infrastructure lifecycle. Network attachment,
// ingress rules, network CIDRs, and public-IP assignment live on the abstract
// MachineRuntimeInstance; the GCE reconciler reads them there so the shape is
// provider-agnostic.
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

	// The SSH username provisioned on the VM.
	SSHUser *string `json:",omitempty" validate:"optional"`

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
