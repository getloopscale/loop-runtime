// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package plutogate provides a hermetic check that the Pluto CI workflow
// is correctly configured: action SHA-pinned, target-versions specified,
// deprecated APIs reporting enabled.
//
// This is NOT a Pluto wrapper (would violate Anti-Pattern #8). It's a
// pure-logic Go test that verifies the workflow YAML has the right
// structure so the external Pluto Action actually runs as intended.
//
// Phase 7 T7.1 of plan docs/plans/2026-05-22-shell-script-strategic-plan.md.
package plutogate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const workflowPath = "../../../.github/workflows/pluto-deprecated-apis.yml"

// TestWorkflow_PlutoJobPresent — verifies workflow file exists and has pluto-check job.
func TestWorkflow_PlutoJobPresent(t *testing.T) {
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "pluto-check:") {
		t.Errorf("workflow should define `pluto-check:` job, got:\n%s", content)
	}
	if !strings.Contains(content, "FairwindsOps/pluto") {
		t.Errorf("workflow should use FairwindsOps/pluto action")
	}
}

// TestWorkflow_PlutoActionSHAPinned — INF-G18 mandates SHA pinning for ALL Actions
// (no floating @v1 / @main / @latest references).
func TestWorkflow_PlutoActionSHAPinned(t *testing.T) {
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	content := string(data)
	// Match `FairwindsOps/pluto/...@<ref>` and require <ref> to be 40-char SHA
	actionRe := regexp.MustCompile(`FairwindsOps/pluto/[^@\s]+@([a-zA-Z0-9._-]+)`)
	matches := actionRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		t.Fatal("FairwindsOps/pluto action ref not found")
	}
	for _, m := range matches {
		ref := m[1]
		// 40-char hex = full SHA. Reject shorter refs (vN, vN.N.N).
		shaRe := regexp.MustCompile(`^[a-f0-9]{40}$`)
		if !shaRe.MatchString(ref) {
			t.Errorf("Pluto action ref %q is not SHA-pinned (INF-G18 violation). Use Renovate to pin to <sha40>.", ref)
		}
	}
}

// TestWorkflow_TargetVersionsSet — workflow must specify K8S_TARGET_VERSION,
// otherwise Pluto scans against default (latest) and may miss deprecations
// relevant to current DOKS production version.
func TestWorkflow_TargetVersionsSet(t *testing.T) {
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "K8S_TARGET_VERSION:") {
		t.Errorf("workflow should declare K8S_TARGET_VERSION env var")
	}
	if !strings.Contains(content, "target-versions:") {
		t.Errorf("workflow should pass target-versions to pluto action")
	}
}

// TestWorkflow_OnlyShowRemovedFalse — must be false so DEPRECATED (not just REMOVED)
// APIs are also reported. Early warning > waiting for removal.
func TestWorkflow_OnlyShowRemovedFalse(t *testing.T) {
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "only-show-removed: false") {
		t.Errorf("workflow should set `only-show-removed: false` to surface DEPRECATED APIs too")
	}
}

// TestFixtures_ExistAndParseable — sanity check that test fixtures live where
// the workflow expects them.
func TestFixtures_ExistAndParseable(t *testing.T) {
	fixtures := []string{
		"../../../infra/tests/pluto/fixtures/deprecated.yaml",
		"../../../infra/tests/pluto/fixtures/current.yaml",
	}
	for _, p := range fixtures {
		abs, err := filepath.Abs(p)
		if err != nil {
			t.Fatalf("abs %s: %v", p, err)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			t.Errorf("fixture %s missing: %v", p, err)
			continue
		}
		// Basic sanity: contains apiVersion + kind tokens.
		if !strings.Contains(string(data), "apiVersion:") {
			t.Errorf("fixture %s should contain apiVersion:", p)
		}
		if !strings.Contains(string(data), "kind:") {
			t.Errorf("fixture %s should contain kind:", p)
		}
	}
}

// TestFixtures_DeprecatedUsesRemovedAPI — sanity check that the deprecated
// fixture really uses an API that Pluto will flag.
func TestFixtures_DeprecatedUsesRemovedAPI(t *testing.T) {
	data, err := os.ReadFile("../../../infra/tests/pluto/fixtures/deprecated.yaml")
	if err != nil {
		t.Fatalf("read deprecated fixture: %v", err)
	}
	content := string(data)
	// Known removed APIs (per Kubernetes deprecated-API-removal docs):
	knownRemoved := []string{
		"apiVersion: policy/v1beta1",        // PDB removed in v1.25
		"apiVersion: extensions/v1beta1",    // multiple kinds removed
		"apiVersion: autoscaling/v2beta1",   // HPA v2beta1 removed in v1.25
		"apiVersion: networking.k8s.io/v1beta1", // Ingress beta removed in v1.22
	}
	found := false
	for _, k := range knownRemoved {
		if strings.Contains(content, k) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("deprecated.yaml fixture should use a KNOWN-REMOVED API; current content does not. Update fixture to test gate sensitivity.")
	}
}

// TestFixtures_CurrentUsesValidAPI — current fixture must use stable APIs
// so it doesn't accidentally fail the gate.
func TestFixtures_CurrentUsesValidAPI(t *testing.T) {
	data, err := os.ReadFile("../../../infra/tests/pluto/fixtures/current.yaml")
	if err != nil {
		t.Fatalf("read current fixture: %v", err)
	}
	content := string(data)
	knownDeprecated := []string{
		"apiVersion: policy/v1beta1",
		"apiVersion: extensions/v1beta1",
		"apiVersion: autoscaling/v2beta1",
		"apiVersion: autoscaling/v2beta2",
	}
	for _, k := range knownDeprecated {
		if strings.Contains(content, k) {
			t.Errorf("current.yaml uses deprecated API %q — should use current stable version", k)
		}
	}
}
