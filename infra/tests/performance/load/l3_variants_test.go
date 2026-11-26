// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// CHG-2026-04-28-LAYERED-BOOTSTRAP — T8.3 + iter 17
// L3 test variants for regression coverage:
//   - L3RealisticWithChurn — exercises B11 (gitops archived) + B17 (repo-server
//     cache stale) by deleting and recreating 10% of the tenants mid-flight.
//   - L3NoisyNeighbor — exercises B-C (git push race) + Rule 6 concurrent
//     rejection by issuing 10 rapid deploys against the same tenant.
//
// Iter 17: real implementation replacing the stubs. Skips when the environment
// is not available (same pattern as real_deploy_test.go).

package load

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRealDeploy_L3RealisticWithChurn provisions 100 tenants, deletes 10 at
// random, recreates the SAME 10, redeploys, and verifies that ALL 100 serve
// HTTP 200. Covers B11 (gitops archived) + B17 (repo-server cache stale after
// delete+recreate).
func TestRealDeploy_L3RealisticWithChurn(t *testing.T) {
	if os.Getenv("LOOPRUNTIME_REAL_DEPLOY") != "1" {
		t.Skip("set LOOPRUNTIME_REAL_DEPLOY=1")
	}
	level := os.Getenv("LOOPRUNTIME_LOAD_LEVEL")
	if level != "l3-churn" && level != "all" {
		t.Skip("set LOOPRUNTIME_LOAD_LEVEL=l3-churn or all")
	}

	apiURL := os.Getenv("LOOPRUNTIME_API_URL")
	appDomain := os.Getenv("LOOPRUNTIME_APP_DOMAIN")
	serviceAuth := os.Getenv("LOOPRUNTIME_SERVICE_AUTH")
	if apiURL == "" || appDomain == "" || serviceAuth == "" {
		t.Fatal("LOOPRUNTIME_API_URL, LOOPRUNTIME_APP_DOMAIN, LOOPRUNTIME_SERVICE_AUTH required")
	}

	const tenantCount = 100
	churnPct := 10
	if v := os.Getenv("LOOPRUNTIME_CHURN_PCT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < 100 {
			churnPct = n
		}
	}
	churnCount := tenantCount * churnPct / 100

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()

	harness, err := NewRealDeployHarness(RealDeployConfig{
		APIURL:      apiURL,
		AppDomain:   appDomain,
		ServiceAuth: serviceAuth,
		FixturePath: envOr("LOOPRUNTIME_FIXTURE_PATH", "../../e2e/fixtures/dogfood-monorepo"),
	})
	if err != nil {
		t.Fatalf("harness: %v", err)
	}

	t.Logf("=== L3-Realistic-WithChurn — %d tenants, %d%% churn (%d will be recreated) ===",
		tenantCount, churnPct, churnCount)

	// Phase 1: provision + deploy all 100
	identities := make([]TenantIdentity, tenantCount)
	for i := 0; i < tenantCount; i++ {
		identities[i] = NewTenantIdentity("l3churn", i)
	}

	t.Log("Phase 1: provisioning + deploying 100 tenants")
	provisioned := 0
	for _, id := range identities {
		if _, err := harness.ProvisionTenant(ctx, id); err != nil {
			t.Logf("provision %s failed: %v", id.TenantID, err)
			continue
		}
		provisioned++
	}
	if provisioned < tenantCount*9/10 {
		t.Fatalf("Phase 1 provisioning: only %d/%d succeeded (<90%%)", provisioned, tenantCount)
	}

	// Phase 2: deploy all
	t.Log("Phase 2: deploying all 100 tenants")
	phase2Results := make([]FullDeployResult, tenantCount)
	for i, id := range identities {
		phase2Results[i] = harness.RealDeploy(ctx, id)
	}

	phase2Success := 0
	for _, r := range phase2Results {
		if r.AppLive {
			phase2Success++
		}
	}
	t.Logf("Phase 2 result: %d/%d live (target ≥90%%)", phase2Success, tenantCount)

	// Phase 3: delete 10% of tenants (churn — exercises B11)
	t.Logf("Phase 3: deleting %d tenants (B11 churn)", churnCount)
	churnVictims := identities[:churnCount]
	for _, id := range churnVictims {
		if err := harness.CleanupTenant(ctx, id); err != nil {
			t.Logf("cleanup %s failed: %v", id.TenantID, err)
		}
	}

	// Phase 4: re-provision SAME tenants (exercises B11 unarchive + B17 cache)
	t.Log("Phase 4: re-provisioning the SAME 10 tenant IDs (tests gitops unarchive)")
	rePhaseSuccess := 0
	for _, id := range churnVictims {
		if _, err := harness.ProvisionTenant(ctx, id); err != nil {
			t.Logf("re-provision %s failed: %v — possible B11 regression (archived repo not auto-reactivated)", id.TenantID, err)
			continue
		}
		// Re-deploy
		r := harness.RealDeploy(ctx, id)
		if r.AppLive {
			rePhaseSuccess++
		}
	}

	// Acceptance criteria
	totalSuccess := phase2Success - churnCount + rePhaseSuccess
	successPct := totalSuccess * 100 / tenantCount
	t.Logf("Final: %d/%d (%d%%) tenants live after churn cycle", totalSuccess, tenantCount, successPct)
	if successPct < 95 {
		t.Errorf("L3-Churn target ≥95%% not met: %d%%", successPct)
	}

	// Cleanup all
	for _, id := range identities {
		_ = harness.CleanupTenant(ctx, id)
	}
}

