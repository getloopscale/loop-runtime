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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestL4_LockStress(t *testing.T) {
	_, sim := requireLoadTestEnv(t)

	const concurrent = 50
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t.Logf("L4 Lock Stress — %d concurrent builds for same project", concurrent)

	projectName := "loadtest-lock-stress"
	if err := sim.CreateProject(ctx, projectName); err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	var (
		wg       sync.WaitGroup
		accepted int64
		rejected int64
		errors   int64
	)

	start := time.Now()
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := sim.CreateBuild(ctx, projectName, "production",
				fmt.Sprintf("stress-sha-%d", idx))
			if err != nil {
				// 409 = lock contention (expected), other errors = unexpected
				errStr := err.Error()
				if strings.Contains(errStr, "409") || strings.Contains(errStr, "conflict") || strings.Contains(errStr, "already") {
					atomic.AddInt64(&rejected, 1)
				} else {
					atomic.AddInt64(&errors, 1)
					t.Logf("  Unexpected error: %v", err)
				}
			} else {
				atomic.AddInt64(&accepted, 1)
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	t.Logf("═══════════════════════════════════════════")
	t.Logf("  LOCK STRESS REPORT")
	t.Logf("═══════════════════════════════════════════")
	t.Logf("  Concurrent:  %d", concurrent)
	t.Logf("  Accepted:    %d (should be exactly 1)", accepted)
	t.Logf("  Rejected:    %d (409 lock contention)", rejected)
	t.Logf("  Errors:      %d (unexpected failures)", errors)
	t.Logf("  Duration:    %s", duration.Round(time.Millisecond))
	t.Logf("═══════════════════════════════════════════")

	// Exactly 1 build should be accepted, rest rejected by lock
	if accepted > 1 {
		t.Errorf("L4 FAIL: %d builds accepted (expected exactly 1 — lock broken)", accepted)
	}
	if errors > 0 {
		t.Errorf("L4 FAIL: %d unexpected errors (not 409 rejections)", errors)
	}
}

func TestL4_500Tenants(t *testing.T) {
	_, sim := requireLoadTestEnv(t)

	const tenantCount = 500
	const concurrency = 20 // 20 concurrent deploys
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Minute)
	defer cancel()

	t.Logf("L4 — %d tenants, %d concurrent, 2 cells", tenantCount, concurrency)

	// Create projects in batches of 50
	for batch := 0; batch < tenantCount; batch += 50 {
		end := batch + 50
		if end > tenantCount {
			end = tenantCount
		}
		for i := batch; i < end; i++ {
			name := fmt.Sprintf("loadtest-%d", i)
			if err := sim.CreateProject(ctx, name); err != nil {
				t.Fatalf("Failed to create project %s: %v", name, err)
			}
		}
		t.Logf("  Projects %d-%d created", batch, end-1)
	}

	// Deploy with concurrency limiter
	report := sim.RunScenario(ctx, tenantCount, fanOutOptions{concurrency: concurrency},
		func(idx int, name string, r DeployResult) {
			if idx%50 == 0 || !r.Success {
				t.Logf("  [%d/%d] %s: success=%v latency=%s",
					idx+1, tenantCount, name, r.Success, r.Latency.Total.Round(time.Second))
			}
		})
	success, failures := report.SuccessCount, report.FailureCount

	printReport(t, report)

	// L4 pass criteria: >99% success, p99 < 10min
	failRate := float64(failures) / float64(max(success+failures, 1)) * 100
	if failRate > 1 {
		t.Errorf("L4 FAIL: failure rate %.1f%% exceeds 1%%", failRate)
	}
	if report.DeployLatencyP99 > 10*time.Minute {
		t.Errorf("L4 FAIL: deploy p99 %s exceeds 10min", report.DeployLatencyP99)
	}
}

func TestL5_1000Tenants_MultiCell(t *testing.T) {
	_, sim := requireLoadTestEnv(t)

	const tenantCount = 1000
	const concurrency = 50 // 50 concurrent deploys
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Minute)
	defer cancel()

	t.Logf("L5 — %d tenants, %d concurrent, multi-cell", tenantCount, concurrency)

	// Create projects in batches of 100
	for batch := 0; batch < tenantCount; batch += 100 {
		end := batch + 100
		if end > tenantCount {
			end = tenantCount
		}
		for i := batch; i < end; i++ {
			name := fmt.Sprintf("loadtest-%d", i)
			if err := sim.CreateProject(ctx, name); err != nil {
				t.Fatalf("Failed to create project %s: %v", name, err)
			}
		}
		t.Logf("  Projects %d-%d created", batch, end-1)
	}

	// Deploy with concurrency limiter
	report := sim.RunScenario(ctx, tenantCount, fanOutOptions{concurrency: concurrency},
		func(idx int, name string, r DeployResult) {
			if idx%100 == 0 || !r.Success {
				t.Logf("  [%d/%d] %s: success=%v latency=%s",
					idx+1, tenantCount, name, r.Success, r.Latency.Total.Round(time.Second))
			}
		})
	success, failures := report.SuccessCount, report.FailureCount

	printReport(t, report)

	// L5 pass criteria: >99% success, p99 < 10min
	failRate := float64(failures) / float64(max(success+failures, 1)) * 100
	if failRate > 1 {
		t.Errorf("L5 FAIL: failure rate %.1f%% exceeds 1%%", failRate)
	}
	if report.DeployLatencyP99 > 10*time.Minute {
		t.Errorf("L5 FAIL: deploy p99 %s exceeds 10min", report.DeployLatencyP99)
	}
	if report.Duration > 240*time.Minute {
		t.Errorf("L5 FAIL: total duration %s exceeds 4h", report.Duration.Round(time.Minute))
	}
}
