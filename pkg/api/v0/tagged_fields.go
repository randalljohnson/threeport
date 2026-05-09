package v0

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gorm.io/gorm"

	"github.com/threeport/threeport/pkg/encryption/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// ProcessCoreTaggedFieldsBeforeCreate runs core tag-triggered behavior on
// an API object before create.
func ProcessCoreTaggedFieldsBeforeCreate(tx *gorm.DB, obj interface{}) error {
	return processEncryptTaggedFields(tx, obj, false)
}

// ProcessCoreTaggedFieldsBeforeUpdate runs core tag-triggered behavior on
// an API object before update.
func ProcessCoreTaggedFieldsBeforeUpdate(tx *gorm.DB, obj interface{}) error {
	if err := processRelationshipTaggedFieldsBeforeUpdate(tx, obj); err != nil {
		return err
	}
	return processEncryptTaggedFields(tx, obj, true)
}

// ProcessCoreTaggedFieldsBeforeDelete runs core tag-triggered behavior on
// an API object before delete.
func ProcessCoreTaggedFieldsBeforeDelete(tx *gorm.DB, obj interface{}) error {
	return processRelationshipTaggedFieldsBeforeDelete(tx, obj)
}

// ProcessCoreTaggedFieldsAfterCreate runs core tag-triggered behavior on
// an API object after create.
func ProcessCoreTaggedFieldsAfterCreate(tx *gorm.DB, obj interface{}) error {
	return processRelationshipTaggedFieldsAfterCreate(tx, obj)
}

// ProcessCoreTaggedFieldsAfterUpdate runs core tag-triggered behavior on
// an API object after update.
func ProcessCoreTaggedFieldsAfterUpdate(tx *gorm.DB, obj interface{}) error {
	return processRelationshipTaggedFieldsAfterUpdate(tx, obj)
}

// ProcessCoreTaggedFieldsAfterDelete runs core tag-triggered behavior on
// an API object after delete.
func ProcessCoreTaggedFieldsAfterDelete(tx *gorm.DB, obj interface{}) error {
	return nil
}

// encryptedField describes an encrypt-tagged field on an API type. The SDK
// generates an EncryptedFields() method per type that returns one entry
// per field, so runtime hooks read the list directly instead of walking
// struct tags via reflection.
type encryptedField struct {
	fieldName  string
	columnName string
	value      interface{} // *string or []string of KEY=VALUE
}

// encryptedFieldProvider is implemented by every API type with at least
// one encrypt-tagged field.
type encryptedFieldProvider interface {
	EncryptedFields() []encryptedField
}

// encryptedFieldsFor returns the encrypt-tagged fields of obj, or nil.
func encryptedFieldsFor(obj interface{}) []encryptedField {
	p, ok := obj.(encryptedFieldProvider)
	if !ok {
		return nil
	}
	return p.EncryptedFields()
}

// processEncryptTaggedFields encrypts struct fields tagged `encrypt:"true"`
// and rejects the redacted placeholder. checkChanged limits work to fields
// the client mutated, used on update.
func processEncryptTaggedFields(tx *gorm.DB, obj interface{}, checkChanged bool) error {
	fields := encryptedFieldsFor(obj)
	if len(fields) == 0 {
		return nil
	}

	// shared symmetric key for AES-GCM, sourced from the API server's env
	encryptionKey := os.Getenv("ENCRYPTION_KEY")
	if encryptionKey == "" {
		return errors.New("environment variable ENCRYPTION_KEY is not set")
	}

	for _, field := range fields {
		// in BeforeUpdate, leave fields the client didn't modify alone — the
		// existing DB ciphertext is already correct
		if checkChanged && !tx.Statement.Changed(field.fieldName) {
			continue
		}
		switch v := field.value.(type) {
		case *string:
			// nil pointer means the client didn't send the field; nothing to
			// encrypt, existing DB value (if any) preserved
			if v == nil {
				continue
			}
			plain := *v
			// reject the redacted placeholder — clients should send a real
			// value to change the field, or omit it to leave it unchanged
			if plain == encryption.RedactedValuePlaceholder {
				return util.NewBadRequestError(
					fmt.Sprintf(
						"field %s contains redacted placeholder; provide a real value or omit the field",
						field.fieldName,
					),
				)
			}
			// skip if the input is already encrypted — controllers are
			// responsible for decrypting before submitting, but a forgotten
			// decrypt round-trips ciphertext; skipping prevents double-encryption.
			if encryption.IsEncrypted(encryptionKey, plain) {
				continue
			}
			enc, err := encryption.Encrypt(encryptionKey, plain)
			if err != nil {
				return fmt.Errorf("failed to encrypt %s: %w", field.fieldName, err)
			}
			tx.Statement.SetColumn(field.columnName, enc)

		case []string:
			// nil/empty slice: nothing to encrypt
			if len(v) == 0 {
				continue
			}
			// each entry is KEY=VALUE; encrypt only VALUE so KEYs stay readable
			// for callers that need to introspect them (e.g. env-var names)
			encSlice := make([]string, len(v))
			for j, entry := range v {
				parts := strings.SplitN(entry, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("%s[%d] is not in KEY=VALUE format", field.fieldName, j)
				}
				// reject the redacted placeholder — clients should send a real
				// value to change the field, or omit it to leave it unchanged
				if parts[1] == encryption.RedactedValuePlaceholder {
					return util.NewBadRequestError(
						fmt.Sprintf(
							"%s[%d] contains redacted placeholder; provide a real value or omit the entry",
							field.fieldName, j,
						),
					)
				}
				// skip if already encrypted — preserves the original entry to
				// avoid double-encryption when a controller round-trips it.
				if encryption.IsEncrypted(encryptionKey, parts[1]) {
					encSlice[j] = entry
					continue
				}
				encValue, err := encryption.Encrypt(encryptionKey, parts[1])
				if err != nil {
					return fmt.Errorf("failed to encrypt %s[%d]: %w", field.fieldName, j, err)
				}
				encSlice[j] = parts[0] + "=" + encValue
			}
			tx.Statement.SetColumn(field.columnName, encSlice)

		default:
			return fmt.Errorf(
				"encrypt tag on unsupported field type for %s",
				field.fieldName,
			)
		}
	}
	return nil
}
