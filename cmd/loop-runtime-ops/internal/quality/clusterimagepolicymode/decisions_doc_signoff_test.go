// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package clusterimagepolicymode

import (
	"os"
	"path/filepath"
	"testing"
)

// The gate accepts a sign-off in the document where decisions actually live.
//
// It searched three directories — `.claude/knowledge-base/decisions`,
// `.claude/knowledge-base/meetings` and `docs/meetings/adr`. Measured 2026-09-16: none of
// the three exists, so the gate was unsatisfiable. Whatever an operator wrote, it could
// not be found, and the remedy the gate printed named the first of those paths.
//
// Two things made that worse than an ordinary stale path. `.claude/` is gitignored, so a
// sign-off written there reaches nobody who clones — the same defect
// `cycle-rule-schema.md` corrected in its own change protocol on 2026-09-07. And the
// comment in the admission-policies values.yaml that named this path existed
// SPECIFICALLY to record that two earlier references were phantom; the replacement was
// phantom of the same kind, because the fix replaced the instance and not the class.
//
// `docs/decisions.md` is one of the six living documents the 2026-09-11 consolidation
// left, it is versioned, and it is where a decision in this repository is written.
func TestEnforcePassesWithSignOffInTheVersionedDecisionsDoc(t *testing.T) {
	root := t.TempDir()
	writeValues(t, root, "enforce")

	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o750); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	doc := "# Decisions\n\n## Admission policy runs in deny-flip mode locally\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(root, "docs", "decisions.md"), []byte(doc), 0o600); err != nil {
		t.Fatalf("write decisions doc: %v", err)
	}

	out := Check(Config{Root: root})
	if !out.Pass {
		t.Fatalf("enforce with a sign-off in docs/decisions.md must pass; got: %s", out.Reason)
	}
}

// And still refuses when the document exists and says nothing about the cutover — the
// gate must read the content, not the filename.
func TestEnforceFailsWhenTheDecisionsDocDoesNotMentionTheCutover(t *testing.T) {
	root := t.TempDir()
	writeValues(t, root, "enforce")

	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o750); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	doc := "# Decisions\n\n## Something unrelated\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(root, "docs", "decisions.md"), []byte(doc), 0o600); err != nil {
		t.Fatalf("write decisions doc: %v", err)
	}

	out := Check(Config{Root: root})
	if out.Pass {
		t.Fatal("a decisions doc that never mentions the cutover is not a sign-off")
	}
}

func writeValues(t *testing.T, root, mode string) {
	t.Helper()
	dir := filepath.Join(root, filepath.Dir(DefaultValuesFile))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir values: %v", err)
	}
	body := "clusterImagePolicy:\n  mode: " + mode + "\n"
	if err := os.WriteFile(filepath.Join(dir, "values.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write values: %v", err)
	}
}
