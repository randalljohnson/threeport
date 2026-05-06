package v0

// AttachedObjectReference is a reference to an attached object.
type AttachedObjectReference struct {
	Common `swaggerignore:"true" mapstructure:",squash"`

	// The object type of the base object.
	ObjectType *string `json:"ObjectType,omitempty" query:"objecttype" gorm:"not null;uniqueIndex:idx_attached_object_unique" validate:"required"`

	// The object ID of the base object.
	ObjectID *uint `json:"ObjectID,omitempty" query:"objectid" gorm:"not null;uniqueIndex:idx_attached_object_unique" validate:"required"`

	// The object type of the attached object.
	AttachedObjectType *string `json:"AttachedObjectType,omitempty" query:"attachedobjecttype" gorm:"not null;uniqueIndex:idx_attached_object_unique" validate:"required"`

	// The object ID of the attached object.
	AttachedObjectID *uint `json:"AttachedObjectID,omitempty" query:"attachedobjectid" gorm:"not null;uniqueIndex:idx_attached_object_unique" validate:"required"`

	// Relationship classifies this reference and drives lifecycle behavior:
	//   - "describes": informational link; does not block deletion or
	//     restrict updates of the base object.
	//   - "requires": the attached object depends on the base object;
	//     deletion of the base object is blocked while this reference
	//     exists. The base object is otherwise externally mutable.
	//   - "owns": same blocking-on-delete behavior as "requires" plus the
	//     base object is locked against external updates and may only be
	//     mutated by tearing down the attached object first.
	Relationship *string `json:"Relationship,omitempty" query:"relationship" gorm:"default:'describes'" validate:"optional"`
}
