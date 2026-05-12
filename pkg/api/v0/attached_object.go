package v0

// AttachedObjectReference is a reference to an attached object.
type AttachedObjectReference struct {
	Common `swaggerignore:"true" mapstructure:",squash"`

	// ObjectType is the kind of the base object being attached to. Read
	// "this attached object attaches to that object": the no-prefix
	// Object* fields name the "that" (anchor) side. The naming is
	// directional because the attached object is the side that can
	// determine the base object's lifecycle, depending on the type of
	// relationship (see below).
	ObjectType *string `json:"ObjectType,omitempty" query:"objecttype" gorm:"not null;uniqueIndex:idx_attached_object_unique" validate:"required"`

	// ObjectID is the database ID of the base object.
	ObjectID *uint `json:"ObjectID,omitempty" query:"objectid" gorm:"not null;uniqueIndex:idx_attached_object_unique" validate:"required"`

	// AttachedObjectType is the kind of the object doing the attaching;
	// the side that can determine the base object's lifecycle.
	AttachedObjectType *string `json:"AttachedObjectType,omitempty" query:"attachedobjecttype" gorm:"not null;uniqueIndex:idx_attached_object_unique" validate:"required"`

	// AttachedObjectID is the database ID of the attaching object.
	AttachedObjectID *uint `json:"AttachedObjectID,omitempty" query:"attachedobjectid" gorm:"not null;uniqueIndex:idx_attached_object_unique" validate:"required"`

	// Relationship classifies this reference and drives lifecycle behavior:
	//   - "describes": informational link; does not block deletion or
	//     restrict updates of the base object.
	//   - "requires": the attached object depends on the base object;
	//     deletion of the base object is blocked while this reference
	//     exists. The base object is otherwise externally mutable.
	//   - "owns": same blocking-on-delete behavior as "requires" plus the
	//     base object is blocked against external updates and may only be
	//     mutated by tearing down the attached object first.
	Relationship *Relationship `json:"Relationship,omitempty" query:"relationship" gorm:"default:'describes'" validate:"optional"`
}
