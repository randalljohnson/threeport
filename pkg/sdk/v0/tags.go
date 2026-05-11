package v0

// Relationship classifies how an AttachedObjectReference relates the base
// object to the attached object. Drives lifecycle behavior (deletion
// blocking, update locking).
type Relationship string

const (
	RelationshipTag                    = "relationship"
	RelationshipDescribes Relationship = "describes"
	RelationshipRequires  Relationship = "requires"
	RelationshipOwns      Relationship = "owns"
	RelationshipTypeKey                = "type"

	EncryptTag  = "encrypt"
	EncryptTrue = "true"

	ValidateTag                 = "validate"
	ValidateRequired            = "required"
	ValidateOptional            = "optional"
	ValidateAssociation         = "association"
	ValidateOptionalAssociation = ValidateOptional + "," + ValidateAssociation

	PersistTag   = "persist"
	PersistFalse = "false"

	QueryTag = "query"
)
