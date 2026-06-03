// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

// secret_wrapper.go — sqlc-backed implementation of
// PostgresSecretStore. T2.2 (Batch B #4) of plan
// docs/plans/2026-05-29-sqlc-migration-plan.md v1.3.
//
// PostgresSecretStore struct + NewPostgresSecretStore constructor
// live in this file (inline — no separate _types.go needed; the
// store is small and the AES-256-GCM encrypt() helper is tightly
// coupled to the wrapper methods).
//
// SEC-001 invariant: encryptionKey != nil → AES-256-GCM
// (nonce || ciphertext) before INSERT. nil key = plaintext (local
// dev only — never in production). ListSecretNames NEVER returns
// the value column.

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"

	"github.com/loopzero/loopzero/api/internal/db/generated"
	"github.com/loopzero/loopzero/api/internal/domain"
)

// VaultTransitClient is the minimal interface PostgresSecretStore requires
// from the Vault transit engine integration (T3.4/T3.5 of openbao-adoption v1.3).
// pkg/vault.CachedClient satisfies it via an adapter; tests can mock cleanly.
//
// EncryptForTenant calls transit/encrypt/tenant-<id> and returns the
// canonical "vault:v1:..." ciphertext (Vault returns string form on the wire).
// DecryptForTenant calls transit/decrypt/tenant-<id> and returns plaintext bytes.
type VaultTransitClient interface {
	EncryptForTenant(ctx context.Context, tenantID string, plaintext []byte) (string, error)
	DecryptForTenant(ctx context.Context, tenantID string, ciphertext string) ([]byte, error)
}

// vaultPrefix is the canonical OpenBao/Vault Transit ciphertext prefix.
// Bytes-on-the-wire start with "vault:v1:" regardless of key type.
// T3.4 detection uses this prefix to route reads to Transit decrypt vs AES.
var vaultPrefix = []byte("vault:v1:")

// PostgresSecretStore manages secrets in PostgreSQL.
//
// Dual-encryption mode (T3.4/T3.5 of openbao-adoption v1.3):
//   - WRITE: when vault != nil → Transit encrypt per-tenant (canonical post-T10).
//     When vault == nil → fall back to AES-256-GCM (legacy / dev).
//   - READ: detect prefix "vault:v1:" → Transit decrypt; otherwise → AES decrypt.
//     This supports coexistence during migration window (Day 0-60).
//
// SEC-001 invariant: encryptionKey != nil → AES-256-GCM (legacy path).
// Vault path is canonical post-F10 / T10.2 AES code deletion.
//
// EC-06 (MUST FIX P2): all read paths check `len(ciphertext) == 0` BEFORE
// prefix detection to avoid panic on NULL/empty PG rows (e.g., legacy data
// or partial-write race conditions).
type PostgresSecretStore struct {
	DB            DBTX
	encryptionKey []byte // 32-byte key for AES-256-GCM; nil disables encryption (read AES fallback only)

	// Vault transit client (T3.4/T3.5 — optional). When set, write path
	// encrypts via Transit. Read path always handles both Vault + AES.
	vault VaultTransitClient
}

// NewPostgresSecretStore creates a secret store with optional AES encryption.
// key must be exactly 32 bytes (AES-256) or nil to disable encryption.
// Use NewPostgresSecretStoreWithVault for FAANG dual-path (T3.5 canonical).
func NewPostgresSecretStore(db DBTX, key []byte) (*PostgresSecretStore, error) {
	if key != nil && len(key) != 32 {
		return nil, fmt.Errorf("secret_store: encryption key must be 32 bytes, got %d", len(key))
	}
	return &PostgresSecretStore{DB: db, encryptionKey: key}, nil
}

// NewPostgresSecretStoreWithVault creates a secret store with Vault transit
// as the canonical write path + dual-read (Vault prefix detect; AES fallback).
//
// vaultClient is REQUIRED (use NewPostgresSecretStore for AES-only mode).
// legacyAESKey is OPTIONAL legacy AES key for reading pre-T3.5 ciphertexts;
// post-T10.2 (Day 60 AES delete cron) this can be nil.
//
// T3.5 of openbao-adoption v1.3 (FAANG dual-path per user "Siga o que for
// mais FAANG" decision iter 27).
func NewPostgresSecretStoreWithVault(db DBTX, vaultClient VaultTransitClient, legacyAESKey []byte) (*PostgresSecretStore, error) {
	if vaultClient == nil {
		return nil, fmt.Errorf("secret_store: vault client required for Vault-canonical mode")
	}
	if legacyAESKey != nil && len(legacyAESKey) != 32 {
		return nil, fmt.Errorf("secret_store: legacy AES key must be 32 bytes, got %d", len(legacyAESKey))
	}
	return &PostgresSecretStore{
		DB:            db,
		encryptionKey: legacyAESKey,
		vault:         vaultClient,
	}, nil
}

