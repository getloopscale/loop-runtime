// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package chartpairs

import (
	"os"
	"path/filepath"
	"testing"
)

// A Chart.yaml the gate cannot read hides a chart name, and with it possibly a
// pair; that is a gate error, not a chart that does not exist.
func TestUnreadableChartYamlIsAnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based read-error test needs non-root (root bypasses chmod 000)")
	}
	base := t.TempDir()
	dir := filepath.Join(base, "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "Chart.yaml")
	if err := os.WriteFile(p, []byte("name: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	if _, err := chartNames(base); err == nil {
		t.Fatal("want an error for an unreadable Chart.yaml, got nil")
	}
}

// A directory without Chart.yaml is not a chart, and an absent tree holds no
// charts; neither is an error.
func TestAbsentTreeAndNonChartDirectoryAreNotErrors(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "not-a-chart"), 0o755); err != nil {
		t.Fatal(err)
	}
	names, err := chartNames(base)
	if err != nil || len(names) != 0 {
		t.Fatalf("want no names and no error, got %v %v", names, err)
	}
	if names, err := chartNames(filepath.Join(base, "missing")); err != nil || len(names) != 0 {
		t.Fatalf("an absent tree holds no charts, got %v %v", names, err)
	}
}
