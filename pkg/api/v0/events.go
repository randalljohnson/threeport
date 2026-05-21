package v0

import "time"

const (
	PathEventsJoinAttachedObjectReferences = "/v0/events-join-attached-object-references"
)

// Event is a record of an event in the system.
type Event struct {
	Common `swaggerignore:"true" mapstructure:",squash"`

	// A short, machine understandable string that gives the reason for the event being generated.
	Reason *string `json:"Reason,omitempty" query:"reason" validate:"required"`

	// A human-readable description of the status of this operation.
	Note *string `json:"Note,omitempty" query:"note" validate:"optional"`

	// The number of times this event has occurred.
	Count *uint `json:"Count,omitempty" query:"count" validate:"required"`

	// Time when this Event was first observed.
	EventTime *time.Time `json:"EventTime,omitempty" query:"eventtime" validate:"required"`

	// The time at which the most recent occurrence of this event was recorded.
	LastObservedTime *time.Time `json:"LastObservedTime,omitempty" query:"lastobservedtime" validate:"required"`

	// Type of this event (Normal, Warning), new types could be added in the future.
	Type *string `json:"Type,omitempty" query:"type" validate:"required"`

	// Name of the controller that emitted this Event.
	ReportingController *string `json:"ReportingController,omitempty" query:"reportingcontroller" validate:"required"`

	// Enrichment fields giving each event context about its subject -
	// what the event is about. Every Event row has a matching
	// AttachedObjectReference where this event is the attached side
	// and the subject object is the base side. Callers that want
	// these fields populated read events via
	// GetEventsJoinAttachedObjectReferenceByQueryString, which
	// projects the base object's type, ID, and name into the response.
	// gorm:"-" keeps them out of normal CRUD writes - they're
	// populated on read, never stored on the Event row.
	//
	// For an event describing a script failure on a
	// MachineRuntimeInstance named "some-host" (id 42), these hold:
	//   ObjectType = "v0.MachineRuntimeInstance"
	//   ObjectID   = 42
	//   ObjectName = "some-host"
	// A consumer like `tptctl get events` uses them to render
	// "machine-runtime-instance/some-host" in the OBJECT column.
	ObjectType *string `json:"ObjectType,omitempty" gorm:"-"`
	ObjectID   *uint   `json:"ObjectID,omitempty" gorm:"-"`
	ObjectName *string `json:"ObjectName,omitempty" gorm:"-"`
}
