// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// secret_wrapper_transit_test.go — T3.4/T3.5 of openbao-adoption v1.3.
//
// Pure unit tests for the encryptForStore / decryptFromStore dispatchers.
// No PG dependency: tests the routing logic between Vault Transit and AES
// legacy in isolation. PG-backed tests live in secret_wrapper_test.go.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// mockVault implements VaultTransitClient for unit tests.
type mockVault struct {
	encryptCalls    int
	decryptCalls    int
	lastTenantID    string
	lastPlaintext   []byte
	encryptError    error
	decryptError    error
	encryptedPrefix string // override "vault:v1:" with custom prefix for negative tests
}

func (m *mockVault) EncryptForTenant(_ context.Context, tenantID string, plaintext []byte) (string, error) {
	m.encryptCalls++
	m.lastTenantID = tenantID
	m.lastPlaintext = plaintext
	if m.encryptError != nil {
		return "", m.encryptError
	}
	prefix := m.encryptedPrefix
	if prefix == "" {
		prefix = "vault:v1:"
	}
	return prefix + "ct(" + string(plaintext) + ")", nil
}

func (m *mockVault) DecryptForTenant(_ context.Context, tenantID string, ciphertext string) ([]byte, error) {
	m.decryptCalls++
	m.lastTenantID = tenantID
	if m.decryptError != nil {
		return nil, m.decryptError
	}
	// Reverse the prefix encoding from EncryptForTenant
	prefix := m.encryptedPrefix
	if prefix == "" {
		prefix = "vault:v1:"
	}
	if !strings.HasPrefix(ciphertext, prefix+"ct(") {
		return nil, fmt.Errorf("mock: bad ciphertext format: %q", ciphertext)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(ciphertext, prefix+"ct("), ")")
	return []byte(inner), nil
}

func TestEncryptForStore_RoutesToVaultWhenSet(t *testing.T) {
	mv := &mockVault{}
	s := &PostgresSecretStore{vault: mv}

	got, err := s.encryptForStore(context.Background(), "tenant-A", []byte("plain"))
	if err != nil {
		t.Fatal(err)
	}
	if mv.encryptCalls != 1 {
		t.Errorf("expected 1 vault encrypt call, got %d", mv.encryptCalls)
	}
	if mv.lastTenantID != "tenant-A" {
		t.Errorf("expected tenant-A, got %s", mv.lastTenantID)
	}
	if !strings.HasPrefix(string(got), "vault:v1:") {
		t.Errorf("expected vault prefix, got %q", got)
	}
}

