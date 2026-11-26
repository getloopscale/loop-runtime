// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package environments

import (
	"context"
)

// envManagerAdapter wraps EnvironmentManager (add + detail-getter) to satisfy
// EnvironmentLister + EnvironmentAdder. Plan T2.2 D5 plumbing.
//
// Existence semantics aligned with TenantCRManager:
//   - GetDetail returns (nonNilDetail, nil) → env exists
//   - GetDetail returns (nil, nil) → env does NOT exist (TenantCRManager pattern)
//   - GetDetail returns (_, non-nil err containing "not found") → does NOT exist
//   - GetDetail returns (_, any other err) → propagate as Lister error
//
// M2 v1.2 (2026-06-07) + m37 plan (2026-06-25): both Add AND GetDetail
// carry the `project` argument end-to-end. The shallow existence check
// is per-(project, env) tuple — matching the canonical Tenant CR
// spec.environments[] uniqueness key. This is what makes
// AddEnvironment fire when a tenant gains a NEW project with an env
// name that already exists on a different project (closes #37).
type envManagerAdapter struct {
	Add       func(ctx context.Context, tenantID, project, envName string) error
	GetDetail func(ctx context.Context, tenantID, project, envName string) (envDetail any, err error)
}

// NewEnvManagerAdapter builds an adapter from two function values. The
// getDetail func returns (nonNilOpaqueDetail, nil) when the (project, env)
// tuple exists, or (nil, nil) when absent (matches
// TenantCRManager.GetEnvironmentDetail contract). Tests can pass any
// compatible func.
//
// M2 v1.2 + m37 plan (2026-06-25): both `add` AND `getDetail` carry the
// project argument end-to-end.
func NewEnvManagerAdapter(
	add func(ctx context.Context, tenantID, project, envName string) error,
	getDetail func(ctx context.Context, tenantID, project, envName string) (any, error),
) (EnvironmentLister, EnvironmentAdder) {
	a := &envManagerAdapter{Add: add, GetDetail: getDetail}
	return a, a
}

// HasEnvironment maps TenantCRManager semantics to (bool, error).
// m37 plan: per-(project, env) tuple — see EnvironmentLister doc.
func (a *envManagerAdapter) HasEnvironment(ctx context.Context, tenantID, project, envName string) (bool, error) {
	detail, err := a.GetDetail(ctx, tenantID, project, envName)
	if err == nil {
		// (nil, nil) → absent; (non-nil, nil) → present.
		return detail != nil, nil
	}
	return false, err
}

// AddEnvironment — straight delegation. M2 v1.2 propagates project.
func (a *envManagerAdapter) AddEnvironment(ctx context.Context, tenantID, project, envName string) error {
	return a.Add(ctx, tenantID, project, envName)
}
