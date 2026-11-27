// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package alertrunbooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An alert file the gate cannot read was never audited for runbook links; the
// verdict must say so instead of treating it as clean.
func TestUnreadableAlertFileFails(t *testing.T) {
	root := rulesRepo(t, map[string]string{"x-alerts.yaml": "groups: []\n"})
	p := filepath.Join(root, DefaultRulesDir, "x-alerts.yaml")
	// A dangling symlink is unreadable for every uid, root included.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(filepath.Dir(p), "does-not-exist"), p); err != nil {
		t.Fatal(err)
	}
	out := Check(cfg(root))
	if out.Pass || !strings.Contains(strings.Join(out.Violations, "\n"), "x-alerts.yaml") {
		t.Fatalf("want a violation naming the unreadable file, got %+v", out)
	}
}
