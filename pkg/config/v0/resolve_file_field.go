package v0

import (
	"fmt"
	"os"
	"strings"
)

// resolveFileField reads path and returns a pointer to its contents with
// trailing newline, CR, tab, and space trimmed.  fieldName is interpolated
// into the read error.
func resolveFileField(fieldName, path string) (*string, error) {
	// read the file named by the *File field
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s file at %s: %w", fieldName, path, err)
	}

	// drop trailing newline, CR, tab, and space
	s := strings.TrimRight(string(content), "\n\r\t ")
	return &s, nil
}
