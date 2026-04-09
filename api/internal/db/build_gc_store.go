// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"
	"time"
)

// PostgresBuildGCStore implements services.BuildGCStore for garbage collection.
type PostgresBuildGCStore struct {
	DB DBTX
}

// DeleteOldBuilds removes builds older than maxAge, keeping at least retainPerProject
// most recent builds per (project_id, environment).
func (s *PostgresBuildGCStore) DeleteOldBuilds(ctx context.Context, maxAge time.Duration, retainPerProject int) (int64, error) {
	cutoff := time.Now().UTC().Add(-maxAge)

	// Delete builds that are:
	// 1. In terminal state (SUCCEEDED, FAILED, CANCELLED)
	// 2. Older than cutoff
	// 3. NOT in the top N most recent per (project, environment)
	result, err := s.DB.Exec(ctx, `
		DELETE FROM builds
		WHERE id IN (
			SELECT b.id FROM builds b
			WHERE b.status IN ('SUCCEEDED', 'FAILED', 'CANCELLED')
			  AND b.created_at < $1
			  AND b.id NOT IN (
				SELECT id FROM (
					SELECT id, ROW_NUMBER() OVER (
						PARTITION BY project_id, environment
						ORDER BY created_at DESC
					) as rn
					FROM builds
				) ranked
				WHERE rn <= $2
			  )
		)
	`, cutoff, retainPerProject)
	if err != nil {
		return 0, fmt.Errorf("build gc: delete old builds: %w", err)
	}
	return result.RowsAffected, nil
}

// DeleteOldEvents removes SSE events older than maxAge.
func (s *PostgresBuildGCStore) DeleteOldEvents(ctx context.Context, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-maxAge)
	result, err := s.DB.Exec(ctx, `
		DELETE FROM build_events WHERE created_at < $1
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("build gc: delete old events: %w", err)
	}
	return result.RowsAffected, nil
}
