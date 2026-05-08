package v0

const (
	RelationshipTag       = "relationship"
	RelationshipDescribes = "describes"
	RelationshipRequires  = "requires"
	RelationshipOwns      = "owns"
	RelationshipTypeKey   = "type"

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
