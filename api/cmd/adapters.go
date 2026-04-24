// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"

	"github.com/loopzero/loopzero/api/internal/adapters"
	"github.com/loopzero/loopzero/api/internal/admission"
	"github.com/loopzero/loopzero/api/internal/config"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/k8s"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/services"
)

// buildStoreAdmissionQuerier bridges *db.PostgresBuildStore to the
// admission gate's ActiveBuildsQuerier contract. Roadmap 07.
type buildStoreAdmissionQuerier struct {
	store *db.PostgresBuildStore
}

func (q *buildStoreAdmissionQuerier) ActiveBuilds(ctx context.Context, cellID string) (admission.ActiveBuildsSnapshot, error) {
	rows, err := q.store.FindActiveBuilds(ctx, cellID)
	if err != nil {
		return admission.ActiveBuildsSnapshot{}, err
	}
	byTenant := make(map[string]int, len(rows))
	for _, b := range rows {
		byTenant[b.TenantID]++
	}
	return admission.ActiveBuildsSnapshot{
		Total:    len(rows),
		ByTenant: byTenant,
	}, nil
}

// appCRAdapter bridges k8s.ApplicationCRManager to services.ApplicationCREnsurer.
type appCRAdapter struct {
	mgr *k8s.ApplicationCRManager
}

func (a *appCRAdapter) EnsureLoopZeroApplication(ctx context.Context, input services.ApplicationCRInput) error {
	return a.mgr.EnsureLoopZeroApplication(ctx, k8s.LoopZeroApplicationInput{
		TenantID:    input.TenantID,
		Project:     input.Project,
		Environment: input.Environment,
		AppName:     input.AppName,
		ServiceName: input.ServiceName,
		ServicePort: input.ServicePort,
	})
}

// buildAdminEvictor constructs the admin evictor with DB + (when possible)
// K8s teardown. Called during server Config assembly. If the multi-cluster
// K8s evictor cannot be built (missing kubeconfig, invalid context, etc.),
// the helper falls back to DB-only — the admin endpoint still works, it just
// skips the K8s phase and operators must run tools/admin-evict-tenants.sh
// for the cluster resources.
//
// The fallback is intentional: local dev (k3d) often has all three contexts
// available, but partial-cluster breakage (e.g. runtime kubeconfig mis-set)
// shouldn't prevent the API from starting. A startup log line makes the
// degraded mode explicit.
func buildAdminEvictor(cfg config.Config, dbAdapter *db.PgxAdapter) routes.TenantEvictor {
	store := &db.PostgresTenantStore{DB: dbAdapter}
	dbDeleter := &adapters.TenantDBDeleterAdapter{Store: store}

	k8sEv, err := k8s.NewTenantK8sEvictor(
		cfg.MgmtKubeconfig, cfg.MgmtKubeContext,
		cfg.BuildKubeconfig, cfg.BuildKubeContext,
		cfg.RuntimeKubeconfig, cfg.RuntimeKubeContext,
		"", "", // default namespaces (loopzero-system, argo)
	)
	if err != nil {
		slog.Warn("admin evict: K8s teardown disabled — falling back to DB-only",
			"error", err.Error(),
			"consequence", "operators must run tools/admin-evict-tenants.sh for K8s cleanup",
		)
		return &tenantEvictorRouteAdapter{Inner: services.NewTenantEvictorWithDB(store, dbDeleter)}
	}
	return &tenantEvictorRouteAdapter{Inner: services.NewTenantEvictorFull(
		store,
		dbDeleter,
		&adapters.TenantK8sEvictorAdapter{Evictor: k8sEv},
	)}
}

// tenantEvictorRouteAdapter exposes *services.TenantEvictor via the
// routes.TenantEvictor interface. The concrete type's pointer-receiver
// methods already satisfy the interface — this wrapper only exists to make
// that relationship explicit (and would allow future per-route concerns
// like logging/tracing to be layered in without touching services).
type tenantEvictorRouteAdapter struct {
	Inner *services.TenantEvictor
}

func (a *tenantEvictorRouteAdapter) MatchTenants(ctx context.Context, pattern string, ids []string) ([]string, error) {
	return a.Inner.MatchTenants(ctx, pattern, ids)
}

func (a *tenantEvictorRouteAdapter) EnqueueEviction(ctx context.Context, tenantIDs []string) (string, error) {
	return a.Inner.EnqueueEviction(ctx, tenantIDs)
}
