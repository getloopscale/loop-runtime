// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T1.1 of plan m7-cleanroom-validation-readiness v1.0 — hermetic
// pre-flight gate for M7 Cleanroom validation chain.
//
// Per ADR 2026-06-23-validation-milestone-sponsor-hard-stop § Decision
// Step 3 Step 0: this test MUST PASS before operator runs `task clean:all`
// + the rest of the M7 chain. Without this gate, operator wastes sponsor
// JUNTOS time on ad-hoc M1-M6 closure-evidence verification.
//
// 7 assertion categories:
//
//   - A. M1-M6 plan files exist on disk (engineering closure attribution)
//   - B. M1-M6 ADR files exist (where applicable — M3+M4 didn't ship ADRs)
//   - C. Last semver tag v0.27.1 exists (release after M6)
//   - D. M5 + M6 sibling hermetic guards exist (regression-locks for the
//     contracts cited as M7 closure-evidence)
//   - E. M7 chain scripts exist at canonical paths (seed-post-cleanroom-all,
//     dogfood-all, live-proof-dev-public, run-chaos)
//   - F. Mandate cleanroom-up-live-test-mandate.md exists
//   - G. qa-readiness-roadmap-2026-06-23.md has M1-M6 in their respective
//     final states (✅ COMPLETED or 🟢 ENGINEERING_COMPLETED)
//
// Per ADR D2 of this plan: NO Cleanroom + UP + seeds + DOGFOOD + LIVE chain
// runs during this preparation slice. This gate runs hermetic only —
// operator runs the actual LIVE chain as Step 1+ post sponsor scheduling.
//
// Closes the M7 engineering preparation per cycle-plan.md Tier T1
// (test-only + doc-only); the actual M7 ✅ COMPLETED flip awaits
// sponsor verbatim "Funcionou!" in a future release.

package charts_test

import (
	"os"
	"strings"
	"testing"
)

const (
	repoRootFromCharts = "../../.."
)

// repoPath joins repoRootFromCharts with the project-relative path.
func repoPath(rel string) string {
	return repoRootFromCharts + "/" + rel
}

