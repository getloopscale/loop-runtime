// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package docsrefs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The B-086 defect: a decision document that cites `ADR-0024` TO SAY THAT IT
// DOES NOT EXIST made the id resolve. Writing the document that records an absence
// must not fabricate a presence.
func TestDecisionDocDoesNotResolveAnIDItOnlyMentions(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/a/a.go", "package a\n\n// per ADR-0024\nvar X = 1\n")
	writeFile(t, root, "docs/decisions/2026-08-12-contract.md",
		"# Two rules command the opposite\n\n**Date:** 2026-08-12\n\n"+
			"`ADR-0024` is cited from 8 places in code and DOES NOT EXIST.\n")

	// The gate RETURNS AN ERROR when a normative reference does not resolve — the
	// error is the expected result here, not a test failure.
	out, err := Check(Config{RepoRoot: root})
	if err != nil || out.Verdict() != verdict.Fail || len(out.Unresolved) == 0 {
		t.Fatal("ADR-0024 resolved because it was mentioned in the body of a decision document — the absence fabricated the presence")
	}
	if out.Unresolved[0].Raw != "ADR-0024" {
		t.Errorf("the wrong id was left unresolved: %+v", out.Unresolved)
	}
}

// The counterpart the DoD demands: the criterion cannot be "a decision document
// never resolves". One that DECLARES the id in its own header is its definition,
// and resolves normally.
func TestDecisionDocResolvesTheIDItDeclaresInItsHeader(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/a/a.go", "package a\n\n// per ADR-0024\nvar X = 1\n")
	writeFile(t, root, "docs/decisions/adr-0024.md",
		"# ADR-0024: who writes the status\n\n**Status:** ACCEPTED\n")

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Unresolved) != 0 {
		t.Errorf("the ADR that DECLARES itself in the header stopped resolving: %+v", out.Unresolved)
	}
}

// Ordinary documentation — runbook, architecture doc — keeps resolving by mention.
// Without this assertion the change would become a header requirement for the whole
// tree, and then the ~30 ids cited by runbooks would start failing.
func TestOrdinaryDocumentationKeepsResolvingByMention(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/a/a.go", "package a\n\n// per ADR-0021\nvar X = 1\n")
	writeFile(t, root, "docs/operations/cell-drain-runbook.md",
		"# Cell drain\n\nNo automatic rebalancing (ADR-0008 / ADR-0021).\n")

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Unresolved) != 0 {
		t.Errorf("a runbook stopped resolving by mention: %+v", out.Unresolved)
	}
}
