package encryption

import (
	"encoding/base64"
	"strings"
	"testing"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// mustGenerateKey returns a fresh AES-256 key or fails the test.
func mustGenerateKey(t *testing.T) string {
	t.Helper()
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	return key
}

// TestKeyFromEnv covers reading the encryption key from the environment and
// erroring when unset.
func TestKeyFromEnv(t *testing.T) {
	t.Run("returns value when env var set", func(t *testing.T) {
		// arrange: seed the env var with a known value scoped to this test
		t.Setenv(KeyEnvVar, "some-key-value")

		// act
		got, err := KeyFromEnv()

		// assert: no error and value round-trips
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "some-key-value" {
			t.Fatalf("got %q, want %q", got, "some-key-value")
		}
	})

	t.Run("errors when env var unset", func(t *testing.T) {
		// arrange: force the env var to empty within the test scope
		t.Setenv(KeyEnvVar, "")

		// act
		_, err := KeyFromEnv()

		// assert: error mentions the env var name
		if err == nil {
			t.Fatal("expected error when env var unset")
		}
		if !strings.Contains(err.Error(), KeyEnvVar) {
			t.Fatalf("error %q should mention %s", err.Error(), KeyEnvVar)
		}
	})
}

// TestGenerateKey covers that GenerateKey emits a base64-encoded 32-byte key
// and produces distinct keys across calls.
func TestGenerateKey(t *testing.T) {
	// act: generate two independent keys
	k1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	k2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	// assert: raw bytes decode to exactly 32 bytes (AES-256 key size)
	raw, err := base64.StdEncoding.DecodeString(k1)
	if err != nil {
		t.Fatalf("key is not valid base64: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("decoded key length = %d, want 32", len(raw))
	}

	// assert: two calls yield distinct keys (randomness sanity check)
	if k1 == k2 {
		t.Fatal("two GenerateKey calls returned identical output")
	}
}

// TestEncryptDecryptRoundtrip covers that Decrypt(Encrypt(x)) == x for a
// representative slice of plaintexts.
func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := mustGenerateKey(t)

	// table: exercise empty, ascii, unicode, and long plaintexts
	tests := []struct {
		name      string
		plaintext string
	}{
		{"empty string", ""},
		{"simple ascii", "hello world"},
		{"unicode content", "héllo, 世界 🎉"},
		{"long string", strings.Repeat("threeport-", 200)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// act: encrypt then decrypt
			ct, err := Encrypt(key, tc.plaintext)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}

			// assert: ciphertext is not the plaintext
			if ct == tc.plaintext && tc.plaintext != "" {
				t.Fatal("ciphertext equals plaintext")
			}

			pt, err := Decrypt(key, ct)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}

			// assert: plaintext round-trips
			if pt != tc.plaintext {
				t.Fatalf("roundtrip mismatch: got %q, want %q", pt, tc.plaintext)
			}
		})
	}
}

// TestEncryptProducesDistinctCiphertexts covers that GCM's random nonce yields
// distinct ciphertexts for the same plaintext under the same key.
func TestEncryptProducesDistinctCiphertexts(t *testing.T) {
	key := mustGenerateKey(t)

	// act: encrypt the same plaintext twice
	c1, err := Encrypt(key, "same-plaintext")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	c2, err := Encrypt(key, "same-plaintext")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// assert: nonce randomness makes the two ciphertexts distinct
	if c1 == c2 {
		t.Fatal("expected distinct ciphertexts across encryptions of same plaintext")
	}
}

