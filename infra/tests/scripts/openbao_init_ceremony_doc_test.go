// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenBaoInitCeremonyIsDocumentedInTheRunbook keeps the OpenBao init
// ceremony written where operators read it (issue #101).
//
// Issue #101: operators assumed `task setup` provisioned OpenBao, when the
// dev-public topology needs a SEPARATE secrets cluster and a 5-share Shamir
// init that only the sponsor can perform, together with an operator. The fix
// documented that ceremony; the documents it first lived in were later
// consolidated into docs/runbooks.md.
//
// This test used to read those documents from git history at fixed commits.
// History is immutable, so it could never fail and protected nothing in the
// tree. It now reads the living runbook, and fails if the ceremony, its
// quorum, its verification step or the in-core prohibition is dropped.
func TestOpenBaoInitCeremonyIsDocumentedInTheRunbook(t *testing.T) {
	runbook := readFileText(t, repoFile(t, "docs", "runbooks.md"))

	for _, want := range []struct{ text, why string }{
		{"OpenBao init ceremony", "the ceremony has a section of its own, so the next operator finds it"},
		{"Shamir", "the ceremony is a Shamir secret-sharing init, not a chart install"},
		{"5 shards, quorum 3", "the quorum is what the custody model is built on"},
		{"Verify before shredding", "shards are verified against their hashes before the originals are destroyed"},
		{"secrets cluster", "OpenBao runs in its own cluster, not in core"},
		{"bootstrap-in-core", "installing OpenBao into core is named as the forbidden shortcut (B-047)"},
		{"topology violation", "the in-core install is written as a prohibition, not as an escape hatch"},
	} {
		if !strings.Contains(runbook, want.text) {
			t.Errorf("docs/runbooks.md no longer contains %q: %s (issue #101)", want.text, want.why)
		}
	}
}

// repoFile resolves a path relative to the repo root. Test lives at
// infra/tests/scripts/; repo root is 3 levels up.
func repoFile(t *testing.T, parts ...string) string {
	t.Helper()
	rel := append([]string{"..", "..", ".."}, parts...)
	p, err := filepath.Abs(filepath.Join(rel...))
	if err != nil {
		t.Fatalf("resolve repo file %v: %v", parts, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("repo file not found at %s: %v", p, err)
	}
	return p
}

// readFileText reads a file verbatim. Wrapper around os.ReadFile that
// fatals on error (test-style).
func readFileText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
