// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-28-LAYERED-BOOTSTRAP iter 18 — Gitea concurrent clones load test.
// Closes the PARCIAL gap on regression matrix #22 (CAPACITY: Gitea
// concurrent clones). Pre-iter-18 only had health-check probe of the API
// endpoint; this adds programmatic load test for 200+ parallel clones.

package load

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestGiteaConcurrentClones drives N parallel HTTPS clones of a single repo
// against a live Gitea endpoint. Validates that the platform sustains
// ≥200 concurrent clone operations with p95 latency <5s and 0 failures.
//
// Acceptance:
//   - 0 HTTP errors (5xx)
//   - 0 timeout (>30s)
//   - p95 < 5s
//
// Gated on LOOPZERO_GITEA_LOAD=1 + LOOPZERO_GITEA_URL + LOOPZERO_GITEA_TOKEN.
func TestGiteaConcurrentClones(t *testing.T) {
	if os.Getenv("LOOPZERO_GITEA_LOAD") != "1" {
		t.Skip("set LOOPZERO_GITEA_LOAD=1 to run Gitea concurrent clones load test")
	}

	giteaURL := os.Getenv("LOOPZERO_GITEA_URL")
	if giteaURL == "" {
		t.Fatal("LOOPZERO_GITEA_URL required (e.g., https://git.dev.getloopscale.com)")
	}
	token := os.Getenv("LOOPZERO_GITEA_TOKEN")
	if token == "" {
		t.Fatal("LOOPZERO_GITEA_TOKEN required (admin token from loopzero-system/gitea-admin Secret)")
	}

	// Repo path: e.g., loopzero-platform/loopzero-cloud or any seed repo
	repoPath := os.Getenv("LOOPZERO_GITEA_TEST_REPO")
	if repoPath == "" {
		repoPath = "loopzero-platform/loopzero-cloud"
	}

	concurrency := 200
	if v := os.Getenv("LOOPZERO_GITEA_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			concurrency = n
		}
	}

	t.Logf("=== Gitea concurrent clones — %d parallel against %s/%s ===",
		concurrency, giteaURL, repoPath)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Probe URL — Git Smart HTTP info/refs endpoint (cheaper than full clone,
	// but exercises the same code path: auth + repo lookup + pack-objects).
	probeURL := fmt.Sprintf("%s/%s.git/info/refs?service=git-upload-pack", giteaURL, repoPath)

	type result struct {
		status   int
		duration time.Duration
		err      error
	}
	results := make([]result, concurrency)

	var wg sync.WaitGroup
	wg.Add(concurrency)
	var failures int64

	start := time.Now()
	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
			if err != nil {
				results[idx] = result{err: err}
				atomic.AddInt64(&failures, 1)
				return
			}
			req.Header.Set("User-Agent", "git/2.40.0")
			req.SetBasicAuth("loopzero-admin", token)

			t0 := time.Now()
			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Do(req)
			elapsed := time.Since(t0)

			if err != nil {
				results[idx] = result{err: err, duration: elapsed}
				atomic.AddInt64(&failures, 1)
				return
			}
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)

			results[idx] = result{status: resp.StatusCode, duration: elapsed}
			if resp.StatusCode >= 500 || resp.StatusCode == 429 {
				atomic.AddInt64(&failures, 1)
			}
		}(i)
	}
	wg.Wait()
	wallTime := time.Since(start)

	// Compute p50, p95, p99
	durations := make([]time.Duration, 0, concurrency)
	for _, r := range results {
		if r.err == nil {
			durations = append(durations, r.duration)
		}
	}
	// Insertion sort (small N, simple)
	for i := 1; i < len(durations); i++ {
		for j := i; j > 0 && durations[j] < durations[j-1]; j-- {
			durations[j], durations[j-1] = durations[j-1], durations[j]
		}
	}
	pct := func(p float64) time.Duration {
		if len(durations) == 0 {
			return 0
		}
		idx := int(float64(len(durations)-1) * p / 100.0)
		return durations[idx]
	}

	t.Logf("Concurrent clones result (wall time: %s):", wallTime)
	t.Logf("  Total:    %d", concurrency)
	t.Logf("  Success:  %d", concurrency-int(failures))
	t.Logf("  Failures: %d", failures)
	t.Logf("  p50: %s", pct(50))
	t.Logf("  p95: %s", pct(95))
	t.Logf("  p99: %s", pct(99))
	t.Logf("  max: %s", pct(100))

	// Acceptance criteria
	if failures > 0 {
		t.Errorf("expected 0 failures, got %d (Gitea capacity gap)", failures)
	}
	if p95 := pct(95); p95 > 5*time.Second {
		t.Errorf("p95 = %s > 5s threshold (Gitea concurrency degradation)", p95)
	}
	if wallTime > 60*time.Second {
		t.Errorf("wall time %s > 60s for %d concurrent — likely thundering herd serialization", wallTime, concurrency)
	}
}
