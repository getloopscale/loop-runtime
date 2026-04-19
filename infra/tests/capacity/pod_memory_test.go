// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package capacity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/loopzero/loopzero/infra/tests/load"
)

// TestCapacityPodMemoryByFramework validates the cell-budget.md assumption:
//
//	"Pod memory real. The 50 Mi/pod average is from one fixture (loadtest-app).
//	 Real tenant apps (Django, Rails, Next.js) will be bigger."
//
// The test clones github.com/getloopscale/loopzero-stacks, deploys each framework
// template via the full pipeline, waits for the pod to stabilize, then
// measures memory via kubectl top. This gives the ACTUAL memory footprint
// of each framework stack that LoopZero supports.
//
// Templates used (from loopzero-stacks): go-api, node-express, python-fastapi,
// ruby-sinatra, rust-axum.
//
// Run:
//
//	LOOPZERO_CAPACITY=1 \
//	LOOPZERO_API_URL=https://api.dev.loopzero.example \
//	LOOPZERO_SERVICE_AUTH=... \
//	LOOPZERO_APP_DOMAIN=dev.loopzero.example \
//	LOOPZERO_RUNTIME_CONTEXT=do-sfo3-loopzero-infra-dev-sfo3-runtime \
//	go test ./infra/tests/capacity/ -run TestCapacityPodMemoryByFramework -v -timeout 120m
func TestCapacityPodMemoryByFramework(t *testing.T) {
	SkipUnlessCapacityEnabled(t)

	apiURL := os.Getenv("LOOPZERO_API_URL")
	serviceAuth := os.Getenv("LOOPZERO_SERVICE_AUTH")
	appDomain := os.Getenv("LOOPZERO_APP_DOMAIN")
	runtimeCtx := os.Getenv("LOOPZERO_RUNTIME_CONTEXT")
	if apiURL == "" || serviceAuth == "" || appDomain == "" || runtimeCtx == "" {
		t.Fatal("LOOPZERO_API_URL, LOOPZERO_SERVICE_AUTH, LOOPZERO_APP_DOMAIN, LOOPZERO_RUNTIME_CONTEXT required")
	}

	cfg := NewConfig("pod-memory")

	const (
		projectedAvgMi = 50 // cell-budget.md: 50 Mi/pod average
		stabilizeWait  = 30 * time.Second
		stacksRepo     = "https://github.com/getloopscale/loopzero-stacks.git"
	)

	// Frameworks to benchmark: template name → loopzero.yaml metadata.
	type frameworkSpec struct {
		Template  string // directory name in loopzero-stacks/templates/
		Framework string // framework field for submitBuild
		Port      int
	}

	frameworks := []frameworkSpec{
		{Template: "go-api", Framework: "custom", Port: 8080},
		{Template: "node-express", Framework: "express", Port: 3000},
		{Template: "python-fastapi", Framework: "fastapi", Port: 8000},
		{Template: "ruby-sinatra", Framework: "sinatra", Port: 4567},
		{Template: "rust-axum", Framework: "axum", Port: 8080},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Minute)
	defer cancel()

	// --- Step 1: Clone loopzero-stacks ---
	stacksDir, err := os.MkdirTemp("", "loopzero-stacks-*")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	defer os.RemoveAll(stacksDir)

	t.Logf("cloning %s...", stacksRepo)
	cloneCmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", stacksRepo, stacksDir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		t.Fatalf("clone loopzero-stacks: %v: %s", err, string(out))
	}

	client := &http.Client{Timeout: 300 * time.Second}

	// --- Step 2: Deploy each framework and measure memory ---
	var results []FrameworkMemory

	for fwIdx, fw := range frameworks {
		t.Logf("=== framework: %s (%d/%d) ===", fw.Template, fwIdx+1, len(frameworks))
		templateDir := filepath.Join(stacksDir, "templates", fw.Template)

		if _, err := os.Stat(templateDir); err != nil {
			t.Logf("  SKIP: template %s not found: %v", fw.Template, err)
			continue
		}

		tenant := load.NewTenantIdentity("pm", fwIdx)
		projectName := fmt.Sprintf("cap-pm-%s", fw.Template)
		tenant.ProjectName = projectName

		// Provision tenant.
		harness, err := load.NewRealDeployHarness(load.RealDeployConfig{
			APIURL:      apiURL,
			AppDomain:   appDomain,
			ServiceAuth: serviceAuth,
			FixturePath: templateDir,
		})
		if err != nil {
			t.Logf("  ERROR creating harness: %v", err)
			continue
		}

		if _, err := harness.ProvisionTenant(ctx, tenant); err != nil {
			t.Logf("  warning: provision %s: %v (may exist)", tenant.TenantID, err)
		}

		defer func(tn load.TenantIdentity) {
			_ = harness.CleanupTenant(context.Background(), tn)
		}(tenant)

		// Prepare bundle from template directory.
		bundle, err := load.PrepareFixtureBundle(templateDir)
		if err != nil {
			t.Logf("  ERROR preparing bundle: %v", err)
			continue
		}

		// Upload with the correct loopzero.yaml for this framework.
		loopzeroYAML := fmt.Sprintf("version: 2\nproject: %s\napps:\n  api:\n    build: dockerfile\n    framework: %s\n    render: server\n    port: %d\n",
			projectName, fw.Framework, fw.Port)

		commitSHA, err := uploadBundle(ctx, client, apiURL, serviceAuth, tenant, loopzeroYAML, bundle)
		if err != nil {
			t.Logf("  ERROR upload: %v", err)
			continue
		}

		// Submit build.
		buildID, err := submitFrameworkBuild(ctx, client, apiURL, serviceAuth, tenant, commitSHA, fw.Framework, fw.Port)
		if err != nil {
			t.Logf("  ERROR submit: %v", err)
			continue
		}
		t.Logf("  build: %s", buildID)

		// Wait for build to complete.
		status, err := waitBuild(ctx, client, apiURL, serviceAuth, tenant, buildID)
		if err != nil || status != "SUCCEEDED" {
			t.Logf("  ERROR build %s: %s (%v)", buildID, status, err)
			continue
		}
		t.Logf("  build SUCCEEDED")

		// Wait for pod to stabilize.
		t.Logf("  waiting %v for pod memory to stabilize...", stabilizeWait)
		time.Sleep(stabilizeWait)

		// Measure memory via kubectl top.
		ns := fmt.Sprintf("%s-%s-production", tenant.TenantID, projectName)
		memMi, cpuMillis, podName, err := kubectlTopPod(ctx, runtimeCtx, ns)
		if err != nil {
			t.Logf("  ERROR kubectl top: %v (trying namespace variants...)", err)
			// Try alternative namespace patterns.
			ns = tenant.TenantID + "-production"
			memMi, cpuMillis, podName, err = kubectlTopPod(ctx, runtimeCtx, ns)
			if err != nil {
				t.Logf("  ERROR kubectl top (alt ns): %v", err)
				continue
			}
		}

		t.Logf("  pod: %s — %d Mi, %d m CPU", podName, memMi, cpuMillis)

		results = append(results, FrameworkMemory{
			Name:      fw.Template,
			MemoryMi:  memMi,
			CPUMillis: cpuMillis,
			PodName:   podName,
		})
	}

	if len(results) == 0 {
		t.Fatal("no frameworks deployed successfully — cannot measure pod memory")
	}

	// --- Step 3: Report ---
	var totalMem int64
	for _, r := range results {
		totalMem += r.MemoryMi
	}
	avgMem := totalMem / int64(len(results))

	result := PerFrameworkResult{
		Subsystem:    "pod-memory-by-framework",
		StartedAt:    time.Now(),
		Frameworks:   results,
		ProjectedAvg: int64(projectedAvgMi) * 1024 * 1024,
		ActualAvg:    avgMem * 1024 * 1024,
		Notes:        fmt.Sprintf("source: getloopscale/loopzero-stacks templates, %d/%d frameworks measured", len(results), len(frameworks)),
	}

	t.Log("=== pod-memory-by-framework ===")
	for _, r := range results {
		t.Logf("  %-20s %4d Mi  %4d m CPU  (%s)", r.Name, r.MemoryMi, r.CPUMillis, r.PodName)
	}
	t.Logf("  ---")
	t.Logf("  average:           %4d Mi (projected: %d Mi)", avgMem, projectedAvgMi)

	if err := WritePerFrameworkResult(result, cfg.OutFile); err != nil {
		t.Errorf("write result: %v", err)
	} else {
		t.Logf("  result:            %s", cfg.OutFile)
	}

	// --- Assertions ---
	for _, r := range results {
		if r.MemoryMi > 256 {
			t.Errorf("framework %s uses %d Mi — exceeds comfortable pod budget (256 Mi)", r.Name, r.MemoryMi)
		}
	}
	if avgMem > 100 {
		t.Errorf("actual average %d Mi significantly exceeds cell-budget projection of %d Mi", avgMem, projectedAvgMi)
	}
	if avgMem > 0 && avgMem <= int64(projectedAvgMi) {
		t.Logf("NOTE: average %d Mi ≤ projected %d Mi — projection holds", avgMem, projectedAvgMi)
	}
}

