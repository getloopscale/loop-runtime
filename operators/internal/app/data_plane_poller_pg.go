// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package app

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// PostgresReadinessSink records readiness snapshots in the environment_targets
// table that the api owns and the dashboard's project summary reads.
//
// This adapter is the one place the operator knows that table's layout: the
// columns last_replicas_ready_value and last_replicas_ready_at, keyed by
// (tenant_id, project_id, environment). A schema change there must change
// this statement too.
type PostgresReadinessSink struct {
	db *sql.DB
}

// NewPostgresReadinessSink returns a sink writing through db.
func NewPostgresReadinessSink(db *sql.DB) *PostgresReadinessSink {
	return &PostgresReadinessSink{db: db}
}

// RecordReplicasReady implements ReadinessSnapshotSink. It returns
// ErrEnvironmentTargetNotFound when the UPDATE matched no row.
func (s *PostgresReadinessSink) RecordReplicasReady(ctx context.Context, target EnvironmentTarget, replicasReady int64, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE environment_targets
		SET last_replicas_ready_value = $1, last_replicas_ready_at = $2
		WHERE tenant_id = $3 AND project_id = $4 AND environment = $5
	`, replicasReady, at, target.Tenant, target.Project, target.Environment)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return ErrEnvironmentTargetNotFound
	}
	return nil
}
