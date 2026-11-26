// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

// secret_wrapper.go — sqlc-backed implementation of
// PostgresSecretStore. T2.2 (Batch B #4) of plan
// 2026-05-29-sqlc-migration-plan v1.3.
//
// PostgresSecretStore struct + NewPostgresSecretStore constructor
// live in this file (inline — no separate _types.go needed; the
// store is small and the legacy AES-256-GCM decrypt() helper is
// tightly coupled to the wrapper methods).
//
// SEC-001 invariant, post-T10.3: the ONLY blob this store writes to
// secrets.encrypted_value is Vault Transit ciphertext ("vault:v1:"
// prefix). A store without a Transit client refuses writes instead of
// falling back to a local AES key or to plaintext. The AES branch
// survives on the READ side only, so rows written before T10.3 stay
// readable. ListSecretNames NEVER returns the value column.

import (
	"errors"

	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"fmt"

	"github.com/jackc/pgx/v5"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"

	"github.com/getloopscale/loop-runtime/api/internal/db"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// VaultTransitClient is the minimal interface PostgresSecretStore requires from the
// Vault transit engine integration (T3.4/T3.5 of openbao-adoption v1.3). It is a
// CONSUMER-DEFINED interface: this package declares what it needs, and does not import
// whoever provides it.
//
// Encrypt returns the canonical "vault:v1:..." ciphertext (Vault returns string form on
// the wire); Decrypt takes one back and returns plaintext bytes. The per-tenant scope
// lives in the tenantID PARAMETER — it does not need to be in the method name too.
//
// The names used to be EncryptForTenant/DecryptForTenant, and an adapter in
// internal/adapters existed for the sole purpose of renaming them onto pkg/vault's
// Encrypt/Decrypt. Its own doc said the signatures matched "byte-for-byte; only the
// method names differ". Go interfaces are structural, so a difference that is only a
// name needs no bridge — the composition root passes the provider straight in, and
// **neither package imports the other**, which was the adapter's stated reason to exist.
// Deleted in 2026-08-16 (B-043, bullet 4). Asserted by
// api/cmd/setup_stores_test.go::TestTransitClientSatisfiesTheStoreContractWithoutAnAdapter.
type VaultTransitClient interface {
	Encrypt(ctx context.Context, tenantID string, plaintext []byte) (string, error)
	Decrypt(ctx context.Context, tenantID string, ciphertext string) ([]byte, error)
}

// vaultPrefix is the canonical OpenBao/Vault Transit ciphertext prefix.
// Bytes-on-the-wire start with "vault:v1:" regardless of key type.
// T3.4 detection uses this prefix to route reads to Transit decrypt vs AES.
var vaultPrefix = []byte("vault:v1:")

// PostgresSecretStore manages secrets in PostgreSQL.
//
// Asymmetric encryption mode (T3.4/T3.5 of openbao-adoption v1.3, as
// amended by T10.3):
//   - WRITE: vault != nil → Transit encrypt per-tenant. vault == nil →
//     refuse. There is no AES write branch: the fallback that used to
//     fire here wrote tenant secrets under a local key (crypto_mode=
//     aes-local) or in the clear (crypto_mode=plaintext), both of which
//     config.Validate still accepts.
//   - READ: detect prefix "vault:v1:" → Transit decrypt; otherwise →
//     AES decrypt. The read branch stays because rows written before
//     T10.3 are only readable through it (B-162 DoD bullet 3).
//
// SEC-001 invariant: every blob this store writes carries the Transit
// prefix, or the write returned an error and nothing was persisted.
//
// EC-06 (MUST FIX P2): all read paths check `len(ciphertext) == 0` BEFORE
// prefix detection to avoid panic on NULL/empty PG rows (e.g., legacy data
// or partial-write race conditions).
type PostgresSecretStore struct {
	DB            db.DBTX
	encryptionKey []byte // 32-byte legacy AES-256-GCM key, READ path only; nil = pre-T10.3 rows are unreadable (decrypt refuses)

	// Vault transit client (T3.4/T3.5). REQUIRED for writes since T10.3:
	// nil means encryptForStore refuses. Read path handles both Vault + AES.
	vault VaultTransitClient
}

// NewPostgresSecretStore creates a READ-ONLY-for-secrets store: since
// T10.3 removed the AES write branch, a store built here can decrypt
// legacy rows but every write through encryptForStore refuses with
// ErrSecretsBackendUnavailable. key must be exactly 32 bytes (AES-256)
// or nil. Use NewPostgresSecretStoreWithVault for the canonical
// write-capable store.
func NewPostgresSecretStore(db db.DBTX, key []byte) (*PostgresSecretStore, error) {
	if key != nil && len(key) != 32 {
		return nil, fmt.Errorf("secret_store: encryption key must be 32 bytes, got %d", len(key))
	}
	return &PostgresSecretStore{DB: db, encryptionKey: key}, nil
}

// NewPostgresSecretStoreWithVault creates a secret store with Vault transit
// as the canonical write path + dual-read (Vault prefix detect; AES fallback).
//
// vaultClient is REQUIRED — it is the only write path there is.
// legacyAESKey is OPTIONAL legacy AES key for reading pre-T3.5 ciphertexts;
// it is never used to write. Pass nil once no AES-written row remains.
//
// T3.5 of openbao-adoption v1.3 (FAANG dual-path, per the user's "go with
// whatever is most FAANG" decision, iter 27).
func NewPostgresSecretStoreWithVault(db db.DBTX, vaultClient VaultTransitClient, legacyAESKey []byte) (*PostgresSecretStore, error) {
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

// tenantAAD returns the AES-GCM additionally-authenticated-data (AAD) for a
// tenant. The AAD binds the ciphertext to a specific tenant: decrypt MUST
// pass the same AAD that encrypt used, otherwise gcm.Open returns an
// authentication-tag-mismatch error.
//
// Sprint D D3 (closes sec-001): pre-fix, encrypt/decrypt used nil AAD,
// which made ciphertexts portable across tenants. An attacker with
// DB-backup-exfiltration + AES-key-leak could:
//
//   - Copy Tenant A's encrypted_value blob into Tenant B's row, AND the
//     decrypt path would happily return Tenant A's plaintext — the AES-GCM
//     authentication didn't bind the row's identity.
//   - Permanently decrypt all tenants' secrets once the leak occurred
//     because there was no per-row binding to invalidate.
//
// Post-fix, tenantAAD is included in Seal+Open so cross-tenant ciphertext
// transplant fails the authentication tag → decrypt returns error rather
// than wrong-tenant plaintext. The AAD is NOT secret (it's the canonical
// label `loopruntime-secret-v1|tenant=<id>`); secrecy comes from the AES key,
// integrity from the AAD-bound tag.
//
// Format chosen for forward extensibility — future iterations may extend
// to bind project/env/app/name; the `v1` prefix lets decrypt detect format
// versions when migrating ciphertexts.
func tenantAAD(tenantID string) []byte {
	return []byte("loopruntime-secret-v1|tenant=" + tenantID)
}

// decrypt reads the legacy AES-256-GCM format. Reads nonce || ciphertext as written
// by encrypt(), returns plaintext. V9 L20: required by ListForResync so
// the deploy hook can repopulate ExternalSecrets with the original values.
//
// SEC-001 invariant: only path that turns AES-persisted bytes back into
// plaintext. T10.3 deleted the seal side, so this format is read-only:
// nothing in the tree can produce a new blob in it. A nil encryptionKey
// means any pre-T10.3 row is unreadable, and the pass-through below
// exists only so a plaintext-mode dev database keeps round-tripping.
//
// Sprint D D3 (closes sec-001): dual-decrypt window. The aad parameter
// binds new (post-D3) ciphertexts to a tenant identity. Open is tried
// FIRST with aad; on auth-tag failure it falls back to nil aad for
// backward compatibility with legacy ciphertexts written before D3 (which
// were Sealed with nil aad). The fallback path emits no logging because
// every legacy row exercises it on every read until the row is naturally
// re-encrypted on next write — too noisy to log per-row.
//
// Once a long enough window has passed (operator-defined; typically one
// full key-rotation cycle) and we are confident all rows have been
// re-encrypted with v1 AAD, the nil-fallback can be deleted in a follow-up
// commit (call it D3 Phase 2 — operator-time, requires running a scan to
// confirm zero legacy rows remain).
func (s *PostgresSecretStore) decrypt(encrypted, aad []byte) ([]byte, error) {
	if s.encryptionKey == nil {
		return nil, fmt.Errorf("secret_store: no legacy AES key configured to read a non-transit row: %w",
			loopruntimeerrors.ErrSecretsBackendUnavailable)
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
	if plain, err := gcm.Open(nil, nonce, ciphertext, aad); err == nil {
		return plain, nil
	}
	// D3 dual-decrypt fallback: legacy ciphertext sealed with nil AAD.
	// Try again without the binding. If THIS also fails the ciphertext is
	// genuinely corrupt or wrong-key; surface the error.
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("secret_store: gcm open: %w", err)
	}
	return plain, nil
}

// encryptForStore is the write-path dispatcher (T3.5, amended by T10.3).
//
// It has exactly two outcomes: Transit ciphertext, or a refusal. The
// AES-256-GCM fallback that fired when s.vault was nil is gone (B-162).
// It was reachable in any deployment that booted without
// OPENBAO_TRANSIT_ENABLED — config.Validate accepts crypto_mode=aes-local
// and, outside production, crypto_mode=plaintext — and the only thing
// that distinguished a secret it wrote from a Transit one was the absent
// "vault:v1:" prefix on a column nobody reads by eye.
//
// The refusal wraps ErrSecretsBackendUnavailable: from the caller's side,
// a store wired without Transit and a Transit backend that cannot be
// reached are the same condition, and both deserve 503 over a generic 500.
//
// The input is always the user's plaintext. It is never sniffed for a
// "vault:v1:" prefix: a value is data, not a format marker, and treating one
// that happens to start with the prefix as already-encrypted let it bypass
// both the transit encryption and the no-transit refusal. The store is the
// single encryption boundary; no caller pre-encrypts.
func (s *PostgresSecretStore) encryptForStore(ctx context.Context, tenantID string, plaintext []byte) ([]byte, error) {
	if s.vault == nil {
		// T10.3: no AES fallback. Refusing loses the write; falling back
		// would have kept it under a key the platform stopped managing.
		return nil, fmt.Errorf(
			"secret_store: no transit client wired, refusing to write secret for tenant %s: %w",
			tenantID, loopruntimeerrors.ErrSecretsBackendUnavailable)
	}
	ct, err := s.vault.Encrypt(ctx, tenantID, plaintext)
	if err != nil {
		// #138 — tag the failure so the HTTP boundary can answer
		// SECRETS_BACKEND_UNAVAILABLE (503) instead of a generic 500.
		// Before this, an unreachable backend was indistinguishable from a
		// database outage and `loopscale secret set` said only "Something went
		// wrong", leaving the operator to guess which subsystem refused.
		// The original error stays in the chain for the logs.
		return nil, fmt.Errorf("secret_store: vault transit encrypt: %w: %w",
			loopruntimeerrors.ErrSecretsBackendUnavailable, err)
	}
	return []byte(ct), nil
}

// decryptFromStore is the read-path dispatcher (T3.4).
// EC-06 MUST FIX P2: empty/nil ciphertext returns error (no AES panic).
// Prefix detection: "vault:v1:" → Transit decrypt (ErrSecretsBackendUnavailable
// when no transit client is wired); else → AES legacy.
// Sprint D D3: AES path passes tenantAAD; decrypt tries AAD first then
// falls back to nil for backward compat with pre-D3 ciphertexts.
//
// B-126: every error exit here also increments
// metrics.SecretDecryptFailedTotal under the reason that says which response
// the failure calls for. This function is the ONLY place in the store that
// increments it — callers (ListForResync) re-wrap the error on the way up and
// must not count the same failure twice. Adding a new error exit to this
// function without a counter puts the read path back where B-126 found it:
// one error per request, nothing that adds up.
//
// The counter aggregates; it does not probe. A tenant whose secrets nobody
// reads stays silent no matter what happened to its key, which is why the
// canary in the item's DoD is a separate design and not this one — see
// docs/decisions.md for the
// deferral and its trigger.
func (s *PostgresSecretStore) decryptFromStore(ctx context.Context, tenantID string, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		metrics.SecretDecryptFailedTotal.WithLabelValues("empty_ciphertext").Inc()
		return nil, fmt.Errorf("secret_store: empty ciphertext for tenant %s (EC-06 NULL row guard)", tenantID)
	}
	if bytes.HasPrefix(ciphertext, vaultPrefix) {
		if s.vault == nil {
			metrics.SecretDecryptFailedTotal.WithLabelValues("vault_transit_error").Inc()
			return nil, fmt.Errorf("secret_store: transit row for tenant %s but no transit client wired: %w",
				tenantID, loopruntimeerrors.ErrSecretsBackendUnavailable)
		}
		plain, err := s.vault.Decrypt(ctx, tenantID, string(ciphertext))
		if err != nil {
			// Key side: a revoked or rotated transit key lands here, and so
			// does an unreachable OpenBao. The store reaches Transit through a
			// consumer-defined interface and does not import pkg/vault, so the
			// typed error stays in the chain for the log rather than becoming
			// a label.
			metrics.SecretDecryptFailedTotal.WithLabelValues("vault_transit_error").Inc()
			return nil, fmt.Errorf("secret_store: vault transit decrypt: %w", err)
		}
		return plain, nil
	}
	plain, err := s.decrypt(ciphertext, tenantAAD(tenantID))
	if err != nil {
		// Data side: a pre-T10.3 AES row that will not open. Nothing writes
		// this format any more (B-162), so a non-zero rate is a legacy row
		// that has become unreadable — restore-a-backup, not restore-a-key.
		metrics.SecretDecryptFailedTotal.WithLabelValues("aes_legacy_error").Inc()
		return nil, err
	}
	return plain, nil
}

// Set creates or updates a secret. Uses sqlc-generated UpsertSecret
// (ON CONFLICT DO UPDATE returning xmax=0 to distinguish INSERT
// from UPDATE).
//
// SEC-001: Values are encrypted via s.encryptForStore before persistence.
// Returns true if the secret was created, false if updated.
// Single atomic statement — no race condition between concurrent
// Set calls for the same secret (the legacy SELECT+INSERT race
// is structurally absent).
func (s *PostgresSecretStore) Set(ctx context.Context, tenantID, projectID, env, app, name string, value []byte) (created bool, err error) {
	// SEC-001 + T3.5 + T10.3: the only blob that reaches encrypted_value
	// is Transit ciphertext ("vault:v1:...", per-tenant key). Without a
	// Transit client this errors out before the UPSERT.
	encrypted, err := s.encryptForStore(ctx, tenantID, value)
	if err != nil {
		return false, fmt.Errorf("secret_store: encrypt: %w", err)
	}

	bridge := s.bridge()
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
	bridge := s.bridge()
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
// after the namespace has been ensured. Wrapper decrypts via the read
// dispatcher before returning, so the caller gets plaintext values.
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
	bridge := s.bridge()
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
func (s *PostgresSecretStore) ListNames(ctx context.Context, tenantID, projectID, env string) ([]ports.SecretMetadata, error) {
	bridge := s.bridge()
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
	result := make([]ports.SecretMetadata, 0, len(rows))
	for _, row := range rows {
		result = append(result, ports.SecretMetadata{
			Name:        row.Name,
			Environment: row.Environment,
			App:         row.App,
		})
	}
	return result, nil
}

// bridge returns the sqlc-compatible DBTX from s.DB.
func (s *PostgresSecretStore) bridge() generated.DBTX {
	return db.Generated(s.DB)
}

// ErrSecretNotFound is returned by ReadValue when no secret matches.
var ErrSecretNotFound = errors.New("secret_store: secret not found")

// ReadValue returns one secret's plaintext, scoped by (tenant, project,
// environment, name). It is the read the External Secrets Operator broker
// serves, and it decrypts through decryptFromStore — the one read-path
// dispatcher — so the broker applies the same transit/legacy policy, refusals
// and SecretDecryptFailedTotal reasons as every other secret read. An absent
// row is ErrSecretNotFound, which is not a decrypt failure and is not counted.
func (s *PostgresSecretStore) ReadValue(ctx context.Context, tenantID, projectID, env, name string) ([]byte, error) {
	bridge := s.bridge()
	encrypted, err := generated.New().GetSecretEncryptedValue(ctx, bridge, generated.GetSecretEncryptedValueParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
		Name:        name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSecretNotFound
		}
		return nil, fmt.Errorf("secret_store: read %s for tenant %s: %w", name, tenantID, err)
	}
	return s.decryptFromStore(ctx, tenantID, encrypted)
}
