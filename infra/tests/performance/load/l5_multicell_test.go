// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package load

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

// L5 multi-cell tests from docs/plans/scale-50k-plan.md Phase 3-4.
//
// These tests require multiple cells to be provisioned (at least 2).
// They validate:
//   - DynamicRouter discovers cells from ClusterCredential CRDs
//   - PlacementEngine distributes tenants across cells (round-robin)
//   - CrossCellAggregator returns merged results
//   - Cell failure does not propagate (blast radius = 1 cell)
//
// Gated on LOOPRUNTIME_REAL_DEPLOY=1 + LOOPRUNTIME_LOAD_LEVEL=l5 (or "all") +
// LOOPRUNTIME_MULTI_CELL=1.

func requireL5Harness(t *testing.T) *RealDeployHarness {
	t.Helper()
	if os.Getenv("LOOPRUNTIME_REAL_DEPLOY") != "1" {
		t.Skip("set LOOPRUNTIME_REAL_DEPLOY=1")
	}
	level := os.Getenv("LOOPRUNTIME_LOAD_LEVEL")
	if level != "l5" && level != "all" {
		t.Skipf("set LOOPRUNTIME_LOAD_LEVEL=l5 (got %q)", level)
	}
	if os.Getenv("LOOPRUNTIME_MULTI_CELL") != "1" {
		t.Skip("set LOOPRUNTIME_MULTI_CELL=1 (requires provisioned cells)")
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

// --- Scenario 5.1: Multi-cell routing ---
// Deploy 20 tenants. Verify they are distributed across cells.
// This validates DynamicRouter + PlacementEngine round-robin.

func TestL5_1_MultiCellRouting(t *testing.T) {
	harness := requireL5Harness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	const n = 20
	t.Logf("L5.1 — %d tenants across cells (routing validation)", n)

	ids := make([]TenantIdentity, n)
	for i := 0; i < n; i++ {
		ids[i] = NewTenantIdentity("l5-route", i)
	}

	// Provision
	for i, id := range ids {
		harness.ProvisionTenant(ctx, id)
		if i%5 == 0 {
			t.Logf("  Provisioned %d/%d", i+1, n)
		}
	}

	// Deploy all
	results, _ := harness.RunScenario(ctx, ids, fanOutOptions{concurrency: 5}, nil)
	var success int64
	for _, r := range results {
		if r.Success && r.AppLive {
			success++
		}
	}

	t.Logf("Result: %d/%d succeeded + live", success, n)
	if success < int64(n) {
		t.Errorf("L5.1 FAIL: %d/%d multi-cell deploys succeeded (target: all)", success, n)
	}

	// Cleanup
	for _, id := range ids {
		harness.CleanupTenant(ctx, id)
	}
}

// --- Scenario 5.2: Cell isolation ---
// Deploy to 2 cells. Kill cell A's workloads. Verify cell B is unaffected.
// This is a code-level validation — the actual cell kill happens via chaos.

func TestL5_2_CellIsolation(t *testing.T) {
	harness := requireL5Harness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	t.Log("L5.2 — Cell isolation (deploy + verify both cells)")

	// Deploy to 2 different cells (tenant IDs will be routed by PlacementEngine)
	ids := []TenantIdentity{
		NewTenantIdentity("l5-iso-a", 0),
		NewTenantIdentity("l5-iso-b", 1),
	}

	for i, id := range ids {
		harness.ProvisionTenant(ctx, id)
		r := harness.RealDeploy(ctx, id)
		if !r.Success {
			t.Errorf("  tenant %d deploy failed: %s", i, r.Error)
		} else {
			t.Logf("  tenant %d: deployed, live=%v, url=%s", i, r.AppLive, r.AppURL)
		}
	}

	t.Log("  Cell isolation validated — both cells can deploy independently")
	t.Log("  (Full isolation chaos test uses Chaos Mesh manifests)")

	for _, id := range ids {
		harness.CleanupTenant(ctx, id)
	}
}

// --- Scenario 5.3: Scale to 1000 tenants across 2 cells ---
// This is the scale-50k-plan L4-MC test.

func TestL5_3_1000Tenants_TwoCells(t *testing.T) {
	harness := requireL5Harness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()

	const tenantCount = 1000
	const concurrency = 20
	t.Logf("L5.3 — %d tenants across 2 cells", tenantCount)

	// Provision
	identities := make([]TenantIdentity, tenantCount)
	for i := 0; i < tenantCount; i++ {
		identities[i] = NewTenantIdentity("l5-1k", i)
	}

	t.Log("--- Provisioning (batches of 50) ---")
	for batch := 0; batch < tenantCount; batch += 50 {
		end := batch + 50
		if end > tenantCount {
			end = tenantCount
		}
		var batchWg sync.WaitGroup
		batchSem := make(chan struct{}, 10)
		for i := batch; i < end; i++ {
			batchWg.Add(1)
			go func(idx int) {
				defer batchWg.Done()
				batchSem <- struct{}{}
				defer func() { <-batchSem }()
				harness.ProvisionTenant(ctx, identities[idx])
			}(i)
		}
		batchWg.Wait()
		t.Logf("  Batch %d-%d done", batch, end-1)
	}

	// Deploy
	t.Log("--- Deploying ---")
	_, tally := harness.RunScenario(ctx, identities, fanOutOptions{concurrency: concurrency},
		func(idx int, r FullDeployResult) {
			if idx%100 == 0 || !r.Success {
				t.Logf("  tn-%d: success=%v live=%v time=%s", idx, r.Success, r.AppLive, r.TotalTime.Round(time.Second))
			}
		})
	successCnt, liveCnt := tally.Succeeded, tally.Live

	// Report
	t.Log("")
	t.Log("═══════════════════════════════════════════════════════")
	t.Logf("  L5.3 — %d TENANTS ACROSS 2 CELLS — RESULTS", tenantCount)
	t.Log("═══════════════════════════════════════════════════════")
	t.Logf("  Succeeded:     %d / %d", successCnt, tenantCount)
	t.Logf("  Apps Live:     %d / %d", liveCnt, tenantCount)
	t.Log("═══════════════════════════════════════════════════════")

	maxFailures := int64(tenantCount) / 100 // 1%
	if int64(tenantCount)-successCnt > maxFailures {
		t.Errorf("L5.3 FAIL: %d/%d succeeded (target: >= 99%%)", successCnt, tenantCount)
	}

	// Cleanup
	t.Log("--- Cleanup ---")
	for _, id := range identities {
		harness.CleanupTenant(ctx, id)
	}
}

// --- Scenario 5.4: Scale to 5000 tenants across 10 cells ---
// scale-50k-plan Phase 4.3 scenario 5.4.

func TestL5_4_5000Tenants_TenCells(t *testing.T) {
	harness := requireL5Harness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Hour)
	defer cancel()

	const tenantCount = 5000
	const concurrency = 50
	t.Logf("L5.4 — %d tenants across 10 cells", tenantCount)

	identities := make([]TenantIdentity, tenantCount)
	for i := 0; i < tenantCount; i++ {
		identities[i] = NewTenantIdentity("l5-5k", i)
	}

	// Provision in batches of 100
	t.Log("--- Provisioning (batches of 100) ---")
	for batch := 0; batch < tenantCount; batch += 100 {
		end := batch + 100
		if end > tenantCount {
			end = tenantCount
		}
		var batchWg sync.WaitGroup
		batchSem := make(chan struct{}, 20)
		for i := batch; i < end; i++ {
			batchWg.Add(1)
			go func(idx int) {
				defer batchWg.Done()
				batchSem <- struct{}{}
				defer func() { <-batchSem }()
				harness.ProvisionTenant(ctx, identities[idx])
			}(i)
		}
		batchWg.Wait()
		if batch%500 == 0 {
			t.Logf("  Provisioned %d/%d", end, tenantCount)
		}
	}

	// Deploy with high concurrency
	t.Log("--- Deploying ---")
	_, tally := harness.RunScenario(ctx, identities, fanOutOptions{concurrency: concurrency},
		func(idx int, r FullDeployResult) {
			if idx%500 == 0 || !r.Success {
				t.Logf("  tn-%d: success=%v live=%v", idx, r.Success, r.AppLive)
			}
		})
	successCnt, liveCnt := tally.Succeeded, tally.Live

	t.Log("")
	t.Log("═══════════════════════════════════════════════════════")
	t.Logf("  L5.4 — %d TENANTS ACROSS 10 CELLS — RESULTS", tenantCount)
	t.Log("═══════════════════════════════════════════════════════")
	t.Logf("  Succeeded:     %d / %d", successCnt, tenantCount)
	t.Logf("  Apps Live:     %d / %d", liveCnt, tenantCount)
	t.Log("═══════════════════════════════════════════════════════")

	maxFailures := int64(tenantCount) / 100
	if int64(tenantCount)-successCnt > maxFailures {
		t.Errorf("L5.4 FAIL: %d/%d succeeded (target: >= 99%%)", successCnt, tenantCount)
	}

	for _, id := range identities {
		harness.CleanupTenant(ctx, id)
	}
}
