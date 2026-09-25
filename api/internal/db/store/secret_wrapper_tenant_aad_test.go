// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"testing"
)

// TestSecretTenantAAD_CrossTenantTransplantFails is the primary closure proof for
// sec-001 (Deep Review Sprint D D3). Pre-fix, encryptForStore used nil AAD
// so ciphertext written for Tenant A could be lifted out of the DB and
// dropped into a row labeled as Tenant B and decryptFromStore would happily
// return Tenant A's plaintext — wrong-tenant decryption with no failure
// signal. Post-fix, the tenantAAD binding makes the AES-GCM auth tag depend
// on the canonical "loopzero-secret-v1|tenant=<id>" label, so cross-tenant
// transplant fails the dual-decrypt window (AAD-tag mismatch on the v1
// path AND the nil-fallback path also fails because the tag was
// computed with AAD).
func TestSecretTenantAAD_CrossTenantTransplantFails(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	s := &PostgresSecretStore{encryptionKey: key}

	plaintext := []byte("tenant-A-confidential-DB_PASSWORD=hunter2")

	// 1. A row Tenant A wrote while the AES write path still existed.
	//    T10.3 deleted that path, so the fixture seals the blob; the
	//    attack this test describes is against a row already on disk,
	//    which is exactly the population the AES read branch still serves.
	ctA := context.Background()
	encryptedA := sealLegacyAES(t, key, plaintext, tenantAAD("tenant-A"))

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

// TestSecretTenantAAD_DualDecryptWindowAcceptsLegacy verifies the backward-compat
// path: ciphertexts written BEFORE D3 (with nil AAD) must still be
// decryptable via the nil-fallback path so existing production data
// continues to be readable. This is what makes D3 non-breaking.
func TestSecretTenantAAD_DualDecryptWindowAcceptsLegacy(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	s := &PostgresSecretStore{encryptionKey: key}

	plaintext := []byte("legacy-pre-d3-ciphertext")

	// Simulate pre-D3 ciphertext: sealed with nil AAD, which is what the
	// store's own encrypt() produced before Sprint D D3. T10.3 deleted
	// that seal from production code, so the fixture builds the blob
	// itself — a legacy row is precisely a shape nothing writes any more.
	legacyEncrypted := sealLegacyAES(t, key, plaintext, nil)

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

// TestSecretTenantAAD_AADFormatIsStable freezes the canonical AAD bytes so a
// future commit that changes the format silently breaks dual-decrypt
// rather than passing CI quietly. The format is part of the data-at-rest
// contract until D3 Phase 2 drops the nil-fallback.
func TestSecretTenantAAD_AADFormatIsStable(t *testing.T) {
	const tenantID = "tn-acme-12345"
	got := tenantAAD(tenantID)
	want := "loopzero-secret-v1|tenant=" + tenantID
	if string(got) != want {
		t.Errorf("AAD format drifted: got %q, want %q", got, want)
	}
}

// TestSecretTenantAAD_AADBoundRowsRejectTheNilAAD verifies that decrypt(_, nil)
// on an AAD-bound ciphertext fails because the tag was computed with AAD.
// This is the inverse of TestSecretTenantAAD_DualDecryptWindowAcceptsLegacy and
// proves the binding is actually enforced (not just decorative).
//
// Renamed from TestSecretTenantAAD_NewWritesAreAADBound by T10.3: there are no
// new AES writes any more. What the assertion covers is unchanged — the
// post-D3 rows already on disk — and the guarantee about new writes now
// lives in TestEncryptForStore_ProducesOnlyTransitCiphertextOrRefuses.
func TestSecretTenantAAD_AADBoundRowsRejectTheNilAAD(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	s := &PostgresSecretStore{encryptionKey: key}

	plaintext := []byte("post-d3-ciphertext")
	const tenantID = "tenant-D3"

	encrypted := sealLegacyAES(t, key, plaintext, tenantAAD(tenantID))

	// Direct call to decrypt with nil AAD MUST fail (the tag binding is
	// real). decrypt() itself tries aad first then nil fallback — when
	// we pass nil explicitly the first attempt is also nil, then the
	// fallback retries with nil, both fail → error returned.
	_, err := s.decrypt(encrypted, nil)
	if err == nil {
		t.Fatal("post-D3 ciphertext decrypted with nil AAD — binding is NOT enforced")
	}
}
