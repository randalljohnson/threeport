package v0

import "gorm.io/datatypes"

// GcpProvider represents a Google Cloud Platform (GCP) project in an account with the GCP service provider.
type GcpProvider struct {
	Common `swaggerignore:"true" mapstructure:",squash"`

	// The unique name of a GCP provider.
	Name *string `gorm:"not null" validate:"required"`

	// The GCP project ID for the Google Cloud account.
	ProjectID *string `gorm:"not null" validate:"required"`

	// If true, is the GCP provider used when none specified for an instance.
	DefaultProvider *bool `json:",omitempty" gorm:"default:false" validate:"optional"`

	// The region to use for GCP managed services if not specified.
	DefaultRegion *string `gorm:"not null" validate:"required"`

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
	ZoneCount *int `gorm:"not null" validate:"required"`

	// The GCP instance type for the default initial node group.
	DefaultNodeGroupInstanceType *string `gorm:"not null" validate:"required"`

	// The number of nodes in the default initial node group.
	DefaultNodeGroupInitialSize *int `gorm:"not null" validate:"required"`

	// The minimum number of nodes the default initial node group should have.
	DefaultNodeGroupMinimumSize *int `gorm:"not null" validate:"required"`

	// The maximum number of nodes the default initial node group should have.
	DefaultNodeGroupMaximumSize *int `gorm:"not null" validate:"required"`

	// The GCP GKE kubernetes runtime instances derived from this definition.
	GcpGkeKubernetesRuntimeInstances []*GcpGkeKubernetesRuntimeInstance `json:",omitempty" validate:"optional,association"`

	// The kubernetes runtime definition for a GKE cluster in GCP.
	KubernetesRuntimeDefinitionID *uint `gorm:"not null" validate:"required" relationship:"marries"`
}

// GcpGkeKubernetesRuntimeInstance is a deployed instance of a GKE cluster.
type GcpGkeKubernetesRuntimeInstance struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Instance       `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The GCP provider in which the GKE cluster is provisioned.
	GcpProviderID *uint `gorm:"not null" validate:"required" relationship:"requires"`

	// The GCP region in which the cluster is provisioned.
	Region *string `json:",omitempty" validate:"optional"`

	// The definition that configures this instance.
	GcpGkeKubernetesRuntimeDefinitionID *uint `gorm:"not null" validate:"required" relationship:"requires"`

	// The kubernetes runtime instance associated with the GKE cluster.
	KubernetesRuntimeInstanceID *uint `gorm:"not null" validate:"required" relationship:"marries"`

	// An inventory of all GCP resources for the GKE cluster.
	ResourceInventory *datatypes.JSON `json:",omitempty" validate:"optional"`
}
