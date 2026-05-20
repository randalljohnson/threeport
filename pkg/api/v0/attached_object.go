package v0

// AttachedObjectReference is a reference to an attached object.
//
// Four DB indexes are declared in the GORM tags below:
//
//   - idx_attached_object_unique: full-table unique composite across
//     (object_type, object_id, attached_object_type, attached_object_id).
//     Enforces that a given (base, attacher) pair appears in at most one
//     row regardless of relationship kind.
//
//   - idx_aor_marries_base: partial unique composite across
//     (object_type, object_id) where relationship = 'marries'. Enforces
//     that the base side of a marriage appears in at most one marries row
//     (1-to-1 cardinality for the base).
//
//   - idx_aor_marries_attached: partial unique composite across
//     (attached_object_type, attached_object_id) where relationship =
//     'marries'. Same constraint applied to the attacher side.
//
//   - idx_aor_owns_base: partial unique composite across
//     (object_type, object_id) where relationship = 'owns'. Enforces
//     that an owned base appears in at most one owns row, so a given
//     object has at most one owner. The attacher side is intentionally
//     unconstrained for owns: an owner may own many bases.
//
// Each participating column repeats the index name in its `uniqueIndex:`
// tag; GORM bundles them by name. The `,where:...` suffix on the marries
// indexes makes them partial indexes: only rows matching the predicate
// are indexed, so non-marries rows are invisible to the uniqueness check.
type AttachedObjectReference struct {
	Common `swaggerignore:"true" mapstructure:",squash"`

	// ObjectType is the kind of the base object being attached to. Read
	// "this attached object attaches to that object": the no-prefix
	// Object* fields name the "that" (anchor) side. The naming is
	// directional because the attached object is the side that can
	// determine the base object's lifecycle, depending on the type of
	// relationship (see below).
	ObjectType *string `json:"ObjectType,omitempty" query:"objecttype" gorm:"not null;uniqueIndex:idx_attached_object_unique;uniqueIndex:idx_aor_marries_base,where:relationship = 'marries';uniqueIndex:idx_aor_owns_base,where:relationship = 'owns'" validate:"required"`

	// ObjectID is the database ID of the base object.
	ObjectID *uint `json:"ObjectID,omitempty" query:"objectid" gorm:"not null;uniqueIndex:idx_attached_object_unique;uniqueIndex:idx_aor_marries_base,where:relationship = 'marries';uniqueIndex:idx_aor_owns_base,where:relationship = 'owns'" validate:"required"`

	// AttachedObjectType is the kind of the object doing the attaching;
	// the side that can determine the base object's lifecycle.
	AttachedObjectType *string `json:"AttachedObjectType,omitempty" query:"attachedobjecttype" gorm:"not null;uniqueIndex:idx_attached_object_unique;uniqueIndex:idx_aor_marries_attached,where:relationship = 'marries'" validate:"required"`

	// AttachedObjectID is the database ID of the attaching object.
	AttachedObjectID *uint `json:"AttachedObjectID,omitempty" query:"attachedobjectid" gorm:"not null;uniqueIndex:idx_attached_object_unique;uniqueIndex:idx_aor_marries_attached,where:relationship = 'marries'" validate:"required"`

	// Relationship classifies this reference and drives lifecycle behavior
	// via gorm hooks and generated code that reveals information about a
	// type's foreign keys:
	//   - "describes": informational; does not block delete or update of the base.
	//   - "requires": blocks any caller from deleting the base while this
	//     reference exists.
	//   - "owns": blocks both delete and update of the base for any caller
	//     except the controller registered for the attached object's type,
	//     identified by its mTLS peer common name.
	//     An owned base has at most one owner (enforced by the partial
	//     index idx_aor_owns_base above); an owner may own many bases.
	//   - "marries": enforces 1-to-1 cardinality between base and attacher
	//     via the partial indexes above; blocks both delete and update of
	//     the base for any caller except the partner's controller.
	Relationship *Relationship `json:"Relationship,omitempty" query:"relationship" gorm:"default:'describes'" validate:"optional"`
}
