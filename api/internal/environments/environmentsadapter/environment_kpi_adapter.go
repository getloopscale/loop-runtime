// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environmentsadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/generated"
	"github.com/getloopscale/loopzero/api/internal/environments/environmentshttp"
)

var _ environmentshttp.EnvKPILookup = (*EnvKPILookup)(nil)

// EnvKPILookup fills the dashboard's per-environment KPI row (last deploy
// time and source commit) from the most recent ACTIVE release of each environment,
// in one query (ListLatestActiveReleasePerEnv).
type EnvKPILookup struct {
	DB db.DBTX
}

// LatestActivePerEnv returns the last-deploy KPI keyed by environment name.
func (l *EnvKPILookup) LatestActivePerEnv(ctx context.Context, tenantID string) (map[string]environmentshttp.EnvKPI, error) {
	rows, err := generated.New().ListLatestActiveReleasePerEnv(ctx, db.Generated(l.DB), tenantID)
	if err != nil {
		return nil, fmt.Errorf("environment KPI lookup: %w", err)
	}
	return envKPIsFromRows(rows), nil
}

func envKPIsFromRows(rows []generated.ListLatestActiveReleasePerEnvRow) map[string]environmentshttp.EnvKPI {
	out := make(map[string]environmentshttp.EnvKPI, len(rows))
	for _, r := range rows {
		// The source commit is what the user deployed; releases.git_commit is the
		// GitOps repository commit, kept only for a release whose build is gone.
		sha := ""
		switch {
		case r.SourceCommit != nil && *r.SourceCommit != "":
			sha = *r.SourceCommit
		case r.GitCommit != nil:
			sha = *r.GitCommit
		}
		out[r.Environment] = environmentshttp.EnvKPI{
			LastDeploy:    r.CreatedAt.Time.UTC().Format(time.RFC3339),
			LastDeploySha: sha,
		}
	}
	return out
}
