// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// B-001 T3.1. Three rows over fixtures this file installs itself, run in one
// order, each refusing a different wrong implementation:
//
//	byte-identical twin      PASS  — a comparison that reports drift on a
//	                                 converged pair is worse than none
//	reformatted, same grants PASS  — a byte or text comparison wearing the name
//	                                 of a cell comparison
//	one verb more            FAIL  — an empty body, which passes the first two
//
// The second row is the one that needs a qualifier, and the qualifier is in the
// test rather than in prose: it asserts the two sides are NOT byte-identical
// before asserting the gate is silent about them. Without that, the row is
// indistinguishable from the first.

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "parity", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return raw
}

// parityTree lays out a hermetic pair from two fixture bodies and returns the
// gate's RBAC finding for it, if any.
func parityTree(t *testing.T, bootstrap, published []byte) (*Outcome, *RBACDrift) {
	t.Helper()
	return parityTreeNamed(t, "loopzero-parityprobe", bootstrap, published)
}

// parityTreeNamed is the same, for a caller that needs the pair's NAME — the
// registry is keyed by it, so a test about declarations cannot share a fixture
// name with a test about comparison.
func parityTreeNamed(t *testing.T, chart string, bootstrap, published []byte) (*Outcome, *RBACDrift) {
	t.Helper()
	return parityTreeWith(t, chart, bootstrap, published, Registry{})
}

// parityTreeWith is the same, audited against reg.
func parityTreeWith(t *testing.T, chart string, bootstrap, published []byte, reg Registry) (*Outcome, *RBACDrift) {
	t.Helper()
	root := t.TempDir()
	write := func(chartPath string, body []byte) {
		dir := filepath.Join(root, chartPath, "templates")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "rbac.yaml"), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("infra/helm/"+chart, bootstrap)
	write("charts/"+chart, published)

	declareFixturePairs(t, root)
	out, err := Check(Config{RepoRoot: root, Renderer: fixtureRenderer{}, Registry: &reg})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	for i := range out.RBACDrifts {
		if out.RBACDrifts[i].Pair == chart+":"+chart {
			return out, &out.RBACDrifts[i]
		}
	}
	return out, nil
}

func TestConvergedPairIsSilent(t *testing.T) {
	base := readFixture(t, "base-clusterrole.yaml")
	reformatted := readFixture(t, "reformatted-clusterrole.yaml")
	oneMore := readFixture(t, "one-verb-more-clusterrole.yaml")

	// The qualifier for row 2, asserted rather than claimed: the reformatted
	// side really is a different file. If these bytes ever converge, row 2 stops
	// testing anything and says so here instead of passing quietly.
	if bytes.Equal(base, reformatted) {
		t.Fatal("base and reformatted fixtures are byte-identical, so the second row " +
			"would prove nothing about comparing grants rather than bytes")
	}

	t.Run("byte-identical twin is silent", func(t *testing.T) {
		out, drift := parityTree(t, base, base)
		if drift != nil {
			t.Errorf("a converged pair produced an RBAC finding: %v", drift.ReportLines())
		}
		if !out.Pass {
			t.Errorf("a converged pair failed the gate: %+v", out.Drifts)
		}
	})

	t.Run("same grants written differently is silent", func(t *testing.T) {
		out, drift := parityTree(t, base, reformatted)
		if drift != nil {
			t.Errorf("the two sides grant identically and differ only in YAML style, and the "+
				"gate reported drift: %v — that is a byte or text comparison wearing the name "+
				"of a cell comparison", drift.ReportLines())
		}
		if !out.Pass {
			t.Errorf("reformatting one side failed the gate: %+v", out.Drifts)
		}
	})

	t.Run("one verb more is refused, and named", func(t *testing.T) {
		out, drift := parityTree(t, oneMore, base)
		if drift == nil {
			t.Fatal("the bootstrap side grants exactly one cell more and the gate stayed " +
				"silent — the first two rows pass over an empty body, this one does not")
		}
		if out.Pass {
			t.Error("an undeclared divergent cell must fail the gate")
		}
		lines := strings.Join(drift.ReportLines(), "\n")
		t.Logf("RBAC report:\n%s", lines)
		if !strings.Contains(lines, "(core)/secrets/delete") {
			t.Errorf("the finding does not name the differing cell:\n%s", lines)
		}
		if len(drift.OnlyInBootstrap) != 1 || drift.OnlyInBootstrap[0].String() != "(core)/secrets/delete" {
			t.Errorf("expected exactly one bootstrap-only cell, got %v", drift.OnlyInBootstrap)
		}
		if len(drift.OnlyInPublished) != 0 {
			t.Errorf("expected no published-only cell, got %v", drift.OnlyInPublished)
		}
	})
}

// A pair where neither side renders a ClusterRole must produce no RBAC finding
// at all — two of the three real pairs are in exactly that state, which is what
// keeps the silence rows above from being tautologies.
func TestPairWithNoClusterRoleOnEitherSideIsSilent(t *testing.T) {
	out, drift := parityTree(t,
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n"))
	if drift != nil {
		t.Errorf("a pair with no RBAC produced an RBAC finding: %v", drift.ReportLines())
	}
	if !out.Pass {
		t.Errorf("expected Pass, got %+v", out.Drifts)
	}
}