// encrypt encrypts plaintext using AES-256-GCM. Returns nonce || ciphertext.
// SEC-001 invariant: only path that turns user-provided bytes into the
// blob persisted in secrets.encrypted_value.
func (s *PostgresSecretStore) encrypt(plaintext []byte) ([]byte, error) {
	if s.encryptionKey == nil {
		return plaintext, nil // no encryption (local dev)
	}
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("secret_store: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret_store: create GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("secret_store: generate nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt is the inverse of encrypt. Reads nonce || ciphertext as written
// by encrypt(), returns plaintext. V9 L20: required by ListForResync so
// the deploy hook can repopulate ExternalSecrets with the original values.
//
// SEC-001 invariant: only path that turns persisted bytes back into
// plaintext. Bypasses the encrypt() when encryptionKey is nil (matches
// the local-dev no-encryption mode).
func (s *PostgresSecretStore) decrypt(encrypted []byte) ([]byte, error) {
	if s.encryptionKey == nil {
		return encrypted, nil // matches encrypt() local-dev path
	}
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("secret_store: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret_store: create GCM: %w", err)
	}
	if len(encrypted) < gcm.NonceSize() {
		return nil, fmt.Errorf("secret_store: ciphertext too short (%d bytes)", len(encrypted))
	}
	nonce, ciphertext := encrypted[:gcm.NonceSize()], encrypted[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("secret_store: gcm open: %w", err)
	}
	return plain, nil
}

// encryptForStore is the write-path dispatcher (T3.5).
// When vault present → Transit encrypt per-tenant (canonical post-T10).
// Otherwise → legacy AES-256-GCM.
func (s *PostgresSecretStore) encryptForStore(ctx context.Context, tenantID string, plaintext []byte) ([]byte, error) {
	if s.vault != nil {
		ct, err := s.vault.EncryptForTenant(ctx, tenantID, plaintext)
		if err != nil {
			return nil, fmt.Errorf("secret_store: vault transit encrypt: %w", err)
		}
		return []byte(ct), nil
	}
	return s.encrypt(plaintext)
}

// decryptFromStore is the read-path dispatcher (T3.4).
// EC-06 MUST FIX P2: empty/nil ciphertext returns error (no AES panic).
// Prefix detection: "vault:v1:" → Transit decrypt; else → AES legacy.
func (s *PostgresSecretStore) decryptFromStore(ctx context.Context, tenantID string, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, fmt.Errorf("secret_store: empty ciphertext for tenant %s (EC-06 NULL row guard)", tenantID)
	}
	if s.vault != nil && bytesHasPrefix(ciphertext, vaultPrefix) {
		plain, err := s.vault.DecryptForTenant(ctx, tenantID, string(ciphertext))
		if err != nil {
			return nil, fmt.Errorf("secret_store: vault transit decrypt: %w", err)
		}
		return plain, nil
	}
	return s.decrypt(ciphertext)
}

// bytesHasPrefix returns true if b starts with prefix. Inlined to avoid
// importing "bytes" just for HasPrefix.
func bytesHasPrefix(b, prefix []byte) bool {
	if len(b) < len(prefix) {
		return false
	}
	for i := range prefix {
		if b[i] != prefix[i] {
			return false
		}
	}
	return true
}

// Set creates or updates a secret. Uses sqlc-generated UpsertSecret
// (ON CONFLICT DO UPDATE returning xmax=0 to distinguish INSERT
// from UPDATE).
//
// SEC-001: Values are encrypted via s.encrypt before persistence.
// Returns true if the secret was created, false if updated.
// Single atomic statement — no race condition between concurrent
// Set calls for the same secret (the legacy SELECT+INSERT race
// is structurally absent).
func (s *PostgresSecretStore) Set(ctx context.Context, tenantID, projectID, env, app, name string, value []byte) (created bool, err error) {
	// SEC-001 + T3.5: Dispatch to Vault Transit OR AES legacy.
	// Vault path returns "vault:v1:..." prefixed bytes (per-tenant key).
	// AES path returns nonce || ciphertext (legacy).
	encrypted, err := s.encryptForStore(ctx, tenantID, value)
	if err != nil {
		return false, fmt.Errorf("secret_store: encrypt: %w", err)
	}

	bridge, err := s.bridge()
	if err != nil {
		return false, err
	}
	q := generated.New()

	// V9 L19 fix: include environment in the deterministic PK. The unique
	// constraint is (tenant_id, project_id, environment, app, name) — the
	// PK MUST be derivable from the SAME tuple, otherwise an UPSERT for a
	// row that differs only in environment from an existing one collides
	// on the PK before reaching the ON CONFLICT clause (which compares
	// the unique-constraint columns, not the PK).
	//
	// Pre-L19, two secrets with the same (tenant, project, app, name) but
	// different env (e.g. j-secrets vs production) shared the same PK
	// "sec-<tenant>-<project>-<app>-<name>" and the second INSERT failed
	// with SQLSTATE 23505 ("duplicate key violates secrets_pkey") —
	// returning HTTP 500 INTERNAL_ERROR even though the user's request
	// was valid.
	//
	// LIVE-discovered via v9L16 dogfood: Journey 03 sets secret on env
	// "j-secrets" for project "secret-app" while an earlier journey had
	// set a secret on env "production" for the same project. The PK
	// collision blocked the second INSERT before ON CONFLICT could fire.
	id := fmt.Sprintf("sec-%s-%s-%s-%s-%s", tenantID, projectID, env, app, name)

	created, err = q.UpsertSecret(ctx, bridge, generated.UpsertSecretParams{
		ID:             id,
		TenantID:       tenantID,
		ProjectID:      projectID,
		Environment:    env,
		App:            app,
		Name:           name,
		EncryptedValue: encrypted,
	})
	if err != nil {
		return false, fmt.Errorf("secret_store: set: %w", err)
	}
	return created, nil
}

