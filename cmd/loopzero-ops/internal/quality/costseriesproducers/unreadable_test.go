// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package costseriesproducers

import (
	"os"
	"path/filepath"
	"testing"
)

// An unreadable Go file may be the one that declares a pinned metric; skipping
// it would report the producer as missing, or the scan as complete, falsely.
func TestUnreadableGoFileFailsTheScan(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based read-error test needs non-root (root bypasses chmod 000)")
	}
	root := fakeRepo(t, threeLineDecl, map[string]string{
		"api/internal/metrics/metrics.go": metricsGo,
		"api/internal/other/locked.go":    "package other\n",
	})
	p := filepath.Join(root, "api", "internal", "other", "locked.go")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	if _, err := Check(Config{RepoRoot: root, SiblingRoot: absentSibling(t)}); err == nil {
		t.Fatal("want an error for an unreadable Go file, got nil")
	}
}
