// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// transit_dispatch_test.go — T2.2 of plan
// 2026-06-10-j22-openbao-transit-eso-sync v1.2. Locks the read-path
// dispatcher invariant: `decryptFromStore` MUST route ciphertext starting
// with "vault:v1:" to the wired VaultTransitClient and fall back to the
// legacy AES path otherwise. EC-06 (empty/nil ciphertext) absorbed.
//
// Pure unit tests — no PG connection required. The decrypt dispatcher
// operates entirely on bytes + the two wired backends; the DBTX field is
// unused on this path.

// fakeTransitT22 records every Decrypt call and supports
// per-tenant failure injection so the dispatcher's error-wrap behavior
// can be locked.
type fakeTransitT22 struct {
	mu           sync.Mutex
	decryptCalls []string // formatted as "tenant:ciphertext"
	encryptCalls []string
	failOnTenant map[string]error
	plaintext    []byte
}

func (f *fakeTransitT22) Encrypt(_ context.Context, tenantID string, plaintext []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.encryptCalls = append(f.encryptCalls, tenantID+":"+string(plaintext))
	return "vault:v1:fake-cipher-" + tenantID, nil
}

func (f *fakeTransitT22) Decrypt(_ context.Context, tenantID string, ciphertext string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decryptCalls = append(f.decryptCalls, tenantID+":"+ciphertext)
	if err, ok := f.failOnTenant[tenantID]; ok {
		return nil, err
	}
	if f.plaintext != nil {
		return f.plaintext, nil
	}
	return []byte("decrypted-by-fake-vault"), nil
}

// newStoreWithFakes constructs a PostgresSecretStore wired with a fake
// VaultTransitClient + a deterministic 32-byte AES key. Skips the DBTX
// (nil) because decryptFromStore does not touch it.
func newStoreWithFakes(t *testing.T, vault VaultTransitClient) *PostgresSecretStore {
	t.Helper()
	// AES-256 requires 32-byte key. Deterministic content so legacy-path
	// tests can construct matching ciphertexts via the store's encrypt.
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := NewPostgresSecretStoreWithVault(nil, vault, key)
	if err != nil {
		t.Fatalf("NewPostgresSecretStoreWithVault: %v", err)
	}
	return store
}

// TestSecretWrapper_RoutesByPrefix — plan T2.2 acceptance: vault:v1:-
// prefixed ciphertext routes to VaultTransitClient.DecryptForTenant; the
// fake records the call AND its plaintext is returned to the caller.
func TestSecretWrapper_RoutesByPrefix(t *testing.T) {
	vault := &fakeTransitT22{plaintext: []byte("postgres://x")}
	store := newStoreWithFakes(t, vault)

	plain, err := store.decryptFromStore(context.Background(), "tn-A", []byte("vault:v1:abc123"))
	if err != nil {
		t.Fatalf("decryptFromStore: %v", err)
	}
	if string(plain) != "postgres://x" {
		t.Errorf("plaintext: got %q want %q", string(plain), "postgres://x")
	}
	if len(vault.decryptCalls) != 1 {
		t.Errorf("vault.Decrypt calls: got %d want 1", len(vault.decryptCalls))
	}
	if vault.decryptCalls[0] != "tn-A:vault:v1:abc123" {
		t.Errorf("vault call: got %q want %q", vault.decryptCalls[0], "tn-A:vault:v1:abc123")
	}
}

// TestSecretWrapper_FallbackToAESWhenNoPrefix — legacy AES path: bytes
// that do NOT start with vault:v1: must NOT touch the vault client. They
// route to the existing aes.NewCipher path. We do not assert successful
// decrypt here (AES needs matching ciphertext + AAD); we only assert the
// vault was bypassed and the error chain came from the AES decoder.
func TestSecretWrapper_FallbackToAESWhenNoPrefix(t *testing.T) {
	vault := &fakeTransitT22{}
	store := newStoreWithFakes(t, vault)

	// Bytes that look like a real (but invalid) AES-GCM ciphertext.
	bogusAES := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	_, err := store.decryptFromStore(context.Background(), "tn-A", bogusAES)
	if err == nil {
		t.Errorf("expected AES decrypt failure on bogus bytes; got nil")
	}
	if len(vault.decryptCalls) != 0 {
		t.Errorf("vault MUST NOT be invoked on non-prefix bytes; got %d calls", len(vault.decryptCalls))
	}
}

