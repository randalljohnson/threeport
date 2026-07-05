//go:build integration

package main

import (
	"fmt"
	"testing"
	"time"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	client "github.com/threeport/threeport/pkg/client/v0"
	encryption "github.com/threeport/threeport/pkg/encryption/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestEncryptDecryptRoundTripThroughAPI writes an encrypted field on a
// standalone KubernetesRuntimeInstance and verifies the API server encrypted
// it on the write path by decrypting the read-back ciphertext with the
// shared key.
func TestEncryptDecryptRoundTripThroughAPI(t *testing.T) {
	requireKube(t)
	apiAddr, apiClient := getAPIServerURL(t)
	key := getEncryptionKey(t)

	// setup: create a runtime definition + instance the encrypted-field
	// test can safely mutate without racing the runtime controller
	defName := fmt.Sprintf("encrypt-integration-def-%d", time.Now().UnixNano())
	instName := fmt.Sprintf("encrypt-integration-inst-%d", time.Now().UnixNano())
	def, err := client.CreateKubernetesRuntimeDefinition(apiClient, apiAddr, &v0.KubernetesRuntimeDefinition{
		Definition:    v0.Definition{Name: util.Ptr(defName)},
		InfraProvider: util.Ptr("kind"),
	})
	if err != nil {
		t.Fatalf("failed to create runtime definition: %v", err)
	}
	defer func() { _, _ = client.DeleteKubernetesRuntimeDefinition(apiClient, apiAddr, *def.ID) }()

	inst, err := client.CreateKubernetesRuntimeInstance(apiClient, apiAddr, &v0.KubernetesRuntimeInstance{
		Instance:                      v0.Instance{Name: util.Ptr(instName)},
		Reconciliation:                v0.Reconciliation{Reconciled: util.Ptr(true)},
		Location:                      util.Ptr("Local"),
		KubernetesRuntimeDefinitionID: def.ID,
	})
	if err != nil {
		t.Fatalf("failed to create runtime instance: %v", err)
	}
	defer func() { _, _ = client.DeleteKubernetesRuntimeInstance(apiClient, apiAddr, *inst.ID) }()

	// action: write the encrypted field, read it back, and decrypt
	plaintext := "encrypt-integration-plaintext-a"
	_, err = client.UpdateKubernetesRuntimeInstance(apiClient, apiAddr, &v0.KubernetesRuntimeInstance{
		Common:          v0.Common{ID: inst.ID},
		ConnectionToken: util.Ptr(plaintext),
	})
	if err != nil {
		t.Fatalf("failed to set encrypted field: %v", err)
	}

	readBack, err := client.GetKubernetesRuntimeInstanceByID(apiClient, apiAddr, *inst.ID)
	if err != nil {
		t.Fatalf("failed to read back runtime instance: %v", err)
	}
	if readBack.ConnectionToken == nil {
		t.Fatal("ConnectionToken should be non-nil on read after set")
	}

	// assert: decrypting the ciphertext with the shared key returns the
	// exact plaintext we wrote
	got, err := encryption.Decrypt(key, *readBack.ConnectionToken)
	if err != nil {
		t.Fatalf("failed to decrypt ConnectionToken: %v", err)
	}
	if got != plaintext {
		t.Fatalf("decrypted mismatch: got %q, want %q", got, plaintext)
	}

	// assert: the wire form is not the plaintext, so the API server did
	// encrypt on write
	if *readBack.ConnectionToken == plaintext {
		t.Fatalf("stored value equals plaintext; API server did not encrypt on write")
	}
}

// TestEncryptDecryptRoundTripMultipleWrites re-encrypts the same field with a
// second value and confirms the new plaintext round-trips independently, so
// the encrypt path is not caching the first ciphertext.
func TestEncryptDecryptRoundTripMultipleWrites(t *testing.T) {
	requireKube(t)
	apiAddr, apiClient := getAPIServerURL(t)
	key := getEncryptionKey(t)

	defName := fmt.Sprintf("encrypt-integration-multi-def-%d", time.Now().UnixNano())
	instName := fmt.Sprintf("encrypt-integration-multi-inst-%d", time.Now().UnixNano())
	def, err := client.CreateKubernetesRuntimeDefinition(apiClient, apiAddr, &v0.KubernetesRuntimeDefinition{
		Definition:    v0.Definition{Name: util.Ptr(defName)},
		InfraProvider: util.Ptr("kind"),
	})
	if err != nil {
		t.Fatalf("failed to create runtime definition: %v", err)
	}
	defer func() { _, _ = client.DeleteKubernetesRuntimeDefinition(apiClient, apiAddr, *def.ID) }()

	inst, err := client.CreateKubernetesRuntimeInstance(apiClient, apiAddr, &v0.KubernetesRuntimeInstance{
		Instance:                      v0.Instance{Name: util.Ptr(instName)},
		Reconciliation:                v0.Reconciliation{Reconciled: util.Ptr(true)},
		Location:                      util.Ptr("Local"),
		KubernetesRuntimeDefinitionID: def.ID,
	})
	if err != nil {
		t.Fatalf("failed to create runtime instance: %v", err)
	}
	defer func() { _, _ = client.DeleteKubernetesRuntimeInstance(apiClient, apiAddr, *inst.ID) }()

	// action: two consecutive writes with different plaintexts
	writes := []string{"encrypt-integration-plaintext-x", "encrypt-integration-plaintext-y"}
	for _, plaintext := range writes {
		_, err = client.UpdateKubernetesRuntimeInstance(apiClient, apiAddr, &v0.KubernetesRuntimeInstance{
			Common:          v0.Common{ID: inst.ID},
			ConnectionToken: util.Ptr(plaintext),
		})
		if err != nil {
			t.Fatalf("failed to set encrypted field to %q: %v", plaintext, err)
		}
		readBack, err := client.GetKubernetesRuntimeInstanceByID(apiClient, apiAddr, *inst.ID)
		if err != nil {
			t.Fatalf("failed to read back runtime instance: %v", err)
		}
		if readBack.ConnectionToken == nil {
			t.Fatalf("ConnectionToken nil after writing %q", plaintext)
		}
		// assert: each write's ciphertext round-trips to its own plaintext
		got, err := encryption.Decrypt(key, *readBack.ConnectionToken)
		if err != nil {
			t.Fatalf("failed to decrypt after writing %q: %v", plaintext, err)
		}
		if got != plaintext {
			t.Fatalf("decrypted mismatch: got %q, want %q", got, plaintext)
		}
	}
}

// TestEncryptDecryptStandaloneKeyIsSymmetric verifies the encryption package
// itself is symmetric under the configured key: encrypt then decrypt returns
// the original input. Guards against key-format mismatches surfacing only in
// full round-trips.
func TestEncryptDecryptStandaloneKeyIsSymmetric(t *testing.T) {
	// skips are the same as the other encrypt tests: no config, no key
	key := getEncryptionKey(t)

	// action: encrypt then decrypt every string in the sample set
	samples := []string{"", "short", "a longer sample with spaces and punctuation!"}
	for _, s := range samples {
		ct, err := encryption.Encrypt(key, s)
		if err != nil {
			t.Fatalf("Encrypt(%q) failed: %v", s, err)
		}
		got, err := encryption.Decrypt(key, ct)
		if err != nil {
			t.Fatalf("Decrypt(%q) failed: %v", s, err)
		}
		if got != s {
			t.Fatalf("Encrypt/Decrypt round-trip mismatch: got %q, want %q", got, s)
		}
	}
}
