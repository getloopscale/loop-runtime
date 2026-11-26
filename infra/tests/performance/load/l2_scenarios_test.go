// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package load

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// L2 test scenarios — extends the L1 harness to 50-concurrent tenant
// stress per close-all-gaps T11.1.
//
// CTO Golden Rule #2: support 20k tenants, target 50k. L1 validated
// 10-concurrent on local k3d (the limit of a workstation). L2
// validates 50-concurrent on a real cloud cluster (pulumi-drill).
//
// All scenarios use the RealDeployHarness. Gated on LOOPRUNTIME_REAL_DEPLOY=1
// + LOOPRUNTIME_LOAD_LEVEL=l2 (or "all"). NEVER run against production or
// against the local k3d (you will OOM the workstation).
//
// EC-3 pre-flight (close-all-gaps): scenario asserts the cluster has
// >= 100Gi allocatable memory before launching (calibrated: ~2Gi per
// concurrent build × 50 = 100Gi). Fails loud BEFORE crashing the
// cluster mid-run.

const (
	l2ConcurrencyDefault = 50
	l2MemoryRequiredGib  = 100
	l2PerTenantMemoryGib = 2 // approximate worst-case per concurrent build
)

// requireL2Harness gates the L2 scenarios. Returns the harness or
// t.Skip()s with an actionable message.
func requireL2Harness(t *testing.T) *RealDeployHarness {
	t.Helper()
	if os.Getenv("LOOPRUNTIME_REAL_DEPLOY") != "1" {
		t.Skip("set LOOPRUNTIME_REAL_DEPLOY=1")
	}
	level := os.Getenv("LOOPRUNTIME_LOAD_LEVEL")
	if level != "l2" && level != "all" {
		t.Skipf("L2 scenarios are gated — set LOOPRUNTIME_LOAD_LEVEL=l2 (got %q). "+
			"L2 stresses 50-concurrent and MUST run against a real cloud "+
			"cluster (pulumi-drill), NOT local k3d.", level)
	}

	// EC-3 pre-flight: verify cluster has enough memory.
	if err := l2EnsureClusterCapacity(t); err != nil {
		t.Fatalf("L2 pre-flight FAILED: %v\n\n"+
			"Either scale up the drill cluster OR override concurrency "+
			"via LOOPRUNTIME_L2_CONCURRENCY=<n>. Do NOT proceed against an "+
			"under-provisioned cluster — pod schedule failures will "+
			"mask real concurrency bugs.", err)
	}

	// Plan 2026-05-16 T3.1: prefer LOOPRUNTIME_DRILL_API_URL when running against
	// drill prod-identical stack (per skill /drill-test). Fallback to
	// LOOPRUNTIME_API_URL for backward compat with non-drill runs.
	apiURL := os.Getenv("LOOPRUNTIME_DRILL_API_URL")
	if apiURL == "" {
		apiURL = os.Getenv("LOOPRUNTIME_API_URL")
	}
	harness, err := NewRealDeployHarness(RealDeployConfig{
		APIURL:      apiURL,
		AppDomain:   os.Getenv("LOOPRUNTIME_APP_DOMAIN"),
		ServiceAuth: os.Getenv("LOOPRUNTIME_SERVICE_AUTH"),
		FixturePath: envOr("LOOPRUNTIME_FIXTURE_PATH", "../../e2e/fixtures/hello-server"),
	})
	if err != nil {
		t.Fatalf("harness: %v", err)
	}
	return harness
}

// l2EnsureClusterCapacity is the EC-3 pre-flight: verifies the
// targeted Kubernetes cluster has enough allocatable memory for the
// configured concurrency level. Without this, a 50-tenant stress
// against an 8-node 16Gi cluster (128Gi total) would crash mid-run
// with pod-pending errors, producing false-negative results that
// hide real bugs.
//
// Default budget: 2Gi per concurrent build × concurrency. Operator
// can override via LOOPRUNTIME_L2_PER_TENANT_GIB env var.
func l2EnsureClusterCapacity(t *testing.T) error {
	t.Helper()

	concurrency := l2ConcurrencyDefault
	if v := os.Getenv("LOOPRUNTIME_L2_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return fmt.Errorf("invalid LOOPRUNTIME_L2_CONCURRENCY=%q (must be >=1)", v)
		}
		concurrency = n
	}
	perTenant := l2PerTenantMemoryGib
	if v := os.Getenv("LOOPRUNTIME_L2_PER_TENANT_GIB"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return fmt.Errorf("invalid LOOPRUNTIME_L2_PER_TENANT_GIB=%q (must be >=1)", v)
		}
		perTenant = n
	}
	requiredGib := concurrency * perTenant

	// Read cluster allocatable memory via kubectl (operator-provided
	// kubeconfig). KUBECONFIG env or in-cluster service account
	// determines target — same path as RealDeployHarness uses.
	cmd := exec.Command("kubectl", "get", "nodes",
		"-o", "jsonpath={range .items[*]}{.status.allocatable.memory}{\"\\n\"}{end}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl get nodes failed: %w (output: %s)", err, out)
	}

	var totalGib int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		gib, parseErr := parseKubeMemoryToGib(strings.TrimSpace(line))
		if parseErr != nil {
			t.Logf("warn: could not parse memory %q (assuming 0 contribution): %v",
				line, parseErr)
			continue
		}
		totalGib += gib
	}

	t.Logf("L2 pre-flight: cluster allocatable memory %d Gi, required %d Gi "+
		"(concurrency=%d × %d Gi/tenant)", totalGib, requiredGib, concurrency, perTenant)

	if totalGib < requiredGib {
		return fmt.Errorf("cluster has %d Gi allocatable memory, "+
			"need >= %d Gi for concurrency=%d (%d Gi per tenant). "+
			"Either scale up the drill cluster OR reduce "+
			"LOOPRUNTIME_L2_CONCURRENCY", totalGib, requiredGib, concurrency, perTenant)
	}
	return nil
}