func TestEncryptForStore_FallsBackToAESWhenNoVault(t *testing.T) {
	// 32 bytes for AES-256
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	s, err := NewPostgresSecretStore(nil, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.encryptForStore(context.Background(), "tenant-A", []byte("plain"))
	if err != nil {
		t.Fatal(err)
	}
	// AES path: should NOT start with vault prefix
	if strings.HasPrefix(string(got), "vault:v1:") {
		t.Errorf("expected AES path (no vault prefix), got %q", got)
	}
	// AES output should be different from plaintext (encrypted)
	if string(got) == "plain" {
		t.Errorf("expected encrypted output, got plaintext")
	}
}

func TestEncryptForStore_PropagatesVaultError(t *testing.T) {
	mv := &mockVault{encryptError: errors.New("vault down")}
	s := &PostgresSecretStore{vault: mv}
	_, err := s.encryptForStore(context.Background(), "tenant-A", []byte("plain"))
	if err == nil {
		t.Error("expected error from vault, got nil")
	}
	if !strings.Contains(err.Error(), "vault down") {
		t.Errorf("expected error to wrap 'vault down', got %v", err)
	}
}

// TestEncryptForStore_PassthroughWhenVaultPrefixPresent — T2.5 of plan
// 2026-06-10-j22-openbao-transit-eso-sync v1.2. The Bulk handler
// pre-encrypts via h.transitClient.Encrypt (T2.3 contract); when the
// store ALSO has s.vault wired (canonical production), encrypting again
// here would produce a vault:v1: envelope around an already-vault:v1:
// payload — DOUBLE ENCRYPT — and reads would unwrap only the outer
// layer, surfacing the inner string as "plaintext". The pass-through
// defense detects the vault:v1: prefix on input and returns the bytes
// unchanged, so the row carries canonical single-encrypt ciphertext.
//
// Sub-assertions:
//   - returned bytes are byte-identical to input (no transformation)
//   - mockVault.encryptCalls did NOT increment (no second-trip to OpenBao)
func TestEncryptForStore_PassthroughWhenVaultPrefixPresent(t *testing.T) {
	mv := &mockVault{}
	s := &PostgresSecretStore{vault: mv}

	// Simulate the Bulk-handler-encrypted payload that Set receives:
	// the canonical OpenBao Transit shape, "vault:v1:" + opaque body.
	preEncrypted := []byte("vault:v1:Hk3KcRTjpUz7n8xQ-already-encrypted")
	got, err := s.encryptForStore(context.Background(), "tenant-A", preEncrypted)
	if err != nil {
		t.Fatalf("encryptForStore: %v", err)
	}
	if string(got) != string(preEncrypted) {
		t.Errorf("encryptForStore mutated pre-encrypted input: want %q, got %q", preEncrypted, got)
	}
	if mv.encryptCalls != 0 {
		t.Errorf("encryptForStore double-encrypted: vault was called %d times, want 0", mv.encryptCalls)
	}
}

// TestEncryptForStore_PassthroughWhenNoVaultButPrefixPresent — defense
// in depth: even when s.vault is nil, an input that already carries the
// vault:v1: prefix MUST NOT be AES-encrypted on top. Such a payload
// belongs to a previous canonical write (e.g., re-import workflow); the
// store stays neutral and the read-path dispatcher handles decryption.
func TestEncryptForStore_PassthroughWhenNoVaultButPrefixPresent(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	s, err := NewPostgresSecretStore(nil, key)
	if err != nil {
		t.Fatalf("NewPostgresSecretStore: %v", err)
	}

	preEncrypted := []byte("vault:v1:Hk3KcRTjpUz7n8xQ-already-encrypted")
	got, err := s.encryptForStore(context.Background(), "tenant-A", preEncrypted)
	if err != nil {
		t.Fatalf("encryptForStore: %v", err)
	}
	if string(got) != string(preEncrypted) {
		t.Errorf("encryptForStore AES-wrapped vault:v1: input: want %q, got %q", preEncrypted, got)
	}
}

func TestDecryptFromStore_DetectsVaultPrefix(t *testing.T) {
	mv := &mockVault{}
	s := &PostgresSecretStore{vault: mv}

	// Vault prefix → route to Transit decrypt
	ciphertext := []byte("vault:v1:ct(myplain)")
	got, err := s.decryptFromStore(context.Background(), "tenant-A", ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if mv.decryptCalls != 1 {
		t.Errorf("expected 1 vault decrypt call, got %d", mv.decryptCalls)
	}
	if string(got) != "myplain" {
		t.Errorf("expected myplain, got %s", got)
	}
}

func TestDecryptFromStore_FallsBackToAESForLegacyCiphertext(t *testing.T) {
	// Setup: encrypt with AES, then decrypt with dispatcher (which detects
	// non-vault prefix and routes to AES).
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	mv := &mockVault{}
	s := &PostgresSecretStore{vault: mv, encryptionKey: key}

	// Round-trip via AES with NIL AAD — simulates legacy ciphertext written
	// before Sprint D D3 (sec-001 closure) added tenant-bound AAD. The
	// dual-decrypt window in decryptFromStore should still recover it via
	// the nil-fallback path (try with tenantAAD first, fall back to nil).
	encrypted, err := s.encrypt([]byte("legacydata"), nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.decryptFromStore(context.Background(), "tenant-A", encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "legacydata" {
		t.Errorf("expected legacydata, got %s", got)
	}
	// Vault decrypt should NOT have been called (no prefix match)
	if mv.decryptCalls != 0 {
		t.Errorf("expected 0 vault decrypt calls for AES path, got %d", mv.decryptCalls)
	}
}

func TestDecryptFromStore_EmptyCiphertextEC06(t *testing.T) {
	// EC-06 MUST FIX P2: empty/nil ciphertext returns error, NEVER panics.
	s := &PostgresSecretStore{}
	for _, ct := range [][]byte{nil, {}} {
		_, err := s.decryptFromStore(context.Background(), "tenant-A", ct)
		if err == nil {
			t.Errorf("expected EC-06 error for empty ciphertext, got nil")
		}
		if !strings.Contains(err.Error(), "EC-06") {
			t.Errorf("expected EC-06 marker in error, got %v", err)
		}
	}
}

func TestDecryptFromStore_PropagatesVaultError(t *testing.T) {
	mv := &mockVault{decryptError: errors.New("transit key not found")}
	s := &PostgresSecretStore{vault: mv}
	_, err := s.decryptFromStore(context.Background(), "tenant-A", []byte("vault:v1:bad"))
	if err == nil {
		t.Error("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "transit key not found") {
		t.Errorf("expected wrapped error, got %v", err)
	}
}

func TestDecryptFromStore_NoVaultClientFallsBackToAES(t *testing.T) {
	// When vault == nil and ciphertext starts with vault prefix, still route
	// to AES (which will likely fail decrypt but not panic).
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	s, err := NewPostgresSecretStore(nil, key)
	if err != nil {
		t.Fatal(err)
	}
	// vault prefix bytes will look like junk to AES → error, not panic
	_, err = s.decryptFromStore(context.Background(), "tenant-A", []byte("vault:v1:would-be-vault"))
	if err == nil {
		t.Error("expected AES decrypt error for non-AES ciphertext, got nil")
	}
}

func TestBytesHasPrefix(t *testing.T) {
	cases := []struct {
		b, prefix []byte
		want      bool
	}{
		{[]byte("vault:v1:abc"), []byte("vault:v1:"), true},
		{[]byte("vault:v1"), []byte("vault:v1:"), false}, // shorter than prefix
		{[]byte("other:v1:abc"), []byte("vault:v1:"), false},
		{[]byte("vault:v1:"), []byte("vault:v1:"), true}, // exact match
		{[]byte{}, []byte("vault:v1:"), false},
		{[]byte("vault:v1:"), []byte{}, true}, // empty prefix matches anything
	}
	for i, c := range cases {
		got := bytesHasPrefix(c.b, c.prefix)
		if got != c.want {
			t.Errorf("case %d: bytesHasPrefix(%q, %q) = %v, want %v", i, c.b, c.prefix, got, c.want)
		}
	}
}

func TestNewPostgresSecretStoreWithVault_NilVault(t *testing.T) {
	_, err := NewPostgresSecretStoreWithVault(nil, nil, nil)
	if err == nil {
		t.Error("expected error for nil vault")
	}
}

func TestNewPostgresSecretStoreWithVault_BadLegacyKeyLength(t *testing.T) {
	mv := &mockVault{}
	_, err := NewPostgresSecretStoreWithVault(nil, mv, []byte("short"))
	if err == nil {
		t.Error("expected error for legacy key wrong length")
	}
}

func TestNewPostgresSecretStoreWithVault_ValidConfig(t *testing.T) {
	mv := &mockVault{}
	key := make([]byte, 32)
	s, err := NewPostgresSecretStoreWithVault(nil, mv, key)
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("expected store, got nil")
	}
	if s.vault != mv {
		t.Error("expected vault field set")
	}
	if len(s.encryptionKey) != 32 {
		t.Errorf("expected 32-byte legacy key, got %d", len(s.encryptionKey))
	}
}

func TestNewPostgresSecretStoreWithVault_NilLegacyKey(t *testing.T) {
	// Post-T10.2 (Day 60), legacy AES code can be nil.
	mv := &mockVault{}
	s, err := NewPostgresSecretStoreWithVault(nil, mv, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.encryptionKey != nil {
		t.Errorf("expected nil legacy key, got %d bytes", len(s.encryptionKey))
	}
}
