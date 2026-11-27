// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package environmentsadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
	"github.com/getloopscale/loop-runtime/api/internal/environments/environmentshttp"
)

var _ environmentshttp.EnvReadinessLookup = (*EnvReadinessLookup)(nil)

// EnvReadinessLookup reads the ready replicas environments.ReadinessPoller
// records in environment_targets, one query per project.
type EnvReadinessLookup struct {
	DB db.DBTX
}

// ReadyReplicasPerEnv implements environmentshttp.EnvReadinessLookup.
func (l *EnvReadinessLookup) ReadyReplicasPerEnv(ctx context.Context, tenantID string, projects []string) (map[environmentshttp.EnvRef]environmentshttp.EnvReadiness, error) {
	out := map[environmentshttp.EnvRef]environmentshttp.EnvReadiness{}
	for _, project := range projects {
		rows, err := generated.New().ListEnvironmentTargetsByProject(ctx, db.Generated(l.DB), generated.ListEnvironmentTargetsByProjectParams{
			TenantID:  tenantID,
			ProjectID: project,
		})
		if err != nil {
			return nil, fmt.Errorf("environment readiness lookup for project %q: %w", project, err)
		}
		addEnvReadiness(out, rows)
	}
	return out, nil
}

// addEnvReadiness keeps the rows the poller observed at least once.
func addEnvReadiness(out map[environmentshttp.EnvRef]environmentshttp.EnvReadiness, rows []generated.EnvironmentTarget) {
	for _, r := range rows {
		if r.LastReplicasReadyValue == nil || !r.LastReplicasReadyAt.Valid {
			continue
		}
		out[environmentshttp.EnvRef{Project: r.ProjectID, Name: r.Environment}] = environmentshttp.EnvReadiness{
			Replicas: int(*r.LastReplicasReadyValue),
			At:       r.LastReplicasReadyAt.Time.UTC().Format(time.RFC3339),
		}
	}
}
