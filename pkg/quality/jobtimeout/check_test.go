// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package jobtimeout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeWorkflows materializes name→content under a temp .github/workflows dir
// and returns that dir.
func writeWorkflows(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".github", "workflows")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestCheck_JobWithTimeout_Pass(t *testing.T) {
	dir := writeWorkflows(t, map[string]string{
		"ci.yml": "name: ci\njobs:\n  build:\n    runs-on: ubuntu-latest\n    timeout-minutes: 30\n    steps:\n      - run: echo hi\n",
	})
	out, err := Check(Config{WorkflowsDir: dir})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass || out.Total != 1 || out.Covered != 1 {
		t.Fatalf("job with timeout must PASS (total=1 covered=1): %+v", out)
	}
}

func TestCheck_JobWithoutTimeout_Fails(t *testing.T) {
	dir := writeWorkflows(t, map[string]string{
		"ci.yml": "name: ci\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n",
	})
	out, _ := Check(Config{WorkflowsDir: dir})
	if out.Pass {
		t.Fatalf("job without timeout must FAIL: %+v", out)
	}
	if len(out.Violations) != 1 || !strings.Contains(out.Violations[0], "build") {
		t.Fatalf("violation must name job 'build': %v", out.Violations)
	}
}

func TestCheck_ReusableCaller_Exempt(t *testing.T) {
	dir := writeWorkflows(t, map[string]string{
		"infra-deploy.yml": "name: deploy\njobs:\n  bootstrap:\n    uses: ./.github/workflows/infra-bootstrap.yml\n    secrets: inherit\n",
	})
	out, _ := Check(Config{WorkflowsDir: dir})
	if !out.Pass {
		t.Fatalf("reusable-workflow caller is exempt (timeout comes from called wf): %+v", out)
	}
	if out.ReusableCallers != 1 || out.Total != 1 {
		t.Fatalf("reusable caller must be counted+exempt: %+v", out)
	}
}

func TestCheck_MixedJobs_OneViolation(t *testing.T) {
	dir := writeWorkflows(t, map[string]string{
		"ci.yml": "name: ci\njobs:\n" +
			"  build:\n    runs-on: ubuntu-latest\n    timeout-minutes: 30\n    steps:\n      - run: echo a\n" +
			"  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo b\n",
	})
	out, _ := Check(Config{WorkflowsDir: dir})
	if out.Pass || out.Total != 2 || out.Covered != 1 || len(out.Violations) != 1 {
		t.Fatalf("mixed: 2 jobs, 1 covered, 1 violation: %+v", out)
	}
	if !strings.Contains(out.Violations[0], "test") {
		t.Fatalf("violation must be job 'test': %v", out.Violations)
	}
}

// TestCheck_StepLevelTimeoutDoesNotCount — a deeper-indented (step-level)
// timeout-minutes must NOT satisfy the job-level requirement (faithful to the
// bash gate which matches exactly 4-space indent).
func TestCheck_StepLevelTimeoutDoesNotCount(t *testing.T) {
	dir := writeWorkflows(t, map[string]string{
		"ci.yml": "name: ci\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo a\n        timeout-minutes: 5\n",
	})
	out, _ := Check(Config{WorkflowsDir: dir})
	if out.Pass {
		t.Fatalf("step-level timeout must NOT count as job-level: %+v", out)
	}
}

// TestCheck_TopLevelKeysIgnored — `on:` / `name:` / `permissions:` at column 0
// end the jobs section and are never treated as jobs.
func TestCheck_TopLevelKeysIgnored(t *testing.T) {
	dir := writeWorkflows(t, map[string]string{
		"ci.yml": "name: ci\non:\n  push:\njobs:\n  build:\n    runs-on: ubuntu-latest\n    timeout-minutes: 30\n    steps:\n      - run: echo a\npermissions:\n  contents: read\n",
	})
	out, _ := Check(Config{WorkflowsDir: dir})
	if !out.Pass || out.Total != 1 {
		t.Fatalf("only 'build' is a job; on:/permissions: are not jobs: %+v", out)
	}
}

func TestCheck_EmptyDir_Pass(t *testing.T) {
	dir := writeWorkflows(t, map[string]string{})
	out, err := Check(Config{WorkflowsDir: dir})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass || out.Total != 0 {
		t.Fatalf("empty workflows dir must PASS with total=0: %+v", out)
	}
}

func TestCheck_MissingDir_Errors(t *testing.T) {
	_, err := Check(Config{WorkflowsDir: filepath.Join(t.TempDir(), "nonexistent")})
	if err == nil {
		t.Fatal("missing workflows dir must error")
	}
}
