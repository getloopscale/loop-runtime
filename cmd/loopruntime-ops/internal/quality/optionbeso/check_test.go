// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package optionbeso

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

func writeFixture(t *testing.T, doc, runtime string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "operations"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "infra", "helmfile"), 0o755); err != nil {
		t.Fatalf("mkdir infra: %v", err)
	}
	if doc != "" {
		if err := os.WriteFile(filepath.Join(root, "docs", "operations", "option-b-mgmt-helmfile-eso-deploy.md"), []byte(doc), 0o644); err != nil {
			t.Fatalf("write doc: %v", err)
		}
	}
	if runtime != "" {
		if err := os.WriteFile(filepath.Join(root, "infra", "helmfile", "cell.helmfile.yaml.gotmpl"), []byte(runtime), 0o644); err != nil {
			t.Fatalf("write runtime: %v", err)
		}
	}
	return root
}

const canonicalDoc = "# Option B — mgmt helmfile ESO deploy\n\n" +
	"Plan slug: 2026-06-10-j22-openbao-transit-eso-sync\n" +
	"Origin: iter 31 architectural finding.\n" +
	"Cross-link: openbao-transit-cutover-runbook.md § Open Question.\n" +
	"Governance: Rule 4 — sponsor verbatim authorization required for destructive.\n" +
	"Memory: feedback_productive_idle_runbook_preauthor_2026_06_10.\n\n" +
	"## Sub-paths\n\n" +
	"### B1 upstream RECOMMENDED\n" +
	"```\n" +
	"helmfile -e mgmt --selector name=external-secrets sync\n" +
	"kubectl wait deployment/external-secrets --for=condition=Available --timeout=120s\n" +
	"kubectl get crd externalsecrets.external-secrets.io\n" +
	"```\n" +
	"version: 0.14.3\n\n" +
	"### B2 cross-cluster fork\n" +
	"### B3 NOT recommended for iter 1\n" +
	"### B4 REJECTED — mandate violation (defeats DIP)\n\n" +
	"## Rollback\n\n" +
	"helmfile -e mgmt --selector name=external-secrets destroy\n" +
	"git restore infra/helmfile/values/\n\n" +
	"## Decision matrix\n\n" +
	"| Option | Time | Risk | Notes |\n" +
	"|---|---|---|---|\n" +
	"| B1 | low | low | recommended |\n\n" +
	"## Cross-references\n\n" +
	"See cell.helmfile.yaml.gotmpl for canonical pattern.\n"

const canonicalRuntime = `releases:
  - name: external-secrets
    namespace: external-secrets
    chart: oci://ghcr.io/external-secrets/charts/external-secrets
    version: 0.14.3
    values:
      - installCRDs: true
`

func TestCheck_CanonicalPasses(t *testing.T) {
	root := writeFixture(t, canonicalDoc, canonicalRuntime)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected PASS, got err=%v failures=%v", err, outcome.Failures)
	}
}

func TestCheck_DocMissingFails(t *testing.T) {
	root := t.TempDir()
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for missing doc, got %v", err)
	}
}

func TestCheck_MissingSubPathFails(t *testing.T) {
	body := canonicalDoc
	// Strip the B3 sub-path header.
	body = removeLine(body, "### B3 NOT recommended for iter 1")
	root := writeFixture(t, body, canonicalRuntime)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when B3 sub-path missing, got %v", err)
	}
}

func TestCheck_ESOVersionDriftFails(t *testing.T) {
	// Doc pins 0.14.3 but runtime bumped to 0.15.0.
	drifted := "releases:\n  - name: external-secrets\n    version: 0.15.0\n"
	root := writeFixture(t, canonicalDoc, drifted)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for ESO version drift, got %v", err)
	}
}

func TestCheck_CoAuthoredByForbidden(t *testing.T) {
	polluted := canonicalDoc + "\n\nCo-Authored-By: Someone <someone@example.com>\n"
	root := writeFixture(t, polluted, canonicalRuntime)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for Co-Authored-By trailer, got %v", err)
	}
}

func TestCheck_MissingRollbackFails(t *testing.T) {
	// Strip the whole Rollback section.
	body := removeSection(canonicalDoc, "## Rollback")
	root := writeFixture(t, body, canonicalRuntime)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when Rollback section missing, got %v", err)
	}
}

func TestCheck_MissingCrossRefsCanonicalFails(t *testing.T) {
	// Cross-references present but does NOT cite runtime helmfile.
	body := canonicalDoc
	body = replaceLine(body, "See cell.helmfile.yaml.gotmpl for canonical pattern.", "See somewhere else.")
	root := writeFixture(t, body, canonicalRuntime)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when Cross-references does not cite runtime helmfile, got %v", err)
	}
}

func TestCheck_RealRepoDocPasses(t *testing.T) {
	abs := qtestutil.RootFile(t, docPath)
	outcome, checkErr := violationOf(Check(Config{RepoRoot: abs}))
	if checkErr != nil {
		t.Fatalf("real doc must satisfy invariant. err=%v failures=%v", checkErr, outcome.Failures)
	}
}

// tiny helpers
func removeLine(s, needle string) string {
	var out []string
	for _, l := range splitLines(s) {
		if l == needle {
			continue
		}
		out = append(out, l)
	}
	return joinLines(out)
}
func replaceLine(s, from, to string) string {
	var out []string
	for _, l := range splitLines(s) {
		if l == from {
			out = append(out, to)
			continue
		}
		out = append(out, l)
	}
	return joinLines(out)
}
func removeSection(s, header string) string {
	var out []string
	skip := false
	for _, l := range splitLines(s) {
		if !skip {
			if len(l) >= len(header) && l[:len(header)] == header {
				skip = true
				continue
			}
			out = append(out, l)
			continue
		}
		if len(l) >= 3 && l[:3] == "## " {
			skip = false
			out = append(out, l)
		}
	}
	return joinLines(out)
}
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
func joinLines(ls []string) string {
	if len(ls) == 0 {
		return ""
	}
	total := 0
	for _, l := range ls {
		total += len(l) + 1
	}
	buf := make([]byte, 0, total)
	for i, l := range ls {
		if i > 0 {
			buf = append(buf, '\n')
		}
		buf = append(buf, l...)
	}
	return string(buf)
}

// The doc is read by people who clone the repository. A citation of an
// assistant's private memory resolves for nobody, so the gate must not demand one.
func TestCheck_DocWithoutAnAssistantMemoryCitationPasses(t *testing.T) {
	doc := strings.Replace(canonicalDoc, "Memory: feedback_productive_idle_runbook_preauthor_2026_06_10.\n", "", 1)
	root := writeFixture(t, doc, canonicalRuntime)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("a doc with no private-memory citation was refused: err=%v failures=%v", err, outcome.Failures)
	}
}
