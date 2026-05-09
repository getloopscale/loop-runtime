// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T4.2 of deep-review-remediation-plan + ADR-0049.
//
// Regression guard for the infra-restart.yml refactor. The workflow used
// to rely on `kubectl rollout restart` against `:dev`/`:prod` MUTABLE
// tags. Now it uses `kubectl set image` against the IMMUTABLE :sha tag
// derived from github.event.workflow_run.head_sha.
//
// This test asserts the structural invariants that prevent regression:
//   - kubectl set image is the only image-promotion verb in the file.
//   - The :sha derivation pattern (${SHA_FULL:0:7}) is present.
//   - kubectl rollout restart against mutable tags is GONE.
//
// We test against the file content (not LIVE GH Actions API) so the
// guard runs in any unit-test pipeline.

package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readInfraRestartWorkflow(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	wfPath := filepath.Join(
		filepath.Dir(thisFile),
		"..", "..", "..",
		".github", "workflows", "infra-restart.yml",
	)
	data, err := os.ReadFile(wfPath)
	if err != nil {
		t.Fatalf("workflow file not found at %s: %v", wfPath, err)
	}
	return string(data)
}

func TestInfraRestart_UsesKubectlSetImageNotRolloutRestart(t *testing.T) {
	wf := readInfraRestartWorkflow(t)

	// kubectl set image MUST be the verb used to promote.
	if !strings.Contains(wf, "kubectl set image deploy") {
		t.Error("workflow must use `kubectl set image deploy` for promotion (T4.2 / ADR-0049)")
	}

	// kubectl rollout RESTART is the legacy mutable-tag pattern; it MUST NOT
	// reappear inside the deploy-promotion loop. We allow the string only
	// in comments referring to legacy/emergency rollback.
	for _, line := range strings.Split(wf, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			continue // comment lines may discuss the legacy pattern
		}
		if strings.Contains(trimmed, "kubectl rollout restart") {
			t.Errorf("non-comment line uses `kubectl rollout restart` — must use `kubectl set image` instead\n  line: %s", line)
		}
	}
}

func TestInfraRestart_ResolvesShortSHAFromWorkflowRun(t *testing.T) {
	wf := readInfraRestartWorkflow(t)

	// The workflow MUST derive the short SHA from workflow_run head_sha
	// (40-char) using bash slice ${SHA_FULL:0:7}. Without this, the env tag
	// promotion regression returns silently.
	if !strings.Contains(wf, "head_sha") {
		t.Error("workflow must read github.event.workflow_run.head_sha")
	}
	if !strings.Contains(wf, "${SHA_FULL:0:7}") {
		t.Error("workflow must slice head_sha to 7-char short SHA matching build-images.yml convention")
	}
}

func TestInfraRestart_DocumentsT42AndAdr0049(t *testing.T) {
	wf := readInfraRestartWorkflow(t)
	// Per Princípio 6 + ADR-0022, the workflow must reference T4.2 + ADR-0049
	// in its header so future readers understand why kubectl set image is
	// the canonical path.
	if !strings.Contains(wf, "T4.2") {
		t.Error("workflow header must reference T4.2 of deep-review-remediation-plan")
	}
	if !strings.Contains(wf, "ADR-0049") {
		t.Error("workflow header must reference ADR-0049 (immutable artifact promotion)")
	}
}
