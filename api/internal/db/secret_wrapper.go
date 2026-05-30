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

// PostgresSecretStore manages secrets in PostgreSQL.
// SEC-001: Values are encrypted with AES-256-GCM before storage.
// When encryptionKey is nil, values are stored as plaintext (local dev only).
// The ListNames method NEVER returns values — only names and context.
//
// T2.2 Batch B #4: DB field type preserved (db.DBTX); methods
// dispatch through SqlcBridge to sqlc-generated queries.
type PostgresSecretStore struct {
	DB            DBTX
	encryptionKey []byte // 32-byte key for AES-256-GCM; nil disables encryption
}

// NewPostgresSecretStore creates a secret store with optional encryption.
// key must be exactly 32 bytes (AES-256) or nil to disable encryption.
func NewPostgresSecretStore(db DBTX, key []byte) (*PostgresSecretStore, error) {
	if key != nil && len(key) != 32 {
		return nil, fmt.Errorf("secret_store: encryption key must be 32 bytes, got %d", len(key))
	}
	return &PostgresSecretStore{DB: db, encryptionKey: key}, nil
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
	// SEC-001: Encrypt the value before storing.
	encrypted, err := s.encrypt(value)
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
