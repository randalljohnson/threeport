package v0

import "time"

const (
	PathEventsJoinAttachedObjectReferences = "/v0/events-join-attached-object-references"
)

// Event is a record of an event in the system.
type Event struct {
	Common `swaggerignore:"true" mapstructure:",squash"`

	// AttachedObjectReferenceID is a reference to an attached object.
	// A foreign key is configured via db migration in cmd/database-migrator/migrations/000010_add_events_foreign_key.go
	AttachedObjectReferenceID *uint `json:",omitempty" query:"attachedobjectreferenceid" validate:"optional"`

	// A short, machine understandable string that gives the reason for the event being generated.
	Reason *string `query:"reason" gorm:"not null" validate:"required"`

	// A human-readable description of the status of this operation.
	Note *string `json:",omitempty" query:"note" validate:"optional"`

	// The number of times this event has occurred.
	Count *uint `query:"count" gorm:"not null" validate:"required"`

	// Time when this Event was first observed.
	EventTime *time.Time `query:"eventtime" gorm:"not null" validate:"required"`

	// The time at which the most recent occurrence of this event was recorded.
	LastObservedTime *time.Time `query:"lastobservedtime" gorm:"not null" validate:"required"`

	// Type of this event (Normal, Warning), new types could be added in the future.
	Type *string `query:"type" gorm:"not null" validate:"required"`

	// Name of the controller that emitted this Event.
	ReportingController *string `query:"reportingcontroller" gorm:"not null" validate:"required"`
}

//// Event is a record of an event in the system.
//type Event struct {
//	Common `swaggerignore:"true" mapstructure:",squash"`
//}
