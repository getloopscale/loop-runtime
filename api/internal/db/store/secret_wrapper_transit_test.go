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

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
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

func (m *mockVault) Encrypt(_ context.Context, tenantID string, plaintext []byte) (string, error) {
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

func (m *mockVault) Decrypt(_ context.Context, tenantID string, ciphertext string) ([]byte, error) {
	m.decryptCalls++
	m.lastTenantID = tenantID
	if m.decryptError != nil {
		return nil, m.decryptError
	}
	// Reverse the prefix encoding from Encrypt
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

// T10.5: the AES write fallback this used to assert was deleted by T10.3.
// Its replacement is
// TestEncryptForStore_ProducesOnlyTransitCiphertextOrRefuses in
// secret_wrapper_write_path_test.go, which asserts the refusal that
// took its place — including the crypto_mode=plaintext case this test
// never covered, where the write path returned the secret unchanged.

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

// A user value is data, never a format marker. A value that happens to start
// with "vault:v1:" must not bypass the write refusal on a store with no transit
// client: that would persist the user's bytes unencrypted on a deployment that
// is supposed to refuse every write.
func TestEncryptForStore_RefusesCiphertextLookingValueWhenNoVault(t *testing.T) {
	key := make([]byte, 32)
	s, err := NewPostgresSecretStore(nil, key)
	if err != nil {
		t.Fatalf("NewPostgresSecretStore: %v", err)
	}

	_, err = s.encryptForStore(context.Background(), "tenant-A", []byte("vault:v1:user-typed-this"))
	if !errors.Is(err, loopzeroerrors.ErrSecretsBackendUnavailable) {
		t.Fatalf("a store without transit must refuse every write; got %v", err)
	}
}

// With transit wired, a value that looks like ciphertext is still the user's
// plaintext and is encrypted like any other — reading it back returns exactly
// what was written.
func TestEncryptForStore_EncryptsCiphertextLookingValueWithVault(t *testing.T) {
	mv := &mockVault{}
	s := &PostgresSecretStore{vault: mv}

	value := []byte("vault:v1:user-typed-this")
	got, err := s.encryptForStore(context.Background(), "tenant-A", value)
	if err != nil {
		t.Fatalf("encryptForStore: %v", err)
	}
	if mv.encryptCalls != 1 {
		t.Fatalf("the value must go through transit; encrypt calls = %d", mv.encryptCalls)
	}
	back, err := s.decryptFromStore(context.Background(), "tenant-A", got)
	if err != nil {
		t.Fatalf("decryptFromStore: %v", err)
	}
	if string(back) != string(value) {
		t.Errorf("round trip: got %q, want %q", back, value)
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
	encrypted := sealLegacyAES(t, key, []byte("legacydata"), nil)
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

// A transit row read by a store with no transit client cannot be opened. It
// must fail as an unavailable backend, not be fed to the AES path as if it were
// a legacy blob.
func TestDecryptFromStore_TransitRowWithoutVaultIsBackendUnavailable(t *testing.T) {
	key := make([]byte, 32)
	s, err := NewPostgresSecretStore(nil, key)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.decryptFromStore(context.Background(), "tenant-A", []byte("vault:v1:would-be-vault"))
	if !errors.Is(err, loopzeroerrors.ErrSecretsBackendUnavailable) {
		t.Fatalf("want ErrSecretsBackendUnavailable, got %v", err)
	}
}

// With neither a transit client nor a legacy AES key, a stored row has no key
// that can open it. Returning the stored bytes as the "plaintext" would hand
// ciphertext to the resync path as the secret value; it must fail instead.
func TestDecryptFromStore_NoKeyAndNoVaultIsBackendUnavailable(t *testing.T) {
	s := &PostgresSecretStore{}
	got, err := s.decryptFromStore(context.Background(), "tenant-A", []byte("stored-bytes"))
	if !errors.Is(err, loopzeroerrors.ErrSecretsBackendUnavailable) {
		t.Fatalf("want ErrSecretsBackendUnavailable, got value %q err %v", got, err)
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
