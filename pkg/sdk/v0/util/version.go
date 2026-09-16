package util

// GetDefaultObjectVersion returns the version every object is served
// under until a second one exists.
func GetDefaultObjectVersion(obj string) string {
	return "v0"
}
