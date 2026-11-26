// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package config

import (
	"strings"
	"testing"
)

// One per-tenant build limit, read from MAX_CONCURRENT_BUILDS_PER_TENANT. The
// older MAX_BUILDS_PER_TENANT set the reconcile loop's copy of the same rule;
// it is still accepted when it agrees, and refused when it does not, so two
// settings for one limit can never silently disagree (principle_violations#49).
func TestLoad_RefusesTwoDisagreeingPerTenantBuildLimits(t *testing.T) {
	t.Setenv("MAX_CONCURRENT_BUILDS_PER_TENANT", "3")
	t.Setenv("MAX_BUILDS_PER_TENANT", "2")

	err := Load().Validate()
	if err == nil || !strings.Contains(err.Error(), "MAX_BUILDS_PER_TENANT") {
		t.Fatalf("want a boot error naming the disagreeing setting, got %v", err)
	}
}

func TestLoad_AcceptsTheLegacyBuildLimitWhenItAgrees(t *testing.T) {
	t.Setenv("MAX_CONCURRENT_BUILDS_PER_TENANT", "4")
	t.Setenv("MAX_BUILDS_PER_TENANT", "4")

	cfg := Load()
	if cfg.MaxConcurrentBuildsPerTenant != 4 {
		t.Errorf("MaxConcurrentBuildsPerTenant = %d, want 4", cfg.MaxConcurrentBuildsPerTenant)
	}
	if err := cfg.Validate(); err != nil && strings.Contains(err.Error(), "MAX_BUILDS_PER_TENANT") {
		t.Errorf("an agreeing legacy setting must not be refused: %v", err)
	}
}

func TestLoad_ThePerTenantBuildLimitDefaultsToThree(t *testing.T) {
	if got := Load().MaxConcurrentBuildsPerTenant; got != 3 {
		t.Errorf("default per-tenant build limit = %d, want 3 (the value enforced in production)", got)
	}
}
