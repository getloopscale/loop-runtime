// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// The production edge Worker routes a dynamic app to its cell by lookup (#448). Its
// behaviour tests are JavaScript, next to it; this runs them with the rest of the suite
// so a Worker change cannot land without them. Node is a declared prerequisite, so its
// absence fails rather than skips — a skipped edge test reads exactly like a passing one.
func TestCloudflareWorkerBehaviour(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("node is not on PATH; it is a prerequisite for the edge Worker tests")
	}
	cmd := exec.Command(node, "--test", filepath.Join(repoRoot(), "infra", "cloudflare", "worker.test.mjs"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("worker tests failed: %v\n%s", err, out)
	}
}
