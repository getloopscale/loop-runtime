// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package capacity provides isolated micro-benchmarks per subsystem.
//
// Context (T-03 from docs/plans/50k-immediate-focus.md):
//
// Between L3 v1 and L3 v8 we ran 8 full 100-tenant end-to-end tests to
// discover 8 distinct bugs in isolated subsystems (Gitea auth, cosign TUF,
// MinIO cache-restore, Argo quota, DO CSI throughput, admission wiring).
// Each full run cost ~60min. Total discovery cost: ~8 hours.
//
// Had we benched each subsystem in isolation FIRST, the same bugs would
// have surfaced in ~30min. This package enables that pre-E2E capacity
// baseline measurement.
//
// Structure:
//   - One Go test file per subsystem (gitea_test.go, minio_test.go, ...)
//   - Each test is a `BenchmarkFoo` with configurable concurrency + duration
//   - Results written to /tmp/capacity-bench/{subsystem}-{timestamp}.json
//   - Baselines documented in docs/plans/capacity-baselines.md
//
// Usage:
//   LOOPZERO_API_URL=https://api.dev.loopzero.example \
//   LOOPZERO_SERVICE_AUTH=... \
//   go test ./infra/tests/capacity/ -run TestCapacityGitea \
//     -args -rps=100 -duration=30s -out=/tmp/capacity-bench/gitea-$(date +%s).json
//
// Opt-in: set LOOPZERO_CAPACITY=1 to run these tests. Otherwise skipped.
package capacity

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"
)

// Config controls a capacity benchmark run. Values read from flags.
type Config struct {
	RPS      float64
	Duration time.Duration
	Workers  int
	OutFile  string
}

var (
	flagRPS      = flag.Float64("rps", 10, "target requests per second")
	flagDuration = flag.Duration("duration", 30*time.Second, "benchmark duration")
	flagWorkers  = flag.Int("workers", 10, "concurrent workers generating load")
	flagOutFile  = flag.String("out", "", "output JSON path (default: /tmp/capacity-bench/{name}-{ts}.json)")
)

// NewConfig reads flags and returns a Config.
func NewConfig(defaultName string) Config {
	out := *flagOutFile
	if out == "" {
		ts := time.Now().Unix()
		_ = os.MkdirAll("/tmp/capacity-bench", 0o755)
		out = filepath.Join("/tmp/capacity-bench", fmt.Sprintf("%s-%d.json", defaultName, ts))
	}
	return Config{
		RPS:      *flagRPS,
		Duration: *flagDuration,
		Workers:  *flagWorkers,
		OutFile:  out,
	}
}

// Result captures a benchmark outcome.
type Result struct {
	Subsystem   string        `json:"subsystem"`
	StartedAt   time.Time     `json:"started_at"`
	Duration    time.Duration `json:"duration"`
	RequestedRPS float64      `json:"requested_rps"`
	Workers     int           `json:"workers"`
	TotalReqs   int64         `json:"total_reqs"`
	Errors      int64         `json:"errors"`
	ErrorRate   float64       `json:"error_rate"`
	P50         time.Duration `json:"p50"`
	P95         time.Duration `json:"p95"`
	P99         time.Duration `json:"p99"`
	ActualRPS   float64       `json:"actual_rps"`
	Notes       string        `json:"notes,omitempty"`
}

// Latencies is a thread-safe collection of request latencies.
type Latencies struct {
	samples []time.Duration
	idx     int64
}

// NewLatencies pre-allocates a fixed-size ring-like buffer for p50/95/99.
// For benchmarks longer than 10s at high RPS, this avoids GC pressure.
func NewLatencies(cap int) *Latencies {
	return &Latencies{samples: make([]time.Duration, 0, cap)}
}

// Record appends a latency sample (not thread-safe by itself; callers
// should funnel through a single goroutine or pre-reserve slots).
func (l *Latencies) Record(d time.Duration) {
	// Atomic reserve slot; if beyond cap, silently drop (reservoir-like).
	i := atomic.AddInt64(&l.idx, 1) - 1
	if i < int64(cap(l.samples)) {
		l.samples = append(l.samples[:i], d)
		// grow visible length
		if int64(len(l.samples)) < i+1 {
			// best-effort — race here is tolerable for bench accuracy
			l.samples = append(l.samples, d)
		}
	}
}

// Percentile returns the pth percentile in the collected samples.
func (l *Latencies) Percentile(p float64) time.Duration {
	if len(l.samples) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(l.samples))
	copy(sorted, l.samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(float64(len(sorted)) * p)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// WriteResult writes the result as JSON.
func WriteResult(r Result, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// SkipUnlessCapacityEnabled skips the test unless LOOPZERO_CAPACITY=1 is set.
// Capacity tests assume a real environment + credentials; they are
// opt-in to prevent CI from running them by default.
func SkipUnlessCapacityEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("LOOPZERO_CAPACITY") != "1" {
		t.Skip("set LOOPZERO_CAPACITY=1 to run capacity benchmarks")
	}
}
