// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package load

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRealDeploy_L1_10Tenants is the Phase 0.0 DoD test from
// docs/plans/scale-50k-plan.md:
//
//   "Running TestL1_10Tenants creates 10 real tenants, deploys real
//   apps, verifies all 10 URLs return HTTP 200, then cleans up.
//   No fake bundles. No SQL seeds."
//
// It replaces the previous TestL1_10Tenants which used fake bundles
// and never verified that the app was actually live.
//
// Environment variables:
//   LOOPZERO_REAL_DEPLOY=1         — gate (skip if unset)
//   LOOPZERO_API_URL               — required (e.g., https://api.dev.loopzero.example)
//   LOOPZERO_APP_DOMAIN            — required (e.g., dev.loopzero.example)
//   LOOPZERO_SERVICE_AUTH          — required (service auth bypass token)
//   LOOPZERO_FIXTURE_PATH          — optional (default: ../e2e/fixtures/hello-server)
func TestRealDeploy_L1_10Tenants(t *testing.T) {
	if os.Getenv("LOOPZERO_REAL_DEPLOY") != "1" {
		t.Skip("skipping real deploy test: set LOOPZERO_REAL_DEPLOY=1")
	}

	apiURL := os.Getenv("LOOPZERO_API_URL")
	if apiURL == "" {
		t.Fatal("LOOPZERO_API_URL required")
	}
	appDomain := os.Getenv("LOOPZERO_APP_DOMAIN")
	if appDomain == "" {
		t.Fatal("LOOPZERO_APP_DOMAIN required")
	}
	serviceAuth := os.Getenv("LOOPZERO_SERVICE_AUTH")
	if serviceAuth == "" {
		t.Fatal("LOOPZERO_SERVICE_AUTH required")
	}
	fixturePath := os.Getenv("LOOPZERO_FIXTURE_PATH")
	if fixturePath == "" {
		fixturePath = "../e2e/fixtures/hello-server"
	}

	const tenantCount = 10
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	t.Logf("=== REAL DEPLOY L1 — %d tenants ===", tenantCount)
	t.Logf("  API:     %s", apiURL)
	t.Logf("  Domain:  %s", appDomain)
	t.Logf("  Fixture: %s", fixturePath)

	// Phase 1: Build the real git bundle from fixture (once, reused)
	harness, err := NewRealDeployHarness(RealDeployConfig{
		APIURL:      apiURL,
		AppDomain:   appDomain,
		ServiceAuth: serviceAuth,
		FixturePath: fixturePath,
	})
	if err != nil {
		t.Fatalf("failed to create harness: %v", err)
	}
	t.Logf("  Bundle:  %d bytes (real git bundle from fixture)", len(harness.bundleBytes))

	// Phase 2: Provision tenants (create via API, not SQL)
	identities := make([]TenantIdentity, tenantCount)
	for i := 0; i < tenantCount; i++ {
		identities[i] = NewTenantIdentity("l1", i)
	}

	t.Log("--- Provisioning tenants ---")
	for i, id := range identities {
		dur, err := harness.ProvisionTenant(ctx, id)
		if err != nil {
			t.Logf("  tn-%d provision FAILED: %v", i, err)
			// Don't fatal — the tenant might already exist from a prior run.
			// The deploy step will catch real failures.
		} else {
			t.Logf("  tn-%d provisioned in %s", i, dur.Round(time.Second))
		}
	}

	// Phase 3: Deploy all tenants concurrently
	t.Log("--- Deploying (real pipeline) ---")
	var (
		results    = make([]FullDeployResult, tenantCount)
		wg         sync.WaitGroup
		successCnt int64
		failCnt    int64
		liveCnt    int64
	)

	for i, id := range identities {
		wg.Add(1)
		go func(idx int, tenant TenantIdentity) {
			defer wg.Done()
			r := harness.RealDeploy(ctx, tenant)
			results[idx] = r
			if r.Success {
				atomic.AddInt64(&successCnt, 1)
			} else {
				atomic.AddInt64(&failCnt, 1)
			}
			if r.AppLive {
				atomic.AddInt64(&liveCnt, 1)
			}
			t.Logf("  tn-%d: build=%s success=%v live=%v time=%s err=%s",
				idx, r.BuildID, r.Success, r.AppLive,
				r.TotalTime.Round(time.Second), r.Error)
		}(i, id)
	}
	wg.Wait()

	// Phase 4: Report
	t.Log("")
	t.Log("═══════════════════════════════════════════════════════")
	t.Log("  REAL DEPLOY L1 — RESULTS")
	t.Log("═══════════════════════════════════════════════════════")

	var totalUpload, totalBuild, totalVerify time.Duration
	for _, r := range results {
		totalUpload += r.Stages.Upload
		totalBuild += r.Stages.BuildComplete
		totalVerify += r.Stages.AppVerify
	}

	t.Logf("  Tenants:       %d", tenantCount)
	t.Logf("  Succeeded:     %d / %d", successCnt, tenantCount)
	t.Logf("  Apps Live:     %d / %d (HTTP 200 verified)", liveCnt, tenantCount)
	t.Logf("  Failed:        %d", failCnt)
	t.Logf("  Avg upload:    %s", (totalUpload / time.Duration(tenantCount)).Round(time.Millisecond))
	t.Logf("  Avg build:     %s", (totalBuild / time.Duration(tenantCount)).Round(time.Second))
	t.Logf("  Avg verify:    %s", (totalVerify / time.Duration(tenantCount)).Round(time.Second))

	for i, r := range results {
		status := "✅"
		if !r.Success {
			status = "❌"
		}
		live := ""
		if r.AppLive {
			live = " [LIVE]"
		}
		t.Logf("  %s tn-%d: %s %s%s", status, i, r.BuildID, r.TotalTime.Round(time.Second), live)
		if r.Error != "" {
			t.Logf("     └ %s", r.Error)
		}
	}
	t.Log("═══════════════════════════════════════════════════════")

	// Phase 5: Assertions per scale-50k-plan.md
	if failCnt > 0 {
		t.Errorf("L1 FAIL: %d/%d deployments failed (target: 0%%)", failCnt, tenantCount)
	}
	if liveCnt < int64(tenantCount) {
		t.Errorf("L1 FAIL: only %d/%d apps verified live (target: 100%%)", liveCnt, tenantCount)
	}

	// Phase 6: Cleanup
	t.Log("--- Cleanup ---")
	cleanupErrors := 0
	for i, id := range identities {
		if err := harness.CleanupTenant(ctx, id); err != nil {
			t.Logf("  tn-%d cleanup failed: %v", i, err)
			cleanupErrors++
		}
	}
	if cleanupErrors > 0 {
		t.Logf("  %d cleanup errors (non-fatal — orphans may remain)", cleanupErrors)
	} else {
		t.Log("  All tenants cleaned up")
	}
}

