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

			// reject the redacted placeholder — it is only emitted by the
			// server when redacting responses and must never round-trip
			// back as input
			if underlyingValue == encryption.RedactedValuePlaceholder {
				return util.NewBadRequestError(
					fmt.Sprintf(
						"field %s contains redacted placeholder; provide a real value or omit the field",
						field.Name,
					),
				)
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

			// reject the redacted placeholder — clients should send a real
			// value to change the field, or omit it to leave it unchanged
			if underlyingValue == encryption.RedactedValuePlaceholder {
				return util.NewBadRequestError(
					fmt.Sprintf(
						"field %s contains redacted placeholder; provide a real value or omit the field",
						field.Name,
					),
				)
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

// BeforeDelete validates a delete request on a machine runtime instance to
// ensure deletion is possible — blocks deletion when any MachineWorkloadInstance
// still references this runtime via its MachineRuntimeInstanceID foreign key.
// Mirrors the KubernetesRuntimeInstance.BeforeDelete convention so callers can
// rely on ordered teardown (workloads first, runtime last).
func (m *MachineRuntimeInstance) BeforeDelete(tx *gorm.DB) error {
	var machineWorkloadInstances []MachineWorkloadInstance
	if result := tx.Where(
		&MachineWorkloadInstance{MachineRuntimeInstanceID: m.ID},
	).Find(&machineWorkloadInstances); result.Error != nil {
		return fmt.Errorf(
			"failed to query machine workload instances for machine runtime instance %s: %w",
			*m.Name, result.Error,
		)
	}

	if len(machineWorkloadInstances) > 0 {
		return util.NewBadRequestError(
			fmt.Sprintf(
				"machine runtime instance %s cannot be deleted until machine workload instances are removed",
				*m.Name,
			),
		)
	}

	return nil
}
