// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"strings"
)

// envManagerAdapter wraps EnvironmentManager (add + detail-getter) to satisfy
// EnvironmentLister + EnvironmentAdder. Plan T2.2 D5 plumbing.
//
// Existence semantics aligned with TenantCRManager:
//   - GetDetail returns (nonNilDetail, nil) → env exists
//   - GetDetail returns (nil, nil) → env does NOT exist (TenantCRManager pattern)
//   - GetDetail returns (_, non-nil err containing "not found") → does NOT exist
//   - GetDetail returns (_, any other err) → propagate as Lister error
// M2 v1.2 (2026-06-07): the Add closure carries the `project` argument
// so the canonical Tenant CR spec.environments entry is populated with
// the field needed to compute `loopzero-<tenant>-<project>-<env>`. The
// GetDetail closure stays project-agnostic — the shallow existence
// check is per-tenant by design.
type envManagerAdapter struct {
	Add       func(ctx context.Context, tenantID, project, envName string) error
	GetDetail func(ctx context.Context, tenantID, envName string) (envDetail any, err error)
}

// NewEnvManagerAdapter builds an adapter from two function values. The
// getDetail func returns (nonNilOpaqueDetail, nil) when env exists, or
// (nil, nil) when absent (matches TenantCRManager.GetEnvironmentDetail
// contract). Tests can pass any compatible func.
//
// M2 v1.2: `add` carries the project argument end-to-end.
func NewEnvManagerAdapter(
	add func(ctx context.Context, tenantID, project, envName string) error,
	getDetail func(ctx context.Context, tenantID, envName string) (any, error),
) (EnvironmentLister, EnvironmentAdder) {
	a := &envManagerAdapter{Add: add, GetDetail: getDetail}
	return a, a
}

// HasEnvironment maps TenantCRManager semantics to (bool, error).
func (a *envManagerAdapter) HasEnvironment(ctx context.Context, tenantID, envName string) (bool, error) {
	detail, err := a.GetDetail(ctx, tenantID, envName)
	if err == nil {
		// (nil, nil) → absent; (non-nil, nil) → present.
		return detail != nil, nil
	}
	// Defensive: legacy implementations may surface "not found" as error.
	if isNotFoundError(err) {
		return false, nil
	}
	return false, err
}

// AddEnvironment — straight delegation. M2 v1.2 propagates project.
func (a *envManagerAdapter) AddEnvironment(ctx context.Context, tenantID, project, envName string) error {
	return a.Add(ctx, tenantID, project, envName)
}

// isNotFoundError matches "not found" anywhere in the error message,
// case-insensitive. Aligned with the existing pattern in routes/environments.go
// which uses message-suffix matching.
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found")
}
