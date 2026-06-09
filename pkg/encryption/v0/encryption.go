package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// KeyEnvVar is the environment variable from which the API server and
// controllers read the shared symmetric encryption key.
const KeyEnvVar = "ENCRYPTION_KEY"

// KeyFromEnv reads the encryption key from the environment, returning an
// error if KeyEnvVar is unset.
func KeyFromEnv() (string, error) {
	key := os.Getenv(KeyEnvVar)
	if key == "" {
		return "", fmt.Errorf("environment variable %s is not set", KeyEnvVar)
	}
	return key, nil
}

// GenerateKey generates a random 32-byte key for use in encryption
// (32 bytes is the maximum key size for AES-256).
func GenerateKey() (string, error) {

	// creates a new byte array the size of our key
	key := make([]byte, 32)

	// populate our key with a cryptographically secure
	// random sequence
	_, err := rand.Read(key)
	if err != nil {
		return "", fmt.Errorf("failed to generate key: %w", err)
	}

	// encode our key in base64 and return it as a string
	return base64.StdEncoding.EncodeToString(key), nil
}

// Encrypt encrypts a string using AES-GCM.
func Encrypt(key, text string) (string, error) {

	// decode the key from base64
	decodedKey, err := util.Base64Decode(key)
	if err != nil {
		return "", fmt.Errorf("failed to decode key: %w", err)
	}

	// creates a new AES cipher using our 32 byte key
	c, err := aes.NewCipher([]byte(decodedKey))
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// configure Galois/Counter mode,
	// which provides both authentication and encryption
	gcm, err := cipher.NewGCM(c)
	if err != nil {
		return "", fmt.Errorf("failed to configure galois counter mode: %w", err)
	}

	// creates a new byte array the size of the nonce
	nonce := make([]byte, gcm.NonceSize())

	// populate nonce with a random and unique value, which is
	// required for GCM to be secure
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// encode our nonce and ciphertext in base64 and return
	// them as a string
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(text), nil)), nil

}

// Decrypt decrypts a string using AES-GCM.
func Decrypt(key, ciphertext string) (string, error) {

	// decode the ciphertext from base64
	decodedCipherText, err := util.Base64Decode(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	// decode the key from base64
	decodedKey, err := util.Base64Decode(key)
	if err != nil {
		return "", fmt.Errorf("failed to decode key: %w", err)
	}

	// create a new AES cipher using our 32 byte key
	c, err := aes.NewCipher([]byte(decodedKey))
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// configure Galois/Counter mode
	gcm, err := cipher.NewGCM(c)
	if err != nil {
		return "", fmt.Errorf("failed to configure galois counter mode: %w", err)
	}

	// get the nonce size
	nonceSize := gcm.NonceSize()
	if len(decodedCipherText) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	// extract the nonce from the ciphertext
	nonce, decodedCipherText := decodedCipherText[:nonceSize], decodedCipherText[nonceSize:]

	// decrypt the ciphertext
	plaintext, err := gcm.Open(nil, []byte(nonce), []byte(decodedCipherText), nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	// return the plaintext as a string
	return string(plaintext), nil
}

// DecryptEnvSlice decrypts the VALUE portion of each KEY=VALUE entry in
// env, leaving keys in plaintext. Returns a new slice — input is not
// mutated.
func DecryptEnvSlice(env []string, encryptionKey string) ([]string, error) {
	decrypted := make([]string, len(env))
	for i, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("entry %d %q is not in KEY=VALUE format", i, entry)
		}
		decValue, err := Decrypt(encryptionKey, value)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt entry %d: %w", i, err)
		}
		decrypted[i] = key + "=" + decValue
	}
	return decrypted, nil
}

// IsEncrypted attempts to decrypt a value.  If decryption fails it returns
// false to indicate the value provided is not encrypted.  If decryption is
// successful it returns true to indicate the value is encrypted.
func IsEncrypted(key, value string) bool {
	_, err := Decrypt(key, value)
	if err != nil {
		return false
	}

	return true
}

// RedactedValuePlaceholder is the string substituted for the plaintext of an
// encrypted field when an API object is serialized for display without the
// encryption key. Callers that round-trip an object through tptctl get →
// tptctl replace (without --decrypt-secrets) will see this value in their
// config file; BeforeCreate / BeforeUpdate hooks skip encryption on fields
// whose incoming value equals this placeholder, so the DB retains its
// existing ciphertext rather than storing the marker as plaintext.
const RedactedValuePlaceholder = "[encrypted value redacted]"

// EncryptStringMap encrypts a map of strings using AES-GCM.
func EncryptStringMap(key string, input map[string]string) (map[string]string, error) {
	encryptedMap := make(map[string]string)
	for k, v := range input {
		encryptedVal, err := Encrypt(key, v)
		if err != nil {
			return input, err
		}
		encryptedMap[k] = encryptedVal
	}
	return encryptedMap, nil
}

// DecryptStringMap encrypts a map of strings using AES-GCM.
func DecryptStringMap(key string, input map[string]string) (map[string]string, error) {
	decryptedMap := make(map[string]string)
	for k, v := range input {
		decryptedVal, err := Decrypt(key, v)
		if err != nil {
			return input, err
		}
		decryptedMap[k] = decryptedVal
	}
	return decryptedMap, nil
}
