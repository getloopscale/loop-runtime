// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package capacity provides isolated micro-benchmarks per subsystem.
//
// Context (T-03 from 50k-immediate-focus):
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
//   - Baselines documented in capacity-baselines
//
// Usage:
//
//	LOOP_RUNTIME_API_URL=https://api.dev.getloopscale.com \
//	LOOP_RUNTIME_SERVICE_AUTH=... \
//	go test ./infra/tests/performance/capacity/ -run TestCapacityGitea \
//	  -args -rps=100 -duration=30s -out=/tmp/capacity-bench/gitea-$(date +%s).json
//
// Opt-in: set LOOP_RUNTIME_CAPACITY=1 to run these tests. Otherwise skipped.
package capacity

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	Subsystem    string        `json:"subsystem"`
	StartedAt    time.Time     `json:"started_at"`
	Duration     time.Duration `json:"duration"`
	RequestedRPS float64       `json:"requested_rps"`
	Workers      int           `json:"workers"`
	TotalReqs    int64         `json:"total_reqs"`
	Errors       int64         `json:"errors"`
	ErrorRate    float64       `json:"error_rate"`
	P50          time.Duration `json:"p50"`
	P95          time.Duration `json:"p95"`
	P99          time.Duration `json:"p99"`
	ActualRPS    float64       `json:"actual_rps"`
	Notes        string        `json:"notes,omitempty"`
}

// Latencies is a bounded, concurrency-safe collection of request latencies.
// Every request goroutine records into it at once.
type Latencies struct {
	mu      sync.Mutex
	samples []time.Duration
}

// NewLatencies pre-allocates room for capacity samples; later samples are
// dropped, so a long high-RPS run cannot grow the buffer without bound.
func NewLatencies(capacity int) *Latencies {
	return &Latencies{samples: make([]time.Duration, 0, capacity)}
}

// Record appends a latency sample, dropping it once the buffer is full.
func (l *Latencies) Record(d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.samples) < cap(l.samples) {
		l.samples = append(l.samples, d)
	}
}

// Count is the number of samples held.
func (l *Latencies) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.samples)
}

// Percentile returns the pth percentile in the collected samples.
func (l *Latencies) Percentile(p float64) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
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

// SkipUnlessCapacityEnabled skips the test unless LOOP_RUNTIME_CAPACITY=1 is set.
// Capacity tests assume a real environment + credentials; they are
// opt-in to prevent CI from running them by default.
func SkipUnlessCapacityEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("LOOP_RUNTIME_CAPACITY") != "1" {
		t.Skip("set LOOP_RUNTIME_CAPACITY=1 to run capacity benchmarks")
	}
}

// ---------------------------------------------------------------------------
// Extended result types for cell-budget.md assumption validation
// ---------------------------------------------------------------------------

// MemoryResult captures heap memory before, during, and after load.
// Used by go_memory_test.go and argocd_memory_test.go.
type MemoryResult struct {
	Subsystem         string                 `json:"subsystem"`
	StartedAt         time.Time              `json:"started_at"`
	BeforeHeapBytes   int64                  `json:"before_heap_bytes"`
	PeakHeapBytes     int64                  `json:"peak_heap_bytes"`
	AfterHeapBytes    int64                  `json:"after_heap_bytes"`
	DeltaHeapBytes    int64                  `json:"delta_heap_bytes"`
	ProjectedBytes    int64                  `json:"projected_bytes"`
	ActualVsProjected float64                `json:"actual_vs_projected"`
	CorrelationData   map[string]interface{} `json:"correlation_data,omitempty"`
	Notes             string                 `json:"notes,omitempty"`
}

