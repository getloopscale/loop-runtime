// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"
	"time"

	"github.com/loopzero/loopzero/api/internal/middleware"
)

// IdempotencyStoreDB combines QueryExecutor and ExecQueryExecutor
// for the idempotency store which needs both read and write operations.
type IdempotencyStoreDB interface {
	QueryExecutor
	ExecQueryExecutor
}

// PostgresIdempotencyStore implements middleware.IdempotencyStore backed by PostgreSQL.
// Uses the idempotency_keys table from migration 000001.
// TTL is 24 hours per spec §4.3.
type PostgresIdempotencyStore struct {
	DB IdempotencyStoreDB
}

const idempotencyTTL = 24 * time.Hour

// Get retrieves an idempotency record by key, scoped to a tenant.
// Returns nil (not error) when the key is not found or has expired.
func (s *PostgresIdempotencyStore) Get(ctx context.Context, key string, tenantID string) (*middleware.IdempotencyRecord, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT key, tenant_id, endpoint, request_hash, response_status, response_body
		 FROM idempotency_keys
		 WHERE key = $1 AND tenant_id = $2 AND expires_at > NOW()`,
		key, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("idempotency store: query failed: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("idempotency store: rows error: %w", err)
		}
		return nil, nil
	}

	var rec middleware.IdempotencyRecord
	if err := rows.Scan(
		&rec.Key,
		&rec.TenantID,
		&rec.Endpoint,
		&rec.RequestHash,
		&rec.ResponseStatus,
		&rec.ResponseBody,
	); err != nil {
		return nil, fmt.Errorf("idempotency store: scan failed: %w", err)
	}

	return &rec, nil
}

// Store persists an idempotency record with a 24-hour TTL.
func (s *PostgresIdempotencyStore) Store(ctx context.Context, rec *middleware.IdempotencyRecord) error {
	expiresAt := time.Now().Add(idempotencyTTL)

	_, err := s.DB.Exec(ctx,
		`INSERT INTO idempotency_keys (key, tenant_id, endpoint, request_hash, response_status, response_body, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		rec.Key, rec.TenantID, rec.Endpoint, rec.RequestHash, rec.ResponseStatus, rec.ResponseBody, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("idempotency store: insert failed: %w", err)
	}

	return nil
}

// Cleanup removes expired idempotency records and returns the number removed.
// This method is not part of the IdempotencyStore interface; it is intended
// to be called by a background job or maintenance routine.
func (s *PostgresIdempotencyStore) Cleanup(ctx context.Context) (int64, error) {
	result, err := s.DB.Exec(ctx,
		`DELETE FROM idempotency_keys WHERE expires_at < NOW()`,
	)
	if err != nil {
		return 0, fmt.Errorf("idempotency store: cleanup failed: %w", err)
	}

	return result.RowsAffected, nil
}
