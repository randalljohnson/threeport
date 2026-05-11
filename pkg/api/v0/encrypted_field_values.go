package v0

import (
	"fmt"

	"github.com/threeport/threeport/pkg/encryption/v0"
)

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