// ConcurrencyResult captures peak observed concurrency from a saturation test.
// Used by buildkit_concurrency_test.go.
type ConcurrencyResult struct {
	Subsystem         string          `json:"subsystem"`
	StartedAt         time.Time       `json:"started_at"`
	SubmittedBuilds   int             `json:"submitted_builds"`
	PeakConcurrent    int             `json:"peak_concurrent"`
	ProjectedCeiling  int             `json:"projected_ceiling"`
	ActualCeiling     int             `json:"actual_ceiling"`
	Nodes             int             `json:"nodes"`
	PerNodeConcurrent float64         `json:"per_node_concurrent"`
	BuildDurations    []time.Duration `json:"build_durations"`
	Notes             string          `json:"notes,omitempty"`
}

// PerFrameworkResult captures memory per framework for pod sizing.
// Used by pod_memory_test.go.
type PerFrameworkResult struct {
	Subsystem    string            `json:"subsystem"`
	StartedAt    time.Time         `json:"started_at"`
	Frameworks   []FrameworkMemory `json:"frameworks"`
	ProjectedAvg int64             `json:"projected_avg_bytes"`
	ActualAvg    int64             `json:"actual_avg_bytes"`
	Notes        string            `json:"notes,omitempty"`
}

// FrameworkMemory captures resource usage for a single framework fixture.
type FrameworkMemory struct {
	Name      string `json:"name"`
	MemoryMi  int64  `json:"memory_mi"`
	CPUMillis int64  `json:"cpu_millis"`
	PodName   string `json:"pod_name"`
}

// WriteMemoryResult writes a MemoryResult as JSON.
func WriteMemoryResult(r MemoryResult, path string) error {
	return writeJSON(r, path)
}

// WriteConcurrencyResult writes a ConcurrencyResult as JSON.
func WriteConcurrencyResult(r ConcurrencyResult, path string) error {
	return writeJSON(r, path)
}

// WritePerFrameworkResult writes a PerFrameworkResult as JSON.
func WritePerFrameworkResult(r PerFrameworkResult, path string) error {
	return writeJSON(r, path)
}

func writeJSON(v interface{}, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ScrapePrometheusMetric fetches the /metrics endpoint and extracts a single
// gauge or counter value by exact metric name. For metrics WITHOUT labels
// (e.g., go_memstats_heap_inuse_bytes, loop_runtime_build_queue_depth) this returns
// the unique value. For metrics WITH labels (e.g., loop_runtime_builds_active{tenant})
// this returns ONLY the first label combination — use ScrapePrometheusMetricSum
// instead to get the total across all label values.
func ScrapePrometheusMetric(metricsURL, metricName string) (float64, error) {
	values, err := scrapeAllValues(metricsURL, metricName)
	if err != nil {
		return 0, err
	}
	if len(values) == 0 {
		return 0, fmt.Errorf("metric %q not found at %s", metricName, metricsURL)
	}
	return values[0], nil
}

// ScrapePrometheusMetricSum fetches the /metrics endpoint and SUMS all values
// for a given metric name across all label combinations. This is required for
// metrics like loop_runtime_builds_active{tenant="..."} where each tenant has its own
// time series. Without summing, you only get one tenant's count instead of the
// total active builds across the cell.
func ScrapePrometheusMetricSum(metricsURL, metricName string) (float64, error) {
	values, err := scrapeAllValues(metricsURL, metricName)
	if err != nil {
		return 0, err
	}
	if len(values) == 0 {
		return 0, fmt.Errorf("metric %q not found at %s", metricName, metricsURL)
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum, nil
}

// scrapeAllValues fetches /metrics and returns ALL values matching metricName,
// one per label combination. Handles both labeled and unlabeled metrics.
func scrapeAllValues(metricsURL, metricName string) ([]float64, error) {
	resp, err := http.Get(metricsURL + "/metrics")
	if err != nil {
		return nil, fmt.Errorf("scrape %s: %w", metricsURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("scrape %s: HTTP %d", metricsURL, resp.StatusCode)
	}

	var values []float64
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, metricName) {
			continue
		}
		rest := line[len(metricName):]
		if len(rest) == 0 {
			continue
		}
		if rest[0] == '{' {
			idx := strings.Index(rest, "}")
			if idx < 0 {
				continue
			}
			rest = rest[idx+1:]
		} else if rest[0] != ' ' {
			// Not an exact match (e.g., metricName is prefix of another metric)
			continue
		}
		rest = strings.TrimSpace(rest)
		val, err := strconv.ParseFloat(rest, 64)
		if err != nil {
			continue
		}
		values = append(values, val)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan metrics: %w", err)
	}
	return values, nil
}

// ---------------------------------------------------------------------------
// kubectl helpers for multi-replica aggregation and node monitoring
// ---------------------------------------------------------------------------

// PodResourceUsage captures CPU and memory for a single pod from kubectl top.
type PodResourceUsage struct {
	Name      string `json:"name"`
	MemoryMi  int64  `json:"memory_mi"`
	CPUMillis int64  `json:"cpu_millis"`
}

// KubectlTopPods runs "kubectl top pod" with a label selector and returns
// resource usage for ALL matching pods. This solves the multi-replica
// aggregation gap: instead of port-forwarding to one pod and measuring
// only that replica's memory, we measure all replicas via the metrics-server.
func KubectlTopPods(ctx context.Context, kubeCtx, namespace, labelSelector string) ([]PodResourceUsage, error) {
	args := []string{"--context=" + kubeCtx, "-n", namespace, "top", "pod", "--no-headers"}
	if labelSelector != "" {
		args = append(args, "-l", labelSelector)
	}
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("kubectl top pod: %w: %s", err, string(out))
	}
	return parseKubectlTop(string(out))
}

