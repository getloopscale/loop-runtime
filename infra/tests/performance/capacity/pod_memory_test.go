// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package capacity

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/infra/tests/performance/load"
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
//	LOOPZERO_API_URL=https://api.dev.getloopscale.com \
//	LOOPZERO_SERVICE_AUTH=... \
//	LOOPZERO_APP_DOMAIN=dev.getloopscale.com \
//	LOOPZERO_CELL_0_CONTEXT=do-sfo3-loopzero-infra-dev-sfo3-runtime \
//	go test ./infra/tests/performance/capacity/ -run TestCapacityPodMemoryByFramework -v -timeout 120m
func TestCapacityPodMemoryByFramework(t *testing.T) {
	SkipUnlessCapacityEnabled(t)

	apiURL := os.Getenv("LOOPZERO_API_URL")
	serviceAuth := os.Getenv("LOOPZERO_SERVICE_AUTH")
	appDomain := os.Getenv("LOOPZERO_APP_DOMAIN")
	runtimeCtx := os.Getenv("LOOPZERO_CELL_0_CONTEXT")
	if apiURL == "" || serviceAuth == "" || appDomain == "" || runtimeCtx == "" {
		t.Fatal("LOOPZERO_API_URL, LOOPZERO_SERVICE_AUTH, LOOPZERO_APP_DOMAIN, LOOPZERO_CELL_0_CONTEXT required")
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

		auth := load.ServiceAuthHeaders(serviceAuth, tenant)
		commitSHA, err := load.UploadSource(ctx, client, apiURL, auth, loopzeroYAML, bundle)
		if err != nil {
			t.Logf("  ERROR upload: %v", err)
			continue
		}

		// Submit build.
		builds := load.BuildAPI{
			APIURL:       apiURL,
			Client:       client,
			Auth:         auth,
			PollInterval: 10 * time.Second,
			RetryBackoff: load.DefaultSubmitBackoff,
		}
		buildID, err := builds.SubmitBuild(ctx, map[string]any{
			"project_id":  tenant.ProjectName,
			"environment": "production",
			"commit_sha":  commitSHA,
			"apps": []map[string]any{{
				"name": "api", "build": "dockerfile", "framework": fw.Framework,
				"render": "server", "port": fw.Port,
			}},
		}, fmt.Sprintf("pm-%s-%d", tenant.TenantID, time.Now().UnixNano()))
		if err != nil {
			t.Logf("  ERROR submit: %v", err)
			continue
		}
		t.Logf("  build: %s", buildID)

		// Wait for build to complete.
		buildCtx, cancelBuild := context.WithTimeout(ctx, 30*time.Minute)
		status, _, err := builds.WaitForBuild(buildCtx, buildID)
		cancelBuild()
		if err != nil || status != "SUCCEEDED" {
			t.Logf("  ERROR build %s: %s (%v)", buildID, status, err)
			continue
		}
		t.Logf("  build SUCCEEDED")

		// Wait for pod to stabilize.
		t.Logf("  waiting %v for pod memory to stabilize...", stabilizeWait)
		// intentional-sleep: benchmark warmup / measurement window
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

// kubectlTopPod reads the first pod of a namespace from the metrics-server
// through the package's one `kubectl top` parser. Returns (memoryMi,
// cpuMillis, podName, error).
func kubectlTopPod(ctx context.Context, kubeCtx, namespace string) (int64, int64, string, error) {
	pods, err := KubectlTopPods(ctx, kubeCtx, namespace, "")
	if err != nil {
		return 0, 0, "", err
	}
	if len(pods) == 0 {
		return 0, 0, "", fmt.Errorf("no pods found in namespace %s", namespace)
	}
	return pods[0].MemoryMi, pods[0].CPUMillis, pods[0].Name, nil
}
