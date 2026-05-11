package v0

// QueryParamIncludeDeleted is the URL query parameter that bypasses the
// soft-delete filter on Get handlers when set to "true". Used for historical
// lookups such as resolving the name of an object that has since been deleted.
const QueryParamIncludeDeleted = "includedeleted"
