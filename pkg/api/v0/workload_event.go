package v0

import (
	"time"
)

// WorkloadEvent is a summary of a Kubernetes Event that is associated with a
// WorkloadResourceInstance.
type WorkloadEvent struct {
	Common `swaggerignore:"true" mapstructure:",squash"`

	// A unique ID for de-duplicating purposes.  It is one of two thing:
	// * The Kubernetes Event resource UID: when the WorkloadEvent is derived
	// directly from a Kubernetes Event.
	// * The workload controller ID: when the WorkloadEvent is emitted by the
	// workload controller.
	RuntimeEventUID *string `json:"RuntimeEventUID,omitempty" query:"runtimeeventuid" gorm:"not null" validate:"required"`

	// The type of event that occurred in Kubernetes.
	Type *string `json:"Type,omitempty" query:"type" gorm:"not null" validate:"required"`

	// The reason for the event.
	Reason *string `json:"Reason,omitempty" query:"reason" gorm:"not null" validate:"required"`

	// The message associated with the event.
	Message *string `json:"Message,omitempty" query:"message" gorm:"not null" validate:"required"`

	// The timestamp for the event in the kubernetes runtime.
	Timestamp *time.Time `json:"Timestamp,omitempty" query:"timestamp" gorm:"not null" validate:"required"`

	// The related workload instance.
	WorkloadInstanceID *uint `json:"WorkloadInstanceID,omitempty" query:"workloadinstanceid" validate:"optional"`

	// The related workload resource instance.
	WorkloadResourceInstanceID *uint `json:"WorkloadResourceInstanceID,omitempty" query:"workloadresourceinstanceid" validate:"optional"`

	// The related helm workload instance.
	HelmWorkloadInstanceID *uint `json:"HelmWorkloadInstanceID,omitempty" query:"helmworkloadinstanceid" validate:"optional"`
}
