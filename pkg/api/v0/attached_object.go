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
	//   - "describes": informational; does not block delete or update of the base.
	//   - "requires": blocks any caller from deleting the base while this
	//     reference exists.
	//   - "owns": blocks both delete and update of the base for any caller
	//     except the controller registered for the attached object's type,
	//     identified by its mTLS peer common name.
	Relationship *Relationship `json:"Relationship,omitempty" query:"relationship" gorm:"default:'describes'" validate:"optional"`
}
