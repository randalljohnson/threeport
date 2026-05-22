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

	// Fields carrying the event's subject - the object the event is
	// about. They flow in both directions:
	//   - On create: the caller sets ObjectType (FQTN form) + ObjectID
	//     in the request body. Event.BeforeCreate validates them;
	//     Event.AfterCreate inserts the matching AttachedObjectReference
	//     in the same transaction. ObjectName is ignored on write.
	//   - On read: GetEventsJoinAttachedObjectReferenceByQueryString
	//     projects the joined AOR's base object back into these
	//     fields, then resolves ObjectName via the type's name resolver.
	// gorm:"-" keeps them off the Event row in the schema - the AOR
	// is the source of truth on disk for the subject linkage.
	//
	// For an event describing a script failure on a
	// MachineRuntimeInstance named "some-host" (id 42), these hold:
	//   ObjectType = "threeport.io/v0.MachineRuntimeInstance"
	//   ObjectID   = 42
	//   ObjectName = "some-host"   (read only - ignored on create)
	// A consumer like `tptctl get events` uses them to render
	// "machine-runtime-instance/some-host" in the OBJECT column.
	ObjectType *string `json:"ObjectType,omitempty" gorm:"-"`
	ObjectID   *uint   `json:"ObjectID,omitempty" gorm:"-"`
	ObjectName *string `json:"ObjectName,omitempty" gorm:"-"`
}
