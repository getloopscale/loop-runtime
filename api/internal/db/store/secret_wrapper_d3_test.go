// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"testing"
)

// TestSecret_D3_CrossTenantTransplantFails is the primary closure proof for
// sec-001 (Deep Review Sprint D D3). Pre-fix, encryptForStore used nil AAD
// so ciphertext written for Tenant A could be lifted out of the DB and
// dropped into a row labeled as Tenant B and decryptFromStore would happily
// return Tenant A's plaintext — wrong-tenant decryption with no failure
// signal. Post-fix, the tenantAAD binding makes the AES-GCM auth tag depend
// on the canonical "loopzero-secret-v1|tenant=<id>" label, so cross-tenant
// transplant fails the dual-decrypt window (AAD-tag mismatch on the v1
// path AND the nil-fallback path also fails because the tag was
// computed with AAD).
func TestSecret_D3_CrossTenantTransplantFails(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	s := &PostgresSecretStore{encryptionKey: key}

	plaintext := []byte("tenant-A-confidential-DB_PASSWORD=hunter2")

	// 1. Tenant A writes a secret.
	ctA := context.Background()
	encryptedA, err := s.encryptForStore(ctA, "tenant-A", plaintext)
	if err != nil {
		t.Fatalf("encryptForStore for tenant-A: %v", err)
	}

	// 2. Confirm same-tenant decrypt works (sanity).
	got, err := s.decryptFromStore(ctA, "tenant-A", encryptedA)
	if err != nil {
		t.Fatalf("decryptFromStore for tenant-A: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("tenant-A round-trip mismatch: got %q, want %q", got, plaintext)
	}

	// 3. CROSS-TENANT TRANSPLANT ATTACK: Tenant B presents Tenant A's
	//    ciphertext. Pre-D3 this returned "tenant-A-confidential-..." to
	//    Tenant B (security boundary broken). Post-D3 this MUST error
	//    because the AAD bound to the ciphertext is tenant=tenant-A but
	//    decrypt now passes tenant=tenant-B.
	ctB := context.Background()
	_, err = s.decryptFromStore(ctB, "tenant-B", encryptedA)
	if err == nil {
		t.Fatal("CRITICAL: cross-tenant ciphertext transplant succeeded — sec-001 NOT closed")
	}
}

// TestSecret_D3_DualDecryptWindowAcceptsLegacy verifies the backward-compat
// path: ciphertexts written BEFORE D3 (with nil AAD) must still be
// decryptable via the nil-fallback path so existing production data
// continues to be readable. This is what makes D3 non-breaking.
func TestSecret_D3_DualDecryptWindowAcceptsLegacy(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	s := &PostgresSecretStore{encryptionKey: key}

	plaintext := []byte("legacy-pre-d3-ciphertext")

	// Simulate pre-D3 ciphertext by calling encrypt directly with nil AAD.
	// This is what s.encrypt produced before Sprint D D3.
	legacyEncrypted, err := s.encrypt(plaintext, nil)
	if err != nil {
		t.Fatalf("legacy encrypt with nil AAD: %v", err)
	}

	// decryptFromStore now uses tenantAAD by default — the first gcm.Open
	// will fail because the legacy blob was sealed with nil AAD, NOT
	// tenantAAD("tenant-X"). The fallback path inside decrypt() should
	// retry with nil AAD and succeed.
	got, err := s.decryptFromStore(context.Background(), "tenant-X", legacyEncrypted)
	if err != nil {
		t.Fatalf("dual-decrypt fallback for legacy ciphertext: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("legacy decrypt mismatch: got %q, want %q", got, plaintext)
	}
}

// TestSecret_D3_AADFormatIsStable freezes the canonical AAD bytes so a
// future commit that changes the format silently breaks dual-decrypt
// rather than passing CI quietly. The format is part of the data-at-rest
// contract until D3 Phase 2 drops the nil-fallback.
func TestSecret_D3_AADFormatIsStable(t *testing.T) {
	const tenantID = "tn-acme-12345"
	got := tenantAAD(tenantID)
	want := "loopzero-secret-v1|tenant=" + tenantID
	if string(got) != want {
		t.Errorf("AAD format drifted: got %q, want %q", got, want)
	}
}

// TestSecret_D3_NewWritesAreAADBound verifies that decrypt(_, nil) on a
// post-D3 ciphertext fails because the tag was computed with AAD. This
// is the inverse of TestSecret_D3_DualDecryptWindowAcceptsLegacy and
// proves the binding is actually enforced (not just decorative).
func TestSecret_D3_NewWritesAreAADBound(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	s := &PostgresSecretStore{encryptionKey: key}

	plaintext := []byte("post-d3-ciphertext")
	const tenantID = "tenant-D3"

	// Write with the new AAD-bound path.
	encrypted, err := s.encryptForStore(context.Background(), tenantID, plaintext)
	if err != nil {
		t.Fatalf("encryptForStore: %v", err)
	}

	// Direct call to decrypt with nil AAD MUST fail (the tag binding is
	// real). decrypt() itself tries aad first then nil fallback — when
	// we pass nil explicitly the first attempt is also nil, then the
	// fallback retries with nil, both fail → error returned.
	_, err = s.decrypt(encrypted, nil)
	if err == nil {
		t.Fatal("post-D3 ciphertext decrypted with nil AAD — binding is NOT enforced")
	}
}
