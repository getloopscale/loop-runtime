// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"strings"
)

// envManagerAdapter wraps a thin EnvironmentManager (just the two methods
// the ensurer needs) to satisfy EnvironmentLister + EnvironmentAdder.
//
// Existence semantics: GetEnvironmentDetail returns a non-nil error containing
// "not found" (case-insensitive) when the env doesn't exist for the tenant.
// Any other error is propagated as a Lister error.
type envManagerAdapter struct {
	Add func(ctx context.Context, tenantID, envName string) error
	Get func(ctx context.Context, tenantID, envName string) error // err==nil → exists
}

// NewEnvManagerAdapter builds an adapter from two function values. Tests can
// pass any function; production code passes EnvironmentManager method refs.
func NewEnvManagerAdapter(
	add func(ctx context.Context, tenantID, envName string) error,
	getExistence func(ctx context.Context, tenantID, envName string) error,
) (EnvironmentLister, EnvironmentAdder) {
	a := &envManagerAdapter{Add: add, Get: getExistence}
	return a, a
}

// HasEnvironment — true when Get returns nil error.
func (a *envManagerAdapter) HasEnvironment(ctx context.Context, tenantID, envName string) (bool, error) {
	err := a.Get(ctx, tenantID, envName)
	if err == nil {
		return true, nil
	}
	// "not found" sentinel handling — we treat the env as absent rather than
	// surfacing a transient/unauthorized error.
	if isNotFoundError(err) {
		return false, nil
	}
	return false, err
}

// AddEnvironment — straight delegation.
func (a *envManagerAdapter) AddEnvironment(ctx context.Context, tenantID, envName string) error {
	return a.Add(ctx, tenantID, envName)
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
