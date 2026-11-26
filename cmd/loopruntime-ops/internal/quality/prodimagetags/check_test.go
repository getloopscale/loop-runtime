// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package prodimagetags

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// TestCheck_ImmutableSha_Pass — :sha7 tags are compliant.
func TestCheck_ImmutableSha_Pass(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "charts/v.yaml", "image: registry.io/loopruntime-api:abc1234\n")
	o, err := Check(Config{Root: dir, ScanPaths: []string{"charts"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass, got %v", o.Violations)
	}
}

// TestCheck_ProdTag_Fail — :prod is a violation.
func TestCheck_ProdTag_Fail(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "charts/v.yaml", "image: registry.io/loopruntime-api:prod\n")
	o, err := Check(Config{Root: dir, ScanPaths: []string{"charts"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass || len(o.Violations) != 1 {
		t.Errorf("expected 1 violation, got %v", o.Violations)
	}
}

// TestCheck_LatestTag_Fail — :latest is a violation.
func TestCheck_LatestTag_Fail(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "charts/v.yaml", "  tag: \"latest\"\n  image: foo/bar:latest\n")
	o, err := Check(Config{Root: dir, ScanPaths: []string{"charts"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail for :latest")
	}
}

// TestCheck_DevFileExempt — dev.yaml files are skipped.
func TestCheck_DevFileExempt(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "infra/helm/dev.yaml", "image: foo/bar:latest\n")
	o, err := Check(Config{Root: dir, ScanPaths: []string{"infra/helm"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (dev exempt), got %v", o.Violations)
	}
}

// TestCheck_StagingFileExempt — staging.yml files are skipped.
func TestCheck_StagingFileExempt(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "infra/helm/staging.yml", "image: foo/bar:prod\n")
	o, err := Check(Config{Root: dir, ScanPaths: []string{"infra/helm"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (staging exempt), got %v", o.Violations)
	}
}

// TestCheck_CommentSkipped — commented mutable tag is not a violation.
func TestCheck_CommentSkipped(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "charts/v.yaml", "# image: foo/bar:prod (example)\n")
	o, err := Check(Config{Root: dir, ScanPaths: []string{"charts"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (comment), got %v", o.Violations)
	}
}

// TestCheck_OverrideAllowed — explicit override marker exempts the line.
func TestCheck_OverrideAllowed(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "charts/v.yaml", "image: foo/bar:latest # loopruntime:mutable-tag-allowed\n")
	o, err := Check(Config{Root: dir, ScanPaths: []string{"charts"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (override), got %v", o.Violations)
	}
}

// TestCheck_MissingPath_NoError — nonexistent scan path is skipped silently.
func TestCheck_MissingPath_NoError(t *testing.T) {
	dir := t.TempDir()
	o, err := Check(Config{Root: dir, ScanPaths: []string{"does/not/exist"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass || o.ScannedFiles != 0 {
		t.Errorf("expected Pass + 0 scanned, got pass=%v files=%d", o.Pass, o.ScannedFiles)
	}
}

// TestCheck_SingleFileScanPath — a file (not dir) scan path works.
func TestCheck_SingleFileScanPath(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "prod.yaml", "image: foo/bar:prod\n")
	o, err := Check(Config{Root: dir, ScanPaths: []string{"prod.yaml"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail for prod.yaml :prod tag")
	}
	if o.ScannedFiles != 1 {
		t.Errorf("expected 1 scanned file, got %d", o.ScannedFiles)
	}
}

// TestCheck_ProdSubstringNotMatched — :production should NOT match (boundary).
func TestCheck_ProdSubstringNotMatched(t *testing.T) {
	dir := t.TempDir()
	// ":prod" followed by alnum (production) is not the mutable tag.
	write(t, dir, "charts/v.yaml", "image: foo/bar:production-ready\n")
	o, err := Check(Config{Root: dir, ScanPaths: []string{"charts"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass — :production is not :prod, got %v", o.Violations)
	}
}

// TestCheck_DefaultPaths — empty ScanPaths uses DefaultScanPaths.
func TestCheck_DefaultPaths(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "charts/v.yaml", "image: foo/bar:prod\n")
	o, err := Check(Config{Root: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail via default paths scanning charts/")
	}
}

// ---------------------------------------------------------------------------
// #232 — prod-overlay split-form mutable tag detection (MEET-2026-07-12-001).
// The existing colon-form regex does NOT catch bare `tag: stable`; these tests
// pin the new split-form detection SCOPED to infra/helmfile/values/prod/ only.
// ---------------------------------------------------------------------------

const prodOverlayPath = "infra/helmfile/values/prod"

// TestProdOverlay_MutableSplitTag_Fails — fixtures 1+2: bare `tag: stable` /
// `tag: develop` in a .gotmpl prod overlay are violations (the actual #232 finding).
func TestProdOverlay_MutableSplitTag_Fails(t *testing.T) {
	for _, tag := range []string{"stable", "develop", "latest", "prod"} {
		dir := t.TempDir()
		write(t, dir, prodOverlayPath+"/loopruntime-cloud.yaml.gotmpl",
			"image:\n  repository: registry/loopruntime-cloud\n  tag: "+tag+"\n")
		o, err := Check(Config{Root: dir, ScanPaths: []string{prodOverlayPath}})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if o.Pass || len(o.Violations) != 1 {
			t.Errorf("tag %q: expected 1 violation, got pass=%v %v", tag, o.Pass, o.Violations)
		}
	}
}

// TestProdOverlay_Pragma_Passes — fixture 3 + bite-proof pair: the SAME overlay
// FAILS without the pragma and PASSES with it, proving the pragma bites on the
// new split-form (not merely the old colon-form).
func TestProdOverlay_Pragma_Passes(t *testing.T) {
	dir := t.TempDir()
	// with pragma → PASS
	write(t, dir, prodOverlayPath+"/loopruntime-cloud.yaml.gotmpl",
		"image:\n  tag: stable  # loopruntime:mutable-tag-allowed (#232 — operator to supply digest)\n")
	o, err := Check(Config{Root: dir, ScanPaths: []string{prodOverlayPath}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("with pragma: expected Pass, got %v", o.Violations)
	}

	// same fixture WITHOUT pragma → FAIL (bite-proof)
	dir2 := t.TempDir()
	write(t, dir2, prodOverlayPath+"/loopruntime-cloud.yaml.gotmpl", "image:\n  tag: stable\n")
	o2, err := Check(Config{Root: dir2, ScanPaths: []string{prodOverlayPath}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o2.Pass {
		t.Error("without pragma: expected Fail (gate must bite)")
	}
}

// TestProdOverlay_FalsePositiveGuards — fixtures 4,5,6,8,10 + comment: these
// prod-overlay lines are legitimate and MUST NOT trip the gate.
func TestProdOverlay_FalsePositiveGuards(t *testing.T) {
	cases := map[string]string{
		"semver":             "  tag: v1.2.3\n",                        // 4
		"sha7":               "  tag: abc1234\n",                       // 5
		"repo_substring":     "  repository: registry.io/foo-stable\n", // 6 (stable in repo name, not tag)
		"templated_gotmpl":   "  tag: {{ .Values.imageTag }}\n",        // 8 (templated — not a literal)
		"word_boundary_coin": "  tag: stablecoin\n",                    // 10 (stable is a prefix)
		"word_boundary_dev":  "  tag: development\n",                   // 10 (develop is a prefix)
		"date_stamped":       "  tag: latest-20260420\n",               // latest-family but pinned; not bare mutable
		"comment":            "  # tag: stable (example)\n",            // 7 (comment line)
	}
	for name, content := range cases {
		dir := t.TempDir()
		write(t, dir, prodOverlayPath+"/x.yaml.gotmpl", content)
		o, err := Check(Config{Root: dir, ScanPaths: []string{prodOverlayPath}})
		if err != nil {
			t.Fatalf("%s: Check: %v", name, err)
		}
		if !o.Pass {
			t.Errorf("%s: expected Pass, got violations %v", name, o.Violations)
		}
	}
}

// TestProdOverlay_InlineMutableTag — closes the review's LOW false-negative:
// an inline `image: repo:stable` / `:develop` in a prod overlay is a violation,
// while `foo-stable:v1` (stable in the repo name) and `repo:stable-v2` (pinned)
// PASS.
func TestProdOverlay_InlineMutableTag(t *testing.T) {
	fail := map[string]string{
		"inline_stable":  "  image: registry.io/loopruntime-cloud:stable\n",
		"inline_develop": "  image: registry.io/loopruntime-cloud:develop\n",
	}
	for name, content := range fail {
		dir := t.TempDir()
		write(t, dir, prodOverlayPath+"/x.yaml.gotmpl", content)
		o, err := Check(Config{Root: dir, ScanPaths: []string{prodOverlayPath}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if o.Pass {
			t.Errorf("%s: expected Fail for inline mutable tag", name)
		}
	}
	pass := map[string]string{
		"repo_name_stable": "  image: registry.io/foo-stable:v1.2.3\n",     // stable in repo name
		"pinned_suffix":    "  image: registry.io/loopruntime:stable-v2\n", // stable-v2 is a pin
	}
	for name, content := range pass {
		dir := t.TempDir()
		write(t, dir, prodOverlayPath+"/x.yaml.gotmpl", content)
		o, err := Check(Config{Root: dir, ScanPaths: []string{prodOverlayPath}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !o.Pass {
			t.Errorf("%s: expected Pass, got %v", name, o.Violations)
		}
	}
}

// TestProdOverlay_BootstrapDefaultsUnaffected — C2: the split-form detection is
// SCOPED to prod overlays. A bootstrap default `tag: latest` under infra/helm/
// (legitimately overridden at deploy time) MUST still PASS — the existing
// colon-form regex never matched the bare split form and we must not widen it.
func TestProdOverlay_BootstrapDefaultsUnaffected(t *testing.T) {
	dir := t.TempDir()
	// bare split-form mutable tags in a NON-overlay bootstrap default file
	write(t, dir, "infra/helm/loopruntime-api/values.yaml", "image:\n  tag: latest\n")
	o, err := Check(Config{Root: dir, ScanPaths: []string{"infra/helm"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("bootstrap default bare `tag: latest` must PASS (not a prod overlay), got %v", o.Violations)
	}
}

// TestProdOverlay_DotGotmplNotScannedOutsideOverlay — C2 corollary: a .gotmpl
// file OUTSIDE values/prod/ is NOT newly scanned (no behavior change for
// infra/helm/**/*.gotmpl), so a bare mutable split tag there PASSES.
func TestProdOverlay_DotGotmplNotScannedOutsideOverlay(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "infra/helm/promtail/values.yaml.gotmpl", "image:\n  tag: stable\n")
	o, err := Check(Config{Root: dir, ScanPaths: []string{"infra/helm"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf(".gotmpl outside values/prod/ must not be split-scanned, got %v", o.Violations)
	}
}

// TestDefaultScanPaths_IncludesProdValues — anti "implemented-but-not-wired":
// the new scan path must be in DefaultScanPaths or the wired gate never sees it.
func TestDefaultScanPaths_IncludesProdValues(t *testing.T) {
	found := false
	for _, p := range DefaultScanPaths {
		if p == "infra/helmfile/values/prod" {
			found = true
		}
	}
	if !found {
		t.Errorf("DefaultScanPaths must include infra/helmfile/values/prod; got %v", DefaultScanPaths)
	}
}

// TestBoundedExemption_ProdOverlays — MEET-2026-07-12-001 C4: exemptions are the
// classic gate-defeat vector. Assert the REAL repo carries EXACTLY ONE
// `loopruntime:mutable-tag-allowed` pragma under infra/helmfile/values/prod/ (the
// tracked #232 loopruntime-cloud instance). A second pragma appearing here must fail
// CI so the gate cannot be hollowed out silently.
func TestBoundedExemption_ProdOverlays(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "..", "..", "..") // → loopruntime/ from prodimagetags/
	overlayDir := filepath.Join(repoRoot, "infra", "helmfile", "values", "prod")
	if _, err := os.Stat(overlayDir); err != nil {
		t.Skipf("prod overlay dir not found from test CWD (%v) — skipping repo-level assertion", err)
	}
	count := 0
	err := filepath.WalkDir(overlayDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "loopruntime:mutable-tag-allowed") {
				count++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 loopruntime:mutable-tag-allowed pragma under values/prod/ (the tracked #232 instance), got %d", count)
	}
}
