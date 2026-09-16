package v0

import (
	"fmt"
	"os"
	"strings"
)

// resolveFileField reads the file at path and returns a pointer to its
// contents with trailing whitespace trimmed. fieldName names the config
// field being resolved for use in the error message.
func resolveFileField(fieldName, path string) (*string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s file at %s: %w", fieldName, path, err)
	}
	s := strings.TrimRight(string(content), "\n\r\t ")
	return &s, nil
}
