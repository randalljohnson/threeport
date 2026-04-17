package v0

import (
	"fmt"
	"regexp"
	"strings"

	util "github.com/threeport/threeport/pkg/util/v0"
	"gorm.io/gorm"
)

// envKeyRegex matches a valid POSIX environment variable name.
var envKeyRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// validateEnv checks that each entry in an Env slice is in KEY=VALUE format
// where KEY matches [a-zA-Z_][a-zA-Z0-9_]*.
func validateEnv(env []string) error {
	for _, entry := range env {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			return util.NewBadRequestError(
				fmt.Sprintf("invalid env entry %q: must be in KEY=VALUE format", entry),
			)
		}
		if !envKeyRegex.MatchString(parts[0]) {
			return util.NewBadRequestError(
				fmt.Sprintf(
					"invalid env key %q: must match [a-zA-Z_][a-zA-Z0-9_]*",
					parts[0],
				),
			)
		}
	}
	return nil
}

// BeforeCreate validates a MachineWorkloadDefinition before persisting to the
// database.
func (m *MachineWorkloadDefinition) BeforeCreate(tx *gorm.DB) error {
	if err := validateEnv(m.Env); err != nil {
		return err
	}
	return nil
}

// BeforeCreate validates a MachineWorkloadInstance before persisting to the
// database.
func (m *MachineWorkloadInstance) BeforeCreate(tx *gorm.DB) error {
	if err := validateEnv(m.Env); err != nil {
		return err
	}
	return nil
}
