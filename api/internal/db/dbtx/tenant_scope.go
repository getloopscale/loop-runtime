// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package dbtx

import "context"

// tenantScopeKey is the context key for the tenant whose rows a request may see.
type tenantScopeKey struct{}

// WithTenantScope returns a context whose database work is confined to
// tenantID's rows. The request pool reads it on every connection it hands out
// and sets the Row Level Security variable `app.tenant_id` from it, so a store
// needs no tenant plumbing of its own to pass the isolation policies of
// migration 000062.
//
// Only code that has AUTHORIZED the caller for tenantID may call it: the scope
// is what makes the tenant's rows visible, so setting it is the grant.
func WithTenantScope(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantScopeKey{}, tenantID)
}

// TenantScope returns the tenant ctx is scoped to, and whether it carries one.
func TenantScope(ctx context.Context) (string, bool) {
	tenantID, ok := ctx.Value(tenantScopeKey{}).(string)
	return tenantID, ok && tenantID != ""
}
