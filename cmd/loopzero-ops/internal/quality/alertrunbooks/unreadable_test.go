// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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
	if os.Geteuid() == 0 {
		t.Skip("permission-based read-error test needs non-root (root bypasses chmod 000)")
	}
	root := rulesRepo(t, map[string]string{"x-alerts.yaml": "groups: []\n"})
	p := filepath.Join(root, DefaultRulesDir, "x-alerts.yaml")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	out := Check(cfg(root))
	if out.Pass || !strings.Contains(strings.Join(out.Violations, "\n"), "x-alerts.yaml") {
		t.Fatalf("want a violation naming the unreadable file, got %+v", out)
	}
}
