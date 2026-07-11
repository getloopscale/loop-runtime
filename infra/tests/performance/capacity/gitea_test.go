// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package capacity

import (
	"context"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestCapacityGiteaAuth benchmarks Gitea basic auth latency.
//
// This is the leading indicator for bug #44: at c=30 concurrent
// git pushes, Gitea CPU saturates and auth latency cascades to 7s,
// causing 401 errors in source upload.
//
// Instead of running full L3 E2E to discover this, we measure Gitea
// auth RPS in isolation. Baseline expectation (post-HPA):
//   - p50  < 200ms
//   - p95  < 1s
//   - p99  < 2s
//   - error rate < 1%
//
// If p95 > 2s: HPA isn't scaling fast enough, OR maxReplicas too low.
//
// Run:
//   LOOPZERO_CAPACITY=1 LOOPZERO_GITEA_URL=http://.../ \
//   LOOPZERO_GITEA_USER=loopzero-admin LOOPZERO_GITEA_PASS=... \
//   go test ./infra/tests/capacity/ -run TestCapacityGiteaAuth -v \
//     -args -rps=100 -duration=60s -workers=50
func TestCapacityGiteaAuth(t *testing.T) {
	SkipUnlessCapacityEnabled(t)

	giteaURL := os.Getenv("LOOPZERO_GITEA_URL")
	user := os.Getenv("LOOPZERO_GITEA_USER")
	pass := os.Getenv("LOOPZERO_GITEA_PASS")
	if giteaURL == "" || user == "" || pass == "" {
		t.Fatal("LOOPZERO_GITEA_URL, LOOPZERO_GITEA_USER, LOOPZERO_GITEA_PASS required")
	}

	cfg := NewConfig("gitea-auth")
	t.Logf("config: rps=%v duration=%v workers=%d", cfg.RPS, cfg.Duration, cfg.Workers)

	lat := NewLatencies(int(cfg.RPS*cfg.Duration.Seconds()) + 1000)
	var total, errors, unauth int64

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

			// GET /api/v1/user — authenticated endpoint, minimal response body.
			// Exercises full auth path including DB lookup.
			req, _ := http.NewRequestWithContext(ctx, "GET", giteaURL+"/api/v1/user", nil)
			req.SetBasicAuth(user, pass)

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
			if resp.StatusCode == 401 {
				atomic.AddInt64(&unauth, 1)
			} else if resp.StatusCode >= 500 {
				atomic.AddInt64(&errors, 1)
			}
		}()
	}
	wg.Wait()

	elapsed := time.Since(start)
	result := Result{
		Subsystem:    "gitea-auth",
		StartedAt:    start,
		Duration:     elapsed,
		RequestedRPS: cfg.RPS,
		Workers:      cfg.Workers,
		TotalReqs:    total,
		Errors:       errors + unauth,
		ErrorRate:    float64(errors+unauth) / float64(total),
		P50:          lat.Percentile(0.50),
		P95:          lat.Percentile(0.95),
		P99:          lat.Percentile(0.99),
		ActualRPS:    float64(total) / elapsed.Seconds(),
		Notes:        "401 counted as error (cred rejection indicates saturation per bug #44)",
	}

	t.Logf("=== gitea-auth ===")
	t.Logf("  requests:    %d (errors: %d [401: %d], rate: %.2f%%)", result.TotalReqs, result.Errors, unauth, result.ErrorRate*100)
	t.Logf("  actual rps:  %.1f (target: %.0f)", result.ActualRPS, cfg.RPS)
	t.Logf("  p50/95/99:   %v / %v / %v", result.P50, result.P95, result.P99)

	if err := WriteResult(result, cfg.OutFile); err != nil {
		t.Errorf("write result: %v", err)
	} else {
		t.Logf("  result:      %s", cfg.OutFile)
	}

	// Baselines post-T-04 HPA + per-pod resource bump
	if result.P95 > 2*time.Second {
		t.Errorf("p95 > 2s — Gitea saturating (bug #44 regression?)")
	}
	if result.ErrorRate > 0.02 {
		t.Errorf("error rate > 2%% indicates 401 cascade (saturation)")
	}
}
