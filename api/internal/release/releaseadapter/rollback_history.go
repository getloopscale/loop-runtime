// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package releaseadapter

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/release/rollbackhttp"
)

type rollbacksOfProject interface {
	ListByProject(ctx context.Context, tenantID, projectID string, limit int) ([]db.Rollback, error)
}

// RollbackHistoryAdapter hands the recorded rollbacks of a project to the
// handler that lists them.
type RollbackHistoryAdapter struct {
	Store rollbacksOfProject
}

func (a *RollbackHistoryAdapter) ListRollbacks(ctx context.Context, tenantID, projectID string, limit int) ([]rollbackhttp.RollbackEntry, error) {
	rows, err := a.Store.ListByProject(ctx, tenantID, projectID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]rollbackhttp.RollbackEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, rollbackhttp.RollbackEntry{
			ID: r.ID, Environment: r.Environment, Status: r.Status, FailureReason: r.FailureReason,
			FromReleaseID: r.FromReleaseID, FromBuildID: r.FromBuildID,
			ToReleaseID: r.ToReleaseID, ToBuildID: r.ToBuildID,
			CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt,
		})
	}
	return out, nil
}

var _ rollbackhttp.RollbackHistory = (*RollbackHistoryAdapter)(nil)
