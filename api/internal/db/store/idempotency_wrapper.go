// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// idempotency_wrapper.go — sqlc-backed implementation of
// PostgresIdempotencyStore. T2.1 Batch A of plan
// 2026-05-29-sqlc-migration-plan v1.3.
//
// The 3 prod call sites (api/cmd/setup_server.go:169,
// api/cmd/setup_background.go:289, api/cmd/controller/setup_services.go:213)
// continue to use `&db.PostgresIdempotencyStore{DB: dbAdapter}` — the
// struct-literal shape is preserved (struct lives in
// idempotency_types.go, methods here).
//
// Invariants preserved:
//   - 24-hour TTL (idempotencyTTL constant) applied at Store-time
//   - Tenant predicate on Get (idempotency_keys is in
//     TenantOwnedTables; Cleanup is cross-tenant exempt by design)
//   - Get returns nil (not error) when no active record matches
//     (matches middleware contract)
//   - Single INSERT shape (column order preserved)

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getloopscale/loopzero/api/internal/ports"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/dbconv"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
)

const idempotencyTTL = 24 * time.Hour

// Get retrieves an idempotency record by key, scoped to a tenant.
// Returns nil (not error) when the key is not found or has expired.
func (s *PostgresIdempotencyStore) Get(ctx context.Context, key string, tenantID string) (*ports.IdempotencyRecord, error) {
	gdb := db.Generated(s.DB)
	q := generated.New()

	row, err := q.GetIdempotencyRecord(ctx, gdb, generated.GetIdempotencyRecordParams{
		Key:      key,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("idempotency store: query failed: %w", err)
	}

	rec := &ports.IdempotencyRecord{
		Key:            row.Key,
		TenantID:       row.TenantID,
		Endpoint:       row.Endpoint,
		RequestHash:    row.RequestHash,
		ResponseStatus: int(row.ResponseStatus),
		ResponseBody:   row.ResponseBody,
	}
	return rec, nil
}

// Store persists an idempotency record with a 24-hour TTL.
func (s *PostgresIdempotencyStore) Store(ctx context.Context, rec *ports.IdempotencyRecord) error {
	gdb := db.Generated(s.DB)
	q := generated.New()

	expiresAt := time.Now().Add(idempotencyTTL)
	if err := q.InsertIdempotencyRecord(ctx, gdb, generated.InsertIdempotencyRecordParams{
		Key:            rec.Key,
		TenantID:       rec.TenantID,
		Endpoint:       rec.Endpoint,
		RequestHash:    rec.RequestHash,
		ResponseStatus: int32(rec.ResponseStatus),
		ResponseBody:   rec.ResponseBody,
		ExpiresAt:      dbconv.TimeToTimestamptz(expiresAt),
	}); err != nil {
		return fmt.Errorf("idempotency store: insert failed: %w", err)
	}
	return nil
}

// Cleanup removes expired idempotency records and returns the number
// removed. Background-job path; cross-tenant by design.
func (s *PostgresIdempotencyStore) Cleanup(ctx context.Context) (int64, error) {
	gdb := db.Generated(s.DB)
	q := generated.New()

	rows, err := q.CleanupExpiredIdempotencyKeys(ctx, gdb)
	if err != nil {
		return 0, fmt.Errorf("idempotency store: cleanup failed: %w", err)
	}
	return rows, nil
}
