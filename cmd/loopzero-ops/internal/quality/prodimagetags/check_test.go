// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package prodimagetags

import (
	"os"
	"path/filepath"
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
	write(t, dir, "charts/v.yaml", "image: registry.io/loopzero-api:abc1234\n")
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
	write(t, dir, "charts/v.yaml", "image: registry.io/loopzero-api:prod\n")
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
	write(t, dir, "charts/v.yaml", "image: foo/bar:latest # loopzero:mutable-tag-allowed\n")
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