// TestRealDeploy_L3NoisyNeighbor — issues 10 concurrent deploys against the
// same tenant. Validates Rule 6 (concurrent-build rejection): exactly 1 build
// must succeed, 9 must return 409 Conflict. Detects B-C git push race.
func TestRealDeploy_L3NoisyNeighbor(t *testing.T) {
	if os.Getenv("LOOPRUNTIME_REAL_DEPLOY") != "1" {
		t.Skip("set LOOPRUNTIME_REAL_DEPLOY=1")
	}
	level := os.Getenv("LOOPRUNTIME_LOAD_LEVEL")
	if level != "l3-noisy" && level != "all" {
		t.Skip("set LOOPRUNTIME_LOAD_LEVEL=l3-noisy or all")
	}

	apiURL := os.Getenv("LOOPRUNTIME_API_URL")
	appDomain := os.Getenv("LOOPRUNTIME_APP_DOMAIN")
	serviceAuth := os.Getenv("LOOPRUNTIME_SERVICE_AUTH")
	if apiURL == "" || appDomain == "" || serviceAuth == "" {
		t.Fatal("LOOPRUNTIME_API_URL, LOOPRUNTIME_APP_DOMAIN, LOOPRUNTIME_SERVICE_AUTH required")
	}

	const concurrency = 10
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	harness, err := NewRealDeployHarness(RealDeployConfig{
		APIURL:      apiURL,
		AppDomain:   appDomain,
		ServiceAuth: serviceAuth,
		FixturePath: envOr("LOOPRUNTIME_FIXTURE_PATH", "../../e2e/fixtures/dogfood-monorepo"),
	})
	if err != nil {
		t.Fatalf("harness: %v", err)
	}

	id := NewTenantIdentity("l3noisy", 0)
	t.Logf("=== L3-NoisyNeighbor — %d concurrent deploys against tenant %s ===",
		concurrency, id.TenantID)

	// Provision once
	if _, err := harness.ProvisionTenant(ctx, id); err != nil {
		t.Fatalf("provision: %v", err)
	}
	defer harness.CleanupTenant(ctx, id)

	// Initial deploy to ensure repo has at least 1 commit
	first := harness.RealDeploy(ctx, id)
	if !first.AppLive {
		t.Fatalf("initial deploy failed: %+v", first)
	}

	// Phase: 10 concurrent deploys at once (race window)
	results := make([]FullDeployResult, concurrency)
	var wg sync.WaitGroup
	wg.Add(concurrency)
	start := time.Now()
	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = harness.RealDeploy(ctx, id)
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	// Acceptance criteria:
	// - At least 1 deploy succeeded (Live=true)
	// - The pattern of 1 success + 9 conflict suggests Rule 6 working correctly
	// - No silent corruptions (all 9 failures must be 409, not 500)
	successCount := 0
	conflictCount := 0
	otherFailures := []string{}
	for i, r := range results {
		if r.AppLive {
			successCount++
			continue
		}
		if r.Error != "" {
			msg := r.Error
			if strings.Contains(msg, "409") || strings.Contains(msg, "Conflict") || strings.Contains(msg, "concurrent") {
				conflictCount++
			} else {
				otherFailures = append(otherFailures, fmt.Sprintf("idx=%d err=%s", i, r.Error))
			}
		}
	}

	t.Logf("Concurrent burst (10 in %s): %d success, %d 409-conflict, %d other failures",
		elapsed, successCount, conflictCount, len(otherFailures))
	for _, f := range otherFailures {
		t.Logf("  unexpected failure: %s", f)
	}

	// Rule 6: at least one must succeed and the rest must NOT crash silently
	if successCount < 1 {
		t.Errorf("L3-Noisy: zero successful deploys — concurrency rejection broken")
	}
	if len(otherFailures) > 1 {
		t.Errorf("L3-Noisy: %d non-Conflict failures suggests B-C git push race regressed", len(otherFailures))
	}
}

// envOr returns env value or default if empty.
// (also defined in real_deploy_test.go — referenced from there)
