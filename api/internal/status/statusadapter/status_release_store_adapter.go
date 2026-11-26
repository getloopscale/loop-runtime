// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statusadapter

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/status/statushttp"

	"github.com/getloopscale/loop-runtime/pkg/naming"
)

// StatusReleaseStoreAdapter implements routes.StatusReleaseStore using db.ReleaseStore.
type StatusReleaseStoreAdapter struct {
	Store activeReleaseByEnv
	// Rollbacks tells when a rollback reactivated a release. Nil reads every
	// release as deployed when it was created.
	Rollbacks rollbackTimes
}

// rollbackTimes is the one rollback read the status makes.
type rollbackTimes interface {
	// LastRollbackTo is when the last successful rollback to the release
	// ended, nil when none did.
	LastRollbackTo(ctx context.Context, tenantID, releaseID string) (*time.Time, error)
}

// deployedAt is when the release became active. A failed rollback read falls
// back to the creation time: the status answer does not fail over it.
func (a *StatusReleaseStoreAdapter) deployedAt(ctx context.Context, tenantID string, release *db.Release) time.Time {
	if a.Rollbacks == nil {
		return release.CreatedAt
	}
	finishedAt, err := a.Rollbacks.LastRollbackTo(ctx, tenantID, release.ID)
	if err != nil {
		slog.Warn("status: could not read the rollbacks to the active release; reporting its creation time",
			"tenant_id", tenantID, "release_id", release.ID, "error", err.Error())
		return release.CreatedAt
	}
	if finishedAt == nil || finishedAt.Before(release.CreatedAt) {
		return release.CreatedAt
	}
	return *finishedAt
}

func (a *StatusReleaseStoreAdapter) GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*statushttp.StatusRelease, error) {
	release, err := a.Store.GetActiveByEnv(ctx, tenantID, projectID, env)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, nil
	}
	// EntryApp resolution: parse release.Apps (json.RawMessage of []db.BuildAppSpec)
	// and pick the first server/frontend app — that name is what the IngressRoute
	// renderer (pkg/naming.AppHostname) uses to compose the canonical Host rule.
	// FAANG canonical (J-25 sponsor walkthrough catch 2026-06-12): without this
	// resolution, status.url couldn't emit the same hostname formula as the
	// IngressRoute, so the dashboard "Visit" link 404'd at Traefik. Worker-only
	// releases (no server/frontend) leave EntryApp empty — StatusHandler then
	// omits status.url and the dashboard renders "URL pending" badge (ADR D3).
	return &statushttp.StatusRelease{
		ID:         release.ID,
		BuildID:    release.BuildID,
		Status:     release.Status,
		GitCommit:  release.GitCommit,
		CreatedAt:  release.CreatedAt,
		DeployedAt: a.deployedAt(ctx, tenantID, release),
		EntryApp:   resolveEntryApp(release.Apps),
	}, nil
}

// resolveEntryApp delegates to the canonical SoT at pkg/naming.
// Kept as a package-private forwarder so existing call sites in adapters
// don't need to change AND so the tests in release_adapter_test.go keep
// exercising the same helper name. Moved to pkg/naming 2026-07-01 (Red
// Team loopzero-cloud#159 closure) so the promote path can reuse it —
// promotion_orchestrator.go now emits the SAME canonical URL formula as
// status.go, closing the "promote prints URL that 404s" divergence.
func resolveEntryApp(appsRaw json.RawMessage) string {
	return naming.ResolveEntryAppFromApps(appsRaw)
}

// The port this adapter satisfies, asserted at compile time.
//
// B-058: the package is called `adapters` and its admission criterion was "it is an adapter" —
// which refuses nothing. Measured on 2026-08-18: of the nine adapters, five NAMED their port in
// prose and only ONE verified it. Prose does not compile; this line does, and it is what lets
// the criterion say no.
var _ statushttp.StatusReleaseStore = (*StatusReleaseStoreAdapter)(nil)
