package v0

import (
	"fmt"
	"strings"

	"github.com/iancoleman/strcase"
	"gorm.io/gorm"

	"github.com/threeport/threeport/pkg/encryption/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// EncryptedField describes an encrypt-tagged field on an API type. The SDK
// generates an EncryptedFields method per type that returns one entry per
// field, so runtime hooks read the list without walking struct tags. Name
// is the Go struct field name; the GORM column is derived via the default
// snake-case naming strategy at write time.
type EncryptedField struct {
	Name  string
	Value interface{} // *string or []string of KEY=VALUE
}

// EncryptedFieldProvider is implemented by every API type with at least one
// encrypt-tagged field.
type EncryptedFieldProvider interface {
	EncryptedFields() []EncryptedField
}

// encryptedFieldsFor returns the encrypt-tagged fields of obj, or nil.
func encryptedFieldsFor(obj interface{}) []EncryptedField {
	p, ok := obj.(EncryptedFieldProvider)
	if !ok {
		return nil
	}
	return p.EncryptedFields()
}

// ProcessEncryptTaggedFields encrypts struct fields tagged `encrypt:"true"`
// and rejects the redacted placeholder. checkChanged limits the work to
// fields the client mutated, and is set on update.
func ProcessEncryptTaggedFields(tx *gorm.DB, obj interface{}, checkChanged bool) error {
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
				tx.Statement.SetColumn(strcase.ToSnake(field.Name), enc)
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
			tx.Statement.SetColumn(strcase.ToSnake(field.Name), encSlice)

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

// RedactEncryptedValues takes an API object, replaces the value on any
// encrypt-tagged fields with the redacted placeholder, and returns the
// object. Nil pointer fields are left as-is since there is nothing to
// redact.
func RedactEncryptedValues(obj interface{}) interface{} {
	p, ok := obj.(EncryptedFieldProvider)
	if !ok {
		return obj
	}
	for _, field := range p.EncryptedFields() {
		switch v := field.Value.(type) {
		case *string:
			if v == nil {
				continue
			}
			*v = encryption.RedactedValuePlaceholder
		case []string:
			for j := range v {
				v[j] = encryption.RedactedValuePlaceholder
			}
		}
	}
	return obj
}

// DecryptValues takes an API object and the encryption key, decrypts any
// encrypt-tagged fields and returns the object with values in plaintext.
// Nil pointer fields are left as-is since there is nothing to decrypt.
// Slice fields are decrypted element-by-element, preserving the KEY=
// prefix on each entry (only the value after the first `=` is encrypted on
// write, so only that portion is decrypted on read).
func DecryptValues(obj interface{}, encryptionKey string) (interface{}, error) {
	p, ok := obj.(EncryptedFieldProvider)
	if !ok {
		return obj, nil
	}
	for _, field := range p.EncryptedFields() {
		switch v := field.Value.(type) {
		case *string:
			if v == nil {
				continue
			}
			decryptedVal, err := encryption.Decrypt(encryptionKey, *v)
			if err != nil {
				return obj, fmt.Errorf("failed to decrypt value in field %s: %w", field.Name, err)
			}
			*v = decryptedVal
		case []string:
			decrypted, err := encryption.DecryptEnvSlice(v, encryptionKey)
			if err != nil {
				return obj, fmt.Errorf("field %s: %w", field.Name, err)
			}
			copy(v, decrypted)
		}
	}
	return obj, nil
}
