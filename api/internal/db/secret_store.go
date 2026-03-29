// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"

	"github.com/loopzero/loopzero/api/internal/routes"
)

// PostgresSecretStore manages secrets in PostgreSQL.
// SEC-001: Values are encrypted with AES-256-GCM before storage.
// When encryptionKey is nil, values are stored as plaintext (local dev only).
// The ListNames method NEVER returns values — only names and context.
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

// Set creates or updates a secret. Uses upsert (ON CONFLICT DO UPDATE).
// SEC-001: Values are encrypted with AES-256-GCM before storage.
// Returns true if the secret was created, false if updated.
func (s *PostgresSecretStore) Set(ctx context.Context, tenantID, projectID, env, app, name string, value []byte) (created bool, err error) {
	// SEC-001: Encrypt the value before storing.
	encrypted, err := s.encrypt(value)
	if err != nil {
		return false, fmt.Errorf("secret_store: encrypt: %w", err)
	}

	// Check if secret exists first (to determine created vs updated)
	rows, err := s.DB.Query(ctx,
		`SELECT 1 FROM secrets WHERE tenant_id = $1 AND project_id = $2 AND environment = $3 AND app = $4 AND name = $5`,
		tenantID, projectID, env, app, name,
	)
	if err != nil {
		return false, fmt.Errorf("secret_store: check exists: %w", err)
	}
	exists := rows.Next()
	rows.Close()

	id := fmt.Sprintf("sec-%s-%s-%s-%s", tenantID, projectID, app, name)

	_, err = s.DB.Exec(ctx,
		`INSERT INTO secrets (id, tenant_id, project_id, environment, app, name, encrypted_value)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (tenant_id, project_id, environment, app, name)
		 DO UPDATE SET encrypted_value = $7, updated_at = NOW()`,
		id, tenantID, projectID, env, app, name, encrypted,
	)
	if err != nil {
		return false, fmt.Errorf("secret_store: set: %w", err)
	}

	return !exists, nil
}

// Delete removes a secret by name within tenant/project/env/app scope.
// Returns true if a secret was deleted, false if it didn't exist.
func (s *PostgresSecretStore) Delete(ctx context.Context, tenantID, projectID, env, app, name string) (deleted bool, err error) {
	result, err := s.DB.Exec(ctx,
		`DELETE FROM secrets WHERE tenant_id = $1 AND project_id = $2 AND environment = $3 AND app = $4 AND name = $5`,
		tenantID, projectID, env, app, name,
	)
	if err != nil {
		return false, fmt.Errorf("secret_store: delete: %w", err)
	}
	return result.RowsAffected > 0, nil
}

// ListNames returns metadata (name, environment, app) for all secrets
// matching the given tenant, project, and environment.
// Results are ordered alphabetically by name.
func (s *PostgresSecretStore) ListNames(ctx context.Context, tenantID, projectID, env string) ([]routes.SecretMetadata, error) {
	rows, err := s.DB.Query(ctx,
		"SELECT name, environment, app FROM secrets WHERE tenant_id = $1 AND project_id = $2 AND environment = $3 ORDER BY name",
		tenantID, projectID, env,
	)
	if err != nil {
		return nil, fmt.Errorf("secret_store: list names: %w", err)
	}
	defer rows.Close()

	var result []routes.SecretMetadata
	for rows.Next() {
		var m routes.SecretMetadata
		if err := rows.Scan(&m.Name, &m.Environment, &m.App); err != nil {
			return nil, fmt.Errorf("secret_store: scan row: %w", err)
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("secret_store: iterate rows: %w", err)
	}

	// Return empty slice instead of nil to avoid null in JSON responses.
	if result == nil {
		result = []routes.SecretMetadata{}
	}

	return result, nil
}
