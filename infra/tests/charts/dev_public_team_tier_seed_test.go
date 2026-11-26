// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Hermetic regression-lock for the canonical dev-public `team` plan
// tier seed (#123, qa-dogfood-cleanup-2026-06-21 T2.2 / ADR-Q7).
//
// Symptom (QA dogfooding 2026-06-21):
//
//	/projects/sample/scale → plan_tier='starter', ceiling=2, slider max=2
//
// while QA doc J-20 asserts `team` tier (replicas=3 succeeds; only
// >team triggers 402 PLAN_UPGRADE_REQUIRED).
//
// Root cause: `seed-post-cleanroom-all.sh` Step 9 invoked
// `seed-tenant-fixture-canonical.sh` WITHOUT `--team`, so the seed
// script's `PLAN_TIER="${LOOPRUNTIME_PLAN_TIER:-team}"` fallback was
// overridden when an operator shell had `LOOPRUNTIME_PLAN_TIER=starter`
// exported (a stale Cleanroom or dev-credentials.json residue).
//
// The audit asserts the canonical Step 9 invocation carries the
// explicit `--team` flag so operator env cannot shadow it.
//
// Cross-references:
//   - Script: infra/scripts/local/seed-post-cleanroom-all.sh § Step 9
//   - Sub-script: infra/scripts/local/seed-tenant-fixture-canonical.sh
//   - Plan: knowledge-base/plans/qa-dogfood-cleanup-2026-06-21-plan.md § T2.2
//   - ADR-Q7 — dev-public tenant seeded with `team` tier
package charts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	// M15 Wave 3 2026-07-08: Step 9 migrated from bash `--team` flag to Go
	// seed with an explicit `LOOPRUNTIME_PLAN_TIER=team` env prefix. The
	// operator-env-shadow invariant is preserved: prefixing the env var on
	// the same command line cannot be shadowed by an outer shell export.
	reStep9TeamFlag = regexp.MustCompile(
		`run_step "9-seed-tenant-fixture"\s+"\(cd \$REPO_ROOT/cmd/loopruntime-ops && LOOPRUNTIME_PLAN_TIER=team go run \. seed tenant-fixture[^"]*"`,
	)
)

// auditTeamTierSeed encodes the Step 9 regression-lock. Returns
// failure messages (empty slice → guard passes).
func auditTeamTierSeed(body string) []string {
	var errs []string

	if !reStep9TeamFlag.MatchString(body) {
		errs = append(errs, "(a) seed-post-cleanroom-all.sh Step 9 does NOT prefix `LOOPRUNTIME_PLAN_TIER=team` on the Go seed invocation — operator env `LOOPRUNTIME_PLAN_TIER=starter` (or any other tier) would shadow the canonical default, leaving the Scale page rendering the wrong tier. Restore the explicit `LOOPRUNTIME_PLAN_TIER=team` prefix per ADR-Q7.")
	}

	// Audit trail comment cites the 2026-06-21 origin so an operator
	// reading this in 6 months knows why the flag is mandatory.
	if !strings.Contains(body, "#123") && !strings.Contains(body, "qa-dogfood-cleanup-2026-06-21") {
		errs = append(errs, "(b) seed-post-cleanroom-all.sh Step 9 audit-trail comment does NOT cite #123 or the 2026-06-21 origin — operator must be able to trace why the explicit `--team` flag is required.")
	}

	return errs
}

func loadSeedPostCleanroomAll(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	p := filepath.Join(root, "infra/scripts/local/seed-post-cleanroom-all.sh")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

func TestDevPublicTeamTierSeed(t *testing.T) {
	body := loadSeedPostCleanroomAll(t)
	if errs := auditTeamTierSeed(body); len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
	}
}

// TestDevPublicTeamTierSeed_NegativeControl proves the audit fires
// against the buggy shape (Step 9 invocation without `--team`).
func TestDevPublicTeamTierSeed_NegativeControl(t *testing.T) {
	body := loadSeedPostCleanroomAll(t)
	mutated := reStep9TeamFlag.ReplaceAllString(
		body,
		`run_step "9-seed-tenant-fixture" "(cd $REPO_ROOT/cmd/loopruntime-ops && go run . seed tenant-fixture)"`,
	)
	if mutated == body {
		t.Fatalf("negative-control setup failed: could not find the canonical `LOOPRUNTIME_PLAN_TIER=team` invocation in script body — either the script changed shape or the regex drifted. The GREEN test is vacuous.")
	}
	errs := auditTeamTierSeed(mutated)
	if len(errs) == 0 {
		t.Errorf("negative control: auditTeamTierSeed() PASSED against a script whose Step 9 dropped `LOOPRUNTIME_PLAN_TIER=team` — the guard would NOT catch the QA 2026-06-21 regression (theatre).")
	}
	for _, e := range errs {
		t.Logf("negative-control verdict (expected to fire): %s", e)
	}
}
