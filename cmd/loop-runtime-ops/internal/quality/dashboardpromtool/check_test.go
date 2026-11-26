// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dashboardpromtool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCheck_BasicJSONSyntax_Valid(t *testing.T) {
	dir := t.TempDir()
	// Write 2 valid dashboards
	for _, name := range []string{"a.json", "b.json"} {
		err := os.WriteFile(filepath.Join(dir, name),
			[]byte(`{"title":"test","panels":[]}`), 0o644)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	got, err := Check(context.Background(), Config{DashboardsDir: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.TotalScanned != 2 {
		t.Errorf("TotalScanned=%d want 2", got.TotalScanned)
	}
	if len(got.SyntaxErrors) != 0 {
		t.Errorf("expected 0 syntax errors, got %v", got.SyntaxErrors)
	}
}

func TestCheck_BasicJSONSyntax_Invalid(t *testing.T) {
	dir := t.TempDir()
	// Write 1 valid + 1 invalid
	_ = os.WriteFile(filepath.Join(dir, "valid.json"), []byte(`{"x":1}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "broken.json"), []byte(`{"x"::}`), 0o644)

	got, err := Check(context.Background(), Config{DashboardsDir: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.TotalScanned != 2 {
		t.Errorf("TotalScanned=%d want 2", got.TotalScanned)
	}
	if len(got.SyntaxErrors) != 1 {
		t.Errorf("expected 1 syntax error, got %v", got.SyntaxErrors)
	}
	if got.Pass {
		t.Error("Pass should be false when syntax errors present")
	}
}

func TestCheck_PromtoolSkipsWhenMissing(t *testing.T) {
	if _, err := exec.LookPath("promtool"); err == nil {
		t.Skip("promtool IS installed — skip-on-missing test N/A")
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "d.json"),
		[]byte(`{"panels":[{"targets":[{"expr":"up"}]}]}`), 0o644)
	got, err := Check(context.Background(), Config{DashboardsDir: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !got.PromtoolSkipped {
		t.Errorf("expected PromtoolSkipped=true, got %+v", got)
	}
	if got.PromtoolReason == "" {
		t.Error("expected non-empty PromtoolReason")
	}
	// Pass should still be true (no syntax errors; promtool skipped is not failure)
	if !got.Pass {
		t.Errorf("Pass should be true when only promtool is skipped, got %+v", got)
	}
}

func TestCheck_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	got, err := Check(context.Background(), Config{DashboardsDir: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.TotalScanned != 0 {
		t.Errorf("empty dir should scan 0 files, got %d", got.TotalScanned)
	}
	if !got.Pass {
		t.Error("empty dir should Pass (vacuously true)")
	}
}

func TestCheck_RequiresDashboardsDir(t *testing.T) {
	_, err := Check(context.Background(), Config{})
	if err == nil {
		t.Error("empty DashboardsDir should return error")
	}
}

func TestExtractPromQLQueries_Basic(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "d.json")
	_ = os.WriteFile(tmp, []byte(`{
  "panels": [
    {"targets": [{"expr": "up"}, {"expr": "rate(http[5m])"}]},
    {"targets": [{"expr": ""}, {"expr": "sum(loop_runtime_builds_total)"}]}
  ]
}`), 0o644)
	queries, err := extractPromQLQueries(tmp)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	// Empty exprs should be filtered out
	if len(queries) != 3 {
		t.Errorf("expected 3 non-empty queries, got %d: %v", len(queries), queries)
	}
}
