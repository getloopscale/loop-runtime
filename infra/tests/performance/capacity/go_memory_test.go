// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package capacity

import (
	"context"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/infra/tests/performance/load"
)

// TestCapacityGoMemory validates the cell-budget.md assumption:
//
//	"Go runtime memory under load. LoopRuntime-api at idle is 15 Mi. The projection
//	 of 200-300 Mi under burst is an educated guess based on typical pgx pool
//	 + buffered JSON encoding working set."
//
// Gaps addressed (from audit):
//   - Gap 1 (sustained load): Runs multiple waves of builds over time to
//     detect memory leaks, not just burst peak.
//   - Gap 2 (multi-replica): Uses kubectl top to measure TOTAL memory across
//     all loop-runtime-api replicas, not just the one behind the port-forward.
//   - Gap 3 (GC pressure): Tracks go_memstats_sys_bytes alongside heap_inuse
//     to capture the true OS-level memory footprint including GC overhead.
//
// Run:
//
//	kubectl --context=$LOOP_RUNTIME_CORE_CONTEXT -n loop-runtime-system port-forward svc/loop-runtime-api-metrics 19090:9090 &
//
//	LOOP_RUNTIME_CAPACITY=1 \
//	LOOP_RUNTIME_API_URL=https://api.dev.getloopscale.com \
//	LOOP_RUNTIME_API_METRICS_URL=http://localhost:19090 \
//	LOOP_RUNTIME_SERVICE_AUTH=... \
//	LOOP_RUNTIME_APP_DOMAIN=dev.getloopscale.com \
//	LOOP_RUNTIME_CORE_CONTEXT=do-sfo3-loop-runtime-infra-dev-sfo3-mgmt \
//	go test ./infra/tests/performance/capacity/ -run TestCapacityGoMemory -v -timeout 45m \
//	  -args -workers=10
func TestCapacityGoMemory(t *testing.T) {
	SkipUnlessCapacityEnabled(t)

	apiURL := os.Getenv("LOOP_RUNTIME_API_URL")
	metricsURL := os.Getenv("LOOP_RUNTIME_API_METRICS_URL")
	serviceAuth := os.Getenv("LOOP_RUNTIME_SERVICE_AUTH")
	appDomain := os.Getenv("LOOP_RUNTIME_APP_DOMAIN")
	mgmtCtx := os.Getenv("LOOP_RUNTIME_CORE_CONTEXT")
	if apiURL == "" || metricsURL == "" || serviceAuth == "" || appDomain == "" {
		t.Fatal("LOOP_RUNTIME_API_URL, LOOP_RUNTIME_API_METRICS_URL, LOOP_RUNTIME_SERVICE_AUTH, LOOP_RUNTIME_APP_DOMAIN required")
	}

	const (
		projectedPeakMi = 300
		projectedPeak   = projectedPeakMi * 1024 * 1024
		pollInterval    = 2 * time.Second
	)

	// Sustained load: number of build waves (Gap 1).
	waves := 3
	if s := os.Getenv("LOOP_RUNTIME_SUSTAINED_WAVES"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			waves = n
		}
	}

	cfg := NewConfig("go-memory")
	workers := cfg.Workers
	if workers < 3 {
		workers = 3
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()

	// --- Step 1: Create deploy harness ---
	fixturePath := os.Getenv("LOOP_RUNTIME_FIXTURE_PATH")
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

	// --- Step 2: Baseline readings ---
	t.Log("reading baseline...")
	beforeHeap, err := ScrapePrometheusMetric(metricsURL, "go_memstats_heap_inuse_bytes")
	if err != nil {
		t.Fatalf("scrape baseline heap: %v", err)
	}
	// Gap 3: Track sys_bytes (includes GC overhead, stack, etc.)
	beforeSys, _ := ScrapePrometheusMetric(metricsURL, "go_memstats_sys_bytes")
	beforePGConns, _ := ScrapePrometheusMetric(metricsURL, "loop_runtime_postgres_connections_active")

	// Gap 2: Multi-replica baseline via kubectl top.
	var baselineKubeMi int64
	if mgmtCtx != "" {
		pods, err := KubectlTopPods(ctx, mgmtCtx, "loop-runtime-system", "app=loop-runtime-api")
		if err != nil {
			t.Logf("  warning: kubectl top baseline: %v (continuing with metrics-only)", err)
		} else {
			baselineKubeMi = TotalMemoryMi(pods)
			t.Logf("  kubectl top baseline: %d Mi across %d replicas", baselineKubeMi, len(pods))
		}
	}

	t.Logf("  heap: %.1f Mi, sys: %.1f Mi, PG conns: %.0f",
		beforeHeap/1024/1024, beforeSys/1024/1024, beforePGConns)

	// --- Step 3: Provision tenants ---
	t.Logf("provisioning %d tenants...", workers)
	tenants := make([]load.TenantIdentity, workers)
	for i := 0; i < workers; i++ {
		tenants[i] = load.NewTenantIdentity("mem", i)
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

	// --- Step 4: Start metrics poller (heap + sys) ---
	var peakHeap, peakSys, peakPGConns int64
	atomic.StoreInt64(&peakHeap, int64(beforeHeap))
	atomic.StoreInt64(&peakSys, int64(beforeSys))

	pollerCtx, pollerCancel := context.WithCancel(ctx)
	pollerDone := make(chan struct{})
	go func() {
		defer close(pollerDone)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-pollerCtx.Done():
				return
			case <-ticker.C:
				// Heap inuse (post-GC working set).
				if heap, err := ScrapePrometheusMetric(metricsURL, "go_memstats_heap_inuse_bytes"); err == nil {
					atomicMax(&peakHeap, int64(heap))
				}
				// Sys bytes (total OS-level, includes GC pressure — Gap 3).
				if sys, err := ScrapePrometheusMetric(metricsURL, "go_memstats_sys_bytes"); err == nil {
					atomicMax(&peakSys, int64(sys))
				}
				// PG connections correlation.
				if pg, err := ScrapePrometheusMetric(metricsURL, "loop_runtime_postgres_connections_active"); err == nil {
					atomicMax(&peakPGConns, int64(pg))
				}
			}
		}
	}()

	// --- Step 5: Sustained load — multiple waves (Gap 1) ---
	totalSucceeded := 0
	totalSubmitted := 0
	for wave := 1; wave <= waves; wave++ {
		t.Logf("--- wave %d/%d: submitting %d concurrent deploys ---", wave, waves, workers)
		var wg sync.WaitGroup
		results := make([]load.FullDeployResult, workers)

		for i, tenant := range tenants {
			wg.Add(1)
			go func(idx int, tn load.TenantIdentity) {
				defer wg.Done()
				results[idx] = harness.RealDeploy(ctx, tn)
			}(i, tenant)
		}
		wg.Wait()
		totalSubmitted += workers

		for _, r := range results {
			if r.Success {
				totalSucceeded++
			}
		}

		currentHeap := atomic.LoadInt64(&peakHeap)
		t.Logf("  wave %d done: peak heap so far %.1f Mi", wave, float64(currentHeap)/1024/1024)

		// Wait between waves for GC and memory stabilization.
		if wave < waves {
			t.Logf("  waiting 30s before next wave (memory leak detection)...")
			// intentional-sleep: benchmark warmup / measurement window
			time.Sleep(30 * time.Second)
		}
	}

	// Give poller final reads, then stop.
	// intentional-sleep: benchmark warmup / measurement window
	time.Sleep(pollInterval + 1*time.Second)
	pollerCancel()
	<-pollerDone

	// --- Step 6: Post-load readings ---
	afterHeap, _ := ScrapePrometheusMetric(metricsURL, "go_memstats_heap_inuse_bytes")
	afterSys, _ := ScrapePrometheusMetric(metricsURL, "go_memstats_sys_bytes")

	// Gap 2: Multi-replica post-load via kubectl top.
	var postKubeMi int64
	if mgmtCtx != "" {
		pods, err := KubectlTopPods(ctx, mgmtCtx, "loop-runtime-system", "app=loop-runtime-api")
		if err == nil {
			postKubeMi = TotalMemoryMi(pods)
			t.Logf("  kubectl top post-load: %d Mi across %d replicas", postKubeMi, len(pods))
		}
	}

	// --- Step 7: Report ---
	peak := atomic.LoadInt64(&peakHeap)
	peakSysVal := atomic.LoadInt64(&peakSys)
	pgPeak := atomic.LoadInt64(&peakPGConns)
	delta := peak - int64(beforeHeap)
	if delta < 0 {
		delta = 0
	}

	result := MemoryResult{
		Subsystem:         "go-memory-under-load",
		StartedAt:         time.Now(),
		BeforeHeapBytes:   int64(beforeHeap),
		PeakHeapBytes:     peak,
		AfterHeapBytes:    int64(afterHeap),
		DeltaHeapBytes:    delta,
		ProjectedBytes:    projectedPeak,
		ActualVsProjected: float64(peak) / float64(projectedPeak),
		CorrelationData: map[string]interface{}{
			// Load profile
			"waves":            waves,
			"workers_per_wave": workers,
			"total_submitted":  totalSubmitted,
			"total_succeeded":  totalSucceeded,
			"poll_interval":    pollInterval.String(),
			// Gap 3: GC pressure (sys includes stacks, GC metadata, mmap'd regions)
			"peak_sys_bytes": peakSysVal,
			"peak_sys_mi":    peakSysVal / 1024 / 1024,
			"before_sys_mi":  int64(beforeSys) / 1024 / 1024,
			"after_sys_mi":   int64(afterSys) / 1024 / 1024,
			// Gap 2: Multi-replica totals
			"kubectl_baseline_mi": baselineKubeMi,
			"kubectl_postload_mi": postKubeMi,
			// PG correlation
			"peak_pg_connections": pgPeak,
			"baseline_pg_conns":   int64(beforePGConns),
		},
	}

	t.Log("=== go-memory-under-load ===")
	t.Logf("  load:        %d waves × %d workers = %d builds (%d succeeded)", waves, workers, totalSubmitted, totalSucceeded)
	t.Logf("  heap before: %.1f Mi", beforeHeap/1024/1024)
	t.Logf("  heap peak:   %.1f Mi (prometheus, single replica)", float64(peak)/1024/1024)
	t.Logf("  heap after:  %.1f Mi", afterHeap/1024/1024)
	t.Logf("  sys peak:    %.1f Mi (includes GC overhead)", float64(peakSysVal)/1024/1024)
	t.Logf("  delta:       %.1f Mi", float64(delta)/1024/1024)
	t.Logf("  projected:   %d Mi", projectedPeakMi)
	t.Logf("  actual/proj: %.2f", result.ActualVsProjected)
	t.Logf("  PG peak:     %d conns (baseline: %.0f)", pgPeak, beforePGConns)
	if mgmtCtx != "" {
		t.Logf("  kubectl:     %d Mi → %d Mi (all replicas)", baselineKubeMi, postKubeMi)
	}

	// Memory leak detection (Gap 1): after-load should be within 2x of before.
	if afterHeap > 0 && beforeHeap > 0 && afterHeap > 2*beforeHeap {
		t.Errorf("LEAK SUSPECT: after-load heap (%.1f Mi) > 2× baseline (%.1f Mi) — memory not reclaimed after %d waves",
			afterHeap/1024/1024, beforeHeap/1024/1024, waves)
	}

	if err := WriteMemoryResult(result, cfg.OutFile); err != nil {
		t.Errorf("write result: %v", err)
	} else {
		t.Logf("  result:      %s", cfg.OutFile)
	}

	// --- Assertions ---
	if peak > 1024*1024*1024 {
		t.Errorf("peak heap %d Mi exceeds 1 GiB — OOMKill risk on 1 Gi limit", peak/1024/1024)
	}
	if peak > projectedPeak {
		t.Errorf("peak heap %d Mi exceeds cell-budget projection of %d Mi", peak/1024/1024, projectedPeakMi)
	}
	// Gap 3: sys_bytes should not exceed 2x heap (excessive GC pressure).
	if peakSysVal > 2*peak && peak > 0 {
		t.Errorf("sys bytes (%.1f Mi) > 2× heap (%.1f Mi) — excessive GC pressure or goroutine leak",
			float64(peakSysVal)/1024/1024, float64(peak)/1024/1024)
	}
	if peak > 0 && peak < 50*1024*1024 {
		t.Logf("NOTE: peak heap %.1f Mi — projection of %d Mi may be overly conservative",
			float64(peak)/1024/1024, projectedPeakMi)
	}
}

// atomicMax updates *addr to max(*addr, val) atomically.
func atomicMax(addr *int64, val int64) {
	for {
		old := atomic.LoadInt64(addr)
		if val <= old || atomic.CompareAndSwapInt64(addr, old, val) {
			return
		}
	}
}
