// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package quarantineprotect

import (
	"strings"
	"testing"
)

// T0.2 of plan canonical-tests-rewrite-v2 v1.0 — RED tests for the
// quarantine-protect gate that blocks PRs touching archive/canonical-tests-v1/**.
//
// Per ADR-2026-06-17-T3-canonical-tests-rewrite D5: archive directory is
// READ-ONLY consultable; modifications are forbidden until DELETE phase
// (signaled by PR title marker `[ARCHIVE-DELETE]`).
//
// Per cycle-implement TDD pattern: these tests fail until Check is implemented.

// helper: build a Config that uses a stub diff (paths simulated) instead of
// invoking git. This makes the gate hermetically testable.
func mockCfg(addedOrModified []string, deleted []string, prTitle string) Config {
	return Config{
		RepoRoot:        "/tmp/quarantineprotect-fixture",
		ArchivePathRoot: "archive/canonical-tests-v1",
		BaseRef:         "origin/main",
		HeadRef:         "HEAD",
		PRTitle:         prTitle,
		DiffAddedOrMod:  addedOrModified,
		DiffDeleted:     deleted,
		mockMode:        true,
	}
}

// Test 1 — ADD/MODIFY in archive/canonical-tests-v1/** is REJECTED (default).
func TestQuarantineProtect_RejectsArchiveModify(t *testing.T) {
	cfg := mockCfg(
		[]string{"archive/canonical-tests-v1/scripts/dogfood-all.sh"},
		nil,
		"feat(test): tweak v1 dogfood",
	)
	outcome, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check returned infrastructure error: %v", err)
	}
	if outcome.Pass {
		t.Fatalf("expected Pass=false on archive modification, got Pass=true")
	}
	if len(outcome.Violations) == 0 {
		t.Fatalf("expected ≥1 violation, got 0")
	}
	expected := "archive/canonical-tests-v1/ is READ-ONLY"
	if !strings.Contains(strings.Join(outcome.Violations, "\n"), expected) {
		t.Fatalf("expected violation message containing %q, got %v", expected, outcome.Violations)
	}
}

// Test 2 — DELETE in archive/** with PR title `[ARCHIVE-DELETE]` marker is ALLOWED.
func TestQuarantineProtect_AllowsDeleteWithMarker(t *testing.T) {
	cfg := mockCfg(
		nil,
		[]string{"archive/canonical-tests-v1/scripts/dogfood-all.sh"},
		"[ARCHIVE-DELETE] close ADR T3 after CP3",
	)
	outcome, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check returned infrastructure error: %v", err)
	}
	if !outcome.Pass {
		t.Fatalf("expected Pass=true on archive DELETE with marker, got Pass=false (violations: %v)",
			outcome.Violations)
	}
}

// Test 3 — DELETE in archive/** WITHOUT the marker is REJECTED (anti-accidental-delete).
func TestQuarantineProtect_RejectsArchiveDeleteWithoutMarker(t *testing.T) {
	cfg := mockCfg(
		nil,
		[]string{"archive/canonical-tests-v1/scripts/dogfood-all.sh"},
		"chore: cleanup",
	)
	outcome, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check returned infrastructure error: %v", err)
	}
	if outcome.Pass {
		t.Fatalf("expected Pass=false on archive DELETE without marker, got Pass=true")
	}
	expected := "ARCHIVE-DELETE"
	if !strings.Contains(strings.Join(outcome.Violations, "\n"), expected) {
		t.Fatalf("expected violation message mentioning %q marker, got %v", expected, outcome.Violations)
	}
}

// Test 4 — Non-archive paths are unaffected (no false positives).
func TestQuarantineProtect_AllowsNonArchiveChange(t *testing.T) {
	cfg := mockCfg(
		[]string{"infra/tests/livetest/cleanroom_test.go", "internal/livetest/cleanroom/helpers.go"},
		[]string{"cmd/loop-runtime-ops/internal/quality/legacy/old.go"},
		"feat(test): T3.1 cleanroom v2 helpers",
	)
	outcome, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check returned infrastructure error: %v", err)
	}
	if !outcome.Pass {
		t.Fatalf("expected Pass=true on non-archive change, got Pass=false (violations: %v)",
			outcome.Violations)
	}
}

// Test 5 — Mixed diff: archive ADD + non-archive change → REJECTED (archive ADD wins).
func TestQuarantineProtect_RejectsMixedArchiveAndNonArchive(t *testing.T) {
	cfg := mockCfg(
		[]string{
			"archive/canonical-tests-v1/skills/cleanroom/SKILL.md",
			"infra/tests/livetest/cleanroom_test.go",
		},
		nil,
		"feat: mixed change",
	)
	outcome, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check returned infrastructure error: %v", err)
	}
	if outcome.Pass {
		t.Fatalf("expected Pass=false on mixed diff with archive ADD, got Pass=true")
	}
	if len(outcome.Violations) < 1 {
		t.Fatalf("expected ≥1 violation, got %d", len(outcome.Violations))
	}
}

// Test 6 — Archive ADD even with marker is REJECTED (marker only allows DELETE).
func TestQuarantineProtect_RejectsArchiveAddEvenWithMarker(t *testing.T) {
	cfg := mockCfg(
		[]string{"archive/canonical-tests-v1/scripts/new-script.sh"},
		nil,
		"[ARCHIVE-DELETE] sneaky add",
	)
	outcome, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check returned infrastructure error: %v", err)
	}
	if outcome.Pass {
		t.Fatalf("expected Pass=false on archive ADD even with marker (marker only allows DELETE), got Pass=true")
	}
}

// Test 7 — Sibling archive paths NOT covered (only archive/canonical-tests-v1/).
func TestQuarantineProtect_AllowsSiblingArchive(t *testing.T) {
	cfg := mockCfg(
		[]string{"archive/something-else/file.txt"},
		nil,
		"feat: not the quarantine",
	)
	outcome, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check returned infrastructure error: %v", err)
	}
	if !outcome.Pass {
		t.Fatalf("expected Pass=true on non-quarantine archive sibling, got Pass=false (violations: %v)",
			outcome.Violations)
	}
}
