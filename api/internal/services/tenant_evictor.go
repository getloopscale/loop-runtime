// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package services — tenant_evictor.go implements T-02 bulk tenant
// teardown service (docs/plans/50k-immediate-focus.md).
//
// This replaces the ad-hoc bash tool (tools/admin-evict-tenants.sh)
// with a proper auditable service. Currently scaffolded for future
// wiring; the bash tool remains the immediate runtime.
//
// Scope:
//   - DELETE from 8 DB tables (FK-ordered transaction)
//   - Force-null finalizers on mgmt CRs (Tenant, Environment,
//     LoopZeroApplication, ExposedRoute, ArgoCD Application)
//   - Runtime namespace finalize via /api/v1/namespaces/{name}/finalize
//   - Argo Workflow cleanup
//
// Non-scope (future):
//   - Gitea repo deletion (user data — requires separate consent step)
//   - Harbor image deletion (optional; retained per retention policy)

package services

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

// TenantDeletionCounts mirrors db.TenantDeletionCounts but is declared here
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
}

// TenantDBDeleter abstracts the FK-ordered DB cleanup. Used by EnqueueEviction
// to perform the synchronous DB teardown portion. Implemented by
// db.PostgresTenantStore. A nil deleter means EnqueueEviction skips the DB
// portion and only returns the job id — useful for tests and for the
// scaffolding phase where K8s teardown is still done by the bash tool.
type TenantDBDeleter interface {
	DeleteTenantsFKOrdered(ctx context.Context, tenantIDs []string) (TenantDeletionCounts, error)
}

// TenantEvictor orchestrates bulk tenant teardown.
type TenantEvictor struct {
	lister     TenantLister         // nil-safe — if nil, only explicit IDs work
	deleter    TenantDBDeleter      // nil-safe — if nil, EnqueueEviction skips DB work
	lastCounts TenantDeletionCounts // result of most recent EnqueueEviction (non-thread-safe)
}

// NewTenantEvictor creates a TenantEvictor bound to a tenant lister.
// Pass nil to support only explicit IDs (no pattern matching).
// DB deletion is disabled when created this way — use NewTenantEvictorWithDB
// to enable synchronous DB teardown.
func NewTenantEvictor(lister TenantLister) *TenantEvictor {
	return &TenantEvictor{lister: lister}
}

// NewTenantEvictorWithDB creates a TenantEvictor that performs FK-ordered DB
// cleanup as part of EnqueueEviction. The K8s-cluster portion (mgmt CRs,
// namespaces, Argo workflows) is still done by tools/admin-evict-tenants.sh
// until the background worker lands.
func NewTenantEvictorWithDB(lister TenantLister, deleter TenantDBDeleter) *TenantEvictor {
	return &TenantEvictor{lister: lister, deleter: deleter}
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

// EnqueueEviction performs the synchronous DB teardown for a concrete list of
// tenant IDs and returns a job id for tracking. The K8s portion (delete mgmt
// CRs, Argo workflows, runtime namespaces) is still handled by
// tools/admin-evict-tenants.sh until the background worker lands (phase 2).
//
// Behavior:
//   - Empty tenantIDs → returns error (caller passed no work).
//   - Deleter nil → returns job id without touching DB (preview mode).
//   - Deleter set → runs FK-ordered cleanup, surfaces the counts via
//     GetLastCounts (the handler doesn't expose these today but they are
//     captured for future job-status endpoints).
//
// Implementation plan (phase 2):
//   - Insert job row into new `eviction_jobs` table
//   - Background worker picks up, processes K8s teardown per tenant
//   - Progress tracked in `eviction_jobs.status` + `.processed`
func (e *TenantEvictor) EnqueueEviction(ctx context.Context, tenantIDs []string) (string, error) {
	if len(tenantIDs) == 0 {
		return "", fmt.Errorf("no tenants to evict")
	}

	jobID := fmt.Sprintf("evict-%d-tenants", len(tenantIDs))

	if e.deleter != nil {
		counts, err := e.deleter.DeleteTenantsFKOrdered(ctx, tenantIDs)
		if err != nil {
			return "", fmt.Errorf("db cleanup: %w", err)
		}
		e.lastCounts = counts
	}
	return jobID, nil
}

// GetLastCounts returns the row deletion counts from the most recent
// EnqueueEviction call. Zero-value when no deletion has happened yet or
// when the evictor was constructed without a deleter. Not thread-safe:
// only valid when called from the same goroutine as EnqueueEviction,
// or under external synchronization.
func (e *TenantEvictor) GetLastCounts() TenantDeletionCounts {
	return e.lastCounts
}
