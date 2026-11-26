// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package load

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// L1 test scenarios from docs/plans/scale-50k-plan.md Phase 1.2.
//
// All scenarios use the RealDeployHarness (real git bundles, real builds,
// real app verification). Every deploy is verified with HTTP 200 on the
// app URL.
//
// Gated on LOOPRUNTIME_REAL_DEPLOY=1 + LOOPRUNTIME_LOAD_LEVEL=l1 (or "all").

func requireL1Harness(t *testing.T) *RealDeployHarness {
	t.Helper()
	if os.Getenv("LOOPRUNTIME_REAL_DEPLOY") != "1" {
		t.Skip("set LOOPRUNTIME_REAL_DEPLOY=1")
	}
	level := os.Getenv("LOOPRUNTIME_LOAD_LEVEL")
	if level != "l1" && level != "all" && level != "" {
		t.Skipf("set LOOPRUNTIME_LOAD_LEVEL=l1 (got %q)", level)
	}

	harness, err := NewRealDeployHarness(RealDeployConfig{
		APIURL:      os.Getenv("LOOPRUNTIME_API_URL"),
		AppDomain:   os.Getenv("LOOPRUNTIME_APP_DOMAIN"),
		ServiceAuth: os.Getenv("LOOPRUNTIME_SERVICE_AUTH"),
		FixturePath: envOr("LOOPRUNTIME_FIXTURE_PATH", "../../e2e/fixtures/hello-server"),
	})
	if err != nil {
		t.Fatalf("harness: %v", err)
	}
	return harness
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// --- Scenario 1.1: Sequential deploys ---
// 10 tenants deployed one at a time. Validates the happy path without
// any concurrency pressure.

func TestL1_1_SequentialDeploys(t *testing.T) {
	harness := requireL1Harness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	const n = 10
	t.Logf("L1.1 — %d sequential deploys", n)

	var success, fail int
	for i := 0; i < n; i++ {
		id := NewTenantIdentity("l1-seq", i)
		harness.ProvisionTenant(ctx, id)
		r := harness.RealDeploy(ctx, id)
		if r.Success && r.AppLive {
			success++
			t.Logf("  tn-%d: ✅ %s (%s)", i, r.BuildID, r.TotalTime.Round(time.Second))
		} else {
			fail++
			t.Logf("  tn-%d: ❌ %s", i, r.Error)
		}
	}

	t.Logf("Result: %d/%d succeeded, %d failed", success, n, fail)
	if fail > 0 {
		t.Errorf("L1.1 FAIL: %d sequential deploys failed (target: 0)", fail)
	}
}

// --- Scenario 1.2: Concurrent deploys ---
// 10 tenants deployed simultaneously. Validates lock contention (only
// 1 build per env can be active — others get 409). All should eventually
// succeed.

func TestL1_2_ConcurrentDeploys(t *testing.T) {
	harness := requireL1Harness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	const n = 10
	t.Logf("L1.2 — %d concurrent deploys", n)

	// Provision first
	ids := make([]TenantIdentity, n)
	for i := 0; i < n; i++ {
		ids[i] = NewTenantIdentity("l1-conc", i)
		harness.ProvisionTenant(ctx, ids[i])
	}

	// Deploy all concurrently
	results := make([]FullDeployResult, n)
	var wg sync.WaitGroup
	var success int64
	for i, id := range ids {
		wg.Add(1)
		go func(idx int, tenant TenantIdentity) {
			defer wg.Done()
			results[idx] = harness.RealDeploy(ctx, tenant)
			if results[idx].Success {
				atomic.AddInt64(&success, 1)
			}
		}(i, id)
	}
	wg.Wait()

	for i, r := range results {
		status := "✅"
		if !r.Success {
			status = "❌"
		}
		t.Logf("  %s tn-%d: %s (%s) live=%v", status, i, r.BuildID, r.TotalTime.Round(time.Second), r.AppLive)
	}

	t.Logf("Result: %d/%d succeeded", success, n)
	if success < int64(n) {
		t.Errorf("L1.2 FAIL: %d/%d concurrent deploys succeeded (target: %d)", success, n, n)
	}
}

// --- Scenario 1.5: Rollback under load ---
// Deploy an app, then deploy again, then rollback to the first version.
// Validates that rollback works while builds are active.

func TestL1_5_RollbackUnderLoad(t *testing.T) {
	if os.Getenv("LOOPRUNTIME_REAL_DEPLOY") != "1" {
		t.Skip("set LOOPRUNTIME_REAL_DEPLOY=1")
	}
	t.Log("L1.5 — Rollback under load")
	t.Skip("TODO: requires rollback API integration in harness")
}
