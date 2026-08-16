// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
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
//
// Updated 2026-08-12 (slice-3 T1.4, ADR D2): the approval is now keyed by FILE,
// not by pair, so this test names files the registry actually approves. The old
// fixture used `rbac.yaml` / `monitoring.yaml` — names that never existed in
// either chart — and passed because the PAIR was blessed. That is the exact
// defect the change removes, and a fixture asserting it had to change with it.
func TestCheck_ApprovedDrift_Pass(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/loopzero-api", "a.yaml", "clusterrole.yaml", "service.yaml")
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

// T1.4 / B-046 RED: an ADR approving a PAIR approves every file that pair will
// ever drift on — including files nobody has seen. That is how two ExternalSecret
// templates and a ValidatingAdmissionPolicy left the published chart without
// anyone noticing: the pair was already blessed, so the gate stopped looking.
//
// The approval must name the FILE.
func TestApprovedPair_DoesNotBlessAnUnseenFile(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	touch := func(rel string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte("kind: X\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("charts/loopzero-api/templates")
	mk("infra/helm/loopzero-api/templates")
	touch("charts/loopzero-api/templates/service.yaml")
	// A file that drifts and that NO per-file approval covers.
	touch("infra/helm/loopzero-api/templates/brand-new-and-unreviewed.yaml")

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if out.Pass {
		t.Errorf("a drifted file with no per-file approval must FAIL the gate; blessing it because the PAIR was blessed is how the VAP went missing (drifts=%+v)", out.Drifts)
	}
}

// TestNoApprovalCarriesAnInheritedReason is the B-076 close.
//
// The allowlist was re-keyed from PAIR to PAIR:FILE by slice-3, but the 21
// entries that already existed were carried over with one generic sentence and
// `Inherited: true` — deliberately, so the debt would be countable instead of
// invisible. The field exists to be driven to zero.
//
// A reason is "measured" here in a specific sense: it says what was OBSERVED
// about the two charts, not what someone believes about them. Twenty-one
// plausible rationales would have closed the item and would have hidden the
// finding that produced this test — that four of the twenty-one are not resource
// drift at all, because both charts render the resource and only the filename
// differs.
func TestNoApprovalCarriesAnInheritedReason(t *testing.T) {
	var inherited []string
	for key, a := range approvedDriftFiles {
		if a.Inherited {
			inherited = append(inherited, key)
		}
	}
	if len(inherited) != 0 {
		sort.Strings(inherited)
		t.Errorf("%d approval(s) still carry an inherited reason, so the gate approves a divergence "+
			"nobody measured:\n  %s\nEach needs a reason stating what was observed about the two "+
			"charts, or removal because the divergence should not exist (B-076).",
			len(inherited), strings.Join(inherited, "\n  "))
	}
}

// TestEveryApprovalNamesItsMeasurement keeps the reasons from decaying back into
// assertions once the inherited flag is gone.
//
// The three classes came from rendering both charts of each pair and comparing
// the kinds actually produced — the same method B-077 established when it proved
// its fence renders rather than checking for a file. A reason that does not name
// its class is a reason nobody can re-derive.
func TestEveryApprovalNamesItsMeasurement(t *testing.T) {
	if len(approvedDriftFiles) == 0 {
		t.Fatal("no approvals to check — the test would pass vacuously")
	}
	for key, a := range approvedDriftFiles {
		if a.ADR == "" {
			t.Errorf("%s carries no ADR — an approval nobody signed", key)
		}
		switch {
		case strings.Contains(a.Reason, "ambos renderizam"),
			strings.Contains(a.Reason, "não renderiza"),
			strings.Contains(a.Reason, "só o publicado renderiza"),
			strings.Contains(a.Reason, "só o bootstrap renderiza"):
			// One of the three measured classes.
		default:
			t.Errorf("%s: reason %q does not name what was measured about the render. The classes are: "+
				"both render it (filename-only drift), neither renders it under default values "+
				"(conditional), or exactly one renders it (real asymmetry).", key, a.Reason)
		}
	}
}
