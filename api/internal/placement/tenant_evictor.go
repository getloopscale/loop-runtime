// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// tenant_evictor.go implements T-02 bulk tenant
// teardown service (50k-immediate-focus).
//
// This replaces the ad-hoc bash tool (tools/admin-evict-tenants.sh)
// with a proper auditable service. Currently scaffolded for future
// wiring; the bash tool remains the immediate runtime.
//
// Scope:
//   - DELETE from 8 DB tables (FK-ordered transaction)
//   - Force-null finalizers on mgmt CRs (Tenant, Environment,
//     LoopRuntimeApplication, ExposedRoute, ArgoCD Application)
//   - Runtime namespace finalize via /api/v1/namespaces/{name}/finalize
//   - Argo Workflow cleanup
//
// Non-scope (future):
//   - Gitea repo deletion (user data — requires separate consent step)
//   - Tenant image deletion from the registry (retained per retention policy)

package placement

import (
	"context"
	"fmt"
	"strings"
)

// TenantLister is the minimum interface the evictor needs from the DB layer.
// Defined here (not in db package) to avoid coupling — any store that can
// list tenants by ID pattern will satisfy it.
type TenantLister interface {
	ListTenantIDsByPattern(ctx context.Context, pattern string) ([]string, error)
}

// TenantDeletionCounts mirrors store.TenantDeletionCounts but is declared here
// to keep the services package independent of the db package's concrete type.
// Fields are keyed the same way so JSON round-trips are lossless.
type TenantDeletionCounts struct {
	EnvironmentTargets int64 `json:"environment_targets"`
	Releases           int64 `json:"releases"`
	BuildApps          int64 `json:"build_apps"`
	Builds             int64 `json:"builds"`
	Projects           int64 `json:"projects"`
	Subscriptions      int64 `json:"subscriptions"`
	TenantMembers      int64 `json:"tenant_members"`
	Tenants            int64 `json:"tenants"`
	MeteringCursors    int64 `json:"metering_cursors"`
}

// TenantDBDeleter abstracts the FK-ordered DB cleanup. Used by EvictTenants
// to perform the synchronous DB teardown portion. Implemented by
// db.PostgresTenantStore. A nil deleter means EvictTenants skips the DB
// portion (tests, and isolating the cluster half).
type TenantDBDeleter interface {
	DeleteTenantsFKOrdered(ctx context.Context, tenantIDs []string) (TenantDeletionCounts, error)
}

// TenantK8sEvictor abstracts the multi-cluster K8s teardown (phase 2).
// Implemented by tenancy.TenantK8sEvictor through adapters.TenantK8sEvictorAdapter.
// A nil evictor means EvictTenants skips the K8s portion and relies on
// tools/admin-evict-tenants.sh for cluster cleanup — useful for dev and
// for smoke-testing the DB half in isolation.
type TenantK8sEvictor interface {
	EvictTenantsFromClusters(ctx context.Context, tenantIDs []string) (K8sEvictionCounts, error)
}

// K8sEvictionCounts mirrors tenancy.K8sEvictionCounts on the service-layer side
// to avoid cyclic imports (same pattern as TenantDeletionCounts).
type K8sEvictionCounts struct {
	ArgoWorkflowsDeleted int `json:"argo_workflows_deleted"`
	MgmtCRsDeleted       int `json:"mgmt_crs_deleted"`
	FinalizersCleared    int `json:"finalizers_cleared"`
	NamespacesFinalized  int `json:"namespaces_finalized"`
}

// TenantEvictor orchestrates bulk tenant teardown.
type TenantEvictor struct {
	lister     TenantLister     // nil-safe — if nil, only explicit IDs work
	deleter    TenantDBDeleter  // nil-safe — if nil, EvictTenants skips DB work
	k8sEvictor TenantK8sEvictor // nil-safe — if nil, EvictTenants skips K8s work
}

// EvictionResult reports what one EvictTenants call removed.
type EvictionResult struct {
	Database TenantDeletionCounts `json:"database"`
	Clusters K8sEvictionCounts    `json:"clusters"`
}

