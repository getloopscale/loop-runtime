// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package opencost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenCostChart_StructureExists — wrapper chart files present
func TestOpenCostChart_StructureExists(t *testing.T) {
	required := []string{
		filepath.Join(repoRoot, "infra/helm/opencost/Chart.yaml"),
		filepath.Join(repoRoot, "infra/helm/opencost/values.yaml"),
		filepath.Join(repoRoot, "infra/helm/opencost/values.schema.json"),
	}
	for _, p := range required {
		abs, _ := filepath.Abs(p)
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			t.Errorf("required file missing: %s", p)
		}
	}
}

// TestOpenCostChart_DeclaresUpstreamDependency — Chart.yaml uses upstream chart
func TestOpenCostChart_DeclaresUpstreamDependency(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "infra/helm/opencost/Chart.yaml"))
	if err != nil {
		t.Fatalf("read Chart.yaml: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "opencost-helm-chart") {
		t.Errorf("Chart.yaml should declare upstream opencost-helm-chart dependency")
	}
	if !strings.Contains(content, "https://opencost.github.io") {
		t.Errorf("Chart.yaml should reference opencost.github.io repo")
	}
}

// TestOpenCostValues_NonRoot — security context per LoopRuntime platform standard
func TestOpenCostValues_NonRoot(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "infra/helm/opencost/values.yaml"))
	if err != nil {
		t.Fatalf("read values.yaml: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "runAsNonRoot: true") {
		t.Errorf("values.yaml should enforce runAsNonRoot: true")
	}
	if !strings.Contains(content, "readOnlyRootFilesystem: true") {
		t.Errorf("values.yaml should enforce readOnlyRootFilesystem: true")
	}
}

// TestOpenCostValues_ExternalPrometheus — uses LoopRuntime's existing Prometheus
// (NOT internal — that would duplicate scrape + waste resources)
func TestOpenCostValues_ExternalPrometheus(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "infra/helm/opencost/values.yaml"))
	if err != nil {
		t.Fatalf("read values.yaml: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "external:") {
		t.Errorf("values.yaml should configure external Prometheus")
	}
	if !strings.Contains(content, "prometheus-server.monitoring.svc.cluster.local") {
		t.Errorf("values.yaml should point external Prometheus to monitoring/prometheus")
	}
}

// TestHelmfile_OpenCostReleasePresent — mgmt helmfile declares opencost release
func TestHelmfile_OpenCostReleasePresent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "infra/helmfile/core.helmfile.yaml.gotmpl"))
	if err != nil {
		t.Fatalf("read mgmt helmfile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "- name: opencost") {
		t.Errorf("mgmt helmfile should declare opencost release")
	}
	if !strings.Contains(content, "../helm/opencost") {
		t.Errorf("mgmt helmfile should reference ../helm/opencost wrapper chart")
	}
}

// TestHelmfile_OpenCostNeedsPrometheus — explicit needs: declaration
// (EC-01 of plan: race conditions in cleanroom resolved via needs:)
func TestHelmfile_OpenCostNeedsPrometheus(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "infra/helmfile/core.helmfile.yaml.gotmpl"))
	if err != nil {
		t.Fatalf("read mgmt helmfile: %v", err)
	}
	content := string(data)
	// Find opencost RELEASE block (LastIndex skips the repository declaration
	// at top of file which doesn't have a needs: directive).
	idx := strings.LastIndex(content, "- name: opencost")
	if idx == -1 {
		t.Fatal("opencost release not found")
	}
	// Look forward for `needs:` within reasonable distance
	block := content[idx:]
	if len(block) > 1500 {
		block = block[:1500]
	}
	if !strings.Contains(block, "needs:") {
		t.Errorf("opencost release should declare needs: explicitly")
	}
	if !strings.Contains(block, "monitoring/prometheus") {
		t.Errorf("opencost should need monitoring/prometheus (so Prometheus is up first)")
	}
}

// TestServiceMonitor_OpenCostExists — Prometheus scrape config
func TestServiceMonitor_OpenCostExists(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "infra/helm/monitoring/manifests/servicemonitor-opencost.yaml"))
	if err != nil {
		t.Fatalf("ServiceMonitor missing: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "kind: ServiceMonitor") {
		t.Errorf("file should be a ServiceMonitor CR")
	}
	if !strings.Contains(content, "app.kubernetes.io/name: opencost") {
		t.Errorf("ServiceMonitor should target opencost workload via app.kubernetes.io/name label")
	}
	if !strings.Contains(content, "path: /metrics") {
		t.Errorf("ServiceMonitor should scrape /metrics endpoint")
	}
}

// TestValuesSchema_Valid — values.schema.json is valid JSON
func TestValuesSchema_Valid(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "infra/helm/opencost/values.schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	content := string(data)
	// Basic sanity — has $schema declaration + type: object
	if !strings.Contains(content, "$schema") {
		t.Errorf("schema should declare $schema")
	}
	if !strings.Contains(content, `"type": "object"`) {
		t.Errorf("schema should declare type: object")
	}
	if !strings.Contains(content, "customPricing") {
		t.Errorf("schema should validate customPricing block (DO pricing override path)")
	}
}

// TestEnvStubs_AllEnvsHaveOpenCostFile — per-env override stubs exist
// for all 5 environments (consistent with prometheus/grafana pattern).
func TestEnvStubs_AllEnvsHaveOpenCostFile(t *testing.T) {
	envs := []string{"dev", "drill", "prod", "standalone", "standalone-multi"}
	for _, env := range envs {
		p := filepath.Join(repoRoot, "infra/helmfile/values", env, "opencost.yaml.gotmpl")
		abs, _ := filepath.Abs(p)
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			t.Errorf("env stub missing for %s: %s", env, p)
		}
	}
}