// KubectlTopNodes runs "kubectl top node" on a cluster and returns resource
// usage for all nodes. Used to monitor build cluster nodes during BuildKit
// concurrency tests — captures CPU/memory saturation that API metrics miss.
func KubectlTopNodes(ctx context.Context, kubeCtx string) ([]PodResourceUsage, error) {
	cmd := exec.CommandContext(ctx, "kubectl", "--context="+kubeCtx, "top", "node", "--no-headers")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("kubectl top node: %w: %s", err, string(out))
	}
	return parseKubectlTop(string(out))
}

// TotalMemoryMi sums MemoryMi across all entries.
func TotalMemoryMi(pods []PodResourceUsage) int64 {
	var total int64
	for _, p := range pods {
		total += p.MemoryMi
	}
	return total
}

// parseKubectlTop parses "kubectl top pod/node --no-headers" output.
// Format: "NAME  CPU(cores)  MEMORY(bytes)" per line.
func parseKubectlTop(output string) ([]PodResourceUsage, error) {
	var result []PodResourceUsage
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		name := fields[0]
		cpuMillis := parseCPU(fields[1])
		memMi := parseMemory(fields[2])

		result = append(result, PodResourceUsage{
			Name:      name,
			MemoryMi:  memMi,
			CPUMillis: cpuMillis,
		})
	}
	return result, nil
}

// parseCPU parses kubectl top CPU format: "1m" → 1, "100m" → 100, "2" → 2000.
func parseCPU(s string) int64 {
	if strings.HasSuffix(s, "m") {
		var v int64
		fmt.Sscanf(s, "%dm", &v)
		return v
	}
	var v int64
	fmt.Sscanf(s, "%d", &v)
	return v * 1000
}

// parseMemory parses kubectl top memory format: "23Mi" → 23, "1Gi" → 1024, "456Ki" → 0.
func parseMemory(s string) int64 {
	if strings.HasSuffix(s, "Gi") {
		var v int64
		fmt.Sscanf(s, "%dGi", &v)
		return v * 1024
	}
	if strings.HasSuffix(s, "Mi") {
		var v int64
		fmt.Sscanf(s, "%dMi", &v)
		return v
	}
	if strings.HasSuffix(s, "Ki") {
		var v int64
		fmt.Sscanf(s, "%dKi", &v)
		return v / 1024
	}
	return 0
}
