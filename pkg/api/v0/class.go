package v0

// Named includes the name field for every object a client addresses by name.
//
// The unique index behind the name covers only rows that are not soft deleted,
// so a name returns to the pool the moment the object holding it is deleted.
// The index is deliberately left unnamed: an index named here would be reused
// verbatim on every table this type is embedded in, and a database that scopes
// index names above the table rejects the second one. Unnamed, each table gets
// an index named for itself.
type Named struct {
	// An arbitrary name for the object, unique among objects of its own kind
	// that have not been deleted.
	Name *string `json:",omitempty" validate:"required" gorm:"not null;uniqueIndex:,where:deleted_at IS NULL"`
}

// Definition includes a set of fields for every definition object.
type Definition struct {
	Named `mapstructure:",squash"`

	// The profile to associate with the definition.  Profile is a named
	// standard configuration for a definition object.
	ProfileID *uint `json:",omitempty" validate:"optional,association"`

	// The tier to associate with the definition.  Tier is a level of
	// criticality for access control.
	TierID *uint `json:",omitempty" validate:"optional,association"`
}

// Instance includes a set of fields for every instance object.
type Instance struct {
	Named `mapstructure:",squash"`

	// The status of the instance.
	//TODO: use a custom type
	Status *string `json:",omitempty" validate:"optional"`
}
