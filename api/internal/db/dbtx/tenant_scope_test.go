// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dbtx

import (
	"context"
	"testing"
)

func TestTenantScopeReturnsTheTenantTheContextWasScopedTo(t *testing.T) {
	got, ok := TenantScope(WithTenantScope(context.Background(), "tn-a"))

	if !ok || got != "tn-a" {
		t.Fatalf("TenantScope = %q, %v; want tn-a, true", got, ok)
	}
}

func TestTenantScopeReportsAnUnscopedContext(t *testing.T) {
	if got, ok := TenantScope(context.Background()); ok || got != "" {
		t.Fatalf("TenantScope of an unscoped context = %q, %v; want \"\", false", got, ok)
	}
}

// An empty tenant is no scope: granting it would be granting nothing, and
// reporting it as a scope would let a caller believe it had one.
func TestTenantScopeTreatsAnEmptyTenantAsNoScope(t *testing.T) {
	if _, ok := TenantScope(WithTenantScope(context.Background(), "")); ok {
		t.Fatal("an empty tenant must not read as a scope")
	}
}
