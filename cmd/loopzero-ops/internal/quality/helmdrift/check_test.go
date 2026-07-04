// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"os"
	"path/filepath"
	"testing"
)

func mkTemplates(t *testing.T, root, chartPath string, files ...string) {
	t.Helper()
	dir := filepath.Join(root, chartPath, "templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("kind: X\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

// TestCheck_NoDrift_Pass — identical templates → no drift.
func TestCheck_NoDrift_Pass(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/loopzero-foo", "a.yaml", "b.yaml")
	mkTemplates(t, root, "infra/helm/loopzero-foo", "a.yaml", "b.yaml")
	o, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (no drift), got %v", o.Drifts)
	}
}

// TestCheck_UnapprovedDrift_Fail — differing templates without ADR → FAIL.
func TestCheck_UnapprovedDrift_Fail(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/loopzero-foo", "a.yaml", "b.yaml", "extra.yaml")
	mkTemplates(t, root, "infra/helm/loopzero-foo", "a.yaml")
	o, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail (unapproved drift)")
	}
	if len(o.Drifts) != 1 || o.Drifts[0].Intentional {
		t.Errorf("expected 1 unintentional drift, got %v", o.Drifts)
	}
}

// TestCheck_ApprovedDrift_Pass — drift with approving ADR → PASS (intentional).
func TestCheck_ApprovedDrift_Pass(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/loopzero-api", "a.yaml", "rbac.yaml", "monitoring.yaml")
	mkTemplates(t, root, "infra/helm/loopzero-api", "a.yaml")
	o, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (loopzero-api drift approved by ADR-0043), got drifts %v", o.Drifts)
	}
	if len(o.Drifts) != 1 || !o.Drifts[0].Intentional || o.Drifts[0].ADR != "ADR-0043" {
		t.Errorf("expected intentional drift w/ ADR-0043, got %v", o.Drifts)
	}
}

// TestCheck_ConceptualPair — loopzero-admission-policies ↔ admission-policies.
func TestCheck_ConceptualPair(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/loopzero-admission-policies", "p.yaml", "extra.yaml")
	mkTemplates(t, root, "infra/helm/admission-policies", "p.yaml")
	o, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	// drift exists, no ADR → fail, but the pair must be DETECTED (conceptual mapping)
	found := false
	for _, d := range o.Drifts {
		if d.Pair == "loopzero-admission-policies:admission-policies" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected conceptual pair detected, got %v", o.Drifts)
	}
}

// TestCheck_OnlyPublishedExists_NoCrash — bootstrap dir absent → no pair, no drift.
func TestCheck_OnlyPublishedExists_NoCrash(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/loopzero-orphan", "a.yaml")
	// no infra/helm/loopzero-orphan
	o, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (no matching bootstrap → not a pair), got %v", o.Drifts)
	}
}

// TestListTemplates — lists yaml/yml filenames sorted.
func TestListTemplates(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/x", "b.yaml", "a.yml", "c.yaml")
	got := listTemplates(filepath.Join(root, "charts/x/templates"))
	if len(got) != 3 || got[0] != "a.yml" || got[1] != "b.yaml" || got[2] != "c.yaml" {
		t.Errorf("expected sorted [a.yml b.yaml c.yaml], got %v", got)
	}
}

// TestSetDiff — onlyA / onlyB computed correctly.
func TestSetDiff(t *testing.T) {
	onlyA, onlyB := setDiff([]string{"a", "b", "c"}, []string{"b", "c", "d"})
	if len(onlyA) != 1 || onlyA[0] != "a" {
		t.Errorf("onlyA: expected [a], got %v", onlyA)
	}
	if len(onlyB) != 1 || onlyB[0] != "d" {
		t.Errorf("onlyB: expected [d], got %v", onlyB)
	}
}
