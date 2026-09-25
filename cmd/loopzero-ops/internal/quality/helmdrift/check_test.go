// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"os"
	"path/filepath"
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
		// The file name IS the kind in the fixture: `ConfigMap.yaml` -> `kind: ConfigMap`.
		//
		// The fixtures used to write `kind: X` in EVERY file, which under the
		// per-resource comparison is no drift at all — they differed by NAME, which
		// is exactly what the gate stopped measuring. And deriving the kind by
		// capitalising the first letter gave `Configmap` against the registry's
		// `ConfigMap`: the same spelling difference this comparison exists to stop
		// treating as a fact.
		kind := strings.TrimSuffix(f, ".yaml")
		if err := os.WriteFile(filepath.Join(dir, f), []byte("kind: "+kind+"\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

// TestCheck_NoDrift_Pass — identical templates → no drift.
func TestCheck_NoDrift_Pass(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/loopzero-foo", "Deployment.yaml", "Service.yaml")
	mkTemplates(t, root, "infra/helm/loopzero-foo", "Deployment.yaml", "Service.yaml")
	declareFixturePairs(t, root)
	o, err := Check(Config{RepoRoot: root, Renderer: fixtureRenderer{}, Registry: realRegistry(t)})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (no drift), got %v", o.Drifts)
	}
}

// TestCheck_UnapprovedDrift_Fail — differing kinds with no registry entry → FAIL.
func TestCheck_UnapprovedDrift_Fail(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/loopzero-foo", "Deployment.yaml", "Service.yaml", "Secret.yaml")
	mkTemplates(t, root, "infra/helm/loopzero-foo", "Deployment.yaml")
	declareFixturePairs(t, root)
	o, err := Check(Config{RepoRoot: root, Renderer: fixtureRenderer{}, Registry: realRegistry(t)})
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

// TestCheck_ApprovedDrift_Pass — drift recorded in the registry → PASS (intentional).
//
// Updated 2026-08-12 (slice-3 T1.4, ADR D2): the approval is now keyed by FILE,
// not by pair, so this test names files the registry actually approves. The old
// fixture used `rbac.yaml` / `monitoring.yaml` — names that never existed in
// either chart — and passed because the PAIR was blessed. That is the exact
// defect the change removes, and a fixture asserting it had to change with it.
func TestCheck_ApprovedDrift_Pass(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/loopzero-api", "Deployment.yaml", "ConfigMap.yaml", "Secret.yaml")
	mkTemplates(t, root, "infra/helm/loopzero-api", "Deployment.yaml")
	declareFixturePairs(t, root)
	o, err := Check(Config{RepoRoot: root, Renderer: fixtureRenderer{}, Registry: realRegistry(t)})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("want Pass (the loopzero-api drift is recorded per kind), got %v", o.Drifts)
	}
	if len(o.Drifts) != 1 || !o.Drifts[0].Intentional || o.Drifts[0].Decision != recordedDriftDecision {
		t.Errorf("want an intentional drift citing %q, got %v", recordedDriftDecision, o.Drifts)
	}
}

// TestCheck_ConceptualPair — loopzero-admission-policies ↔ admission-policies.
func TestCheck_ConceptualPair(t *testing.T) {
	root := t.TempDir()
	mkTemplates(t, root, "charts/loopzero-admission-policies", "ConfigMap.yaml", "Secret.yaml")
	mkTemplates(t, root, "infra/helm/admission-policies", "ConfigMap.yaml")
	declareFixturePairs(t, root)
	o, err := Check(Config{RepoRoot: root, Renderer: fixtureRenderer{}, Registry: realRegistry(t)})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	// drift exists and is not recorded → fail, but the pair must be DETECTED (conceptual mapping)
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
	declareFixturePairs(t, root)
	o, err := Check(Config{RepoRoot: root, Renderer: fixtureRenderer{}, Registry: realRegistry(t)})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (no matching bootstrap → not a pair), got %v", o.Drifts)
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
	touch := func(rel, kind string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte("kind: "+kind+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("charts/loopzero-api/templates")
	mk("infra/helm/loopzero-api/templates")
	// A RECORDED resource beside one that NO entry covers. The loopzero-api pair has
	// entries in the registry, and the question is whether having entries absolves
	// it of the next resource — that is how a ValidatingAdmissionPolicy vanished
	// from the published chart without anyone noticing.
	touch("charts/loopzero-api/templates/a.yaml", "ConfigMap")
	touch("charts/loopzero-api/templates/b.yaml", "CronJob")

	declareFixturePairs(t, root)
	out, err := Check(Config{RepoRoot: root, Renderer: fixtureRenderer{}, Registry: realRegistry(t)})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if out.Pass {
		t.Errorf("a drifted file with no per-file approval must FAIL the gate; blessing it because the PAIR was blessed is how the VAP went missing (drifts=%+v)", out.Drifts)
	}
}

// fixtureRenderer returns the content of the `templates/` files as if it were the
// rendered manifest.
//
// The Check tests lay out fixtures on disk that are NOT valid charts — they have
// no Chart.yaml and no values. The real helm renderer would fail on all of them,
// and the failure would be the fixture's, not the contract's under test. Injecting
// the double is what lets these tests keep asserting what they always asserted
// (recorded drift, unrecorded drift, conceptual pair) over a new comparison basis.
type fixtureRenderer struct{}

// The `sets` are ignored: the fixture files on disk ARE the rendered output, so
// there is no template for a value to reach. Ignoring them in plain sight beats a
// second interface the fixture would not implement — a hermetic test of the
// COMPARISON has no values to vary, and the matrix is exercised by the real-tree
// test that renders real charts.
func (fixtureRenderer) Render(chartDir string, _ []string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(chartDir, "templates"))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range entries {
		raw, rerr := os.ReadFile(filepath.Join(chartDir, "templates", e.Name()))
		if rerr != nil {
			return "", rerr
		}
		b.WriteString("---\n")
		b.Write(raw)
	}
	return b.String(), nil
}
