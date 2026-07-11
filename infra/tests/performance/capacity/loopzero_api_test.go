// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package capacity

import (
	"bytes"
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

// TestCapacityLoopZeroAPIBuildsSubmit benchmarks POST /api/v1/builds.
//
// Measures: latency + admission gate rejection rate under concurrency.
// Useful for validating:
//   - MaxConcurrentBuildsPerCell effective setting (bug #43)
//   - Admission gate backpressure response time
//   - Database pool saturation under write burst
//
// Run:
//   LOOPZERO_CAPACITY=1 LOOPZERO_API_URL=https://api.dev.loopzero.example \
//   LOOPZERO_SERVICE_AUTH=... \
//   go test ./infra/tests/capacity/ -run TestCapacityLoopZeroAPI -v \
//     -args -rps=50 -duration=60s -workers=20
func TestCapacityLoopZeroAPIBuildsSubmit(t *testing.T) {
	SkipUnlessCapacityEnabled(t)

	apiURL := os.Getenv("LOOPZERO_API_URL")
	serviceAuth := os.Getenv("LOOPZERO_SERVICE_AUTH")
	if apiURL == "" || serviceAuth == "" {
		t.Fatal("LOOPZERO_API_URL and LOOPZERO_SERVICE_AUTH required")
	}

	cfg := NewConfig("loopzero-api-build-submit")
	t.Logf("config: rps=%v duration=%v workers=%d", cfg.RPS, cfg.Duration, cfg.Workers)

	lat := NewLatencies(int(cfg.RPS*cfg.Duration.Seconds()) + 1000)
	// total + errors protected via atomic ops below — no Mutex needed.
	// (Earlier revision had a sync.Mutex here; go vet 2026-04-30 caught a
	// dead-code copy: `_ = mu` flagged "assignment copies lock value to _".)
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

			// Non-destructive request: POST with an invalid tenant to
			// hit admission/auth path without creating real builds.
			body := []byte(`{"project_id":"cap-bench-nope","environment":"production","commit_sha":"0000","apps":[{"name":"api","build":"buildpack","framework":"node","render":"server","port":3000}]}`)
			req, _ := http.NewRequestWithContext(ctx, "POST", apiURL+"/api/v1/builds", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+serviceAuth)
			req.Header.Set("X-Service-Auth", serviceAuth)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", fmt.Sprintf("cap-%d", time.Now().UnixNano()))

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

			// 400/404/403 expected (non-existent tenant). 5xx = real error.
			if resp.StatusCode >= 500 {
				atomic.AddInt64(&errors, 1)
			}
		}()
	}
	wg.Wait()

	elapsed := time.Since(start)
	result := Result{
		Subsystem:    "loopzero-api-build-submit",
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
	}

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
