package v0

import (
	"errors"
	"fmt"
	"os"
	"reflect"

	"github.com/threeport/threeport/pkg/encryption/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// BeforeCreate validates and encrypts sensitive fields on a
// MachineRuntimeInstance before persisting to the database.
func (m *MachineRuntimeInstance) BeforeCreate(tx *gorm.DB) error {
	// validate that at least one of SSHKey or SSHPassword is provided
	if m.SSHKey == nil && m.SSHPassword == nil {
		return util.NewBadRequestError(
			fmt.Sprintf(
				"machine runtime instance %s must have at least one of SSHKey or SSHPassword",
				*m.Name,
			),
		)
	}

	// encrypt sensitive values
	var encryptionKey = os.Getenv("ENCRYPTION_KEY")
	if encryptionKey == "" {
		return errors.New("environment variable ENCRYPTION_KEY is not set")
	}

	createdObj := *m
	objVal := reflect.ValueOf(&createdObj).Elem()
	objType := objVal.Type()
	ns := schema.NamingStrategy{}
	for i := 0; i < objType.NumField(); i++ {
		field := objType.Field(i)
		fieldVal := objVal.Field(i)

		// skip nil fields
		if !util.IsNonNilPtr(fieldVal) {
			continue
		}

		// encrypt field if encrypt tag is present
		encrypt := field.Tag.Get("encrypt")
		if encrypt == "true" {
			underlyingValue, err := util.GetPtrValue(fieldVal)
			if err != nil {
				return fmt.Errorf("failed to get string value for %s: %w", field.Name, err)
			}

			// caller round-tripped without decrypting; preserve existing DB ciphertext
			if underlyingValue == encryption.RedactedValuePlaceholder {
				continue
			}

			encryptedVal, err := encryption.Encrypt(encryptionKey, underlyingValue)
			if err != nil {
				return fmt.Errorf("failed to encrypt %s for storage: %w", field.Name, err)
			}

			// use gorm to get column name from field name
			columnName := ns.ColumnName("", field.Name)
			tx.Statement.SetColumn(columnName, encryptedVal)
		}
	}

	return nil
}

// BeforeUpdate encrypts sensitive fields on a MachineRuntimeInstance before
// updating in the database.
func (m *MachineRuntimeInstance) BeforeUpdate(tx *gorm.DB) error {
	// encrypt sensitive values
	var encryptionKey = os.Getenv("ENCRYPTION_KEY")
	if encryptionKey == "" {
		return errors.New("environment variable ENCRYPTION_KEY is not set")
	}

	updatedObj := tx.Statement.Dest.(*MachineRuntimeInstance)
	objVal := reflect.ValueOf(updatedObj).Elem()
	objType := objVal.Type()
	for i := 0; i < objType.NumField(); i++ {
		field := objType.Field(i)
		fieldVal := objVal.Field(i)

		// skip nil fields
		if !util.IsNonNilPtr(fieldVal) {
			continue
		}

		encrypt := field.Tag.Get("encrypt")
		if encrypt == "true" && tx.Statement.Changed(field.Name) {
			underlyingValue, err := util.GetPtrValue(fieldVal)
			if err != nil {
				return fmt.Errorf("failed to get string value for %s: %w", field.Name, err)
			}

			// caller round-tripped without decrypting; preserve existing DB ciphertext
			if underlyingValue == encryption.RedactedValuePlaceholder {
				continue
			}

			encryptedVal, err := encryption.Encrypt(encryptionKey, underlyingValue)
			if err != nil {
				return fmt.Errorf("failed to encrypt %s for storage: %w", field.Name, err)
			}

			// use gorm to get column name from field name
			ns := schema.NamingStrategy{}
			columnName := ns.ColumnName("", field.Name)
			tx.Statement.SetColumn(columnName, encryptedVal)
		}
	}

	return nil
}
