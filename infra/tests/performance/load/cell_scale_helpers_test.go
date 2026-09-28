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
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Config & types
// ---------------------------------------------------------------------------

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
	builds      BuildAPI // the shared build contract client
}

// simulatorPollInterval is how often a simulated deploy asks for its build.
const simulatorPollInterval = 5 * time.Second

func NewDeploySimulator(apiURL, authToken string) *DeploySimulator {
	return newDeploySimulator(&DeploySimulator{apiURL: apiURL, authToken: authToken})
}

// NewServiceAuthSimulator creates a simulator using X-Service-Auth bypass.
// This is the recommended approach for load testing — no GitHub OAuth needed.
func NewServiceAuthSimulator(apiURL, serviceAuth, userID, tenantID, email string) *DeploySimulator {
	return newDeploySimulator(&DeploySimulator{
		apiURL:      apiURL,
		serviceAuth: serviceAuth,
		userID:      userID,
		tenantID:    tenantID,
		email:       email,
	})
}

func newDeploySimulator(s *DeploySimulator) *DeploySimulator {
	s.client = &http.Client{Timeout: 180 * time.Second}
	s.builds = BuildAPI{
		APIURL:       s.apiURL,
		Client:       s.client,
		Auth:         s.setAuth,
		PollInterval: simulatorPollInterval,
		RetryBackoff: DefaultSubmitBackoff,
	}
	return s
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

// CreateBuild triggers a build via POST /api/v1/builds and returns its id.
func (s *DeploySimulator) CreateBuild(ctx context.Context, projectName, env, commitSHA string) (string, error) {
	payload := map[string]string{"project_id": projectName, "environment": env, "commit_sha": commitSHA}
	key := fmt.Sprintf("load-test-%s-%s-%d", projectName, env, time.Now().UnixNano())
	id, err := s.builds.SubmitBuild(ctx, payload, key)
	if err != nil {
		return "", fmt.Errorf("create build %s: %w", projectName, err)
	}
	return id, nil
}

// WaitForBuild polls GET /api/v1/builds/:id until terminal state.
func (s *DeploySimulator) WaitForBuild(ctx context.Context, buildID string) (string, time.Duration, error) {
	return s.builds.WaitForBuild(ctx, buildID)
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

// summarizeSimulatedDeploys turns a finished set of simulated deploys into the
// report every cell-scale scenario prints and asserts on.
func summarizeSimulatedDeploys(results []DeployResult, elapsed time.Duration) *LoadTestResult {
	report := &LoadTestResult{TotalDeploys: len(results), Duration: elapsed, Results: results}
	totals := make([]time.Duration, 0, len(results))
	uploads := make([]time.Duration, 0, len(results))
	for _, r := range results {
		if r.Success {
			report.SuccessCount++
		} else {
			report.FailureCount++
		}
		totals = append(totals, r.Latency.Total)
		uploads = append(uploads, r.Latency.Upload)
	}
	report.DeployLatencyP50 = percentile(totals, 50)
	report.DeployLatencyP95 = percentile(totals, 95)
	report.DeployLatencyP99 = percentile(totals, 99)
	report.UploadLatencyP50 = percentile(uploads, 50)
	return report
}

// RunScenario deploys the projects loadtest-0..loadtest-(n-1) through the full
// simulated flow with fanOut's bounds and pacing, waits for every deploy, and
// summarizes them. onResult, when set, logs progress as each one finishes.
func (s *DeploySimulator) RunScenario(ctx context.Context, n int, opts fanOutOptions,
	onResult func(idx int, name string, r DeployResult)) *LoadTestResult {
	start := time.Now()
	results := fanOut(n, opts, func(idx int) DeployResult {
		name := fmt.Sprintf("loadtest-%d", idx)
		r := s.SimulateDeploy(ctx, name, "production")
		r.TenantIdx = idx
		if onResult != nil {
			onResult(idx, name, r)
		}
		return r
	})
	return summarizeSimulatedDeploys(results, time.Since(start))
}
