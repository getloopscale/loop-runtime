// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package capacity

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
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
//   LOOPZERO_CAPACITY=1 LOOPZERO_ARGOCD_URL=... \
//   LOOPZERO_ARGOCD_TOKEN=... \
//   go test ./infra/tests/performance/capacity/ -run TestCapacityArgoCD -v \
//     -args -rps=50 -duration=60s -workers=20
func TestCapacityArgoCDAppGet(t *testing.T) {
	SkipUnlessCapacityEnabled(t)

	argocdURL := os.Getenv("LOOPZERO_ARGOCD_URL")
	token := os.Getenv("LOOPZERO_ARGOCD_TOKEN")
	appName := os.Getenv("LOOPZERO_ARGOCD_APP") // pick any existing app; falls to first if empty
	if argocdURL == "" || token == "" {
		t.Fatal("LOOPZERO_ARGOCD_URL and LOOPZERO_ARGOCD_TOKEN required")
	}

	// If no specific app given, use /api/v1/applications (list all) instead.
	path := "/api/v1/applications"
	if appName != "" {
		path = fmt.Sprintf("/api/v1/applications/%s", appName)
	}

	cfg := NewConfig("argocd-app-get")
	t.Logf("config: rps=%v duration=%v workers=%d path=%s", cfg.RPS, cfg.Duration, cfg.Workers, path)

	lat := NewLatencies(int(cfg.RPS*cfg.Duration.Seconds()) + 1000)
	var total, errors int64

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration+30*time.Second)
	defer cancel()
	deadline := time.Now().Add(cfg.Duration)
	interval := time.Duration(float64(time.Second) / cfg.RPS)
	start := time.Now()

	var wg sync.WaitGroup
	sem := make(chan struct{}, cfg.Workers)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
		case <-time.After(interval):
		}
		if time.Now().After(deadline) {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			req, _ := http.NewRequestWithContext(ctx, "GET", argocdURL+path, nil)
			req.Header.Set("Authorization", "Bearer "+token)

			t0 := time.Now()
			resp, err := http.DefaultClient.Do(req)
			d := time.Since(t0)
			lat.Record(d)
			atomic.AddInt64(&total, 1)

			if err != nil {
				atomic.AddInt64(&errors, 1)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 400 {
				atomic.AddInt64(&errors, 1)
			}
		}()
	}
	wg.Wait()

	elapsed := time.Since(start)
	result := Result{
		Subsystem:    "argocd-app-get",
		StartedAt:    start,
		Duration:     elapsed,
		RequestedRPS: cfg.RPS,
		Workers:      cfg.Workers,
		TotalReqs:    total,
		Errors:       errors,
		ErrorRate:    float64(errors) / float64(total),
		P50:          lat.Percentile(0.50),
		P95:          lat.Percentile(0.95),
		P99:          lat.Percentile(0.99),
		ActualRPS:    float64(total) / elapsed.Seconds(),
		Notes:        fmt.Sprintf("path=%s; exercises repo-server + redis cache", path),
	}

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
