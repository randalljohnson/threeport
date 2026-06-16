package provider

import (
	"gorm.io/datatypes"
)

// InfraProvider defines the minimal interface any infrastructure backend must
// implement to benefit from durable lifecycle management. Providers that use
// Pulumi, Terraform, vanilla Go SDK calls, or Helm all satisfy this interface.
type InfraProvider interface {
	// DeployInfra creates the infrastructure (blocking).
	DeployInfra() error

	// DestroyInfra tears down the infrastructure (blocking).
	DestroyInfra() error

	// SetStackState restores previously persisted state for crash recovery.
	SetStackState(state *datatypes.JSON) error

	// GetStackState returns the current infrastructure state for persistence.
	GetStackState() (*datatypes.JSON, error)
}

// StreamableProvider supports real-time state streaming during operations.
// Only backends that write state to a local file (e.g. Pulumi, Terraform)
// implement this. The lifecycle handler uses type assertion to check for
// streaming support and skips it for non-streaming providers.
type StreamableProvider interface {
	InfraProvider

	// GetStateFilePath returns the path to the state file on disk.
	GetStateFilePath() (string, error)

	// ReadStateFile reads the current state directly from disk.
	// Returns nil (not error) if the file doesn't exist yet.
	ReadStateFile() (*datatypes.JSON, error)
}

// RefreshableProvider supports syncing state with cloud reality before
// operations. Providers that track resource state (Pulumi, Terraform)
// implement this to clear stale pending operations and detect drift.
type RefreshableProvider interface {
	InfraProvider

	// RefreshStack refreshes the state to match cloud reality.
	RefreshStack() error
}

// AdoptableProvider supports re-acquiring orphaned cloud resources that were
// created before their state reached the database. Providers whose resource
// names are deterministic implement this so a create that finds no usable
// state can adopt an already-existing resource instead of colliding on its
// name. The lifecycle handler calls DiscoverAndAdopt only on the no-existing-
// state path, right before DeployInfra.
type AdoptableProvider interface {
	InfraProvider

	// DiscoverAndAdopt checks the cloud for resources matching this
	// provider's deterministic names and arranges for the next deploy to
	// adopt any that already exist rather than create them.
	DiscoverAndAdopt() error
}
