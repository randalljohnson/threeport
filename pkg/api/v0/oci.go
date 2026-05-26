package v0

import (
	"gorm.io/datatypes"
)

// OciProvider is a provider account with the Oracle Cloud Infrastructure service provider.
type OciProvider struct {
	Common `swaggerignore:"true" mapstructure:",squash"`

	// The unique name of an OCI provider.
	Name *string `gorm:"not null" validate:"required"`

	// The user OCID credentials for the OCI provider.
	UserOCID *string `gorm:"not null" validate:"required"`

	// The tenancy OCID for the OCI provider account.
	TenancyOCID *string `gorm:"not null" validate:"required"`

	// The compartment OCID for the OCI provider.
	CompartmentOCID *string `gorm:"not null" validate:"required"`

	// If true is the OCI provider used if none specified in an instance.
	DefaultProvider *bool `json:",omitempty" gorm:"default:false" validate:"optional"`

	// The region to use for OCI managed services if not specified.
	DefaultRegion *string `gorm:"not null" validate:"required"`

	// The fingerprint of the API key for the OCI provider.
	KeyFingerprint *string `gorm:"not null" validate:"required"`

	// The private key for the OCI provider.
	PrivateKey *string `gorm:"not null" validate:"required" encrypt:"true"`

	// The cluster instances deployed with this OCI provider.
	OciOkeKubernetesRuntimeInstances []*OciOkeKubernetesRuntimeInstance `json:",omitempty" validate:"optional,association"`
}

// OciOkeKubernetesRuntimeDefinition provides the configuration for OKE cluster instances.
type OciOkeKubernetesRuntimeDefinition struct {
	Common     `swaggerignore:"true" mapstructure:",squash"`
	Definition `mapstructure:",squash"`

	// The OCI shape for the worker nodes.
	WorkerNodeShape *string `gorm:"not null" validate:"required"`

	// The number of nodes in the worker node pool.
	WorkerNodeInitialCount *int32 `gorm:"not null" validate:"required"`

	// The OCI OKE kubernetes runtime instances derived from this definition.
	OciOkeKubernetesRuntimeInstances []*OciOkeKubernetesRuntimeInstance `json:",omitempty" validate:"optional,association"`

	// The kubernetes runtime definition for an OKE cluster in OCI.
	KubernetesRuntimeDefinitionID *uint `gorm:"not null" validate:"required" relationship:"marries"`
}

// OciOkeKubernetesRuntimeInstance is a deployed instance of an OKE cluster.
type OciOkeKubernetesRuntimeInstance struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Instance       `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The OCI provider used to provision this instance.
	OciProviderID *uint `gorm:"not null" validate:"required" relationship:"requires"`

	// The OCI Region in which the cluster is provisioned. This field is
	// stored in the instance (as well as definition) since a change to the
	// definition will not move a cluster.
	Region *string `json:",omitempty" validate:"optional"`

	// The definition that configures this instance.
	OciOkeKubernetesRuntimeDefinitionID *uint `gorm:"not null" validate:"required" relationship:"requires"`

	// An inventory of all OCI resources for the OKE cluster.
	ResourceInventory *datatypes.JSON `json:",omitempty" validate:"optional"`

	// The kubernetes runtime instance associated with the OCI OKE cluster.
	KubernetesRuntimeInstanceID *uint `gorm:"not null" validate:"required" relationship:"marries"`

	// The OCID for the OKE cluster. Populated by the controller after cluster creation.
	ClusterOCID *string `json:",omitempty" validate:"optional"`
}
