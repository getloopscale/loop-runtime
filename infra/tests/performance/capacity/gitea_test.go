// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package capacity

import (
	"context"
	"net/http"
	"os"
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
//
//	LOOPZERO_CAPACITY=1 LOOPZERO_GITEA_URL=http://.../ \
//	LOOPZERO_GITEA_USER=loopzero-admin LOOPZERO_GITEA_PASS=... \
//	go test ./infra/tests/performance/capacity/ -run TestCapacityGiteaAuth -v \
//	  -args -rps=100 -duration=60s -workers=50
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

	// GET /api/v1/user — authenticated endpoint, minimal response body.
	// Exercises the full auth path including the DB lookup. A 401 counts as an
	// error: credential rejection under load is the saturation signal (bug #44).
	result, statuses := RunOpenLoop(context.Background(), cfg, "gitea-auth",
		func(status int) bool { return status == http.StatusUnauthorized || status >= 500 },
		func(ctx context.Context, _ int64) (int, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, giteaURL+"/api/v1/user", nil)
			if err != nil {
				return 0, err
			}
			req.SetBasicAuth(user, pass)
			return DoHTTP(req)
		})
	unauth := statuses[http.StatusUnauthorized]
	result.Notes = "401 counted as error (cred rejection indicates saturation per bug #44)"

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
