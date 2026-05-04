package v0

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"

	"github.com/threeport/threeport/pkg/encryption/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
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

// BeforeCreate validates and encrypts fields on a MachineWorkloadDefinition
// before persisting to the database.
func (m *MachineWorkloadDefinition) BeforeCreate(tx *gorm.DB) error {
	if err := validateEnv(m.Env); err != nil {
		return err
	}
	return encryptTaggedFields(tx, m, false)
}

// BeforeUpdate re-encrypts changed encrypt-tagged fields on a
// MachineWorkloadDefinition before updating in the database.
func (m *MachineWorkloadDefinition) BeforeUpdate(tx *gorm.DB) error {
	updated := tx.Statement.Dest.(*MachineWorkloadDefinition)
	if err := validateEnv(updated.Env); err != nil {
		return err
	}
	return encryptTaggedFields(tx, updated, true)
}

// BeforeCreate validates and encrypts fields on a MachineWorkloadInstance
// before persisting to the database.
func (m *MachineWorkloadInstance) BeforeCreate(tx *gorm.DB) error {
	if err := validateEnv(m.Env); err != nil {
		return err
	}
	return encryptTaggedFields(tx, m, false)
}

// BeforeUpdate re-encrypts changed encrypt-tagged fields on a
// MachineWorkloadInstance before updating in the database.
func (m *MachineWorkloadInstance) BeforeUpdate(tx *gorm.DB) error {
	updated := tx.Statement.Dest.(*MachineWorkloadInstance)
	if err := validateEnv(updated.Env); err != nil {
		return err
	}
	return encryptTaggedFields(tx, updated, true)
}

// encryptTaggedFields iterates struct fields looking for `encrypt:"true"` tags
// and encrypts the value before writing to the DB via tx.Statement.SetColumn.
// Supports *string fields (encrypts the full value) and []string fields
// containing KEY=VALUE entries (encrypts only the VALUE portion so keys remain
// readable). When checkChanged is true, only changed fields are re-encrypted
// (for BeforeUpdate).
func encryptTaggedFields(tx *gorm.DB, obj interface{}, checkChanged bool) error {
	encryptionKey := os.Getenv("ENCRYPTION_KEY")
	if encryptionKey == "" {
		return errors.New("environment variable ENCRYPTION_KEY is not set")
	}

	objVal := reflect.ValueOf(obj).Elem()
	objType := objVal.Type()
	ns := schema.NamingStrategy{}

	for i := 0; i < objType.NumField(); i++ {
		field := objType.Field(i)
		if field.Tag.Get("encrypt") != "true" {
			continue
		}
		if checkChanged && !tx.Statement.Changed(field.Name) {
			continue
		}

		fieldVal := objVal.Field(i)
		columnName := ns.ColumnName("", field.Name)

		switch fieldVal.Kind() {
		case reflect.Ptr:
			if !util.IsNonNilPtr(fieldVal) {
				continue
			}
			plain, err := util.GetPtrValue(fieldVal)
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", field.Name, err)
			}
			// reject the redacted placeholder — clients should send a real
			// value to change the field, or omit it to leave it unchanged
			if plain == encryption.RedactedValuePlaceholder {
				return util.NewBadRequestError(
					fmt.Sprintf(
						"field %s contains redacted placeholder; provide a real value or omit the field",
						field.Name,
					),
				)
			}
			enc, err := encryption.Encrypt(encryptionKey, plain)
			if err != nil {
				return fmt.Errorf("failed to encrypt %s: %w", field.Name, err)
			}
			tx.Statement.SetColumn(columnName, enc)

		case reflect.Slice:
			if fieldVal.IsNil() || fieldVal.Len() == 0 {
				continue
			}
			slice, ok := fieldVal.Interface().([]string)
			if !ok {
				return fmt.Errorf("encrypt tag on non-[]string slice field %s", field.Name)
			}
			// encrypt only the VALUE portion of each KEY=VALUE entry so keys
			// remain readable
			encSlice := make([]string, len(slice))
			for j, entry := range slice {
				parts := strings.SplitN(entry, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("%s[%d] is not in KEY=VALUE format", field.Name, j)
				}
				// reject the redacted placeholder — clients should send a real
				// value to change the field, or omit it to leave it unchanged
				if parts[1] == encryption.RedactedValuePlaceholder {
					return util.NewBadRequestError(
						fmt.Sprintf(
							"%s[%d] contains redacted placeholder; provide a real value or omit the entry",
							field.Name, j,
						),
					)
				}
				encValue, err := encryption.Encrypt(encryptionKey, parts[1])
				if err != nil {
					return fmt.Errorf("failed to encrypt %s[%d]: %w", field.Name, j, err)
				}
				encSlice[j] = parts[0] + "=" + encValue
			}
			tx.Statement.SetColumn(columnName, encSlice)
		}
	}
	return nil
}