// TestRealDeploy_L2_50Tenants runs 50 real deploys with sustained arrival rate.
// Gated on LOOPZERO_REAL_DEPLOY=1 + LOOPZERO_LOAD_LEVEL=l2.
func TestRealDeploy_L2_50Tenants(t *testing.T) {
	if os.Getenv("LOOPZERO_REAL_DEPLOY") != "1" {
		t.Skip("skipping real deploy test: set LOOPZERO_REAL_DEPLOY=1")
	}
	if os.Getenv("LOOPZERO_LOAD_LEVEL") != "l2" && os.Getenv("LOOPZERO_LOAD_LEVEL") != "all" {
		t.Skip("skipping L2: set LOOPZERO_LOAD_LEVEL=l2 or LOOPZERO_LOAD_LEVEL=all")
	}
	t.Log("L2 — 50 tenants sustained (real deploy harness)")
	t.Skip("TODO: implement when L1 passes on real infra")
}

// TestRealDeploy_L3_100Tenants runs 100 real deploys.
func TestRealDeploy_L3_100Tenants(t *testing.T) {
	if os.Getenv("LOOPZERO_REAL_DEPLOY") != "1" {
		t.Skip("skipping real deploy test: set LOOPZERO_REAL_DEPLOY=1")
	}
	if os.Getenv("LOOPZERO_LOAD_LEVEL") != "l3" && os.Getenv("LOOPZERO_LOAD_LEVEL") != "all" {
		t.Skip("skipping L3: set LOOPZERO_LOAD_LEVEL=l3 or LOOPZERO_LOAD_LEVEL=all")
	}
	t.Log("L3 — 100 tenants sustained (real deploy harness)")
	t.Skip("TODO: implement when L2 passes")
}

// TestPrepareFixtureBundle_HelloServer verifies the bundle builder works
// with the hello-server fixture. This is a unit-level test (no infra
// needed) that proves the fixture → git bundle pipeline works.
func TestPrepareFixtureBundle_HelloServer(t *testing.T) {
	bundle, err := PrepareFixtureBundle("../e2e/fixtures/hello-server")
	if err != nil {
		t.Fatalf("PrepareFixtureBundle: %v", err)
	}
	if len(bundle) < 100 {
		t.Errorf("bundle too small: %d bytes (expected real git bundle)", len(bundle))
	}
	// Git bundles start with "# v2 git bundle" or "# v3 git bundle"
	header := string(bundle[:20])
	if !containsString(header, "v2 git bundle") && !containsString(header, "v3 git bundle") {
		t.Errorf("bundle does not start with git bundle header: %q", header)
	}
	t.Logf("bundle: %d bytes, header: %q", len(bundle), header)
}

func containsString(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestNewTenantIdentity verifies the identity generator produces
// deterministic, collision-free identities.
func TestNewTenantIdentity(t *testing.T) {
	id0 := NewTenantIdentity("l1", 0)
	id1 := NewTenantIdentity("l1", 1)
	id0b := NewTenantIdentity("l2", 0)

	if id0.TenantID == id1.TenantID {
		t.Error("same-level different-index must produce different tenant IDs")
	}
	if id0.TenantID == id0b.TenantID {
		t.Error("different-level same-index must produce different tenant IDs")
	}
	if id0.Email == "" || id0.UserID == "" || id0.ProjectName == "" {
		t.Error("identity fields must not be empty")
	}

	// Deterministic: same input → same output
	id0c := NewTenantIdentity("l1", 0)
	if id0 != id0c {
		t.Error("same input must produce identical identity")
	}

	_ = fmt.Sprintf // keep import
}
