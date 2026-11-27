// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package capacity

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestCapacityArgoCDAppGet benchmarks the ArgoCD API (app-controller sync
// read path).
//
// Context: in L3 v1 (pre-T-05), single argocd-application-controller saturated
// reconciling 100+ Apps — sync Unknown >15min. Post T-05 (replicas=3 +
// sharding), expected:
//   - p95 < 500ms for GET /api/v1/applications/{name}
//   - error rate < 1%
//
// Run:
//
//	LOOP_RUNTIME_CAPACITY=1 LOOP_RUNTIME_ARGOCD_URL=... \
//	LOOP_RUNTIME_ARGOCD_TOKEN=... \
//	go test ./infra/tests/performance/capacity/ -run TestCapacityArgoCD -v \
//	  -args -rps=50 -duration=60s -workers=20
func TestCapacityArgoCDAppGet(t *testing.T) {
	SkipUnlessCapacityEnabled(t)

	argocdURL := os.Getenv("LOOP_RUNTIME_ARGOCD_URL")
	token := os.Getenv("LOOP_RUNTIME_ARGOCD_TOKEN")
	appName := os.Getenv("LOOP_RUNTIME_ARGOCD_APP") // pick any existing app; falls to first if empty
	if argocdURL == "" || token == "" {
		t.Fatal("LOOP_RUNTIME_ARGOCD_URL and LOOP_RUNTIME_ARGOCD_TOKEN required")
	}

	// If no specific app given, use /api/v1/applications (list all) instead.
	path := "/api/v1/applications"
	if appName != "" {
		path = fmt.Sprintf("/api/v1/applications/%s", appName)
	}

	cfg := NewConfig("argocd-app-get")
	t.Logf("config: rps=%v duration=%v workers=%d path=%s", cfg.RPS, cfg.Duration, cfg.Workers, path)

	result, _ := RunOpenLoop(context.Background(), cfg, "argocd-app-get", AnyHTTPError,
		func(ctx context.Context, _ int64) (int, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, argocdURL+path, nil)
			if err != nil {
				return 0, err
			}
			req.Header.Set("Authorization", "Bearer "+token)
			return DoHTTP(req)
		})
	result.Notes = fmt.Sprintf("path=%s; exercises repo-server + redis cache", path)

	t.Logf("=== argocd-app-get ===")
	t.Logf("  requests:    %d (errors: %d, rate: %.2f%%)", result.TotalReqs, result.Errors, result.ErrorRate*100)
	t.Logf("  actual rps:  %.1f (target: %.0f)", result.ActualRPS, cfg.RPS)
	t.Logf("  p50/95/99:   %v / %v / %v", result.P50, result.P95, result.P99)

	if err := WriteResult(result, cfg.OutFile); err != nil {
		t.Errorf("write result: %v", err)
	} else {
		t.Logf("  result:      %s", cfg.OutFile)
	}

	// Soft assertions — L3 v1 had p95 >15min stuck
	if result.P95 > 500*time.Millisecond {
		t.Errorf("p95 > 500ms — argocd-repo-server saturating")
	}
	if result.ErrorRate > 0.01 {
		t.Errorf("error rate > 1%%")
	}
}