// TestEncryptErrors covers Encrypt's error paths for invalid keys.
func TestEncryptErrors(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantSub string
	}{
		{
			name:    "key not base64",
			key:     "!!!not-base64!!!",
			wantSub: "failed to decode key",
		},
		{
			name:    "key wrong size for AES",
			key:     util.Base64Encode("too-short"),
			wantSub: "failed to create cipher",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// act
			_, err := Encrypt(tc.key, "plaintext")

			// assert: error wraps the expected stage
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q missing %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// TestDecryptErrors covers Decrypt's error paths: bad ciphertext base64, bad
// key, wrong-size key, short ciphertext, and tampered ciphertext.
func TestDecryptErrors(t *testing.T) {
	goodKey := mustGenerateKey(t)

	// prepare a valid ciphertext to derive tampering / short cases from
	validCT, err := Encrypt(goodKey, "hello")
	if err != nil {
		t.Fatalf("setup Encrypt: %v", err)
	}

	// craft a ciphertext shorter than the GCM nonce size
	shortCT := base64.StdEncoding.EncodeToString([]byte("tiny"))

	tests := []struct {
		name       string
		key        string
		ciphertext string
		wantSub    string
	}{
		{
			name:       "ciphertext not base64",
			key:        goodKey,
			ciphertext: "!!!not-base64!!!",
			wantSub:    "failed to decode ciphertext",
		},
		{
			name:       "key not base64",
			key:        "!!!not-base64!!!",
			ciphertext: validCT,
			wantSub:    "failed to decode key",
		},
		{
			name:       "key wrong size for AES",
			key:        util.Base64Encode("too-short"),
			ciphertext: validCT,
			wantSub:    "failed to create cipher",
		},
		{
			name:       "ciphertext shorter than nonce",
			key:        goodKey,
			ciphertext: shortCT,
			wantSub:    "ciphertext too short",
		},
		{
			name:       "wrong key fails GCM open",
			key:        mustGenerateKey(t),
			ciphertext: validCT,
			wantSub:    "failed to decrypt",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// act
			_, err := Decrypt(tc.key, tc.ciphertext)

			// assert: error wraps the expected stage
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q missing %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// TestDecryptEnvSlice covers the happy path, malformed entries, and per-entry
// decrypt failures.
func TestDecryptEnvSlice(t *testing.T) {
	key := mustGenerateKey(t)

	// arrange: build KEY=<ciphertext> entries
	ctFoo, err := Encrypt(key, "bar")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	ctPing, err := Encrypt(key, "pong")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	t.Run("decrypts each entry", func(t *testing.T) {
		// act: decrypt a two-entry slice
		got, err := DecryptEnvSlice([]string{"FOO=" + ctFoo, "PING=" + ctPing}, key)

		// assert: values round-trip while keys stay plaintext
		if err != nil {
			t.Fatalf("DecryptEnvSlice: %v", err)
		}
		want := []string{"FOO=bar", "PING=pong"}
		if len(got) != len(want) {
			t.Fatalf("length mismatch: got %d, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("entry %d: got %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("empty input yields empty slice", func(t *testing.T) {
		// act: empty slice should produce empty output without error
		got, err := DecryptEnvSlice([]string{}, key)

		// assert: non-nil empty result
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty result, got %v", got)
		}
	})

	t.Run("entry missing equals errors", func(t *testing.T) {
		// act: an entry without '=' violates the KEY=VALUE contract
		_, err := DecryptEnvSlice([]string{"no-equals-sign"}, key)

		// assert: error mentions format expectation
		if err == nil {
			t.Fatal("expected error for missing '='")
		}
		if !strings.Contains(err.Error(), "KEY=VALUE") {
			t.Fatalf("error %q should mention KEY=VALUE format", err.Error())
		}
	})

	t.Run("decrypt failure propagates entry index", func(t *testing.T) {
		// act: second entry has an undecryptable value
		_, err := DecryptEnvSlice([]string{"FOO=" + ctFoo, "BAD=not-valid-b64!!"}, key)

		// assert: error names entry index and wraps underlying decrypt failure
		if err == nil {
			t.Fatal("expected error for undecryptable entry")
		}
		if !strings.Contains(err.Error(), "entry 1") {
			t.Fatalf("error %q should reference entry 1", err.Error())
		}
	})
}

// TestIsEncrypted covers that IsEncrypted returns true only when the value
// decrypts cleanly under the given key.
func TestIsEncrypted(t *testing.T) {
	key := mustGenerateKey(t)
	otherKey := mustGenerateKey(t)

	// prepare a known-encrypted value
	ct, err := Encrypt(key, "secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	tests := []struct {
		name  string
		key   string
		value string
		want  bool
	}{
		{"real ciphertext returns true", key, ct, true},
		{"plaintext returns false", key, "plaintext-value", false},
		{"empty value returns false", key, "", false},
		{"wrong key returns false", otherKey, ct, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// act + assert
			if got := IsEncrypted(tc.key, tc.value); got != tc.want {
				t.Fatalf("IsEncrypted = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEncryptStringMap covers happy-path encryption of each map entry and the
// error path when the key is invalid.
func TestEncryptStringMap(t *testing.T) {
	key := mustGenerateKey(t)

	t.Run("encrypts every value", func(t *testing.T) {
		// arrange: two-entry map with distinct plaintexts
		in := map[string]string{"a": "one", "b": "two"}

		// act
		got, err := EncryptStringMap(key, in)
		if err != nil {
			t.Fatalf("EncryptStringMap: %v", err)
		}

		// assert: shape preserved
		if len(got) != len(in) {
			t.Fatalf("length mismatch: got %d, want %d", len(got), len(in))
		}
		// assert: each value differs from its plaintext and round-trips via Decrypt
		for k, plain := range in {
			ct, ok := got[k]
			if !ok {
				t.Fatalf("key %q missing from result", k)
			}
			if ct == plain {
				t.Fatalf("key %q: ciphertext equals plaintext", k)
			}
			back, err := Decrypt(key, ct)
			if err != nil {
				t.Fatalf("Decrypt for key %q: %v", k, err)
			}
			if back != plain {
				t.Fatalf("key %q: decrypted %q, want %q", k, back, plain)
			}
		}
	})

	t.Run("empty map yields empty map", func(t *testing.T) {
		// act
		got, err := EncryptStringMap(key, map[string]string{})

		// assert
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty map, got %v", got)
		}
	})

	t.Run("errors on bad key", func(t *testing.T) {
		// act: invalid key surfaces error, input is returned untouched
		in := map[string]string{"a": "one"}
		got, err := EncryptStringMap("!!!not-base64!!!", in)

		// assert
		if err == nil {
			t.Fatal("expected error for invalid key")
		}
		if len(got) != len(in) {
			t.Fatalf("on error, input map should be returned; got %v", got)
		}
	})
}

// TestDecryptStringMap covers happy-path decryption and error propagation.
func TestDecryptStringMap(t *testing.T) {
	key := mustGenerateKey(t)

	// arrange: seed an encrypted-values map
	plain := map[string]string{"a": "one", "b": "two"}
	enc, err := EncryptStringMap(key, plain)
	if err != nil {
		t.Fatalf("EncryptStringMap: %v", err)
	}

	t.Run("decrypts every value", func(t *testing.T) {
		// act
		got, err := DecryptStringMap(key, enc)

		// assert: exact match against the original plaintext map
		if err != nil {
			t.Fatalf("DecryptStringMap: %v", err)
		}
		if len(got) != len(plain) {
			t.Fatalf("length mismatch: got %d, want %d", len(got), len(plain))
		}
		for k, want := range plain {
			if got[k] != want {
				t.Fatalf("key %q: got %q, want %q", k, got[k], want)
			}
		}
	})

	t.Run("errors on undecryptable value", func(t *testing.T) {
		// arrange: mix a bad ciphertext into the map
		bad := map[string]string{"a": "not-valid-b64!!"}

		// act
		_, err := DecryptStringMap(key, bad)

		// assert
		if err == nil {
			t.Fatal("expected error for undecryptable value")
		}
	})

	t.Run("empty map yields empty map", func(t *testing.T) {
		// act
		got, err := DecryptStringMap(key, map[string]string{})

		// assert
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty map, got %v", got)
		}
	})
}
