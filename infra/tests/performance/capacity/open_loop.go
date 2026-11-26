// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package capacity

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Request issues one benchmark request. It returns the HTTP status, or an error
// when no response arrived (transport failure, or a request it could not
// build). seq numbers the requests of a run from 0.
type Request func(ctx context.Context, seq int64) (status int, err error)

// StatusIsError decides whether a response counts as an error for a bench.
// Each subsystem answers differently — a rejected build submit is the expected
// 4xx, a MinIO 403 is a real failure — so the bench passes its rule in, and the
// runner applies it the same way everywhere.
type StatusIsError func(status int) bool

// ServerErrors counts only 5xx: the bench expects client rejections.
func ServerErrors(status int) bool { return status >= 500 }

// AnyHTTPError counts every 4xx and 5xx.
func AnyHTTPError(status int) bool { return status >= 400 }

// RunOpenLoop drives do at cfg.RPS for cfg.Duration with at most cfg.Workers in
// flight, waits for every issued request, and returns the benchmark Result plus
// how many responses carried each status. A transport failure always counts as
// an error; a status counts when isError says so.
//
// It is the one copy of the pacing / concurrency / accounting loop the capacity
// benches used to re-type, each re-deciding its own error accounting.
func RunOpenLoop(ctx context.Context, cfg Config, subsystem string, isError StatusIsError, do Request) (Result, map[int]int64) {
	lat := NewLatencies(int(cfg.RPS*cfg.Duration.Seconds()) + 1000)
	var total, errCount int64
	var statusMu sync.Mutex
	statuses := map[int]int64{}

	runCtx, cancel := context.WithTimeout(ctx, cfg.Duration+30*time.Second)
	defer cancel()
	interval := time.Duration(float64(time.Second) / cfg.RPS)
	workers := cfg.Workers
	if workers < 1 {
		workers = 1
	}

	start := time.Now()
	deadline := start.Add(cfg.Duration)
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for seq := int64(0); time.Now().Before(deadline); seq++ {
		select {
		case <-runCtx.Done():
		case <-time.After(interval):
		}
		if !time.Now().Before(deadline) || runCtx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(seq int64) {
			defer wg.Done()
			defer func() { <-sem }()
			t0 := time.Now()
			status, err := do(runCtx, seq)
			lat.Record(time.Since(t0))
			atomic.AddInt64(&total, 1)
			if err != nil {
				atomic.AddInt64(&errCount, 1)
				return
			}
			statusMu.Lock()
			statuses[status]++
			statusMu.Unlock()
			if isError(status) {
				atomic.AddInt64(&errCount, 1)
			}
		}(seq)
	}
	wg.Wait()

	elapsed := time.Since(start)
	result := Result{
		Subsystem:    subsystem,
		StartedAt:    start,
		Duration:     elapsed,
		RequestedRPS: cfg.RPS,
		Workers:      cfg.Workers,
		TotalReqs:    total,
		Errors:       errCount,
		P50:          lat.Percentile(0.50),
		P95:          lat.Percentile(0.95),
		P99:          lat.Percentile(0.99),
	}
	if total > 0 {
		result.ErrorRate = float64(errCount) / float64(total)
	}
	if elapsed > 0 {
		result.ActualRPS = float64(total) / elapsed.Seconds()
	}
	return result, statuses
}

// DoHTTP sends req with the default client, drains and closes the body so the
// connection is reused, and returns the status.
func DoHTTP(req *http.Request) (int, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return 0, fmt.Errorf("drain %s %s: %w", req.Method, req.URL, err)
	}
	return resp.StatusCode, nil
}
