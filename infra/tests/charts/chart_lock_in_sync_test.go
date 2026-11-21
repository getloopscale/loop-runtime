// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestEveryChartLockMatchesItsChartYAML runs `helm dependency build` on every published chart
// with a Chart.lock, the way an install from a clone does. helm refuses a lock whose digest does
// not match the dependencies in Chart.yaml. The render tests do not catch that: chartrender runs
// `helm dependency update`, which rewrites the lock instead of checking it.
//
// The charts are copied to a temporary directory first, so the vendored .tgz files helm writes
// do not land in the working tree.
func TestEveryChartLockMatchesItsChartYAML(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm is not on PATH and is a declared prerequisite of the chart tests: %v", err)
	}
	copied := filepath.Join(t.TempDir(), "charts")
	if err := os.CopyFS(copied, os.DirFS(chartsRoot(t))); err != nil {
		t.Fatalf("copying the charts: %v", err)
	}
	locks, err := filepath.Glob(filepath.Join(copied, "*", "Chart.lock"))
	if err != nil {
		t.Fatalf("listing the Chart.lock files: %v", err)
	}
	if len(locks) == 0 {
		t.Fatalf("no chart under %s has a Chart.lock", chartsRoot(t))
	}
	for _, lock := range locks {
		chart := filepath.Dir(lock)
		t.Run(filepath.Base(chart), func(t *testing.T) {
			out, err := exec.Command("helm", "dependency", "build", "--skip-refresh", chart).CombinedOutput()
			if err != nil {
				t.Errorf("helm dependency build %s: %v\n%s\nRegenerate the lock with `helm dependency update charts/%s`.",
					filepath.Base(chart), err, out, filepath.Base(chart))
			}
		})
	}
}
