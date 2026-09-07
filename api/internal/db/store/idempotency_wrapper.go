// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// idempotency_wrapper.go — sqlc-backed implementation of
// PostgresIdempotencyStore. T2.1 Batch A of plan
// docs/plans/2026-05-29-sqlc-migration-plan.md v1.3.
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

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/dbconv"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
	policy "github.com/getloopscale/loopzero/api/internal/middleware/policy"
)

const idempotencyTTL = 24 * time.Hour

// Get retrieves an idempotency record by key, scoped to a tenant.
// Returns nil (not error) when the key is not found or has expired.
func (s *PostgresIdempotencyStore) Get(ctx context.Context, key string, tenantID string) (*policy.IdempotencyRecord, error) {
	bridge, ok := s.DB.(db.SqlcBridge)
	if !ok {
		return nil, fmt.Errorf("idempotency store: DBTX %T does not implement SqlcBridge (per T0.6 reshape matrix)", s.DB)
	}
	q := generated.New()

	row, err := q.GetIdempotencyRecord(ctx, bridge.GeneratedDB(), generated.GetIdempotencyRecordParams{
		Key:      key,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("idempotency store: query failed: %w", err)
	}

	rec := &policy.IdempotencyRecord{
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
func (s *PostgresIdempotencyStore) Store(ctx context.Context, rec *policy.IdempotencyRecord) error {
	bridge, ok := s.DB.(db.SqlcBridge)
	if !ok {
		return fmt.Errorf("idempotency store: DBTX %T does not implement SqlcBridge (per T0.6 reshape matrix)", s.DB)
	}
	q := generated.New()

	expiresAt := time.Now().Add(idempotencyTTL)
	if err := q.InsertIdempotencyRecord(ctx, bridge.GeneratedDB(), generated.InsertIdempotencyRecordParams{
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
	bridge, ok := s.DB.(db.SqlcBridge)
	if !ok {
		return 0, fmt.Errorf("idempotency store: DBTX %T does not implement SqlcBridge (per T0.6 reshape matrix)", s.DB)
	}
	q := generated.New()

	rows, err := q.CleanupExpiredIdempotencyKeys(ctx, bridge.GeneratedDB())
	if err != nil {
		return 0, fmt.Errorf("idempotency store: cleanup failed: %w", err)
	}
	return rows, nil
}