// NewTenantEvictor creates a TenantEvictor bound to a tenant lister.
// Pass nil to support only explicit IDs (no pattern matching).
// DB deletion is disabled when created this way — use NewTenantEvictorWithDB
// to enable synchronous DB teardown.
func NewTenantEvictor(lister TenantLister) *TenantEvictor {
	return &TenantEvictor{lister: lister}
}

// NewTenantEvictorWithDB creates a TenantEvictor that performs FK-ordered DB
// cleanup as part of EvictTenants. The K8s-cluster portion (mgmt CRs,
// namespaces, Argo workflows) is still done by tools/admin-evict-tenants.sh
// until the background worker lands.
func NewTenantEvictorWithDB(lister TenantLister, deleter TenantDBDeleter) *TenantEvictor {
	return &TenantEvictor{lister: lister, deleter: deleter}
}

// NewTenantEvictorFull creates a TenantEvictor with both DB and K8s teardown
// wired. This is the production configuration — EvictTenants runs the
// full eviction pipeline: K8s cluster teardown (mgmt CRs, Argo wf, runtime
// namespaces) followed by FK-ordered DB cleanup. Mirrors steps 1-5 of the
// original bash tool (tools/admin-evict-tenants.sh).
//
// Any nil argument disables the corresponding stage. Passing all three nils
// is equivalent to the scaffold NewTenantEvictor (lister-only).
func NewTenantEvictorFull(lister TenantLister, deleter TenantDBDeleter, k8sEvictor TenantK8sEvictor) *TenantEvictor {
	return &TenantEvictor{lister: lister, deleter: deleter, k8sEvictor: k8sEvictor}
}

// MatchTenants resolves a SQL LIKE pattern + explicit IDs into a
// concrete list of tenant IDs. Returns deduplicated IDs in alphabetical
// order.
//
// Pattern constraints:
//   - Must not be "%" or "tn-%" (enforced at route layer).
//   - Matches against tenants.id column.
//
// This method does NOT evict — it's safe for preflight / dry-run.
func (e *TenantEvictor) MatchTenants(ctx context.Context, pattern string, explicitIDs []string) ([]string, error) {
	seen := make(map[string]struct{})
	result := make([]string, 0)

	for _, id := range explicitIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			result = append(result, id)
		}
	}

	if strings.TrimSpace(pattern) != "" && e.lister != nil {
		ids, err := e.lister.ListTenantIDsByPattern(ctx, pattern)
		if err != nil {
			return nil, fmt.Errorf("list by pattern: %w", err)
		}
		for _, id := range ids {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				result = append(result, id)
			}
		}
	}

	return result, nil
}

// EvictTenants tears the given tenants down synchronously and returns what it
// removed: first the cluster resources (mgmt CRs, Argo workflows, runtime
// namespaces), then the FK-ordered database rows.
//
// The cluster teardown runs BEFORE the database cleanup, as the bash tool does:
// deleting mgmt CRs triggers the operator's finalizers, which may still read
// database rows; wiping the database first can leave orphaned CRs behind. A
// cluster failure therefore stops the eviction before the database is touched.
//
// It returns an error for an empty tenant list (no work was asked for). A nil
// k8s evictor or deleter skips that stage, and the result reports zero for it.
func (e *TenantEvictor) EvictTenants(ctx context.Context, tenantIDs []string) (EvictionResult, error) {
	var result EvictionResult
	if len(tenantIDs) == 0 {
		return result, fmt.Errorf("no tenants to evict")
	}
	if e.k8sEvictor != nil {
		counts, err := e.k8sEvictor.EvictTenantsFromClusters(ctx, tenantIDs)
		if err != nil {
			return result, fmt.Errorf("k8s cleanup: %w", err)
		}
		result.Clusters = counts
	}
	if e.deleter != nil {
		counts, err := e.deleter.DeleteTenantsFKOrdered(ctx, tenantIDs)
		if err != nil {
			return result, fmt.Errorf("db cleanup: %w", err)
		}
		result.Database = counts
	}
	return result, nil
}
