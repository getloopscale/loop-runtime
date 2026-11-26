// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package integration — T0.5 of deep-review-remediation-plan.md.
//
// This is the master invariant test that asserts every PostgreSQL CHECK
// constraint on a state-bearing column matches EXACTLY the State constants
// declared in the corresponding pkg/statemachine/*.go file. No more, no less.
//
// Why this exists:
//   - find_data_001 (CRITICAL): migration 000010 missed DRIFT_ALERT and
//     ROLLBACK_FAILED → state machine can panic on transition because DB
//     CHECK rejects the row.
//   - find_data_002 (CRITICAL): migration 000008 introduced phantom states
//     BUILDING and FAILED that no state machine declares → DB allows values
//     the runtime cannot represent.
//   - find_test_001 (HIGH): the previous safety-net test queried the wrong
//     constraint name and was fail-open. This test is fail-closed via
//     canary assertions.
//
// The test runs against a real PostgreSQL connection (integration tier).
// It is a required PR gate (per find_cicd_004 fix in T4.8).
package integration

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// constraintCase — one (table, constraint, expectedStates) tuple.
type constraintCase struct {
	table      string
	constraint string
	expected   []string // sorted ASCENDING
	// Note: the `notInCheck []string` field was declared here for
	// "intentionally not persisted to DB" opt-outs but never read by
	// any test case (all current state machines persist every state).
	// Removed 2026-05-15 iter 14. Re-add when a state machine genuinely
	// has transient-only states.
}

// stateConstantsAsSortedStringSlice — extract State constants from a slice
// of statemachine.State, drop opt-outs, return sorted []string.
func stateConstantsAsSortedStringSlice(states []statemachine.State, optOut []string) []string {
	skip := make(map[string]bool, len(optOut))
	for _, s := range optOut {
		skip[s] = true
	}
	out := make([]string, 0, len(states))
	for _, s := range states {
		v := string(s)
		if v == "" {
			continue // skip Empty sentinel
		}
		if skip[v] {
			continue
		}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// extractStateSetFromConstraintDef — parse "CHECK (status IN ('A', 'B'))"
// and return sorted []string of values. Tolerant of whitespace and quotes.
func extractStateSetFromConstraintDef(consrc string) []string {
	// pg_get_constraintdef returns e.g.: CHECK (status IN ('READY', 'PENDING', ...))
	re := regexp.MustCompile(`'([^']+)'`)
	matches := re.FindAllStringSubmatch(consrc, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

// TestStateMachineCheckConstraintInvariant — for every state-bearing table,
// assert that pg_constraint.consrc exactly matches the State constants
// declared in pkg/statemachine.
//
// EC-6: this test must run against an existing-state DB (migrations applied
// sequentially), not just a fresh fake schema. setupHardeningTestDB applies
// real migrations from infra/scripts/production/run-migrations.sh.
func TestStateMachineCheckConstraintInvariant(t *testing.T) {
	pool := setupHardeningTestDB(t)
	ctx := context.Background()

	// releases.status — defined in pkg/statemachine/release.go. Includes
	// DRIFT_ALERT and ROLLBACK_FAILED (find_data_001 fix). Plus ARCHIVED
	// (terminal added by migration 000008 for archive feature; not in the
	// state machine because archived rows are read-only metadata).
	releaseStatesInDB := stateConstantsAsSortedStringSlice([]statemachine.State{
		statemachine.State(statemachine.RPhReady),
		statemachine.State(statemachine.RPhPending),
		statemachine.State(statemachine.RPhActive),
		statemachine.State(statemachine.RPhGitMirrorFailed),
		statemachine.State(statemachine.RPhSyncFailed),
		statemachine.State(statemachine.RPhDriftAlert),
		statemachine.State(statemachine.RPhSuperseded),
		statemachine.State(statemachine.RPhRollingBack),
		statemachine.State(statemachine.RPhRolledBack),
		statemachine.State(statemachine.RPhRollbackFailed),
		statemachine.State(statemachine.RPhCancelled),
	}, nil)
	releaseStatesInDB = append(releaseStatesInDB, "ARCHIVED")
	sort.Strings(releaseStatesInDB)

	// builds.status — defined in pkg/statemachine/build.go.
	buildStatesInDB := stateConstantsAsSortedStringSlice([]statemachine.State{
		statemachine.State(statemachine.BPhQueued),
		statemachine.State(statemachine.BPhRunning),
		statemachine.State(statemachine.BPhReleasing),
		statemachine.State(statemachine.BPhSucceeded),
		statemachine.State(statemachine.BPhFailed),
		statemachine.State(statemachine.BPhCancelling),
		statemachine.State(statemachine.BPhCancelled),
	}, nil)

	cases := []constraintCase{
		{
			table:      "releases",
			constraint: "releases_status_check",
			expected:   releaseStatesInDB,
		},
		{
			table:      "builds",
			constraint: "builds_status_check",
			expected:   buildStatesInDB,
		},
		// Future expansion (deep-review-remediation T0.5 scope):
		// tenants, environments, preview_environments, promotions,
		// rollbacks, exposures, applications, platform_bootstraps.
	}

	for _, tc := range cases {
		t.Run(tc.table, func(t *testing.T) {
			var consrc string
			err := pool.QueryRow(ctx, `
				SELECT pg_get_constraintdef(oid)
				FROM pg_constraint
				WHERE conname = $1
			`, tc.constraint).Scan(&consrc)

			// CANARY (T0.4 + T0.5): if the constraint is not found, the test
			// MUST fail loudly. fail-open here is what masked find_data_001.
			if err != nil {
				t.Fatalf("constraint %s not found via pg_get_constraintdef — test is broken or migration regressed: %v", tc.constraint, err)
			}
			if !strings.Contains(consrc, "CHECK") {
				t.Fatalf("constraint %s exists but is not a CHECK: %s", tc.constraint, consrc)
			}

			actual := extractStateSetFromConstraintDef(consrc)
			if len(actual) == 0 {
				t.Fatalf("constraint %s parsed zero state values from consrc=%q — parser is broken", tc.constraint, consrc)
			}

			// Compare sorted slices exactly.
			expected := tc.expected
			if !sliceEqual(actual, expected) {
				missing := setDiff(expected, actual)
				phantom := setDiff(actual, expected)
				t.Errorf(
					"constraint %s state set drift:\n  expected (%d): %v\n  actual   (%d): %v\n  missing from CHECK (find_data_001 class): %v\n  phantom in CHECK (find_data_002 class): %v",
					tc.constraint,
					len(expected), expected,
					len(actual), actual,
					missing, phantom,
				)
			}
		})
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// setDiff returns elements in a but not in b.
func setDiff(a, b []string) []string {
	bset := make(map[string]bool, len(b))
	for _, v := range b {
		bset[v] = true
	}
	out := []string{}
	for _, v := range a {
		if !bset[v] {
			out = append(out, v)
		}
	}
	return out
}
