// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

// secret_wrapper_legacy_aes_fixture_test.go — the AES-256-GCM seal, as a test
// fixture only.
//
// T10.3 (B-162) deleted PostgresSecretStore.encrypt: production code has no way
// to write a secret in the legacy AES format any more. The read path stayed —
// rows written before T10.3 are only readable through it — and the tests that
// guard that read path need a legacy blob to hand it. They build one here.
//
// Keeping the seal in the test package instead of the store is the point, not a
// convenience: a legacy format is exactly one that nothing writes and something
// still reads, and the code layout now says that. The `no-aes-remaining` gate
// scans api/internal/db/store/secret_wrapper.go, which this file is not; what
// makes that honest is that no production caller can reach this function.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"io"
	"testing"
)

// sealLegacyAES produces nonce || ciphertext in the pre-T10.3 on-disk format:
// AES-256-GCM with the given AAD (pass nil to reproduce a pre-Sprint-D-D3 row,
// tenantAAD(tenantID) to reproduce a post-D3 one).
func sealLegacyAES(t *testing.T, key, plaintext, aad []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("legacy fixture: create cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("legacy fixture: create GCM: %v", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatalf("legacy fixture: generate nonce: %v", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, aad)
}
