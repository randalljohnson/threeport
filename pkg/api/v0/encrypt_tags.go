package v0

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/threeport/threeport/pkg/encryption/v0"
	sdk "github.com/threeport/threeport/pkg/sdk/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// encryptedFieldsFor returns the encrypt-tagged fields of obj, or nil.
func encryptedFieldsFor(obj interface{}) []sdk.EncryptedField {
	p, ok := obj.(sdk.EncryptedFieldProvider)
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
			plain := *v
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
			// skip if the input is already encrypted — controllers are
			// responsible for decrypting before submitting, but a forgotten
			// decrypt round-trips ciphertext; skipping prevents double-encryption.
			if encryption.IsEncrypted(encryptionKey, plain) {
				continue
			}
			enc, err := encryption.Encrypt(encryptionKey, plain)
			if err != nil {
				return fmt.Errorf("failed to encrypt %s: %w", field.Name, err)
			}
			tx.Statement.SetColumn(field.Name.Column(), enc)

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
				// reject the redacted placeholder — clients should send a real
				// value to change the field, or omit it to leave it unchanged
				if value == encryption.RedactedValuePlaceholder {
					return util.NewBadRequestError(
						fmt.Sprintf(
							"%s[%d] contains redacted placeholder; provide a real value or omit the entry",
							field.Name, j,
						),
					)
				}
				// skip if already encrypted — preserves the original entry to
				// avoid double-encryption when a controller round-trips it.
				if encryption.IsEncrypted(encryptionKey, value) {
					encSlice[j] = entry
					continue
				}
				encValue, err := encryption.Encrypt(encryptionKey, value)
				if err != nil {
					return fmt.Errorf("failed to encrypt %s[%d]: %w", field.Name, j, err)
				}
				encSlice[j] = key + "=" + encValue
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
