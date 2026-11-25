// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environmentsadapter

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
	"github.com/getloopscale/loopzero/api/internal/environments/environmentshttp"
)

func TestEnvKPIsReportTheLastDeployPerEnvironmentInUTC(t *testing.T) {
	sha := "abc123"
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.FixedZone("BRT", -3*3600))
	rows := []generated.ListLatestActiveReleasePerEnvRow{
		{ProjectID: "web", Environment: "production", GitCommit: &sha, CreatedAt: pgtype.Timestamptz{Time: at, Valid: true}},
	}

	got := envKPIsFromRows(rows)

	kpi := got[environmentshttp.EnvRef{Project: "web", Name: "production"}]
	if kpi.LastDeploy != "2026-09-01T15:00:00Z" || kpi.LastDeploySha != "abc123" {
		t.Fatalf("KPI = %+v", kpi)
	}
}

func TestEnvKPIsReportAnEmptyShaForAReleaseWithoutACommit(t *testing.T) {
	rows := []generated.ListLatestActiveReleasePerEnvRow{
		{ProjectID: "web", Environment: "staging", CreatedAt: pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true}},
	}

	if got := envKPIsFromRows(rows)[environmentshttp.EnvRef{Project: "web", Name: "staging"}].LastDeploySha; got != "" {
		t.Fatalf("LastDeploySha = %q, want empty for a NULL git_commit", got)
	}
}

// releases.git_commit is the commit of the GitOps repository; the user deployed the
// build's commit. The KPI showed the GitOps one.
func TestEnvKPIsReportTheSourceCommitOfTheActiveRelease(t *testing.T) {
	gitopsCommit := "e0f47de8f57e55ee7fbdbbc91b526834819ae55f"
	sourceCommit := "15fec6419dfb54982308b095ca2239a693a00d39"
	rows := []generated.ListLatestActiveReleasePerEnvRow{
		{
			ProjectID: "web", Environment: "production", GitCommit: &gitopsCommit, SourceCommit: &sourceCommit,
			CreatedAt: pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true},
		},
	}

	if got := envKPIsFromRows(rows)[environmentshttp.EnvRef{Project: "web", Name: "production"}].LastDeploySha; got != sourceCommit {
		t.Fatalf("LastDeploySha = %q, want the source commit %q", got, sourceCommit)
	}
}

// A release whose build was pruned has no source commit left; the release's own
// commit is still better than nothing.
func TestEnvKPIsFallBackToTheReleaseCommitWithoutASourceCommit(t *testing.T) {
	gitopsCommit := "e0f47de8f57e55ee7fbdbbc91b526834819ae55f"
	rows := []generated.ListLatestActiveReleasePerEnvRow{
		{ProjectID: "web", Environment: "production", GitCommit: &gitopsCommit, CreatedAt: pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true}},
	}

	if got := envKPIsFromRows(rows)[environmentshttp.EnvRef{Project: "web", Name: "production"}].LastDeploySha; got != gitopsCommit {
		t.Fatalf("LastDeploySha = %q, want the release commit %q", got, gitopsCommit)
	}
}

// Two projects with a production environment are two rows and two KPIs.
func TestEnvKPIsKeepProjectsApart(t *testing.T) {
	at := pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true}
	web, worker := "aaa", "bbb"
	rows := []generated.ListLatestActiveReleasePerEnvRow{
		{ProjectID: "web", Environment: "production", SourceCommit: &web, CreatedAt: at},
		{ProjectID: "worker", Environment: "production", SourceCommit: &worker, CreatedAt: at},
	}

	got := envKPIsFromRows(rows)

	if sha := got[environmentshttp.EnvRef{Project: "web", Name: "production"}].LastDeploySha; sha != "aaa" {
		t.Errorf("web/production sha = %q, want aaa", sha)
	}
	if sha := got[environmentshttp.EnvRef{Project: "worker", Name: "production"}].LastDeploySha; sha != "bbb" {
		t.Errorf("worker/production sha = %q, want bbb", sha)
	}
}
