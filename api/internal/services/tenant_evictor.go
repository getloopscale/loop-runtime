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

// TenantEvictor orchestrates bulk tenant teardown.
type TenantEvictor struct {
	lister TenantLister // nil-safe — if nil, only explicit IDs work
}

// NewTenantEvictor creates a TenantEvictor bound to a tenant lister.
// Pass nil to support only explicit IDs (no pattern matching).
func NewTenantEvictor(lister TenantLister) *TenantEvictor {
	return &TenantEvictor{lister: lister}
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

// EnqueueEviction submits tenant IDs for async teardown.
//
// Implementation plan (phase 2):
//   - Insert job row into new `eviction_jobs` table
//   - Background worker picks up, processes up to N concurrent
//   - Progress tracked in `eviction_jobs.status` + `.processed`
//
// Currently returns a deterministic job ID; the actual work is not
// performed (scaffold). Next commit wires this to a background worker.
func (e *TenantEvictor) EnqueueEviction(ctx context.Context, tenantIDs []string) (string, error) {
	if len(tenantIDs) == 0 {
		return "", fmt.Errorf("no tenants to evict")
	}
	// TODO(T-02 phase 2): persist job + spawn worker.
	// For now, return a placeholder job_id. Handler layer has already
	// validated input; this just acknowledges acceptance.
	return fmt.Sprintf("evict-%d-tenants", len(tenantIDs)), nil
}
