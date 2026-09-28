// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environmentsadapter

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
)

func TestEnvKPIsReportTheLastDeployPerEnvironmentInUTC(t *testing.T) {
	sha := "abc123"
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.FixedZone("BRT", -3*3600))
	rows := []generated.ListLatestActiveReleasePerEnvRow{
		{Environment: "production", GitCommit: &sha, CreatedAt: pgtype.Timestamptz{Time: at, Valid: true}},
	}

	got := envKPIsFromRows(rows)

	kpi := got["production"]
	if kpi.LastDeploy != "2026-09-01T15:00:00Z" || kpi.LastDeploySha != "abc123" {
		t.Fatalf("KPI = %+v", kpi)
	}
}

func TestEnvKPIsReportAnEmptyShaForAReleaseWithoutACommit(t *testing.T) {
	rows := []generated.ListLatestActiveReleasePerEnvRow{
		{Environment: "staging", CreatedAt: pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true}},
	}

	if got := envKPIsFromRows(rows)["staging"].LastDeploySha; got != "" {
		t.Fatalf("LastDeploySha = %q, want empty for a NULL git_commit", got)
	}
}
