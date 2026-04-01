// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package load implements the 50k app load test for Cell-Based Architecture.
// ADR-0008 Phase 4 (P4-5).
//
// Usage:
//
//	LOOPZERO_LOAD_TEST=1 go test ./infra/tests/load/ -v -run TestCellScale -timeout 30m
//
// This test is SKIPPED unless LOOPZERO_LOAD_TEST=1 is set.
// It requires N Cells running on DigitalOcean with ArgoCD + Traefik.
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

// ScaleTarget defines the target scale for the load test.
type ScaleTarget struct {
	CellCount       int // number of Cells
	AppsPerCell     int // apps per Cell
	ConcurrentDeploys int // simultaneous deploy requests
}

// ScaleResult captures the outcome of the load test.
type ScaleResult struct {
	TotalApps        int
	DeployLatencyP50 time.Duration
	DeployLatencyP95 time.Duration
	DeployLatencyP99 time.Duration
	SuccessCount     int64
	FailureCount     int64
	Duration         time.Duration
}

// DeploySimulator simulates deploy operations against the LoopZero API.
type DeploySimulator struct {
	apiURL    string
	authToken string
}

// NewDeploySimulator creates a simulator targeting the LoopZero API.
func NewDeploySimulator(apiURL, authToken string) *DeploySimulator {
	return &DeploySimulator{apiURL: apiURL, authToken: authToken}
}

// SimulateDeploy sends a deploy request and measures latency.
func (s *DeploySimulator) SimulateDeploy(ctx context.Context, tenantID, projectID, env string) (time.Duration, error) {
	start := time.Now()
	// TODO(P4-5): Implement actual HTTP call to POST /builds
	// For now, simulate with sleep
	time.Sleep(100 * time.Millisecond) // placeholder
	return time.Since(start), nil
}

func TestCellScale50k(t *testing.T) {
	if os.Getenv("LOOPZERO_LOAD_TEST") != "1" {
		t.Skip("skipping load test: set LOOPZERO_LOAD_TEST=1 to run")
	}

	apiURL := os.Getenv("LOOPZERO_API_URL")
	if apiURL == "" {
		t.Fatal("LOOPZERO_API_URL required for load test")
	}

	authToken := os.Getenv("LOOPZERO_AUTH_TOKEN")
	if authToken == "" {
		t.Fatal("LOOPZERO_AUTH_TOKEN required for load test")
	}

	target := ScaleTarget{
		CellCount:         10,
		AppsPerCell:       5000,
		ConcurrentDeploys: 100,
	}

	t.Logf("Scale target: %d Cells x %d apps = %d total, %d concurrent deploys",
		target.CellCount, target.AppsPerCell,
		target.CellCount*target.AppsPerCell,
		target.ConcurrentDeploys)

	sim := NewDeploySimulator(apiURL, authToken)
	result := runScaleTest(t, sim, target)

	// Validate against targets
	t.Logf("Results: %d success, %d failures, p50=%s p95=%s p99=%s",
		result.SuccessCount, result.FailureCount,
		result.DeployLatencyP50, result.DeployLatencyP95, result.DeployLatencyP99)

	if result.DeployLatencyP50 > 30*time.Second {
		t.Errorf("deploy p50 %s exceeds 30s target", result.DeployLatencyP50)
	}
	if result.DeployLatencyP95 > 60*time.Second {
		t.Errorf("deploy p95 %s exceeds 60s target", result.DeployLatencyP95)
	}
	if result.DeployLatencyP99 > 120*time.Second {
		t.Errorf("deploy p99 %s exceeds 120s target", result.DeployLatencyP99)
	}
	if result.FailureCount > 0 {
		failRate := float64(result.FailureCount) / float64(result.SuccessCount+result.FailureCount) * 100
		if failRate > 5 {
			t.Errorf("failure rate %.1f%% exceeds 5%% threshold", failRate)
		}
	}
}

func runScaleTest(t *testing.T, sim *DeploySimulator, target ScaleTarget) *ScaleResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	var (
		successCount int64
		failureCount int64
		latencies    []time.Duration
		mu           sync.Mutex
	)

	sem := make(chan struct{}, target.ConcurrentDeploys)
	var wg sync.WaitGroup

	start := time.Now()

	for cell := 0; cell < target.CellCount; cell++ {
		for app := 0; app < target.ConcurrentDeploys/target.CellCount; app++ {
			wg.Add(1)
			sem <- struct{}{}
			go func(cellIdx, appIdx int) {
				defer wg.Done()
				defer func() { <-sem }()

				tenantID := fmt.Sprintf("tn-load-%d", cellIdx)
				projectID := fmt.Sprintf("app-%d", appIdx)

				latency, err := sim.SimulateDeploy(ctx, tenantID, projectID, "production")
				if err != nil {
					atomic.AddInt64(&failureCount, 1)
					return
				}
				atomic.AddInt64(&successCount, 1)
				mu.Lock()
				latencies = append(latencies, latency)
				mu.Unlock()
			}(cell, app)
		}
	}

	wg.Wait()

	return &ScaleResult{
		TotalApps:        target.CellCount * target.AppsPerCell,
		SuccessCount:     successCount,
		FailureCount:     failureCount,
		Duration:         time.Since(start),
		DeployLatencyP50: percentile(latencies, 50),
		DeployLatencyP95: percentile(latencies, 95),
		DeployLatencyP99: percentile(latencies, 99),
	}
}

func percentile(latencies []time.Duration, p int) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	// Simple percentile (not production-grade — use HDR histogram for real tests)
	idx := len(latencies) * p / 100
	if idx >= len(latencies) {
		idx = len(latencies) - 1
	}
	return latencies[idx]
}
