// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package load implements progressive load tests for the LoopZero platform.
//
// Usage:
//
//	# L0 — Baseline (current 2 tenants, single deploy)
//	LOOPZERO_LOAD_TEST=1 LOOPZERO_DRILL_API_URL=https://api.loopzero-drill.example LOOPZERO_AUTH_TOKEN=<token> \
//	  go test ./infra/tests/load/ -v -run TestL0_Baseline -timeout 10m
//
//	# L1 — 10 tenants, concurrent deploys
//	LOOPZERO_LOAD_TEST=1 LOOPZERO_DRILL_API_URL=https://api.loopzero-drill.example LOOPZERO_AUTH_TOKEN=<token> \
//	  go test ./infra/tests/load/ -v -run TestL1_10Tenants -timeout 30m
//
//	# L2 — 50 tenants, sustained load
//	LOOPZERO_LOAD_TEST=1 LOOPZERO_DRILL_API_URL=https://api.loopzero-drill.example LOOPZERO_AUTH_TOKEN=<token> \
//	  go test ./infra/tests/load/ -v -run TestL2_50Tenants -timeout 60m
//
// This test is SKIPPED unless LOOPZERO_LOAD_TEST=1 is set.
package load

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestL3_100Tenants(t *testing.T) {
	_, sim := requireLoadTestEnv(t)

	const tenantCount = 100
	const concurrency = 10 // 10 concurrent at a time to avoid overwhelming API
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Minute)
	defer cancel()

	t.Logf("L3 — %d tenants, %d concurrent deploys", tenantCount, concurrency)

	// Create projects in batches
	for i := 0; i < tenantCount; i++ {
		name := fmt.Sprintf("loadtest-%d", i)
		if err := sim.CreateProject(ctx, name); err != nil {
			t.Fatalf("Failed to create project %s: %v", name, err)
		}
	}
	t.Logf("  %d projects created", tenantCount)

	// Deploy with concurrency limiter
	var (
		results  []DeployResult
		mu       sync.Mutex
		success  int64
		failures int64
	)

	start := time.Now()
	sem := make(chan struct{}, concurrency)

	var wg sync.WaitGroup
	for i := 0; i < tenantCount; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()

			name := fmt.Sprintf("loadtest-%d", idx)
			r := sim.SimulateDeploy(ctx, name, "production")
			r.TenantIdx = idx

			mu.Lock()
			results = append(results, r)
			mu.Unlock()

			if r.Success {
				atomic.AddInt64(&success, 1)
			} else {
				atomic.AddInt64(&failures, 1)
			}

			t.Logf("  [%d/%d] %s: success=%v latency=%s",
				idx+1, tenantCount, name, r.Success, r.Latency.Total.Round(time.Second))
		}(i)
	}

	wg.Wait()
	totalDuration := time.Since(start)

	var totalLatencies, uploadLatencies []time.Duration
	for _, r := range results {
		totalLatencies = append(totalLatencies, r.Latency.Total)
		uploadLatencies = append(uploadLatencies, r.Latency.Upload)
	}

	report := &LoadTestResult{
		TotalDeploys:     len(results),
		SuccessCount:     success,
		FailureCount:     failures,
		Duration:         totalDuration,
		DeployLatencyP50: percentile(totalLatencies, 50),
		DeployLatencyP95: percentile(totalLatencies, 95),
		DeployLatencyP99: percentile(totalLatencies, 99),
		UploadLatencyP50: percentile(uploadLatencies, 50),
		Results:          results,
	}

	printReport(t, report)

	// L3 pass criteria
	failRate := float64(failures) / float64(max(success+failures, 1)) * 100
	if failRate > 10 {
		t.Errorf("L3 FAIL: failure rate %.1f%% exceeds 10%%", failRate)
	}
	if report.DeployLatencyP99 > 20*time.Minute {
		t.Errorf("L3 FAIL: deploy p99 %s exceeds 20min", report.DeployLatencyP99)
	}
}
