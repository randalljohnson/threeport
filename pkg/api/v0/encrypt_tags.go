package v0

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/threeport/threeport/pkg/encryption/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// encryptedFieldsFor returns the encrypt-tagged fields of obj, or nil.
func encryptedFieldsFor(obj interface{}) []EncryptedField {
	p, ok := obj.(EncryptedFieldProvider)
	if !ok {
		return nil
	}
	return p.EncryptedFields()
}

// processEncryptTaggedFields encrypts struct fields tagged `encrypt:"true"`
// and rejects the redacted placeholder. checkChanged limits the work to
// fields the client mutated, and is set on update.
func processEncryptTaggedFields(tx *gorm.DB, obj interface{}, checkChanged bool) error {
	fields := encryptedFieldsFor(obj)
	if len(fields) == 0 {
		return nil
	}

	// shared symmetric key for AES-GCM, sourced from the API server's env
	encryptionKey, err := encryption.KeyFromEnv()
	if err != nil {
		return err
	}

	for _, field := range fields {
		// in BeforeUpdate, leave fields the client didn't modify alone — the
		// existing DB ciphertext is already correct
		if checkChanged && !tx.Statement.Changed(string(field.Name)) {
			continue
		}
		switch v := field.Value.(type) {
		case *string:
			// nil pointer means the client didn't send the field; nothing to
			// encrypt, existing DB value (if any) preserved
			if v == nil {
				continue
			}
			enc, err := encryptValue(tx, encryptionKey, *v, string(field.Name))
			if err != nil {
				return err
			}
			if enc != *v {
				tx.Statement.SetColumn(field.Name.Column(), enc)
			}

		case []string:
			// nil/empty slice: nothing to encrypt
			if len(v) == 0 {
				continue
			}
			// each entry is KEY=VALUE; encrypt only VALUE so KEYs stay readable
			// for callers that need to introspect them (e.g. env-var names)
			encSlice := make([]string, len(v))
			for j, entry := range v {
				key, value, ok := strings.Cut(entry, "=")
				if !ok {
					return fmt.Errorf("%s[%d] is not in KEY=VALUE format", field.Name, j)
				}
				encValue, err := encryptValue(tx, encryptionKey, value, fmt.Sprintf("%s[%d]", field.Name, j))
				if err != nil {
					return err
				}
				if encValue == value {
					encSlice[j] = entry
				} else {
					encSlice[j] = key + "=" + encValue
				}
			}
			tx.Statement.SetColumn(field.Name.Column(), encSlice)

		default:
			return fmt.Errorf(
				"encrypt tag on unsupported field type for %s",
				field.Name,
			)
		}
	}
	return nil
}

// encryptValue encrypts plain and returns the ciphertext. Returns plain
// unchanged when already encrypted, and a bad-request error when plain is
// the redacted placeholder. fieldRef appears in error and log messages.
func encryptValue(tx *gorm.DB, encryptionKey, plain, fieldRef string) (string, error) {
	// reject the redacted placeholder; clients send a real value to change
	// the field or omit it to leave existing ciphertext in place
	if plain == encryption.RedactedValuePlaceholder {
		return "", util.NewBadRequestError(
			fmt.Sprintf("%s contains redacted placeholder; provide a real value or omit it", fieldRef),
		)
	}
	// skip if already encrypted; a forgotten decrypt by a controller would
	// otherwise double-encrypt the value
	if encryption.IsEncrypted(encryptionKey, plain) {
		tx.Logger.Info(
			tx.Statement.Context,
			"skipping encryption of %s: value already encrypted (likely a controller resubmitted ciphertext)",
			fieldRef,
		)
		return plain, nil
	}
	enc, err := encryption.Encrypt(encryptionKey, plain)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt %s: %w", fieldRef, err)
	}
	return enc, nil
}
