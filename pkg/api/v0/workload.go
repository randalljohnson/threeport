package v0

import (
	"time"

	"gorm.io/datatypes"
)

const (
	PathWorkloadResourceDefinitionSets = "/v0/workload-resource-definition-sets"
)

// WorkloadDefinition is a collection of Kubernetes manifests that define a
// distinct workload.
type WorkloadDefinition struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Definition     `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The yaml manifests that define the workload configuration.
	YAMLDocument *string `gorm:"not null" validate:"required"`

	// The associated workload resource definitions that are derived.
	WorkloadResourceDefinitions []*WorkloadResourceDefinition `json:"WorkloadResourceDefinitions,omitempty" validate:"optional,association"`

	// The associated workload instances that are deployed from this definition.
	WorkloadInstances []*WorkloadInstance `json:"WorkloadInstances,omitempty" validate:"optional,association"`
}

// WorkloadResourceDefinition is an individual Kubernetes resource manifest.
type WorkloadResourceDefinition struct {
	Common `swaggerignore:"true" mapstructure:",squash"`

	// The individual manifest in JSON format.
	JSONDefinition *datatypes.JSON `gorm:"not null" validate:"required"`

	// The workload definition this resource belongs to.
	WorkloadDefinitionID *uint `query:"workloaddefinitionid" gorm:"not null" validate:"required"`
}

// WorkloadInstance is a deployed instance of a workload.
type WorkloadInstance struct {
	Common         `swaggerignore:"true" mapstructure:",squash"`
	Instance       `mapstructure:",squash"`
	Reconciliation `mapstructure:",squash"`

	// The kubernetes runtime to which the workload is deployed.
	KubernetesRuntimeInstanceID *uint `query:"kubernetesruntimeinstanceid" gorm:"not null" validate:"required" relationship:"requires"`

	// The definition used to configure the workload instance.
	WorkloadDefinitionID *uint `query:"workloaddefinitionid" gorm:"not null" validate:"required" relationship:"requires"`

	// The associated workload resource definitions that are derived.
	WorkloadResourceInstances []*WorkloadResourceInstance `json:"WorkloadResourceInstances,omitempty" validate:"optional,association"`

	// The latest status of a workload instance.
	Status *string `query:"status" validate:"optional"`

	// All events generated for the workload instance that aren't related to a
	// particular workload resource instance.
	Events []*WorkloadEvent `json:"Events,omitempty" query:"events" validate:"optional"`
}

// WorkloadResourceInstance is a Kubernetes resource instance.
type WorkloadResourceInstance struct {
	Common `swaggerignore:"true" mapstructure:",squash"`

	// The individual manifest in JSON format.  This field is a superset of
	// WorkloadResourceDefinition.JSONDefinition in that it has namespace
	// management and other configuration - such as resource allocation
	// management - added.
	JSONDefinition *datatypes.JSON `gorm:"not null" validate:"required"`

	// The workload instance this resource belongs to.
	WorkloadInstanceID *uint `query:"workloadinstanceid" gorm:"not null" validate:"required"`

	// The most recent operation performed on a Kubernete resource in the
	// kubernetes runtime.
	LastOperation *string `query:"lastoperation" validate:"optional"`

	// Indicates if object is considered to be reconciled by workload controller.
	Reconciled *bool `query:"reconciled" gorm:"default:false" validate:"optional"`

	// The JSON definition of a Kubernetes resource as stored in etcd in the
	// kubernetes runtime.
	RuntimeDefinition *datatypes.JSON `query:"runtimedefinition" validate:"optional"`

	// All events that have occured related to this object.
	Events []*WorkloadEvent `json:"Events,omitempty" query:"events" validate:"optional"`

	// Whether another controller has scheduled this resource for deletion
	ScheduledForDeletion *time.Time `query:"scheduledfordeletion" validate:"optional"`
}

// WorkloadEvent is a summary of an event associated with a workload instance.
type WorkloadEvent struct {
	Common `swaggerignore:"true" mapstructure:",squash"`

	// A unique ID for de-duplicating purposes.  It is one of:
	// * The Kubernetes Event resource UID: when the WorkloadEvent is derived
	// directly from a Kubernetes Event.
	// * The workload controller ID: when the WorkloadEvent is emitted by the
	// workload controller.
	// * The machine workload controller ID: when the WorkloadEvent is emitted
	// by the machine workload controller.
	RuntimeEventUID *string `query:"runtimeeventuid" gorm:"not null" validate:"required"`

	// The type of event that occurred (e.g. Normal, Warning).
	Type *string `query:"type" gorm:"not null" validate:"required"`

	// The reason for the event.
	Reason *string `query:"reason" gorm:"not null" validate:"required"`

	// The message associated with the event.
	Message *string `query:"message" gorm:"not null" validate:"required"`

	// The timestamp for the event.
	Timestamp *time.Time `query:"timestamp" gorm:"not null" validate:"required"`

	// The related workload instance.
	WorkloadInstanceID *uint `query:"workloadinstanceid" validate:"optional"`

	// The related workload resource instance.
	WorkloadResourceInstanceID *uint `query:"workloadresourceinstanceid" validate:"optional"`

	// The related helm workload instance.
	HelmWorkloadInstanceID *uint `query:"helmworkloadinstanceid" validate:"optional"`

	// The related machine workload instance.
	MachineWorkloadInstanceID *uint `query:"machineworkloadinstanceid" validate:"optional"`
}