// Delete removes a secret by name within tenant/project/env/app scope.
// Returns true if a secret was deleted, false if it didn't exist.
func (s *PostgresSecretStore) Delete(ctx context.Context, tenantID, projectID, env, app, name string) (deleted bool, err error) {
	bridge, err := s.bridge()
	if err != nil {
		return false, err
	}
	q := generated.New()
	n, err := q.DeleteSecret(ctx, bridge, generated.DeleteSecretParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
		App:         app,
		Name:        name,
	})
	if err != nil {
		return false, fmt.Errorf("secret_store: delete: %w", err)
	}
	return n > 0, nil
}

// SecretResyncEntry is a (name, app, plaintext-value) tuple returned by
// ListForResync. V9 L20: only consumed by the deploy-time resync hook
// in builds.go which (re)creates ExternalSecrets on the workload cluster
// after the namespace has been ensured. Wrapper decrypts via SEC-001
// AES-GCM before returning, so the caller gets plaintext values.
type SecretResyncEntry struct {
	Name  string
	App   string
	Value string
}

// ListForResync returns plaintext (name, app, value) for all secrets in
// an env tuple. V9 L20: used by the deploy handler post-EnsureEnvironment
// to repopulate ExternalSecrets on the workload cluster. This closes the
// "set secret BEFORE first deploy" race where the K8s sync at POST time
// fails (no namespace) and pods later fail to start with
// CreateContainerConfigError.
//
// SEC-001: values are decrypted in this function before return; callers
// MUST scope use to the resync path. Other read paths (CLI list, audit)
// MUST use ListNames which never returns the value column.
func (s *PostgresSecretStore) ListForResync(ctx context.Context, tenantID, projectID, env string) ([]SecretResyncEntry, error) {
	bridge, err := s.bridge()
	if err != nil {
		return nil, err
	}
	q := generated.New()
	rows, err := q.ListSecretsForResync(ctx, bridge, generated.ListSecretsForResyncParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		return nil, fmt.Errorf("secret_store: list for resync: %w", err)
	}
	out := make([]SecretResyncEntry, 0, len(rows))
	for _, row := range rows {
		// T3.4 dual-read: dispatcher detects "vault:v1:" prefix → Transit,
		// else AES legacy. EC-06 guard: empty ciphertext returns error (no panic).
		plain, derr := s.decryptFromStore(ctx, tenantID, row.EncryptedValue)
		if derr != nil {
			// Skip undecryptable rows (defense-in-depth: never panic in
			// a deploy hook). Surface as ERROR so operators see drift.
			return nil, fmt.Errorf("secret_store: decrypt %q: %w", row.Name, derr)
		}
		out = append(out, SecretResyncEntry{
			Name:  row.Name,
			App:   row.App,
			Value: string(plain),
		})
	}
	return out, nil
}

// ListNames returns metadata (name, environment, app) for all secrets
// matching the given tenant, project, and environment.
// Results are ordered alphabetically by name.
// SEC-001 defense-in-depth: never returns the value column.
func (s *PostgresSecretStore) ListNames(ctx context.Context, tenantID, projectID, env string) ([]domain.SecretMetadata, error) {
	bridge, err := s.bridge()
	if err != nil {
		return nil, err
	}
	q := generated.New()
	rows, err := q.ListSecretNames(ctx, bridge, generated.ListSecretNamesParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		return nil, fmt.Errorf("secret_store: list names: %w", err)
	}

	// Return empty slice instead of nil to avoid null in JSON responses.
	result := make([]domain.SecretMetadata, 0, len(rows))
	for _, row := range rows {
		result = append(result, domain.SecretMetadata{
			Name:        row.Name,
			Environment: row.Environment,
			App:         row.App,
		})
	}
	return result, nil
}

// bridge returns the sqlc-compatible DBTX from s.DB.
func (s *PostgresSecretStore) bridge() (generated.DBTX, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("secret_store: nil DB")
	}
	bridge, ok := s.DB.(SqlcBridge)
	if !ok {
		return nil, fmt.Errorf("secret_store: DBTX %T does not implement SqlcBridge", s.DB)
	}
	return bridge.GeneratedDB(), nil
}