// TestSecretWrapper_EmptyCiphertextRejected — EC-06 absorption: empty or
// nil ciphertext returns a typed error BEFORE either dispatch path fires.
// Defense against NULL row state (which should not exist post-migration
// but the guard prevents an AES panic if it ever does).
func TestSecretWrapper_EmptyCiphertextRejected(t *testing.T) {
	vault := &fakeTransitT22{}
	store := newStoreWithFakes(t, vault)

	_, err := store.decryptFromStore(context.Background(), "tn-A", []byte{})
	if err == nil {
		t.Fatalf("empty ciphertext should return error; got nil")
	}
	if !strings.Contains(err.Error(), "empty ciphertext") {
		t.Errorf("error should mention empty ciphertext; got %v", err)
	}
	if len(vault.decryptCalls) != 0 {
		t.Errorf("vault MUST NOT be invoked on empty ciphertext; got %d calls", len(vault.decryptCalls))
	}

	// Also nil bytes (defensive).
	_, err = store.decryptFromStore(context.Background(), "tn-A", nil)
	if err == nil {
		t.Errorf("nil ciphertext should return error; got nil")
	}
}

// TestSecretWrapper_VaultErrorIsWrapped — when the wired vault returns an
// error (e.g. ErrVaultUnreachable from upstream pkg/vault), the dispatcher
// wraps with "vault transit decrypt:" context so the caller can identify
// the failure layer. Unwrap chain MUST preserve errors.Is matching.
func TestSecretWrapper_VaultErrorIsWrapped(t *testing.T) {
	upstreamErr := errors.New("vault: unreachable (fake)")
	vault := &fakeTransitT22{failOnTenant: map[string]error{"tn-A": upstreamErr}}
	store := newStoreWithFakes(t, vault)

	_, err := store.decryptFromStore(context.Background(), "tn-A", []byte("vault:v1:abc"))
	if err == nil {
		t.Fatalf("expected vault error to propagate; got nil")
	}
	if !errors.Is(err, upstreamErr) {
		t.Errorf("errors.Is should match upstream; got %v", err)
	}
	if !strings.Contains(err.Error(), "vault transit decrypt") {
		t.Errorf("error should be wrapped with layer context; got %v", err)
	}
}

// TestSecretWrapper_VaultPrefixOnlyButVaultNil — defensive: when the
// store was constructed via NewPostgresSecretStore (vault nil) but
// somehow a vault:v1:-prefixed row exists in PG (e.g. mixed migration
// window), the dispatcher falls through to AES decrypt, which will fail
// with an AES error rather than a NPE on s.vault.DecryptForTenant.
//
// This case is unlikely in production (vault-prefixed rows imply a vault
// client wired upstream) but documents the safe fallback contract.
func TestSecretWrapper_VaultPrefixOnlyButVaultNil(t *testing.T) {
	// Construct with the legacy AES-only constructor.
	key := make([]byte, 32)
	store, err := NewPostgresSecretStore(nil, key)
	if err != nil {
		t.Fatalf("NewPostgresSecretStore: %v", err)
	}
	_, err = store.decryptFromStore(context.Background(), "tn-A", []byte("vault:v1:abc"))
	if err == nil {
		t.Errorf("expected AES decrypt failure (no vault wired); got nil")
	}
	// Should NOT panic and should NOT contain "vault transit" — the AES
	// path is what attempts (and fails) to decrypt this byte sequence.
	if strings.Contains(err.Error(), "vault transit") {
		t.Errorf("vault should not have been invoked; got %v", err)
	}
}
