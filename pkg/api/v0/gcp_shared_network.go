package v0

// GcpSharedNetwork represents a VPC network and subnetwork pair shared across
// multiple GCE machine instances scoped to a (GcpProvider, region) tuple. The
// GCE reconciler looks one up (or creates it) when a
// GcpGceMachineRuntimeInstance is provisioned; requires-AOR wiring holds the
// shared network in place while any instance still references it.
type GcpSharedNetwork struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Instance       `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The GCP provider that owns this shared network.
	GcpProviderID *uint `json:",omitempty" validate:"required" gorm:"not null" relationship:"requires"`

	// The GCP region this shared network lives in.
	Region *string `json:",omitempty" validate:"required" gorm:"not null"`

	// NetworkCIDR is the CIDR range for the VPC network.
	NetworkCIDR *string `json:",omitempty" validate:"required" gorm:"not null"`

	// SubnetCIDR is the CIDR range for the primary subnetwork; must be
	// contained in NetworkCIDR (validated by the reconciler via
	// CidrContainsSubnet).
	SubnetCIDR *string `json:",omitempty" validate:"required" gorm:"not null"`

	// NetworkID is the provider network identifier populated after apply.
	NetworkID *string `json:",omitempty" validate:"optional"`

	// SubnetworkID is the provider subnetwork identifier populated after apply.
	SubnetworkID *string `json:",omitempty" validate:"optional"`
}
