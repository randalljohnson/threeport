// Package v0 holds the API model for the reconciler test fixture. The fixture
// is a miniature threeport module: the generators that build a real module's
// api methods, client, and reconciler run over this package the same way, so a
// change to any of them shows up here as a compile or test failure.
//
// This file is generator input, the counterpart of a model file under
// pkg/api/v0 in the real tree. Everything beside it is generated.
package v0

import (
	tpapi "github.com/threeport/threeport/pkg/api/v0"
)

// ReconcilerTestInstance is the object the generated reconciler under test
// dispatches on. It carries no domain meaning. The embedded base types are
// what the generators read: Reconciliation supplies DeletionScheduled, which
// the deletion guard in each generated operation branch reads.
type ReconcilerTestInstance struct {
	tpapi.Common         `swaggerignore:"true" mapstructure:",squash"`
	tpapi.Instance       `mapstructure:",squash"`
	tpapi.Reconciliation `mapstructure:",squash"`

	// The latest status recorded by the reconciler.
	Status *string `json:",omitempty" validate:"optional"`
}
