// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package load

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRealDeploy_L1_10Tenants is the Phase 0.0 DoD test from
// docs/plans/scale-50k-plan.md:
//
//	"Running TestL1_10Tenants creates 10 real tenants, deploys real
//	apps, verifies all 10 URLs return HTTP 200, then cleans up.
//	No fake bundles. No SQL seeds."
//
// It replaces the previous TestL1_10Tenants which used fake bundles
// and never verified that the app was actually live.
//
// Environment variables:
//
//	LOOPZERO_REAL_DEPLOY=1         — gate (skip if unset)
//	LOOPZERO_API_URL               — required (e.g., https://api.dev.loopzero.example)
//	LOOPZERO_APP_DOMAIN            — required (e.g., dev.loopzero.example)
//	LOOPZERO_SERVICE_AUTH          — required (service auth bypass token)
//	LOOPZERO_FIXTURE_PATH          — optional (default: ../../e2e/fixtures/hello-server)
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
		fixturePath = "../../e2e/fixtures/hello-server"
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
	// Limit concurrency to 3 to avoid overwhelming port-forward / CF Tunnel.
	// When running in-cluster (k6 Job), this can be increased to 10+.
	results, tally := harness.RunScenario(ctx, identities, fanOutOptions{concurrency: 3},
		func(idx int, r FullDeployResult) {
			t.Logf("  tn-%d: build=%s success=%v live=%v time=%s err=%s",
				idx, r.BuildID, r.Success, r.AppLive,
				r.TotalTime.Round(time.Second), r.Error)
		})
	successCnt, failCnt, liveCnt := tally.Succeeded, tally.Failed, tally.Live

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
// scale-50k-plan Phase 1.3: validates sustained throughput at 50 tenants.
// Deploys 5 at a time (arrival rate) over ~30 minutes.
// Gated on LOOPZERO_REAL_DEPLOY=1 + LOOPZERO_LOAD_LEVEL=l2.
func TestRealDeploy_L2_50Tenants(t *testing.T) {
	if os.Getenv("LOOPZERO_REAL_DEPLOY") != "1" {
		t.Skip("skipping real deploy test: set LOOPZERO_REAL_DEPLOY=1")
	}
	if os.Getenv("LOOPZERO_LOAD_LEVEL") != "l2" && os.Getenv("LOOPZERO_LOAD_LEVEL") != "all" {
		t.Skip("skipping L2: set LOOPZERO_LOAD_LEVEL=l2 or LOOPZERO_LOAD_LEVEL=all")
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

	const tenantCount = 50
	const concurrency = 5 // sustained arrival: 5 concurrent deploys
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	harness, err := NewRealDeployHarness(RealDeployConfig{
		APIURL:      apiURL,
		AppDomain:   appDomain,
		ServiceAuth: serviceAuth,
		FixturePath: envOr("LOOPZERO_FIXTURE_PATH", "../../e2e/fixtures/hello-server"),
	})
	if err != nil {
		t.Fatalf("harness: %v", err)
	}

	t.Logf("=== REAL DEPLOY L2 — %d tenants, concurrency %d ===", tenantCount, concurrency)

	// Phase 1: Provision all tenants
	identities := make([]TenantIdentity, tenantCount)
	for i := 0; i < tenantCount; i++ {
		identities[i] = NewTenantIdentity("l2", i)
	}

	t.Log("--- Provisioning tenants ---")
	provSem := make(chan struct{}, 5)
	var provWg sync.WaitGroup
	for i, id := range identities {
		provWg.Add(1)
		go func(idx int, tenant TenantIdentity) {
			defer provWg.Done()
			provSem <- struct{}{}
			defer func() { <-provSem }()
			dur, err := harness.ProvisionTenant(ctx, tenant)
			if err != nil {
				t.Logf("  tn-%d provision: %v", idx, err)
			} else {
				t.Logf("  tn-%d provisioned in %s", idx, dur.Round(time.Second))
			}
		}(i, id)
	}
	provWg.Wait()

	// Phase 2: Deploy with sustained concurrency
	t.Log("--- Deploying (sustained arrival rate) ---")
	// Stagger starts by 30s to simulate sustained arrival (not burst).
	_, tally := harness.RunScenario(ctx, identities,
		fanOutOptions{concurrency: concurrency, arrivalGap: 30 * time.Second},
		func(idx int, r FullDeployResult) {
			t.Logf("  tn-%d: success=%v live=%v time=%s", idx, r.Success, r.AppLive, r.TotalTime.Round(time.Second))
		})
	successCnt, failCnt, liveCnt := tally.Succeeded, tally.Failed, tally.Live

	// Phase 3: Report
	t.Log("")
	t.Log("═══════════════════════════════════════════════════════")
	t.Logf("  REAL DEPLOY L2 — %d TENANTS — RESULTS", tenantCount)
	t.Log("═══════════════════════════════════════════════════════")
	t.Logf("  Succeeded:     %d / %d", successCnt, tenantCount)
	t.Logf("  Apps Live:     %d / %d (HTTP 200 verified)", liveCnt, tenantCount)
	t.Logf("  Failed:        %d", failCnt)
	t.Log("═══════════════════════════════════════════════════════")

	// Assertions per scale-50k-plan
	if failCnt > 0 {
		t.Errorf("L2 FAIL: %d/%d deployments failed (target: 0%%)", failCnt, tenantCount)
	}
	if liveCnt < int64(tenantCount) {
		t.Errorf("L2 FAIL: only %d/%d apps verified live (target: 100%%)", liveCnt, tenantCount)
	}

	// Phase 4: Cleanup
	t.Log("--- Cleanup ---")
	for i, id := range identities {
		if err := harness.CleanupTenant(ctx, id); err != nil {
			t.Logf("  tn-%d cleanup: %v", i, err)
		}
	}
}

// TestRealDeploy_L3_100Tenants runs 100 real deploys with noisy neighbor.
// scale-50k-plan Phase 2.1: validates cell capacity under contention.
// Includes a noisy neighbor scenario (1 tenant doing 10 rapid deploys).
// Gated on LOOPZERO_REAL_DEPLOY=1 + LOOPZERO_LOAD_LEVEL=l3.
func TestRealDeploy_L3_100Tenants(t *testing.T) {
	if os.Getenv("LOOPZERO_REAL_DEPLOY") != "1" {
		t.Skip("skipping real deploy test: set LOOPZERO_REAL_DEPLOY=1")
	}
	if os.Getenv("LOOPZERO_LOAD_LEVEL") != "l3" && os.Getenv("LOOPZERO_LOAD_LEVEL") != "all" {
		t.Skip("skipping L3: set LOOPZERO_LOAD_LEVEL=l3 or LOOPZERO_LOAD_LEVEL=all")
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

	const tenantCount = 100
	// Concurrency 30 — stress test beyond base admission gate (bumped to 40 for this).
	// All blockers fixed: local-path workspace (#21/#40), argo quota 48CPU (#41),
	// operator RBAC (#36/#37), cosign HOME (#39). Build cluster scaled to ~7 nodes
	// (2 default + 3 overflow + 2 c30-prep) ≈ 24 CPU allocatable.
	const concurrency = 30
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
	defer cancel()

	harness, err := NewRealDeployHarness(RealDeployConfig{
		APIURL:      apiURL,
		AppDomain:   appDomain,
		ServiceAuth: serviceAuth,
		FixturePath: envOr("LOOPZERO_FIXTURE_PATH", "../../e2e/fixtures/hello-server"),
	})
	if err != nil {
		t.Fatalf("harness: %v", err)
	}

	t.Logf("=== REAL DEPLOY L3 — %d tenants, concurrency %d ===", tenantCount, concurrency)

	// Phase 1: Provision
	identities := make([]TenantIdentity, tenantCount)
	for i := 0; i < tenantCount; i++ {
		identities[i] = NewTenantIdentity("l3", i)
	}

	t.Log("--- Provisioning tenants ---")
	provSem := make(chan struct{}, 10)
	var provWg sync.WaitGroup
	for i, id := range identities {
		provWg.Add(1)
		go func(idx int, tenant TenantIdentity) {
			defer provWg.Done()
			provSem <- struct{}{}
			defer func() { <-provSem }()
			harness.ProvisionTenant(ctx, tenant)
		}(i, id)
	}
	provWg.Wait()
	t.Log("  All tenants provisioned")

	// Phase 2: Deploy all tenants + noisy neighbor on tn-0
	t.Log("--- Deploying (concurrent + noisy neighbor on tn-0) ---")

	var (
		successCnt int64
		liveCnt    int64
	)

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	// Normal tenants: 1 deploy each
	for i, id := range identities {
		wg.Add(1)
		go func(idx int, tenant TenantIdentity) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r := harness.RealDeploy(ctx, tenant)
			if r.Success {
				atomic.AddInt64(&successCnt, 1)
			}
			if r.AppLive {
				atomic.AddInt64(&liveCnt, 1)
			}
			if !r.Success {
				t.Logf("  tn-%d: FAILED %s", idx, r.Error)
			}
		}(i, id)
	}

	// Noisy neighbor: tn-0 does 10 rapid re-deploys (tests per-tenant
	// admission quota and build supersede behavior).
	for j := 0; j < 10; j++ {
		wg.Add(1)
		go func(attempt int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// Small stagger to simulate rapid pushes
			// intentional-sleep: load-tier pacing (interleaved deploy submission)
			time.Sleep(time.Duration(attempt) * 5 * time.Second)
			r := harness.RealDeploy(ctx, identities[0])
			// We expect some of these to be superseded (409) — that's correct behavior.
			if r.Success {
				t.Logf("  noisy-neighbor attempt %d: succeeded", attempt)
			} else {
				t.Logf("  noisy-neighbor attempt %d: %s (expected: some fail with 409)", attempt, r.Error)
			}
		}(j)
	}
	wg.Wait()

	// Phase 3: Report
	t.Log("")
	t.Log("═══════════════════════════════════════════════════════")
	t.Logf("  REAL DEPLOY L3 — %d TENANTS — RESULTS", tenantCount)
	t.Log("═══════════════════════════════════════════════════════")
	t.Logf("  Succeeded:     %d / %d", successCnt, tenantCount)
	t.Logf("  Apps Live:     %d / %d (HTTP 200 verified)", liveCnt, tenantCount)
	t.Log("═══════════════════════════════════════════════════════")

	// L3 allows < 0.5% failure rate per scale plan
	maxFailures := int64(tenantCount) / 200 // 0.5% of 100 = 0
	if int64(tenantCount)-successCnt > maxFailures {
		t.Errorf("L3 FAIL: %d/%d succeeded (target: >= 99.5%%)", successCnt, tenantCount)
	}

	// Cleanup
	t.Log("--- Cleanup ---")
	for _, id := range identities {
		harness.CleanupTenant(ctx, id)
	}
}

// TestRealDeploy_L3Realistic_100Tenants runs 100 real deploys against the
// dogfood-monorepo fixture — a representative full-stack SaaS layout
// (NestJS API + Next.js web + Express gateway + Node worker + 2 shared
// packages, Turborepo). This validates that LoopZero cell capacity holds for
// realistic workloads, not just the hello-server smoke. Sprint 8 / CHG-002b
// stabilization deliverable: hello-server only proved "the platform accepts
// 100 trivial servers"; L3-realistic proves "the platform deploys 100 real
// SaaS stacks".
//
// Trade-offs vs L3 hello-server:
//   - 4 apps per tenant (vs 1) → up to 400 concurrent build images
//   - Concurrency capped at 15 (vs 30) so build cluster does not saturate
//     beyond observed L3 ceiling. Bump via LOOPZERO_L3REAL_CONCURRENCY.
//   - Verify URL targets gateway (Express public ingress) by default; use
//     LOOPZERO_VERIFY_APP_NAME=api/web/gateway to switch.
//
// Gated on LOOPZERO_REAL_DEPLOY=1 + LOOPZERO_LOAD_LEVEL=l3-realistic.
// Default fixture: ../../e2e/fixtures/dogfood-monorepo (override via
// LOOPZERO_FIXTURE_PATH).
func TestRealDeploy_L3Realistic_100Tenants(t *testing.T) {
	if os.Getenv("LOOPZERO_REAL_DEPLOY") != "1" {
		t.Skip("skipping real deploy test: set LOOPZERO_REAL_DEPLOY=1")
	}
	if os.Getenv("LOOPZERO_LOAD_LEVEL") != "l3-realistic" && os.Getenv("LOOPZERO_LOAD_LEVEL") != "all" {
		t.Skip("skipping L3-realistic: set LOOPZERO_LOAD_LEVEL=l3-realistic or LOOPZERO_LOAD_LEVEL=all")
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

	const tenantCount = 100
	concurrency := 15
	if v := os.Getenv("LOOPZERO_L3REAL_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			concurrency = n
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()

	// Default to gateway (the Express HTTP entry point that proxies to api
	// and serves health on root). hello-server's `api` does not exist in
	// dogfood-monorepo's URL space; gateway is the public-facing app.
	if os.Getenv("LOOPZERO_VERIFY_APP_NAME") == "" {
		t.Setenv("LOOPZERO_VERIFY_APP_NAME", "gateway")
	}

	harness, err := NewRealDeployHarness(RealDeployConfig{
		APIURL:      apiURL,
		AppDomain:   appDomain,
		ServiceAuth: serviceAuth,
		FixturePath: envOr("LOOPZERO_FIXTURE_PATH", "../../e2e/fixtures/dogfood-monorepo"),
	})
	if err != nil {
		t.Fatalf("harness: %v", err)
	}

	t.Logf("=== REAL DEPLOY L3-realistic — %d tenants × dogfood-monorepo (4 apps), concurrency %d ===", tenantCount, concurrency)

	// Phase 1: Provision
	identities := make([]TenantIdentity, tenantCount)
	for i := 0; i < tenantCount; i++ {
		identities[i] = NewTenantIdentity("l3r", i)
	}

	t.Log("--- Provisioning tenants ---")
	provSem := make(chan struct{}, 10)
	var provWg sync.WaitGroup
	for i, id := range identities {
		provWg.Add(1)
		go func(idx int, tenant TenantIdentity) {
			defer provWg.Done()
			provSem <- struct{}{}
			defer func() { <-provSem }()
			harness.ProvisionTenant(ctx, tenant)
		}(i, id)
	}
	provWg.Wait()
	t.Log("  All tenants provisioned")

	// Phase 2: Deploy all tenants. No noisy neighbor here — the fixture's
	// 4-app DAG already exercises the per-tenant build admission gate
	// because each deploy submits 4 build images concurrently.
	t.Log("--- Deploying full-stack tenants ---")

	_, tally := harness.RunScenario(ctx, identities, fanOutOptions{concurrency: concurrency},
		func(idx int, r FullDeployResult) {
			if !r.Success {
				t.Logf("  tn-%d: FAILED %s", idx, r.Error)
			}
		})
	successCnt, liveCnt := tally.Succeeded, tally.Live

	// Phase 3: Report
	t.Log("")
	t.Log("═══════════════════════════════════════════════════════")
	t.Logf("  REAL DEPLOY L3-realistic — %d FULL-STACK TENANTS — RESULTS", tenantCount)
	t.Log("═══════════════════════════════════════════════════════")
	t.Logf("  Succeeded:     %d / %d", successCnt, tenantCount)
	t.Logf("  Apps Live:     %d / %d (HTTP 200 verified on %s)", liveCnt, tenantCount, os.Getenv("LOOPZERO_VERIFY_APP_NAME"))
	t.Log("═══════════════════════════════════════════════════════")

	// L3-realistic accepts >= 95% (Sprint 8 multi-cell Gate #1 progress
	// metric; the cell-architecture decision was recorded in meeting
	// MEET-2026-04-17-051 — `git show 57e8a5968:docs/meetings/records/MEET-2026-04-17-051.md`
	// — and its ADR document never entered this tree). hello-server L3 demands 99.5% because its DAG is trivial;
	// realistic 4-app builds add legitimate failure surface (npm registry
	// flakes, transient OOMs on Next.js build). 95% is the documented
	// gate threshold — tighten as we drive root causes out.
	maxFailures := int64(tenantCount) * 5 / 100 // 5%
	if int64(tenantCount)-successCnt > maxFailures {
		t.Errorf("L3-realistic FAIL: %d/%d succeeded (target: >= 95%%)", successCnt, tenantCount)
	}

	// Cleanup
	t.Log("--- Cleanup ---")
	for _, id := range identities {
		harness.CleanupTenant(ctx, id)
	}
}

// TestRealDeploy_L4_500Tenants runs 500 real deploys — pushes one cell
// to its validated limit. scale-50k-plan Phase 2.2.
// Gated on LOOPZERO_REAL_DEPLOY=1 + LOOPZERO_LOAD_LEVEL=l4.
func TestRealDeploy_L4_500Tenants(t *testing.T) {
	if os.Getenv("LOOPZERO_REAL_DEPLOY") != "1" {
		t.Skip("skipping real deploy test: set LOOPZERO_REAL_DEPLOY=1")
	}
	if os.Getenv("LOOPZERO_LOAD_LEVEL") != "l4" && os.Getenv("LOOPZERO_LOAD_LEVEL") != "all" {
		t.Skip("skipping L4: set LOOPZERO_LOAD_LEVEL=l4 or LOOPZERO_LOAD_LEVEL=all")
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

	const tenantCount = 500
	const concurrency = 20
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()

	harness, err := NewRealDeployHarness(RealDeployConfig{
		APIURL:      apiURL,
		AppDomain:   appDomain,
		ServiceAuth: serviceAuth,
		FixturePath: envOr("LOOPZERO_FIXTURE_PATH", "../../e2e/fixtures/hello-server"),
	})
	if err != nil {
		t.Fatalf("harness: %v", err)
	}

	t.Logf("=== REAL DEPLOY L4 — %d tenants, concurrency %d ===", tenantCount, concurrency)

	// Phase 1: Provision in batches of 50
	identities := make([]TenantIdentity, tenantCount)
	for i := 0; i < tenantCount; i++ {
		identities[i] = NewTenantIdentity("l4", i)
	}

	t.Log("--- Provisioning tenants (batches of 50) ---")
	batchSize := 50
	for batch := 0; batch < tenantCount; batch += batchSize {
		end := batch + batchSize
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
		t.Logf("  Batch %d-%d provisioned", batch, end-1)
	}

	// Phase 2: Deploy with sustained concurrency
	t.Log("--- Deploying (sustained) ---")
	_, tally := harness.RunScenario(ctx, identities, fanOutOptions{concurrency: concurrency},
		func(idx int, r FullDeployResult) {
			// Log failures and every 50th success for progress
			if !r.Success || idx%50 == 0 {
				t.Logf("  tn-%d: success=%v live=%v time=%s", idx, r.Success, r.AppLive, r.TotalTime.Round(time.Second))
			}
		})
	successCnt, liveCnt := tally.Succeeded, tally.Live

	// Phase 3: Report
	t.Log("")
	t.Log("═══════════════════════════════════════════════════════")
	t.Logf("  REAL DEPLOY L4 — %d TENANTS — RESULTS", tenantCount)
	t.Log("═══════════════════════════════════════════════════════")
	t.Logf("  Succeeded:     %d / %d", successCnt, tenantCount)
	t.Logf("  Apps Live:     %d / %d (HTTP 200 verified)", liveCnt, tenantCount)
	t.Log("═══════════════════════════════════════════════════════")

	// L4 allows < 1% failure rate per scale plan
	maxFailures := int64(tenantCount) / 100 // 1% of 500 = 5
	if int64(tenantCount)-successCnt > maxFailures {
		t.Errorf("L4 FAIL: %d/%d succeeded (target: >= 99%%)", successCnt, tenantCount)
	}

	// Cleanup
	t.Log("--- Cleanup ---")
	for _, id := range identities {
		harness.CleanupTenant(ctx, id)
	}
}

// TestPrepareFixtureBundle_HelloServer verifies the bundle builder works
// with the hello-server fixture. This is a unit-level test (no infra
// needed) that proves the fixture → git bundle pipeline works.
func TestPrepareFixtureBundle_HelloServer(t *testing.T) {
	bundle, err := PrepareFixtureBundle("../../e2e/fixtures/hello-server")
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
