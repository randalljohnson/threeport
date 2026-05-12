package v0

// AttachedObjectReference is a reference to an attached object.
type AttachedObjectReference struct {
	Common `swaggerignore:"true" mapstructure:",squash"`

	// The object type of the base object.
	ObjectType *string `query:"objecttype" gorm:"not null;uniqueIndex:idx_attached_object_unique" validate:"required"`

	// The object ID of the base object.
	ObjectID *uint `query:"objectid" gorm:"not null;uniqueIndex:idx_attached_object_unique" validate:"required"`

	// The object type of the attached object.
	AttachedObjectType *string `query:"attachedobjecttype" gorm:"not null;uniqueIndex:idx_attached_object_unique" validate:"required"`

	// The object ID of the attached object.
	AttachedObjectID *uint `query:"attachedobjectid" gorm:"not null;uniqueIndex:idx_attached_object_unique" validate:"required"`
}
