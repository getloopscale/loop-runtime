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
	root := fakeRepo(t, threeLineDecl, map[string]string{
		"api/internal/metrics/metrics.go": metricsGo,
		"api/internal/other/locked.go":    "package other\n",
	})
	p := filepath.Join(root, "api", "internal", "other", "locked.go")
	// A dangling symlink is unreadable for every uid, root included.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(filepath.Dir(p), "does-not-exist"), p); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(Config{RepoRoot: root, SiblingRoot: absentSibling(t)}); err == nil {
		t.Fatal("want an error for an unreadable Go file, got nil")
	}
}
