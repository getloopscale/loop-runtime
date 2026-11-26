// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package clusterrenameguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// The baseline's `amendments` field is read.
//
// It was written and never consumed: `Baseline` declared only `tree_sha` and `generic`,
// so `json.Unmarshal` dropped `amendments` on the floor. The person who recorded one — on
// 2026-08-09, naming four paths and a reason — was writing into a field nothing looked
// at, and the gate went on reporting those four lines as missing.
//
// A convention with no reader is worse than no convention: it looks like the escape hatch
// is there, so the next person uses it instead of solving the problem, and nothing tells
// them it did nothing.
//
// An amendment says: these captured lines are legitimately gone, and here is why. It is
// scoped by path prefix, so it covers both a single file and a subtree.
func TestAmendmentExemptsARemovedPath(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "baseline.json")
	writeAmendedBaseline(t, base, map[string]any{
		"tree_sha": "irrelevant",
		"generic": []map[string]any{
			{"path": "docs/wiki/gone.md", "content": "vault means the product here"},
		},
		"amendments": []map[string]any{
			{"kind": "removed", "date": "2026-09-11", "paths": []string{"docs/wiki/"},
				"reason": "the docs consolidation deleted this subtree; the lines went with the documents, not with the rename"},
		},
	})

	out, err := Check(Config{RepoRoot: root, BaselinePath: base, SkipAncestryCheck: true})
	if err != nil || out.Verdict() != verdict.Pass {
		t.Fatalf("an amended path must not fail the gate: %v (%v)", err, out.Failures)
	}
}

// An amendment with no reason is a silent exemption, which is the thing the allowlist
// sunset contract refuses next door. It must not be honoured.
func TestAmendmentWithoutAReasonIsNotHonoured(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "baseline.json")
	writeAmendedBaseline(t, base, map[string]any{
		"tree_sha": "irrelevant",
		"generic": []map[string]any{
			{"path": "docs/wiki/gone.md", "content": "vault means the product here"},
		},
		"amendments": []map[string]any{
			{"kind": "removed", "paths": []string{"docs/wiki/"}, "reason": "   "},
		},
	})

	if out, err := Check(Config{RepoRoot: root, BaselinePath: base, SkipAncestryCheck: true}); err != nil || out.Verdict() != verdict.Fail {
		t.Fatalf("an amendment with no reason must not exempt anything (err=%v)", err)
	}
}

// A path no amendment covers still fails.
func TestUnamendedRemovalStillFails(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "baseline.json")
	writeAmendedBaseline(t, base, map[string]any{
		"tree_sha": "irrelevant",
		"generic": []map[string]any{
			{"path": "api/internal/real.go", "content": "vault means the product here"},
		},
		"amendments": []map[string]any{
			{"kind": "removed", "paths": []string{"docs/wiki/"}, "reason": "unrelated subtree"},
		},
	})

	if out, err := Check(Config{RepoRoot: root, BaselinePath: base, SkipAncestryCheck: true}); err != nil || out.Verdict() != verdict.Fail {
		t.Fatalf("a removal nobody amended must still fail (err=%v)", err)
	}
}

func writeAmendedBaseline(t *testing.T, path string, doc map[string]any) {
	t.Helper()
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
}
