// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package capacity

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestCapacityLoopZeroAPIBuildsSubmit benchmarks POST /api/v1/builds.
//
// Measures: latency + admission gate rejection rate under concurrency.
// Useful for validating:
//   - MaxConcurrentBuildsPerCell effective setting (bug #43)
//   - Admission gate backpressure response time
//   - Database pool saturation under write burst
//
// Run:
//
//	LOOPZERO_CAPACITY=1 LOOPZERO_API_URL=https://api.dev.getloopscale.com \
//	LOOPZERO_SERVICE_AUTH=... \
//	go test ./infra/tests/performance/capacity/ -run TestCapacityLoopZeroAPI -v \
//	  -args -rps=50 -duration=60s -workers=20
func TestCapacityLoopZeroAPIBuildsSubmit(t *testing.T) {
	SkipUnlessCapacityEnabled(t)

	apiURL := os.Getenv("LOOPZERO_API_URL")
	serviceAuth := os.Getenv("LOOPZERO_SERVICE_AUTH")
	if apiURL == "" || serviceAuth == "" {
		t.Fatal("LOOPZERO_API_URL and LOOPZERO_SERVICE_AUTH required")
	}

	cfg := NewConfig("loopzero-api-build-submit")
	t.Logf("config: rps=%v duration=%v workers=%d", cfg.RPS, cfg.Duration, cfg.Workers)

	// Non-destructive request: POST with an invalid tenant to hit the
	// admission/auth path without creating real builds. A 400/403/404 is the
	// expected rejection; only a 5xx is a real error.
	body := []byte(`{"project_id":"cap-bench-nope","environment":"production","commit_sha":"0000","apps":[{"name":"api","build":"buildpack","framework":"node","render":"server","port":3000}]}`)
	result, _ := RunOpenLoop(context.Background(), cfg, "loopzero-api-build-submit", ServerErrors,
		func(ctx context.Context, seq int64) (int, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api/v1/builds", bytes.NewReader(body))
			if err != nil {
				return 0, err
			}
			req.Header.Set("Authorization", "Bearer "+serviceAuth)
			req.Header.Set("X-Service-Auth", serviceAuth)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", fmt.Sprintf("cap-%d-%d", seq, time.Now().UnixNano()))
			return DoHTTP(req)
		})

	t.Logf("=== loopzero-api-build-submit ===")
	t.Logf("  requests:    %d (errors: %d, rate: %.2f%%)", result.TotalReqs, result.Errors, result.ErrorRate*100)
	t.Logf("  actual rps:  %.1f (target: %.0f)", result.ActualRPS, cfg.RPS)
	t.Logf("  p50/95/99:   %v / %v / %v", result.P50, result.P95, result.P99)

	if err := WriteResult(result, cfg.OutFile); err != nil {
		t.Errorf("write result: %v", err)
	} else {
		t.Logf("  result:      %s", cfg.OutFile)
	}

	// Soft assertions
	if result.ErrorRate > 0.50 {
		t.Errorf("error rate > 50%% indicates real platform problem (not just rejection)")
	}
	if result.P95 > 5*time.Second {
		t.Errorf("p95 latency > 5s — API is struggling")
	}
}
