// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package chartpairs

import (
	"os"
	"path/filepath"
	"testing"
)

// A Chart.yaml the gate cannot read hides a chart name, and with it possibly a
// duplicate; that is a gate error, not a chart that does not exist.
func TestUnreadableChartYamlIsAnError(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A Chart.yaml that is a directory cannot be read by any uid.
	if err := os.MkdirAll(filepath.Join(dir, "Chart.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := scanTree(base); err == nil {
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
	charts, err := scanTree(base)
	if err != nil || len(charts) != 0 {
		t.Fatalf("want no charts and no error, got %v %v", charts, err)
	}
	if charts, err := scanTree(filepath.Join(base, "missing")); err != nil || len(charts) != 0 {
		t.Fatalf("an absent tree holds no charts, got %v %v", charts, err)
	}
}
