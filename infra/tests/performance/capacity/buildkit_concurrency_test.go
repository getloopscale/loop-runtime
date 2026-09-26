// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package capacity

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/infra/tests/performance/load"
)

// TestCapacityBuildKitConcurrency validates the cell-budget.md assumption:
//
//	"BuildKit concurrent-solve ceiling. The ~4 solves/node figure is a
//	 community observation, not a LoopZero-measured value."
//
// The test submits N builds simultaneously (N > projected ceiling) and
// monitors loopzero_builds_active to find the actual peak concurrency. This
// reveals how many builds truly make forward progress at the same time
// vs queueing at the BuildKit level.
//
// Run:
//
//	# Port-forward loopzero-api metrics:
//	kubectl --context=$LOOPZERO_CORE_CONTEXT -n loopzero-system port-forward svc/loopzero-api-metrics 19090:9090 &
//
//	LOOPZERO_CAPACITY=1 \
//	LOOPZERO_API_URL=https://api.dev.loopzero.example \
//	LOOPZERO_API_METRICS_URL=http://localhost:19090 \
//	LOOPZERO_SERVICE_AUTH=... \
//	LOOPZERO_APP_DOMAIN=dev.loopzero.example \
//	LOOPZERO_BUILD_NODES=2 \
//	go test ./infra/tests/performance/capacity/ -run TestCapacityBuildKitConcurrency -v -timeout 60m
func TestCapacityBuildKitConcurrency(t *testing.T) {
	SkipUnlessCapacityEnabled(t)

	apiURL := os.Getenv("LOOPZERO_API_URL")
	metricsURL := os.Getenv("LOOPZERO_API_METRICS_URL")
	serviceAuth := os.Getenv("LOOPZERO_SERVICE_AUTH")
	appDomain := os.Getenv("LOOPZERO_APP_DOMAIN")
	if apiURL == "" || metricsURL == "" || serviceAuth == "" || appDomain == "" {
		t.Fatal("LOOPZERO_API_URL, LOOPZERO_API_METRICS_URL, LOOPZERO_SERVICE_AUTH, LOOPZERO_APP_DOMAIN required")
	}

	buildNodes := 2
	if s := os.Getenv("LOOPZERO_BUILD_NODES"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			buildNodes = n
		}
	}

	// Gap 4: Build cluster context for node-level CPU/memory monitoring.
	// Without this, we only see API-level concurrency metrics and miss
	// disk I/O and CPU saturation on the actual build nodes.
	buildCtx := os.Getenv("LOOPZERO_BUILD_CONTEXT")

	const (
		numBuilds        = 12 // Exceeds projected ceiling (2 nodes * 4 = 8)
		projectedPerNode = 4  // Community observation from cell-budget.md
		pollInterval     = 1 * time.Second
	)
	projectedCeiling := buildNodes * projectedPerNode

	cfg := NewConfig("buildkit-concurrency")
	t.Logf("config: builds=%d, nodes=%d, projected_ceiling=%d", numBuilds, buildNodes, projectedCeiling)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Minute)
	defer cancel()

	// --- Step 1: Create deploy harness ---
	fixturePath := os.Getenv("LOOPZERO_FIXTURE_PATH")
	if fixturePath == "" {
		fixturePath = "../../e2e/fixtures/hello-server"
	}
	harness, err := load.NewRealDeployHarness(load.RealDeployConfig{
		APIURL:      apiURL,
		AppDomain:   appDomain,
		ServiceAuth: serviceAuth,
		FixturePath: fixturePath,
	})
	if err != nil {
		t.Fatalf("create harness: %v", err)
	}

	// --- Step 2: Provision tenants ---
	t.Logf("provisioning %d tenants...", numBuilds)
	tenants := make([]load.TenantIdentity, numBuilds)
	for i := 0; i < numBuilds; i++ {
		tenants[i] = load.NewTenantIdentity("bk", i)
	}
	for _, tenant := range tenants {
		if _, err := harness.ProvisionTenant(ctx, tenant); err != nil {
			t.Logf("  warning: provision %s: %v", tenant.TenantID, err)
		}
	}

	defer func() {
		t.Log("cleaning up tenants...")
		for _, tenant := range tenants {
			_ = harness.CleanupTenant(context.Background(), tenant)
		}
	}()

	// --- Step 3: Start concurrency + node monitor ---
	var peakConcurrent int64
	var peakNodeCPU, peakNodeMem int64 // Gap 4: build node saturation
	var samples []int64
	var samplesMu sync.Mutex

	monitorCtx, monitorCancel := context.WithCancel(ctx)
	monitorDone := make(chan struct{})

	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-monitorCtx.Done():
				return
			case <-ticker.C:
				active, err := ScrapePrometheusMetricSum(metricsURL, "loopzero_builds_active")
				if err != nil {
					continue
				}
				activeInt := int64(active)

				samplesMu.Lock()
				samples = append(samples, activeInt)
				samplesMu.Unlock()

				atomicMax(&peakConcurrent, activeInt)

				// Gap 4: Monitor build node CPU/memory to detect disk/CPU
				// saturation that API metrics cannot see.
				if buildCtx != "" {
					nodes, err := KubectlTopNodes(monitorCtx, buildCtx)
					if err == nil {
						var totalCPU, totalMem int64
						for _, n := range nodes {
							totalCPU += n.CPUMillis
							totalMem += n.MemoryMi
						}
						atomicMax(&peakNodeCPU, totalCPU)
						atomicMax(&peakNodeMem, totalMem)
					}
				}
			}
		}
	}()

	// --- Step 4: Submit ALL builds simultaneously ---
	t.Logf("submitting %d builds simultaneously...", numBuilds)
	submitStart := time.Now()

	var wg sync.WaitGroup
	results := make([]load.FullDeployResult, numBuilds)
	for i, tenant := range tenants {
		wg.Add(1)
		go func(idx int, t load.TenantIdentity) {
			defer wg.Done()
			results[idx] = harness.RealDeploy(ctx, t)
		}(i, tenant)
	}
	wg.Wait()
	totalDuration := time.Since(submitStart)

	// Let monitor catch tail end.
	// intentional-sleep: benchmark warmup / measurement window
	time.Sleep(2 * pollInterval)
	monitorCancel()
	<-monitorDone

	// --- Step 5: Analyze results ---
	peak := atomic.LoadInt64(&peakConcurrent)
	succeeded := 0
	var buildDurations []time.Duration
	for _, r := range results {
		if r.Success {
			succeeded++
		}
		if r.Stages.BuildComplete > 0 {
			buildDurations = append(buildDurations, r.Stages.BuildComplete)
		}
	}

	// Compute average concurrent from samples.
	var avgConcurrent float64
	samplesMu.Lock()
	if len(samples) > 0 {
		var sum int64
		for _, s := range samples {
			sum += s
		}
		avgConcurrent = float64(sum) / float64(len(samples))
	}
	samplesMu.Unlock()

	perNode := float64(0)
	if buildNodes > 0 {
		perNode = float64(peak) / float64(buildNodes)
	}

	nodeCPU := atomic.LoadInt64(&peakNodeCPU)
	nodeMem := atomic.LoadInt64(&peakNodeMem)

	result := ConcurrencyResult{
		Subsystem:         "buildkit-concurrency",
		StartedAt:         submitStart,
		SubmittedBuilds:   numBuilds,
		PeakConcurrent:    int(peak),
		ProjectedCeiling:  projectedCeiling,
		ActualCeiling:     int(peak),
		Nodes:             buildNodes,
		PerNodeConcurrent: perNode,
		BuildDurations:    buildDurations,
		Notes: fmt.Sprintf(
			"succeeded=%d/%d, avg_concurrent=%.1f, total_duration=%v, samples=%d, peak_node_cpu=%dm, peak_node_mem=%dMi",
			succeeded, numBuilds, avgConcurrent, totalDuration, len(samples), nodeCPU, nodeMem,
		),
	}

	t.Log("=== buildkit-concurrency ===")
	t.Logf("  submitted:     %d", numBuilds)
	t.Logf("  succeeded:     %d / %d", succeeded, numBuilds)
	t.Logf("  peak active:   %d", peak)
	t.Logf("  avg active:    %.1f", avgConcurrent)
	t.Logf("  per node:      %.1f (projected: %d)", perNode, projectedPerNode)
	t.Logf("  projected:     %d (nodes=%d × %d/node)", projectedCeiling, buildNodes, projectedPerNode)
	t.Logf("  total time:    %v", totalDuration)
	t.Logf("  samples:       %d", len(samples))
	if buildCtx != "" {
		t.Logf("  node peak:     %d m CPU, %d Mi mem (all build nodes)", nodeCPU, nodeMem)
	}

	if err := WriteConcurrencyResult(result, cfg.OutFile); err != nil {
		t.Errorf("write result: %v", err)
	} else {
		t.Logf("  result:        %s", cfg.OutFile)
	}

	// --- Assertions ---
	if peak < int64(projectedCeiling)/2 {
		t.Errorf("peak concurrent (%d) is < 50%% of projected ceiling (%d) — BuildKit more constrained than expected", peak, projectedCeiling)
	}
	if peak > int64(projectedCeiling)*3/2 {
		t.Logf("NOTE: peak concurrent (%d) exceeds projected ceiling (%d) by >50%% — projection is conservative", peak, projectedCeiling)
	}
	if succeeded == 0 {
		t.Errorf("zero builds succeeded — test environment issue, not a concurrency measurement")
	}
}
