package v0

import (
	"gorm.io/datatypes"
)

// OciProvider is a provider account with the Oracle Cloud Infrastructure service provider.
type OciProvider struct {
	Common `swaggerignore:"true" mapstructure:",squash"`

	// The unique name of an OCI provider.
	Name *string `json:"Name,omitempty" query:"name" gorm:"not null" validate:"required"`

	// The user OCID credentials for the OCI provider.
	UserOCID *string `json:"UserOCID,omitempty" query:"userocid" gorm:"not null" validate:"required"`

	// The tenancy OCID for the OCI provider account.
	TenancyOCID *string `json:"TenancyOCID,omitempty" query:"tenancyocid" gorm:"not null" validate:"required"`

	// The compartment OCID for the OCI provider.
	CompartmentOCID *string `json:"CompartmentOCID,omitempty" query:"compartmentocid" gorm:"not null" validate:"required"`

	// If true is the OCI provider used if none specified in an instance.
	DefaultProvider *bool `json:"DefaultProvider,omitempty" query:"defaultprovider" gorm:"default:false" validate:"optional"`

	// The region to use for OCI managed services if not specified.
	DefaultRegion *string `json:"DefaultRegion,omitempty" query:"defaultregion" gorm:"not null" validate:"required"`

	// The fingerprint of the API key for the OCI provider.
	KeyFingerprint *string `json:"KeyFingerprint,omitempty" gorm:"not null" validate:"required"`

	// The private key for the OCI provider.
	PrivateKey *string `json:"PrivateKey,omitempty" gorm:"not null" validate:"required" encrypt:"true"`

	// The cluster instances deployed with this OCI provider.
	OciOkeKubernetesRuntimeInstances []*OciOkeKubernetesRuntimeInstance `json:"OciOkeKubernetesRuntimeInstances,omitempty" validate:"optional,association"`
}

// OciOkeKubernetesRuntimeDefinition provides the configuration for OKE cluster instances.
type OciOkeKubernetesRuntimeDefinition struct {
	Common     `swaggerignore:"true" mapstructure:",squash"`
	Definition `mapstructure:",squash"`

	// The OCI shape for the worker nodes.
	WorkerNodeShape *string `json:"WorkerNodeShape,omitempty" query:"workernodeshape" gorm:"not null" validate:"required"`

	// The number of nodes in the worker node pool.
	WorkerNodeInitialCount *int32 `json:"WorkerNodeInitialCount,omitempty" query:"workernodeinitialcount" gorm:"not null" validate:"required"`

	// The OCI OKE kubernetes runtime instances derived from this definition.
	OciOkeKubernetesRuntimeInstances []*OciOkeKubernetesRuntimeInstance `json:"OciOkeKubernetesRuntimeInstances,omitempty" validate:"optional,association"`

	// The kubernetes runtime definition for an OKE cluster in OCI.
	KubernetesRuntimeDefinitionID *uint `json:"KubernetesRuntimeDefinitionID,omitempty" query:"kubernetesruntimedefinitionid" gorm:"not null" validate:"required" relationship:"requires"`
}

// OciOkeKubernetesRuntimeInstance is a deployed instance of an OKE cluster.
type OciOkeKubernetesRuntimeInstance struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Instance       `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The OCI provider used to provision this instance.
	OciProviderID *uint `json:"OciProviderID,omitempty" query:"ociproviderid" gorm:"not null" validate:"required" relationship:"requires"`

	// The OCI Region in which the cluster is provisioned. This field is
	// stored in the instance (as well as definition) since a change to the
	// definition will not move a cluster.
	Region *string `json:"Region,omitempty" query:"region" validate:"optional"`

	// The definition that configures this instance.
	OciOkeKubernetesRuntimeDefinitionID *uint `json:"OciOkeKubernetesRuntimeDefinitionID,omitempty" query:"ociokekubernetesruntimedefinitionid" gorm:"not null" validate:"required" relationship:"requires"`

	// An inventory of all OCI resources for the OKE cluster.
	ResourceInventory *datatypes.JSON `json:"ResourceInventory,omitempty" validate:"optional"`

	// The kubernetes runtime instance associated with the OCI OKE cluster.
	KubernetesRuntimeInstanceID *uint `json:"KubernetesRuntimeInstanceID,omitempty" query:"kubernetesruntimeinstanceid" gorm:"not null" validate:"required" relationship:"requires"`

	// The OCID for the OKE cluster. Populated by the controller after cluster creation.
	ClusterOCID *string `json:"ClusterOCID,omitempty" query:"clusterocid" validate:"optional"`
}
