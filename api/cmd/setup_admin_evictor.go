// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"log/slog"

	"github.com/getloopscale/loopzero/api/internal/routes/admin"

	"github.com/getloopscale/loopzero/api/internal/services/placement"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/adapters"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/k8s/tenancy"
)

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
func buildAdminEvictor(cfg config.Config, dbAdapter *db.PgxAdapter) admin.TenantEvictor {
	// T1.4 (plan 2026-05-29-sqlc-migration v1.3): sqlc-backed admin
	// tenant store. ListTenantIDsByPattern + 8-step FK-ordered
	// eviction cascade preserved verbatim; all queries are
	// tenantpredicate:exempt per the admin path (LOOPZERO_ADMIN_API gate).
	store := store.NewTenantStoreSqlc(dbAdapter)
	dbDeleter := &adapters.TenantDBDeleterAdapter{Store: store}

	k8sEv, err := tenancy.NewTenantK8sEvictor(tenancy.EvictorConfig{
		Mgmt:    tenancy.ClusterAccess{Kubeconfig: cfg.MgmtKubeconfig, Context: cfg.MgmtKubeContext},
		Build:   tenancy.ClusterAccess{Kubeconfig: cfg.BuildKubeconfig, Context: cfg.BuildKubeContext},
		Runtime: tenancy.ClusterAccess{Kubeconfig: cfg.RuntimeKubeconfig, Context: cfg.RuntimeKubeContext},
		// Namespaces default to the control-plane namespace and "argo".
	})
	if err != nil {
		slog.Warn("admin evict: K8s teardown disabled — falling back to DB-only",
			"error", err.Error(),
			"consequence", "operators must run tools/admin-evict-tenants.sh for K8s cleanup",
		)
		return &adapters.TenantEvictorRouteAdapter{Inner: placement.NewTenantEvictorWithDB(store, dbDeleter)}
	}
	return &adapters.TenantEvictorRouteAdapter{Inner: placement.NewTenantEvictorFull(
		store,
		dbDeleter,
		&adapters.TenantK8sEvictorAdapter{Evictor: k8sEv},
	)}
}
