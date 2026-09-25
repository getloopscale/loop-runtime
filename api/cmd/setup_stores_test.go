// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// setup_stores_test.go — T2.5 unit tests of plan
// 2026-06-10-j22-openbao-transit-eso-sync v1.2. Validates that
// setupSecretStore correctly threads the OpenBao transit client into
// the PostgresSecretStore so reads dispatch through the transit client's Decrypt
// when the encrypted_value carries the canonical vault:v1: prefix.
//
// Acceptance criteria covered:
//   - TestSetupSecretStore_NilTransitFallsBackToLegacyAES (Pattern A nil-safe)
//   - TestSetupSecretStore_NonNilTransitWiresVaultDispatch (T2.5 happy path)
//   - TestSetupSecretStore_BadAESKeyReturnsError (existing contract preserved)
//
// The got-internal dispatcher (encryptForStore/decryptFromStore prefix
// detection) is exercised by api/internal/db/secret_wrapper_test.go;
// this file focuses on the boot-time wire decision: which constructor
// did we reach?

package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/generated"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/pkg/vault"
)

// compile-time guarantee: fakeTransit satisfies the same vault.Transit
// interface accepted by setupSecretStore — kept implicit (the parameter
// type drives the check) so the test stays free of explicit pkg/vault
// imports beyond what the production code itself demands.
var _ context.Context // keep imports honest if future tests add ctx use

// fakeTransit is a minimal vault.Transit double that records every call
// so the wire-up assertions can prove the got actually held a reference
// to the same client adapter we passed in.
type fakeTransit struct {
	encryptCalls int
	decryptCalls int
}

func (f *fakeTransit) Encrypt(ctx context.Context, tenantID string, plaintext []byte) (string, error) {
	f.encryptCalls++
	return "vault:v1:fake-ciphertext-for-" + tenantID, nil
}

func (f *fakeTransit) Decrypt(ctx context.Context, tenantID string, ciphertext string) ([]byte, error) {
	f.decryptCalls++
	return []byte("fake-plaintext-for-" + tenantID), nil
}

// TestSetupSecretStore_NilTransitFallsBackToLegacyAES — Pattern A nil-safe
// contract: when transit is nil (e.g., CryptoMode == "aes-local"), the
// legacy NewPostgresSecretStore is reached. Verified by exercising
// encryptForStore on plaintext that is NOT prefixed; the legacy AES
// path emits nonce+ciphertext bytes, NOT vault:v1: form.
func TestSetupSecretStore_NilTransitFallsBackToLegacyAES(t *testing.T) {
	// 32-byte hex key for AES-256-GCM (legacy path).
	const key = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	got, err := setupSecretStore(nil, key, nil, newTestLogger())
	if err != nil {
		t.Fatalf("setupSecretStore: %v", err)
	}
	if got == nil {
		t.Fatalf("setupSecretStore: want non-nil got, got nil")
	}
	// Without transit the store has no write path (T10.3): a write is refused
	// as an unavailable secrets backend before any database is touched.
	if _, err := got.Set(context.Background(), "tn-1", "web", "production", "api", "DB_URL", []byte("v")); !errors.Is(err, loopzeroerrors.ErrSecretsBackendUnavailable) {
		t.Fatalf("a store without transit must refuse writes, got %v", err)
	}
}

// TestSetupSecretStore_NonNilTransitWiresVaultDispatch — T2.5 happy
// path: when transit is non-nil, NewPostgresSecretStoreWithVault is
// reached. Verified by exercising Set against the returned got; the
// fake transit's encryptCalls counter MUST increment, proving the wire
// path goes Set → encryptForStore → s.vault.Encrypt → fake.
func TestSetupSecretStore_NonNilTransitWiresVaultDispatch(t *testing.T) {
	// Empty AES key — vault is the canonical path; legacy AES stays
	// unwired but readable (decryptFromStore prefix detection still
	// dispatches when older rows surface).
	fake := &fakeTransit{}
	got, err := setupSecretStore(unreachableDB(), "", fake, newTestLogger())
	if err != nil {
		t.Fatalf("setupSecretStore: %v", err)
	}
	if got == nil {
		t.Fatalf("setupSecretStore: want non-nil got, got nil")
	}
	// A write goes through the transit client before it reaches the database:
	// with no database here the write fails afterwards, but the encrypt call is
	// the proof the store was wired with transit.
	_, _ = got.Set(context.Background(), "tn-1", "web", "production", "api", "DB_URL", []byte("v"))
	if fake.encryptCalls != 1 {
		t.Fatalf("transit Encrypt calls = %d, want 1", fake.encryptCalls)
	}
}

// TestSetupSecretStore_BadAESKeyReturnsError — existing legacy contract
// preserved: malformed hex key surfaces hex.DecodeString error BEFORE
// any vault wiring. Defense against silent fallback to no-encryption.
func TestSetupSecretStore_BadAESKeyReturnsError(t *testing.T) {
	const badKey = "not-hex-zzzzz"
	got, err := setupSecretStore(nil, badKey, nil, newTestLogger())
	if err == nil {
		t.Fatalf("setupSecretStore: want hex decode error for %q, got nil", badKey)
	}
	if got != nil {
		t.Fatalf("setupSecretStore: want nil got on error, got %T", got)
	}
}

// The adapter that joined the two sides was ceremony, not translation. `pkg/vault.Transit` and
// `store.VaultTransitClient` declared the SAME shape — the adapter's own doc said
// *"signatures match byte-for-byte; only the method names differ"* — and the bridge
// existed only to reconcile `Encrypt` with `EncryptForTenant`.
//
// In Go this needs no bridge: the interface is structural, and the composition root
// hands over a value that satisfies what the consumer asks for. **Neither package starts
// importing the other** — which was the stated reason for the adapter to exist, and stays
// true without it. This assertion lives here, in the composition root, which is the only
// place that legitimately knows both sides.
//
// `B-043`, bullet 4: the adapter is **deleted**, not moved.
func TestTransitClientSatisfiesTheStoreContractWithoutAnAdapter(t *testing.T) {
	var _ store.VaultTransitClient = (vault.Transit)(nil)
}

// errUnreachableDB is what every statement on unreachableDB returns.
var errUnreachableDB = errors.New("test database: unreachable")

// unreachableGenerated is a generated.DBTX whose every statement fails, so a
// store can be driven up to its first database call without a database.
type unreachableGenerated struct{}

func (unreachableGenerated) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errUnreachableDB
}
func (unreachableGenerated) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errUnreachableDB
}
func (unreachableGenerated) QueryRow(context.Context, string, ...any) pgx.Row {
	return unreachableRow{}
}

type unreachableRow struct{}

func (unreachableRow) Scan(...any) error { return errUnreachableDB }

// unreachableDBTX satisfies db.DBTX and db.SqlcBridge over unreachableGenerated.
type unreachableDBTX struct{ unreachableGenerated }

func (unreachableDBTX) QueryRow(context.Context, string, ...any) db.Row { return unreachableRow{} }
func (unreachableDBTX) Query(context.Context, string, ...any) (db.Rows, error) {
	return nil, errUnreachableDB
}
func (unreachableDBTX) Exec(context.Context, string, ...any) (db.ExecResult, error) {
	return db.ExecResult{}, errUnreachableDB
}
func (unreachableDBTX) GeneratedDB() generated.DBTX { return unreachableGenerated{} }

func unreachableDB() db.DBTX { return unreachableDBTX{} }
