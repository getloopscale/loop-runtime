// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package load implements progressive load tests for the LoopZero platform.
//
// Usage:
//
//	# L0 — Baseline (current 2 tenants, single deploy)
//	LOOPZERO_LOAD_TEST=1 LOOPZERO_DRILL_API_URL=https://api.loopzero-drill.example LOOPZERO_AUTH_TOKEN=<token> \
//	  go test ./infra/tests/load/ -v -run TestL0_Baseline -timeout 10m
//
//	# L1 — 10 tenants, concurrent deploys
//	LOOPZERO_LOAD_TEST=1 LOOPZERO_DRILL_API_URL=https://api.loopzero-drill.example LOOPZERO_AUTH_TOKEN=<token> \
//	  go test ./infra/tests/load/ -v -run TestL1_10Tenants -timeout 30m
//
//	# L2 — 50 tenants, sustained load
//	LOOPZERO_LOAD_TEST=1 LOOPZERO_DRILL_API_URL=https://api.loopzero-drill.example LOOPZERO_AUTH_TOKEN=<token> \
//	  go test ./infra/tests/load/ -v -run TestL2_50Tenants -timeout 60m
//
// This test is SKIPPED unless LOOPZERO_LOAD_TEST=1 is set.
package load

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Config & types
// ---------------------------------------------------------------------------

type LoadTestConfig struct {
	Tenants       int           // Number of synthetic tenants
	DeploysPerMin int           // Target deploy rate (0 = all at once)
	Duration      time.Duration // Test duration (0 = single round)
	BurstSize     int           // Concurrent deploys in burst mode
	APIEndpoint   string
	AuthToken     string
}

type StageLatency struct {
	Upload time.Duration
	Build  time.Duration
	Total  time.Duration
}

type DeployResult struct {
	TenantIdx int
	ProjectID string
	BuildID   string
	Success   bool
	Error     string
	Latency   StageLatency
}

type LoadTestResult struct {
	TotalDeploys     int
	SuccessCount     int64
	FailureCount     int64
	Duration         time.Duration
	DeployLatencyP50 time.Duration
	DeployLatencyP95 time.Duration
	DeployLatencyP99 time.Duration
	UploadLatencyP50 time.Duration
	Results          []DeployResult
}

// ---------------------------------------------------------------------------
// DeploySimulator — real HTTP calls to LoopZero API
// ---------------------------------------------------------------------------

type DeploySimulator struct {
	apiURL      string
	authToken   string // Bearer token (GitHub)
	serviceAuth string // X-Service-Auth (internal bypass)
	userID      string // For service auth identity
	tenantID    string // For service auth identity
	email       string // For service auth identity
	client      *http.Client
}

func NewDeploySimulator(apiURL, authToken string) *DeploySimulator {
	return &DeploySimulator{
		apiURL:    apiURL,
		authToken: authToken,
		client: &http.Client{
			Timeout: 180 * time.Second,
		},
	}
}

// NewServiceAuthSimulator creates a simulator using X-Service-Auth bypass.
// This is the recommended approach for load testing — no GitHub OAuth needed.
func NewServiceAuthSimulator(apiURL, serviceAuth, userID, tenantID, email string) *DeploySimulator {
	return &DeploySimulator{
		apiURL:      apiURL,
		serviceAuth: serviceAuth,
		userID:      userID,
		tenantID:    tenantID,
		email:       email,
		client: &http.Client{
			Timeout: 180 * time.Second,
		},
	}
}

// setAuth applies auth headers to the request.
func (s *DeploySimulator) setAuth(req *http.Request) {
	if s.serviceAuth != "" {
		req.Header.Set("X-Service-Auth", s.serviceAuth)
		req.Header.Set("X-LoopZero-User-ID", s.userID)
		req.Header.Set("X-LoopZero-Tenant", s.tenantID)
		req.Header.Set("X-LoopZero-Email", s.email)
	} else if s.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.authToken)
	}
}

// CreateProject creates a project via POST /api/v1/projects.
// Returns nil if project already exists (idempotent).
func (s *DeploySimulator) CreateProject(ctx context.Context, name string) error {
	body := fmt.Sprintf(`{"name":%q}`, name)
	req, err := http.NewRequestWithContext(ctx, "POST",
		s.apiURL+"/api/v1/projects",
		bytes.NewBufferString(body))
	if err != nil {
		return err
	}
	s.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	defer resp.Body.Close()

	// 201 = created, 409 = already exists — both OK
	if resp.StatusCode != 201 && resp.StatusCode != 409 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create project %s: HTTP %d: %s", name, resp.StatusCode, string(respBody))
	}
	return nil
}