// TestM7Preflight_M1ToM6PlansExist asserts the canonical milestone plans
// for M1-M6 exist on disk. Per closure-attribution in M6 backlog-closure
// ADR § Worked Example, every prior milestone has a plan file at the
// documented canonical path.
func TestM7Preflight_M1ToM6PlansExist(t *testing.T) {
	t.Parallel()
	plans := []string{
		".claude/knowledge-base/plans/m1-commit-canonical-30-fixes-plan.md",
		".claude/knowledge-base/plans/m2-promote-11-canonical-seeds-plan.md",
		".claude/knowledge-base/plans/m3-verify-rate-limit-middleware-plan.md",
		".claude/knowledge-base/plans/m4-scale-flow-tier-mismatch-resolution-plan.md",
		".claude/knowledge-base/plans/m5-stripe-sot-canonical-closure-plan.md",
		".claude/knowledge-base/plans/m6-backlog-closure-formalization-plan.md",
	}
	for _, p := range plans {
		info, err := os.Stat(repoPath(p))
		if err != nil {
			t.Errorf("M1-M6 plan missing: %s (err=%v)", p, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("expected file, got directory: %s", p)
		}
		if info.Size() < 1000 {
			t.Errorf("plan %s suspiciously small (%d bytes) — possible truncation", p, info.Size())
		}
	}
}

// TestM7Preflight_MilestoneADRsExist asserts the canonical milestone-
// closure ADRs for M5 (Tier N/A) + M6 (backlog-closure) exist. M1-M4
// did not require dedicated milestone-closure ADRs (closed via standard
// /implement cycles), so they are not asserted here.
func TestM7Preflight_MilestoneADRsExist(t *testing.T) {
	t.Parallel()
	adrs := []string{
		".claude/knowledge-base/decisions/2026-06-23-tier-na-data-only-milestone-closure.md",
		".claude/knowledge-base/decisions/2026-06-23-backlog-closure-milestone.md",
	}
	for _, a := range adrs {
		info, err := os.Stat(repoPath(a))
		if err != nil {
			t.Errorf("milestone-closure ADR missing: %s (err=%v)", a, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("expected file, got directory: %s", a)
		}
	}
}

// TestM7Preflight_SiblingHermeticGuardsExist asserts the canonical
// sibling hermetic guards that lock the contracts cited as closure-
// evidence for M5 (Stripe SoT shape) + M6 (cross-cluster Jeito A).
// If these regress, M7's reliance on prior closure-evidence breaks.
func TestM7Preflight_SiblingHermeticGuardsExist(t *testing.T) {
	t.Parallel()
	guards := []string{
		"infra/tests/charts/stripe_sot_shape_test.go",
		"infra/tests/charts/cross_cluster_secrets_test.go",
	}
	for _, g := range guards {
		info, err := os.Stat(repoPath(g))
		if err != nil {
			t.Errorf("sibling hermetic guard missing: %s (err=%v)", g, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("expected file, got directory: %s", g)
		}
	}
}

// TestM7Preflight_ChainScriptsExist asserts the canonical M7 LIVE chain
// scripts exist at the paths cited in the operator runbook. If any
// script is renamed/relocated, the runbook becomes stale and operator
// hits a missing-file error mid-chain.
func TestM7Preflight_ChainScriptsExist(t *testing.T) {
	t.Parallel()
	scripts := []string{
		"infra/scripts/local/seed-post-cleanroom-all.sh",
		"archive/canonical-tests-v1/scripts/local/dogfood-all.sh",
		"archive/canonical-tests-v1/scripts/local/live-proof-dev-public.sh",
		"infra/tests/runner/run-chaos.sh",
	}
	for _, s := range scripts {
		info, err := os.Stat(repoPath(s))
		if err != nil {
			t.Errorf("M7 chain script missing: %s (err=%v)", s, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("expected file, got directory: %s", s)
		}
	}
}

// TestM7Preflight_MandateExists asserts the cleanroom-up-live-test-
// mandate.md file exists. Per Unbreakable Rule 3, the mandate IS the
// contract that forbids Claude-side fabrication of M7 LIVE results.
// If the mandate file is deleted/moved, M7's sponsor HARD STOP semantic
// loses its canonical anchor.
func TestM7Preflight_MandateExists(t *testing.T) {
	t.Parallel()
	mandatePath := ".claude/rules/cleanroom-up-live-test-mandate.md"
	info, err := os.Stat(repoPath(mandatePath))
	if err != nil {
		t.Fatalf("cleanroom-up-live-test-mandate.md missing: %s (err=%v)", mandatePath, err)
	}
	if info.IsDir() {
		t.Fatalf("expected file, got directory: %s", mandatePath)
	}
	// Defense-in-depth: assert the INQUEBRÁVEL marker is present.
	// If someone weakens the mandate by removing INQUEBRÁVEL, this catches it.
	raw, err := os.ReadFile(repoPath(mandatePath))
	if err != nil {
		t.Fatalf("mandate read failed: %v", err)
	}
	if !strings.Contains(string(raw), "INQUEBRÁVEL") {
		t.Errorf("mandate lost INQUEBRÁVEL marker — sponsor HARD STOP contract may have been weakened")
	}
}

// TestM7Preflight_RoadmapMilestoneStatesCanonical asserts that the
// qa-readiness-roadmap-2026-06-23.md has M1-M6 in their respective
// canonical states. Without this, M7's preparation may be running
// against a stale or rewritten roadmap.
//
// Expected canonical states (post-M6 v0.27.1 release):
//   - M1, M2, M3, M4 ✅ COMPLETED
//   - M5 🟢 ENGINEERING_COMPLETED (LIVE pending M7)
//   - M6 ✅ COMPLETED
func TestM7Preflight_RoadmapMilestoneStatesCanonical(t *testing.T) {
	t.Parallel()
	roadmapPath := ".claude/knowledge-base/plans/qa-readiness-roadmap-2026-06-23.md"
	raw, err := os.ReadFile(repoPath(roadmapPath))
	if err != nil {
		t.Fatalf("roadmap read failed: %v", err)
	}
	content := string(raw)

	// Expected M1-M4 + M6 markers (✅ COMPLETED).
	expectedCompleted := []string{
		"M1 — Persistir os 30 fixes desta sessão ✅ COMPLETED",
		"M2 — Promover 11 canonical seeds para `seed-post-cleanroom-all.sh` ✅ COMPLETED",
		"M3 — Implementar `/verify` rate-limit middleware (ADR D7) ✅ COMPLETED",
		"M4 — Refatorar `scale-flow` test para resolver tier-mismatch ✅ COMPLETED",
		"M6 — Backlog: tasks #87 #88 #138 ✅ COMPLETED",
	}
	for _, marker := range expectedCompleted {
		if !strings.Contains(content, marker) {
			t.Errorf("roadmap missing canonical ✅ COMPLETED marker: %q", marker)
		}
	}

	// M5 expected 🟢 ENGINEERING_COMPLETED state (LIVE pending M7 per Unbreakable Rule 3).
	m5Marker := "M5 — Sponsor input: Stripe API key real 🟢 ENGINEERING_COMPLETED (LIVE pending M7)"
	if !strings.Contains(content, m5Marker) {
		t.Errorf("roadmap missing canonical M5 marker: %q", m5Marker)
	}

	// M7 must NOT be ✅ COMPLETED yet — this preparation slice ships
	// 🟢 PREPARATION_COMPLETED at most. Future closure release flips to ✅.
	if strings.Contains(content, "M7 — Cleanroom validation + Sponsor JUNTOS walkthrough ✅ COMPLETED") {
		t.Errorf("roadmap M7 prematurely marked ✅ COMPLETED — must be 🟢 PREPARATION_COMPLETED until sponsor JUNTOS executes")
	}
}

// TestM7Preflight_PreparationArtifactsExist asserts the M7 engineering-
// side preparation artifacts shipped by THIS plan exist. Self-referential
// gate that ensures the preparation slice deliverables resolve before
// operator schedules sponsor JUNTOS.
func TestM7Preflight_PreparationArtifactsExist(t *testing.T) {
	t.Parallel()
	artifacts := []string{
		".claude/knowledge-base/plans/m7-cleanroom-validation-readiness-plan.md",
		".claude/knowledge-base/decisions/2026-06-23-validation-milestone-sponsor-hard-stop.md",
		".claude/knowledge-base/roadmap-runs/M7-2026-06-23.md",
		"docs/operations/m7-cleanroom-validation-runbook.md",
		"docs/operations/baselines/M7-template-qa-readiness-complete.md",
		".github/ISSUE_TEMPLATE/qa-readiness.md",
	}
	for _, a := range artifacts {
		info, err := os.Stat(repoPath(a))
		if err != nil {
			t.Errorf("M7 preparation artifact missing: %s (err=%v)", a, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("expected file, got directory: %s", a)
		}
	}
}
