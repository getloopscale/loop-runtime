// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"encoding/json"

	"github.com/getloopscale/loopzero/api/internal/routes/status"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/pkg/naming"
)

// StatusReleaseStoreAdapter implements routes.StatusReleaseStore using db.ReleaseStore.
type StatusReleaseStoreAdapter struct {
	Store db.ReleaseStore
}

func (a *StatusReleaseStoreAdapter) GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*status.StatusRelease, error) {
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
	return &status.StatusRelease{
		ID:        release.ID,
		BuildID:   release.BuildID,
		Status:    release.Status,
		GitCommit: release.GitCommit,
		CreatedAt: release.CreatedAt,
		EntryApp:  resolveEntryApp(release.Apps),
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

// A porta que este adaptador satisfaz, afirmada em tempo de compilacao.
//
// B-058: o pacote se chama `adapters` e o criterio de admissao dele era "e um adapter" —
// que nao recusa nada. Medido em 2026-08-18: dos nove adaptadores, cinco NOMEAVAM a porta em
// prosa e apenas UM a verificava. Prosa nao compila; esta linha sim, e e ela que faz o
// criterio conseguir dizer nao.
var _ status.StatusReleaseStore = (*StatusReleaseStoreAdapter)(nil)
