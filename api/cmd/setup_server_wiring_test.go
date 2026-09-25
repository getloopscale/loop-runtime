// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/server"
)

func quietWiring(t *testing.T) serverWiring {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return serverWiring{
		cfg:            config.Config{},
		k8sDeps:        &K8sDeps{},
		shutdownCtx:    ctx,
		shutdownCancel: cancel,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// Without an orchestrator the preview handler must see NO service, so it
// answers read-only instead of calling a method on a nil pointer. A typed-nil
// *PreviewService in the interface is not nil to the handler's guard.
func TestPreviewServiceIsAbsentNotTypedNilWithoutAnOrchestrator(t *testing.T) {
	svc := newPreviewService(quietWiring(t), nil)
	if svc != nil {
		t.Fatalf("newPreviewService without an orchestrator = %#v, want a nil interface", svc)
	}
}

func TestCostEnforcementStaysOffWithoutAPlanLimiter(t *testing.T) {
	w := quietWiring(t)
	w.dbDeps = &DatabaseDeps{}
	sc := buildServerConfig(w)
	if sc.Guards.CostEnforcer != nil || sc.PlanTierStore != nil {
		t.Fatalf("cost enforcement wired without a plan limiter: enforcer=%v tierStore=%v", sc.Guards.CostEnforcer, sc.PlanTierStore)
	}
}

func TestTheNoDatabaseBootRegistersNoDatabaseBackedHandler(t *testing.T) {
	sc := buildServerConfig(quietWiring(t))
	if sc.BillingHandler != nil || sc.TeamHandler != nil || sc.Guards.PGPoolGate != nil ||
		sc.PreviewHandler != nil || sc.Admin.Pinner != nil || sc.CellPlacement != nil {
		t.Fatal("a database-backed handler was wired with no database; its routes would be registered and fail on first use")
	}
}

// Tenant provisioning counts every cell's tenants and writes a tenant that
// does not exist yet, so it must run on the cross-tenant pool: over the request
// pool, fail-closed RLS counts zero tenants per cell and refuses the INSERT.
func TestProvisioningIsWiredOnTheCrossTenantPoolOnly(t *testing.T) {
	requestPool, crossTenant := &db.PgxAdapter{}, &db.PgxAdapter{}

	t.Run("with the cross-tenant pool", func(t *testing.T) {
		w := quietWiring(t)
		w.dbAdapter, w.crossTenant = requestPool, crossTenant
		var sc server.Config

		wirePlacement(&sc, w)

		bootstrapStore, ok := sc.BootstrapStore.(*store.PostgresBootstrapStore)
		if !ok || bootstrapStore.DB != crossTenant || sc.CellPlacement == nil {
			t.Fatalf("provisioning wired as store=%#v placement=%v; want both on the cross-tenant pool", sc.BootstrapStore, sc.CellPlacement)
		}
	})

	t.Run("without it", func(t *testing.T) {
		w := quietWiring(t)
		w.dbAdapter = requestPool
		var sc server.Config

		wirePlacement(&sc, w)

		if sc.BootstrapStore != nil || sc.CellPlacement != nil {
			t.Fatalf("provisioning wired on the request pool: store=%#v placement=%v", sc.BootstrapStore, sc.CellPlacement)
		}
	})
}