// UploadBundle uploads a minimal source bundle via POST /api/v1/source/upload.
// Returns the commit SHA from the response.
func (s *DeploySimulator) UploadBundle(ctx context.Context, projectName string) (string, time.Duration, error) {
	start := time.Now()

	// Create minimal loopzero.yaml
	loopzeroYAML := fmt.Sprintf(`version: "2"
project: %s
apps:
  api:
    build: buildpack
    framework: node
    render: server
`, projectName)

	// Create minimal bundle (empty tar.gz — the build will fail but we measure upload+queue latency)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// loopzero_yaml field
	yamlPart, err := writer.CreateFormFile("loopzero_yaml", "loopzero.yaml")
	if err != nil {
		return "", 0, err
	}
	if _, err := yamlPart.Write([]byte(loopzeroYAML)); err != nil {
		return "", 0, err
	}

	// bundle field — minimal valid git bundle header
	bundlePart, err := writer.CreateFormFile("bundle", "source.bundle")
	if err != nil {
		return "", 0, err
	}
	// Git bundle v2 header (minimal — enough to pass upload validation)
	bundleHeader := "# v2 git bundle\n-0000000000000000000000000000000000000000 HEAD\n\n"
	if _, err := bundlePart.Write([]byte(bundleHeader)); err != nil {
		return "", 0, err
	}
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST",
		s.apiURL+"/api/v1/source/upload",
		&body)
	if err != nil {
		return "", 0, err
	}
	s.setAuth(req)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := s.client.Do(req)
	if err != nil {
		return "", time.Since(start), fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()
	uploadLatency := time.Since(start)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", uploadLatency, fmt.Errorf("upload %s: HTTP %d: %s", projectName, resp.StatusCode, string(respBody))
	}

	var uploadResp struct {
		CommitSHA string `json:"commit_sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return "", uploadLatency, nil // Non-fatal — some endpoints don't return JSON
	}

	return uploadResp.CommitSHA, uploadLatency, nil
}

// CreateBuild triggers a build via POST /api/v1/builds.
// Returns the build ID.
func (s *DeploySimulator) CreateBuild(ctx context.Context, projectName, env, commitSHA string) (string, error) {
	reqBody := fmt.Sprintf(`{"project_id":%q,"environment":%q,"commit_sha":%q}`,
		projectName, env, commitSHA)

	req, err := http.NewRequestWithContext(ctx, "POST",
		s.apiURL+"/api/v1/builds",
		bytes.NewBufferString(reqBody))
	if err != nil {
		return "", err
	}
	s.setAuth(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprintf("load-test-%s-%s-%d", projectName, env, time.Now().UnixNano()))

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("create build: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 201 && resp.StatusCode != 202 {
		return "", fmt.Errorf("create build %s: HTTP %d: %s", projectName, resp.StatusCode, string(respBody))
	}

	var buildResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &buildResp); err != nil {
		return "", nil
	}

	return buildResp.ID, nil
}

// WaitForBuild polls GET /api/v1/builds/:id until terminal state.
func (s *DeploySimulator) WaitForBuild(ctx context.Context, buildID string) (string, time.Duration, error) {
	start := time.Now()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "TIMEOUT", time.Since(start), ctx.Err()
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, "GET",
				s.apiURL+"/api/v1/builds/"+buildID, nil)
			if err != nil {
				continue
			}
			s.setAuth(req)

			resp, err := s.client.Do(req)
			if err != nil {
				continue
			}
			var build struct {
				State string `json:"state"`
			}
			json.NewDecoder(resp.Body).Decode(&build)
			resp.Body.Close()

			switch build.State {
			case "SUCCEEDED":
				return "SUCCEEDED", time.Since(start), nil
			case "FAILED", "CANCELLED":
				return build.State, time.Since(start), fmt.Errorf("build %s: %s", buildID, build.State)
			}
			// QUEUED, RUNNING, RELEASING — keep waiting
		}
	}
}

// SimulateDeploy runs the full deploy flow: upload → build → wait.
func (s *DeploySimulator) SimulateDeploy(ctx context.Context, projectName, env string) DeployResult {
	result := DeployResult{ProjectID: projectName}
	totalStart := time.Now()

	// Upload
	commitSHA, uploadLatency, err := s.UploadBundle(ctx, projectName)
	result.Latency.Upload = uploadLatency
	if err != nil {
		result.Error = fmt.Sprintf("upload: %v", err)
		result.Latency.Total = time.Since(totalStart)
		return result
	}

	// Create build
	buildID, err := s.CreateBuild(ctx, projectName, env, commitSHA)
	if err != nil {
		result.Error = fmt.Sprintf("build create: %v", err)
		result.Latency.Total = time.Since(totalStart)
		return result
	}
	result.BuildID = buildID

	// Wait for completion
	state, buildLatency, err := s.WaitForBuild(ctx, buildID)
	result.Latency.Build = buildLatency
	result.Latency.Total = time.Since(totalStart)

	if err != nil {
		result.Error = fmt.Sprintf("build %s: %s", buildID, state)
		return result
	}

	result.Success = true
	return result
}

// ---------------------------------------------------------------------------
// Metrics helpers
// ---------------------------------------------------------------------------

func percentile(latencies []time.Duration, p int) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	idx := int(math.Ceil(float64(len(latencies)*p)/100.0)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(latencies) {
		idx = len(latencies) - 1
	}
	return latencies[idx]
}

func printReport(t *testing.T, result *LoadTestResult) {
	t.Logf("═══════════════════════════════════════════")
	t.Logf("  LOAD TEST REPORT")
	t.Logf("═══════════════════════════════════════════")
	t.Logf("  Total deploys:  %d", result.TotalDeploys)
	t.Logf("  Success:        %d", result.SuccessCount)
	t.Logf("  Failures:       %d", result.FailureCount)
	t.Logf("  Duration:       %s", result.Duration.Round(time.Second))
	t.Logf("───────────────────────────────────────────")
	t.Logf("  Deploy p50:     %s", result.DeployLatencyP50.Round(time.Millisecond))
	t.Logf("  Deploy p95:     %s", result.DeployLatencyP95.Round(time.Millisecond))
	t.Logf("  Deploy p99:     %s", result.DeployLatencyP99.Round(time.Millisecond))
	t.Logf("  Upload p50:     %s", result.UploadLatencyP50.Round(time.Millisecond))
	t.Logf("═══════════════════════════════════════════")

	if result.FailureCount > 0 {
		t.Logf("  Failed deploys:")
		for _, r := range result.Results {
			if !r.Success {
				t.Logf("    - %s: %s", r.ProjectID, r.Error)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Test functions
// ---------------------------------------------------------------------------

// requireLoadTestEnv returns API URL and a configured DeploySimulator.
// Supports two auth modes:
//   - LOOPZERO_AUTH_TOKEN: Bearer token (GitHub OAuth)
//   - LOOPZERO_SERVICE_AUTH: X-Service-Auth bypass (recommended for load tests)
func requireLoadTestEnv(t *testing.T) (string, *DeploySimulator) {
	t.Helper()
	if os.Getenv("LOOPZERO_LOAD_TEST") != "1" {
		t.Skip("skipping load test: set LOOPZERO_LOAD_TEST=1 to run")
	}
	// Plan 2026-05-16 T3.1: prefer LOOPZERO_DRILL_API_URL when running against
	// drill prod-identical stack (per skill /drill-test). Fallback to
	// LOOPZERO_API_URL for backward compat with dev cluster runs.
	apiURL := os.Getenv("LOOPZERO_DRILL_API_URL")
	if apiURL == "" {
		apiURL = os.Getenv("LOOPZERO_API_URL")
	}
	if apiURL == "" {
		t.Fatal("LOOPZERO_DRILL_API_URL or LOOPZERO_API_URL required")
	}
	if drillURL := os.Getenv("LOOPZERO_DRILL_API_URL"); drillURL != "" {
		t.Logf("Running against drill prod-identical stack: %s", drillURL)
	}

	// Prefer service auth (no GitHub token needed)
	if serviceAuth := os.Getenv("LOOPZERO_SERVICE_AUTH"); serviceAuth != "" {
		tenantID := os.Getenv("LOOPZERO_LOAD_TENANT")
		if tenantID == "" {
			tenantID = "loadtest-tn-0"
		}
		t.Logf("Using X-Service-Auth (tenant=%s)", tenantID)
		return apiURL, NewServiceAuthSimulator(apiURL, serviceAuth,
			"loadtest-user-0", tenantID, "loadtest-0@loadtest.loopzero.dev")
	}

	// Fallback to Bearer token
	authToken := os.Getenv("LOOPZERO_AUTH_TOKEN")
	if authToken == "" {
		t.Fatal("LOOPZERO_AUTH_TOKEN or LOOPZERO_SERVICE_AUTH required")
	}
	return apiURL, NewDeploySimulator(apiURL, authToken)
}

// TestL0_Baseline measures a single deploy against an existing project.
// No synthetic tenants created — uses whatever exists.
func TestL4_LockStress(t *testing.T) {
	_, sim := requireLoadTestEnv(t)

	const concurrent = 50
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t.Logf("L4 Lock Stress — %d concurrent builds for same project", concurrent)

	projectName := "loadtest-lock-stress"
	if err := sim.CreateProject(ctx, projectName); err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	var (
		wg       sync.WaitGroup
		accepted int64
		rejected int64
		errors   int64
	)

	start := time.Now()
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := sim.CreateBuild(ctx, projectName, "production",
				fmt.Sprintf("stress-sha-%d", idx))
			if err != nil {
				// 409 = lock contention (expected), other errors = unexpected
				errStr := err.Error()
				if contains(errStr, "409") || contains(errStr, "conflict") || contains(errStr, "already") {
					atomic.AddInt64(&rejected, 1)
				} else {
					atomic.AddInt64(&errors, 1)
					t.Logf("  Unexpected error: %v", err)
				}
			} else {
				atomic.AddInt64(&accepted, 1)
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	t.Logf("═══════════════════════════════════════════")
	t.Logf("  LOCK STRESS REPORT")
	t.Logf("═══════════════════════════════════════════")
	t.Logf("  Concurrent:  %d", concurrent)
	t.Logf("  Accepted:    %d (should be exactly 1)", accepted)
	t.Logf("  Rejected:    %d (409 lock contention)", rejected)
	t.Logf("  Errors:      %d (unexpected failures)", errors)
	t.Logf("  Duration:    %s", duration.Round(time.Millisecond))
	t.Logf("═══════════════════════════════════════════")

	// Exactly 1 build should be accepted, rest rejected by lock
	if accepted > 1 {
		t.Errorf("L4 FAIL: %d builds accepted (expected exactly 1 — lock broken)", accepted)
	}
	if errors > 0 {
		t.Errorf("L4 FAIL: %d unexpected errors (not 409 rejections)", errors)
	}
}

func TestL4_500Tenants(t *testing.T) {
	_, sim := requireLoadTestEnv(t)

	const tenantCount = 500
	const concurrency = 20 // 20 concurrent deploys
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Minute)
	defer cancel()

	t.Logf("L4 — %d tenants, %d concurrent, 2 cells", tenantCount, concurrency)

	// Create projects in batches of 50
	for batch := 0; batch < tenantCount; batch += 50 {
		end := batch + 50
		if end > tenantCount {
			end = tenantCount
		}
		for i := batch; i < end; i++ {
			name := fmt.Sprintf("loadtest-%d", i)
			if err := sim.CreateProject(ctx, name); err != nil {
				t.Fatalf("Failed to create project %s: %v", name, err)
			}
		}
		t.Logf("  Projects %d-%d created", batch, end-1)
	}

	// Deploy with concurrency limiter
	var (
		results  []DeployResult
		mu       sync.Mutex
		success  int64
		failures int64
	)

	start := time.Now()
	sem := make(chan struct{}, concurrency)

	var wg sync.WaitGroup
	for i := 0; i < tenantCount; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()

			name := fmt.Sprintf("loadtest-%d", idx)
			r := sim.SimulateDeploy(ctx, name, "production")
			r.TenantIdx = idx

			mu.Lock()
			results = append(results, r)
			mu.Unlock()

			if r.Success {
				atomic.AddInt64(&success, 1)
			} else {
				atomic.AddInt64(&failures, 1)
			}

			if idx%50 == 0 || !r.Success {
				t.Logf("  [%d/%d] %s: success=%v latency=%s",
					idx+1, tenantCount, name, r.Success, r.Latency.Total.Round(time.Second))
			}
		}(i)
	}

	wg.Wait()
	totalDuration := time.Since(start)

	var totalLatencies, uploadLatencies []time.Duration
	for _, r := range results {
		totalLatencies = append(totalLatencies, r.Latency.Total)
		uploadLatencies = append(uploadLatencies, r.Latency.Upload)
	}

	report := &LoadTestResult{
		TotalDeploys:     len(results),
		SuccessCount:     success,
		FailureCount:     failures,
		Duration:         totalDuration,
		DeployLatencyP50: percentile(totalLatencies, 50),
		DeployLatencyP95: percentile(totalLatencies, 95),
		DeployLatencyP99: percentile(totalLatencies, 99),
		UploadLatencyP50: percentile(uploadLatencies, 50),
		Results:          results,
	}

	printReport(t, report)

	// L4 pass criteria: >99% success, p99 < 10min
	failRate := float64(failures) / float64(max(success+failures, 1)) * 100
	if failRate > 1 {
		t.Errorf("L4 FAIL: failure rate %.1f%% exceeds 1%%", failRate)
	}
	if report.DeployLatencyP99 > 10*time.Minute {
		t.Errorf("L4 FAIL: deploy p99 %s exceeds 10min", report.DeployLatencyP99)
	}
}

func TestL5_1000Tenants_MultiCell(t *testing.T) {
	_, sim := requireLoadTestEnv(t)

	const tenantCount = 1000
	const concurrency = 50 // 50 concurrent deploys
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Minute)
	defer cancel()

	t.Logf("L5 — %d tenants, %d concurrent, multi-cell", tenantCount, concurrency)

	// Create projects in batches of 100
	for batch := 0; batch < tenantCount; batch += 100 {
		end := batch + 100
		if end > tenantCount {
			end = tenantCount
		}
		for i := batch; i < end; i++ {
			name := fmt.Sprintf("loadtest-%d", i)
			if err := sim.CreateProject(ctx, name); err != nil {
				t.Fatalf("Failed to create project %s: %v", name, err)
			}
		}
		t.Logf("  Projects %d-%d created", batch, end-1)
	}

	// Deploy with concurrency limiter
	var (
		results  []DeployResult
		mu       sync.Mutex
		success  int64
		failures int64
	)

	start := time.Now()
	sem := make(chan struct{}, concurrency)

	var wg sync.WaitGroup
	for i := 0; i < tenantCount; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()

			name := fmt.Sprintf("loadtest-%d", idx)
			r := sim.SimulateDeploy(ctx, name, "production")
			r.TenantIdx = idx

			mu.Lock()
			results = append(results, r)
			mu.Unlock()

			if r.Success {
				atomic.AddInt64(&success, 1)
			} else {
				atomic.AddInt64(&failures, 1)
			}

			if idx%100 == 0 || !r.Success {
				t.Logf("  [%d/%d] %s: success=%v latency=%s",
					idx+1, tenantCount, name, r.Success, r.Latency.Total.Round(time.Second))
			}
		}(i)
	}

	wg.Wait()
	totalDuration := time.Since(start)

	var totalLatencies, uploadLatencies []time.Duration
	for _, r := range results {
		totalLatencies = append(totalLatencies, r.Latency.Total)
		uploadLatencies = append(uploadLatencies, r.Latency.Upload)
	}

	report := &LoadTestResult{
		TotalDeploys:     len(results),
		SuccessCount:     success,
		FailureCount:     failures,
		Duration:         totalDuration,
		DeployLatencyP50: percentile(totalLatencies, 50),
		DeployLatencyP95: percentile(totalLatencies, 95),
		DeployLatencyP99: percentile(totalLatencies, 99),
		UploadLatencyP50: percentile(uploadLatencies, 50),
		Results:          results,
	}

	printReport(t, report)

	// L5 pass criteria: >99% success, p99 < 10min
	failRate := float64(failures) / float64(max(success+failures, 1)) * 100
	if failRate > 1 {
		t.Errorf("L5 FAIL: failure rate %.1f%% exceeds 1%%", failRate)
	}
	if report.DeployLatencyP99 > 10*time.Minute {
		t.Errorf("L5 FAIL: deploy p99 %s exceeds 10min", report.DeployLatencyP99)
	}
	if totalDuration > 240*time.Minute {
		t.Errorf("L5 FAIL: total duration %s exceeds 4h", totalDuration.Round(time.Minute))
	}
}
