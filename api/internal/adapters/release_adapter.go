// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"encoding/json"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/routes"
)

// StatusReleaseStoreAdapter implements routes.StatusReleaseStore using db.ReleaseStore.
type StatusReleaseStoreAdapter struct {
	Store db.ReleaseStore
}

func (a *StatusReleaseStoreAdapter) GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*routes.StatusRelease, error) {
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
	return &routes.StatusRelease{
		ID:        release.ID,
		BuildID:   release.BuildID,
		Status:    release.Status,
		GitCommit: release.GitCommit,
		CreatedAt: release.CreatedAt,
		EntryApp:  resolveEntryApp(release.Apps),
	}, nil
}

// resolveEntryApp picks the canonical "entry" app — the one whose IngressRoute
// the dashboard's "Visit" link should target. Resolution rules (FAANG canonical
// J-25 sponsor walkthrough 2026-06-12 LIVE catch — second-order bug after the
// first FAANG fix shipped in commit bfb21e0f):
//
//  1. Prefer the FIRST `server` app — server apps are always Traefik-routed
//     via IngressRoute, so the canonical URL formula resolves cleanly.
//  2. Fall back to the first `frontend` with rendering != "static" — those
//     also get Traefik IngressRoutes (containerized HTTP server).
//  3. SKIP `rendering: static` frontends entirely — they are delivered via
//     CF Worker + DO Spaces (`api/internal/services/container_delivery.go`),
//     NOT Traefik. Their URL field in release.Apps uses the same internal
//     SNI formula but no IngressRoute exists at that host, so emitting it
//     as status.url produces a Traefik 404 (the exact symptom sponsor caught
//     when teste/web=static was picked instead of teste/api=server).
//  4. Worker-only releases (no server/frontend) AND frontend-only releases
//     that are 100% static return "" — StatusHandler then suppresses
//     status.url and the dashboard renders "URL pending" (ADR D3 fail-closed).
//
// Malformed JSON (should never happen — emitted by the API at release
// activation) returns "" rather than propagating an error, degrading the URL
// gracefully into "URL pending".
func resolveEntryApp(appsRaw json.RawMessage) string {
	if len(appsRaw) == 0 {
		return ""
	}
	var specs []db.BuildAppSpec
	if err := json.Unmarshal(appsRaw, &specs); err != nil {
		return ""
	}
	// Pass 1: prefer server apps — Traefik-routed by definition.
	for _, s := range specs {
		if s.Type == "server" {
			return s.Name
		}
	}
	// Pass 2: fall back to non-static frontend — also Traefik-routed.
	for _, s := range specs {
		if s.Type == "frontend" && s.Rendering != "static" {
			return s.Name
		}
	}
	return ""
}
