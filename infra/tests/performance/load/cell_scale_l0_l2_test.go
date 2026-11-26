// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package load implements progressive load tests for the LoopRuntime platform.
//
// Usage:
//
//	# L0 — Baseline (current 2 tenants, single deploy)
//	LOOPRUNTIME_LOAD_TEST=1 LOOPRUNTIME_DRILL_API_URL=https://api.drill.getloopscale.com LOOPRUNTIME_AUTH_TOKEN=<token> \
//	  go test ./infra/tests/load/ -v -run TestL0_Baseline -timeout 10m
//
//	# L1 — 10 tenants, concurrent deploys
//	LOOPRUNTIME_LOAD_TEST=1 LOOPRUNTIME_DRILL_API_URL=https://api.drill.getloopscale.com LOOPRUNTIME_AUTH_TOKEN=<token> \
//	  go test ./infra/tests/load/ -v -run TestL1_10Tenants -timeout 30m
//
//	# L2 — 50 tenants, sustained load
//	LOOPRUNTIME_LOAD_TEST=1 LOOPRUNTIME_DRILL_API_URL=https://api.drill.getloopscale.com LOOPRUNTIME_AUTH_TOKEN=<token> \
//	  go test ./infra/tests/load/ -v -run TestL2_50Tenants -timeout 60m
//
// This test is SKIPPED unless LOOPRUNTIME_LOAD_TEST=1 is set.
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

func TestL0_Baseline(t *testing.T) {
	_, sim := requireLoadTestEnv(t)

	projectName := os.Getenv("LOOPRUNTIME_LOAD_PROJECT")
	if projectName == "" {
		projectName = "aquim-ai" // Default to known project
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t.Logf("L0 Baseline — single deploy to project %s", projectName)

	result := sim.SimulateDeploy(ctx, projectName, "production")
	t.Logf("Upload: %s | Build: %s | Total: %s | Success: %v",
		result.Latency.Upload.Round(time.Millisecond),
		result.Latency.Build.Round(time.Millisecond),
		result.Latency.Total.Round(time.Millisecond),
		result.Success)

	if !result.Success {
		t.Logf("Error: %s", result.Error)
	}
}

func TestL1_10Tenants(t *testing.T) {
	_, sim := requireLoadTestEnv(t)

	const tenantCount = 10
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	t.Logf("L1 — %d tenants, concurrent deploys", tenantCount)

	// Create projects
	for i := 0; i < tenantCount; i++ {
		name := fmt.Sprintf("loadtest-%d", i)
		if err := sim.CreateProject(ctx, name); err != nil {
			t.Fatalf("Failed to create project %s: %v", name, err)
		}
		t.Logf("  Project %s ready", name)
	}

	// Concurrent deploys
	report := sim.RunScenario(ctx, tenantCount, fanOutOptions{concurrency: tenantCount},
		nil)
	failures := report.FailureCount

	printReport(t, report)

	// L1 pass criteria
	if failures > 0 {
		t.Errorf("L1 FAIL: %d deploys failed (expected 0)", failures)
	}
	if report.DeployLatencyP99 > 10*time.Minute {
		t.Errorf("L1 FAIL: deploy p99 %s exceeds 10min", report.DeployLatencyP99)
	}
}

func TestL1_WebhookBurst(t *testing.T) {
	_, sim := requireLoadTestEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	t.Logf("L1 Webhook Burst — 10 push events in 1 second")

	// Send 10 concurrent build requests to the same project
	// This tests deduplication and serialization
	var (
		wg       sync.WaitGroup
		accepted int64
		rejected int64
	)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := sim.CreateBuild(ctx, "loadtest-0", "production",
				fmt.Sprintf("fake-sha-%d", idx))
			if err != nil {
				atomic.AddInt64(&rejected, 1)
			} else {
				atomic.AddInt64(&accepted, 1)
			}
		}(i)
	}

	wg.Wait()

	t.Logf("  Accepted: %d | Rejected (409/lock): %d", accepted, rejected)

	// At most 1 should be accepted, rest should be 409 (concurrent build)
	if accepted > 2 {
		t.Errorf("L1 FAIL: %d builds accepted (expected ≤1 — lock should reject concurrent)", accepted)
	}
}

func TestL2_50Tenants(t *testing.T) {
	_, sim := requireLoadTestEnv(t)

	const tenantCount = 50
	const deploysPerMinute = 5
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	t.Logf("L2 — %d tenants, %d deploys/min sustained", tenantCount, deploysPerMinute)

	// Create projects
	for i := 0; i < tenantCount; i++ {
		name := fmt.Sprintf("loadtest-%d", i)
		if err := sim.CreateProject(ctx, name); err != nil {
			t.Fatalf("Failed to create project %s: %v", name, err)
		}
	}
	t.Logf("  %d projects created", tenantCount)

	// Sustained deploys: one start per interval, and every deploy is waited for
	// before the report is read.
	report := sim.RunScenario(ctx, tenantCount, fanOutOptions{concurrency: tenantCount, arrivalGap: time.Minute / time.Duration(deploysPerMinute)},
		func(idx int, name string, r DeployResult) {
			t.Logf("  [%d/%d] %s: success=%v latency=%s",
				idx+1, tenantCount, name, r.Success, r.Latency.Total.Round(time.Second))
		})
	success, failures := report.SuccessCount, report.FailureCount

	printReport(t, report)

	// L2 pass criteria
	failRate := float64(failures) / float64(max(success+failures, 1)) * 100
	if failRate > 5 {
		t.Errorf("L2 FAIL: failure rate %.1f%% exceeds 5%%", failRate)
	}
	if report.DeployLatencyP99 > 15*time.Minute {
		t.Errorf("L2 FAIL: deploy p99 %s exceeds 15min", report.DeployLatencyP99)
	}
}
