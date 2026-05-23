// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package advisorymode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeWorkflows(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".github", "workflows")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return dir
}

func TestCheck_WithRationale_Pass(t *testing.T) {
	dir := writeWorkflows(t, map[string]string{
		"ci.yml": "jobs:\n  b:\n    steps:\n      # rationale: post-task continues even if main job failed\n      - run: echo a\n        continue-on-error: true\n",
	})
	out, err := Check(Config{WorkflowsDir: dir})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass || out.Annotated != 1 {
		t.Fatalf("continue-on-error with rationale must PASS (annotated=1): %+v", out)
	}
}

func TestCheck_WithGraduation_Pass(t *testing.T) {
	dir := writeWorkflows(t, map[string]string{
		"ci.yml": "jobs:\n  b:\n    steps:\n      # graduation: 2026-09-01\n      # issue: #123\n      - run: echo a\n        continue-on-error: true\n",
	})
	out, _ := Check(Config{WorkflowsDir: dir})
	if !out.Pass {
		t.Fatalf("graduation marker must PASS: %+v", out)
	}
}

func TestCheck_Unannotated_Fails(t *testing.T) {
	dir := writeWorkflows(t, map[string]string{
		"ci.yml": "jobs:\n  b:\n    steps:\n      - run: echo a\n        continue-on-error: true\n",
	})
	out, _ := Check(Config{WorkflowsDir: dir})
	if out.Pass || len(out.Violations) != 1 {
		t.Fatalf("un-annotated continue-on-error must FAIL: %+v", out)
	}
	if !strings.Contains(out.Violations[0], "ci.yml") {
		t.Fatalf("violation must name the file: %v", out.Violations)
	}
}

func TestCheck_CaseInsensitiveRationale_Pass(t *testing.T) {
	dir := writeWorkflows(t, map[string]string{
		"ci.yml": "jobs:\n  b:\n    steps:\n      # Rationale: auto-issue creation requires this\n      - run: echo a\n        continue-on-error: true\n",
	})
	out, _ := Check(Config{WorkflowsDir: dir})
	if !out.Pass {
		t.Fatalf("case-insensitive 'Rationale:' must PASS: %+v", out)
	}
}

func TestCheck_AnnotationTooFarAbove_Fails(t *testing.T) {
	// rationale at line 1; continue-on-error at line 8 → 7 lines above, outside
	// the 5-line lookback window (start = 8-5 = line 3). Must FAIL.
	dir := writeWorkflows(t, map[string]string{
		"ci.yml": "# rationale: x\njobs:\n  b:\n    steps:\n      - run: echo 1\n      - run: echo 2\n      - run: echo 3\n        continue-on-error: true\n",
	})
	out, _ := Check(Config{WorkflowsDir: dir})
	if out.Pass {
		t.Fatalf("annotation >5 lines above must NOT annotate → FAIL: %+v", out)
	}
}

func TestCheck_NoContinueOnError_Pass(t *testing.T) {
	dir := writeWorkflows(t, map[string]string{
		"ci.yml": "jobs:\n  b:\n    steps:\n      - run: echo a\n",
	})
	out, _ := Check(Config{WorkflowsDir: dir})
	if !out.Pass || out.Total != 0 {
		t.Fatalf("no continue-on-error → PASS total=0: %+v", out)
	}
}