// --- Helpers ---

func uploadBundle(ctx context.Context, client *http.Client, apiURL, serviceAuth string, t load.TenantIdentity, loopzeroYAML string, bundle []byte) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	yamlPart, err := writer.CreateFormField("loopzero_yaml")
	if err != nil {
		return "", err
	}
	yamlPart.Write([]byte(loopzeroYAML))

	bundlePart, err := writer.CreateFormFile("bundle", "source.bundle")
	if err != nil {
		return "", err
	}
	bundlePart.Write(bundle)
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL+"/api/v1/source/upload", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Service-Auth", serviceAuth)
	req.Header.Set("X-LoopZero-User-ID", t.UserID)
	req.Header.Set("X-LoopZero-Tenant", t.TenantID)
	req.Header.Set("X-LoopZero-Email", t.Email)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload HTTP %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		CommitSHA string `json:"commit_sha"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.CommitSHA, nil
}

func submitFrameworkBuild(ctx context.Context, client *http.Client, apiURL, serviceAuth string, t load.TenantIdentity, commitSHA, framework string, port int) (string, error) {
	reqBody := fmt.Sprintf(`{
		"project_id": %q,
		"environment": "production",
		"commit_sha": %q,
		"apps": [{"name":"api","build":"dockerfile","framework":%q,"render":"server","port":%d}]
	}`, t.ProjectName, commitSHA, framework, port)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL+"/api/v1/builds", strings.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Service-Auth", serviceAuth)
	req.Header.Set("X-LoopZero-User-ID", t.UserID)
	req.Header.Set("X-LoopZero-Tenant", t.TenantID)
	req.Header.Set("X-LoopZero-Email", t.Email)
	req.Header.Set("Idempotency-Key", fmt.Sprintf("pm-%s-%d", t.TenantID, time.Now().UnixNano()))

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 202 && resp.StatusCode != 201 {
		return "", fmt.Errorf("submit HTTP %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		BuildID string `json:"build_id"`
	}
	json.Unmarshal(b, &result)
	return result.BuildID, nil
}

func waitBuild(ctx context.Context, client *http.Client, apiURL, serviceAuth string, t load.TenantIdentity, buildID string) (string, error) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	deadline := time.After(30 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return "TIMEOUT", ctx.Err()
		case <-deadline:
			return "TIMEOUT", fmt.Errorf("build %s timed out after 30m", buildID)
		case <-ticker.C:
			req, _ := http.NewRequestWithContext(ctx, "GET", apiURL+"/api/v1/builds/"+buildID, nil)
			req.Header.Set("X-Service-Auth", serviceAuth)
			req.Header.Set("X-LoopZero-User-ID", t.UserID)
			req.Header.Set("X-LoopZero-Tenant", t.TenantID)
			req.Header.Set("X-LoopZero-Email", t.Email)

			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			var build struct {
				Status string `json:"status"`
			}
			json.NewDecoder(resp.Body).Decode(&build)
			resp.Body.Close()

			switch build.Status {
			case "SUCCEEDED":
				return "SUCCEEDED", nil
			case "FAILED", "CANCELLED":
				return build.Status, fmt.Errorf("build %s: %s", buildID, build.Status)
			}
		}
	}
}

// kubectlTopPod runs "kubectl top pod" in a namespace and parses the first
// pod's memory and CPU. Returns (memoryMi, cpuMillis, podName, error).
func kubectlTopPod(ctx context.Context, kubeCtx, namespace string) (int64, int64, string, error) {
	cmd := exec.CommandContext(ctx, "kubectl", "--context="+kubeCtx, "-n", namespace, "top", "pod", "--no-headers")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, "", fmt.Errorf("kubectl top: %w: %s", err, string(out))
	}

	// Parse output format: "POD_NAME  CPU(cores)  MEMORY(bytes)"
	// Example: "api-7f8b4c9d-x2k4j  1m  23Mi"
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		return 0, 0, "", fmt.Errorf("no pods found in namespace %s", namespace)
	}

	// Use the first pod.
	fields := strings.Fields(lines[0])
	if len(fields) < 3 {
		return 0, 0, "", fmt.Errorf("unexpected kubectl top output: %s", lines[0])
	}

	podName := fields[0]
	cpuStr := fields[1]
	memStr := fields[2]

	// Parse CPU: "1m" → 1, "100m" → 100, "1" → 1000
	var cpuMillis int64
	if strings.HasSuffix(cpuStr, "m") {
		fmt.Sscanf(cpuStr, "%dm", &cpuMillis)
	} else {
		var cores int64
		fmt.Sscanf(cpuStr, "%d", &cores)
		cpuMillis = cores * 1000
	}

	// Parse memory: "23Mi" → 23, "1Gi" → 1024, "23456Ki" → 22
	var memMi int64
	if strings.HasSuffix(memStr, "Mi") {
		fmt.Sscanf(memStr, "%dMi", &memMi)
	} else if strings.HasSuffix(memStr, "Gi") {
		var gi int64
		fmt.Sscanf(memStr, "%dGi", &gi)
		memMi = gi * 1024
	} else if strings.HasSuffix(memStr, "Ki") {
		var ki int64
		fmt.Sscanf(memStr, "%dKi", &ki)
		memMi = ki / 1024
	}

	return memMi, cpuMillis, podName, nil
}
