package provider

import (
	"fmt"

	kube "github.com/threeport/threeport/pkg/kube/v0"
)

const (
	// Max length of runtime names prevents infra provider resource names
	// exceeding maximum lengths imposed by provider.
	RuntimeNameMaxLength = 30

	// The name of the cloud provider account that is used to create a genesis Threeport
	// control plane.  When a genesis control plane is created, the account that was used
	// to create the control plane is stored in the Threeport API with this name.
	DefaultAccountName = "default-account"

	// ManagedByLabelKey is the cloud resource label or tag key set on
	// provider-provisioned infrastructure to mark it as managed by threeport.
	// It is valid across cloud providers; kubernetes resources use a separate
	// slash-namespaced key that cloud resource labels reject.
	ManagedByLabelKey = "managed-by"

	// ManagedByLabelValue is the value paired with ManagedByLabelKey on
	// infrastructure managed by threeport.
	ManagedByLabelValue = "threeport"
)

// KubernetesRuntimeInfra is the interface each provider has to satisfy to manage
// Kubernetes runtime infra.
type KubernetesRuntimeInfra interface {
	Create() (*kube.KubeConnectionInfo, error)
	Delete() error
}

// ThreeportRuntimeName returns the name for a Kubernetes runtime that hosts the
// threeport control plane.
func ThreeportRuntimeName(threeportInstanceName string) string {
	return fmt.Sprintf("threeport-%s", threeportInstanceName)
}

// ThreeportProviderTags returns the standard tags applied to cloud provider
// infrastructure resources to properly identify them.
func ThreeportProviderTags() map[string]string {
	return map[string]string{"ProvisionedBy": "threeport"}
}