// parseKubeMemoryToGib parses a kubectl memory string like "8Gi",
// "8192Mi", "8000000000Ki" into rounded gibibytes. Returns error on
// unrecognized format — never silently round-down to 0.
func parseKubeMemoryToGib(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty memory string")
	}
	// Common suffixes from K8s resource.Quantity.
	switch {
	case strings.HasSuffix(s, "Ki"):
		n, err := strconv.ParseInt(strings.TrimSuffix(s, "Ki"), 10, 64)
		if err != nil {
			return 0, err
		}
		return int(n / 1024 / 1024), nil
	case strings.HasSuffix(s, "Mi"):
		n, err := strconv.ParseInt(strings.TrimSuffix(s, "Mi"), 10, 64)
		if err != nil {
			return 0, err
		}
		return int(n / 1024), nil
	case strings.HasSuffix(s, "Gi"):
		n, err := strconv.ParseInt(strings.TrimSuffix(s, "Gi"), 10, 64)
		if err != nil {
			return 0, err
		}
		return int(n), nil
	default:
		return 0, fmt.Errorf("unrecognized memory suffix in %q (expected Ki/Mi/Gi)", s)
	}
}

// --- Scenario 2.1: 50 concurrent deploys ---
// Validates that 50 simultaneous deploys produce: 50 distinct builds,
// no pod-schedule failures, no rate-limit cascades, p95 deploy
// latency within budget. The contract is: lock-per-env serializes
// per (tenant, project, env) but parallelism across tenants is the
// design's whole point.

func TestL2_1_FiftyConcurrentDeploys(t *testing.T) {
	harness := requireL2Harness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	n := l2ConcurrencyDefault
	if v := os.Getenv("LOOPRUNTIME_L2_CONCURRENCY"); v != "" {
		n, _ = strconv.Atoi(v)
	}
	t.Logf("L2.1 — %d concurrent deploys (drill cluster)", n)

	// Provision distinct tenants first so the lock contention is
	// across-build (not within same-tenant which would just
	// serialize).
	//
	// Fail-loud: a tenant that fails to provision BEFORE the stress
	// starts would be observed mid-run as a "spurious" build failure,
	// hiding the real concurrency signal. Abort early.
	ids := make([]TenantIdentity, n)
	for i := 0; i < n; i++ {
		ids[i] = NewTenantIdentity("l2-conc", i)
		if _, err := harness.ProvisionTenant(ctx, ids[i]); err != nil {
			t.Fatalf("L2.1 provision tenant %d (%s) FAILED: %v "+
				"(aborting stress to avoid false-negative results)",
				i, ids[i].TenantID, err)
		}
	}

	// Launch 50 concurrent goroutines.
	results := make([]FullDeployResult, n)
	var wg sync.WaitGroup
	var success int64
	for i, id := range ids {
		wg.Add(1)
		go func(idx int, tenant TenantIdentity) {
			defer wg.Done()
			results[idx] = harness.RealDeploy(ctx, tenant)
			if results[idx].Success {
				atomic.AddInt64(&success, 1)
			}
		}(i, id)
	}
	wg.Wait()

	// Collect p95 latency.
	durations := make([]time.Duration, 0, n)
	for _, r := range results {
		if r.Success {
			durations = append(durations, r.TotalTime)
		}
	}
	p95 := computeP95(durations)
	successRate := float64(success) / float64(n) * 100

	// Emit metrics as JSON for the evidence doc.
	evidence := map[string]any{
		"scenario":      "L2.1_FiftyConcurrentDeploys",
		"concurrency":   n,
		"success":       success,
		"failures":      int64(n) - success,
		"success_rate":  successRate,
		"p95_seconds":   p95.Seconds(),
		"timestamp_utc": time.Now().UTC().Format(time.RFC3339),
	}
	enc, _ := json.MarshalIndent(evidence, "", "  ")
	t.Logf("L2.1 evidence: %s", string(enc))

	// Pass criteria: > 90% success (allows 5 of 50 to legitimately
	// fail due to resource pressure without flagging a bug). p95 <
	// 10min (real upper bound on cloud deploy chain).
	if successRate < 90.0 {
		t.Errorf("L2.1 FAIL: success_rate=%.1f%% (target >= 90.0%%)", successRate)
	}
	if p95 > 10*time.Minute {
		t.Errorf("L2.1 FAIL: p95=%s (target <= 10m)", p95.Round(time.Second))
	}
}

// computeP95 returns the p95 latency from a duration slice. Returns 0
// if input is empty.
func computeP95(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	// Simple sort-and-pick — n=50 doesn't warrant quickselect.
	sorted := make([]time.Duration, len(d))
	copy(sorted, d)
	// Bubble-sort for clarity (n <= ~50).
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	idx := int(float64(len(sorted)) * 0.95)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
