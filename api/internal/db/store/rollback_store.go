// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"fmt"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/generated"
)

// RollbackStoreSqlc keeps the record of each rollback (table rollbacks).
type RollbackStoreSqlc struct {
	pool db.DBTX
	q    *generated.Queries
}

// NewRollbackStoreSqlc builds the store over a tenant-scoped handle.
func NewRollbackStoreSqlc(pool db.DBTX) *RollbackStoreSqlc {
	return &RollbackStoreSqlc{pool: pool, q: generated.New()}
}

// Start records a rollback as in progress. tx is the transaction that moves
// the active release to ROLLING_BACK, so the two are written together.
func (s *RollbackStoreSqlc) Start(ctx context.Context, tx db.DBTX, r *db.Rollback) error {
	target := tx
	if target == nil {
		target = s.pool
	}
	err := s.q.InsertRollback(ctx, db.Generated(target), generated.InsertRollbackParams{
		ID:            r.ID,
		TenantID:      r.TenantID,
		ProjectID:     r.ProjectID,
		Environment:   r.Environment,
		FromReleaseID: r.FromReleaseID,
		FromBuildID:   r.FromBuildID,
		ToReleaseID:   r.ToReleaseID,
		ToBuildID:     r.ToBuildID,
		CreatedBy:     r.CreatedBy,
		CorrelationID: r.CorrelationID,
	})
	if err != nil {
		return fmt.Errorf("record rollback %s: %w", r.ID, err)
	}
	return nil
}

// Finish closes a rollback with its outcome. Closing one that is already
// closed changes nothing: the first outcome recorded stands.
func (s *RollbackStoreSqlc) Finish(ctx context.Context, tenantID, id, status, failureReason string) error {
	if _, err := s.q.FinishRollback(ctx, db.Generated(s.pool), generated.FinishRollbackParams{
		ID:            id,
		TenantID:      tenantID,
		Status:        status,
		FailureReason: failureReason,
	}); err != nil {
		return fmt.Errorf("close rollback %s as %s: %w", id, status, err)
	}
	return nil
}

// LastRollbackTo is when the last successful rollback to the release ended,
// nil when none did.
func (s *RollbackStoreSqlc) LastRollbackTo(ctx context.Context, tenantID, releaseID string) (*time.Time, error) {
	finishedAt, err := s.q.LastRollbackTo(ctx, db.Generated(s.pool), generated.LastRollbackToParams{
		TenantID:    tenantID,
		ToReleaseID: releaseID,
	})
	if err != nil {
		return nil, fmt.Errorf("read the last rollback to %s: %w", releaseID, err)
	}
	if !finishedAt.Valid {
		return nil, nil
	}
	at := finishedAt.Time
	return &at, nil
}

// ListByProject returns the project's rollbacks, newest first.
func (s *RollbackStoreSqlc) ListByProject(ctx context.Context, tenantID, projectID string, limit int) ([]db.Rollback, error) {
	rows, err := s.q.ListRollbacksByProject(ctx, db.Generated(s.pool), generated.ListRollbacksByProjectParams{
		TenantID:  tenantID,
		ProjectID: projectID,
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list rollbacks of %s: %w", projectID, err)
	}
	out := make([]db.Rollback, 0, len(rows))
	for _, row := range rows {
		r := db.Rollback{
			ID: row.ID, TenantID: row.TenantID, ProjectID: row.ProjectID, Environment: row.Environment,
			FromReleaseID: row.FromReleaseID, FromBuildID: row.FromBuildID,
			ToReleaseID: row.ToReleaseID, ToBuildID: row.ToBuildID,
			Status: row.Status, FailureReason: row.FailureReason,
			CreatedBy: row.CreatedBy, CorrelationID: row.CorrelationID,
			CreatedAt: row.CreatedAt.Time,
		}
		if row.FinishedAt.Valid {
			finished := row.FinishedAt.Time
			r.FinishedAt = &finished
		}
		out = append(out, r)
	}
	return out, nil
}
