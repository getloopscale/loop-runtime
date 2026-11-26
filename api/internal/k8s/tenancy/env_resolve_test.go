// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// RED test for issue #139: `loopruntime env inspect production` answers
// "Environment 'production' not found" while `loopruntime env list` shows it active
// and a release is serving HTTP 200 from it.
//
// This is the MIRROR of issue #142. Both were caused by the #124 backfill
// changing what `Project == ""` means — from "the entry" to "an obsolete
// entry" — while two predicates written under the old meaning stayed put:
//
//	#142  env list:    `|| e.Project == ""`      matched too MUCH → duplicate
//	#139  env inspect: `entry.Project == project` matched too LITTLE → not found
//
// `loopruntime env inspect <name>` has no `--project` flag, so the API receives
// `project=""`. Before the backfill that resolved the legacy entry, which was
// the only one. After the backfill the entry carries
// `Project: "loopzerokit-example"`, strict equality fails, and the lookup returns
// not-found for an environment that is demonstrably live.
//
// The handler comment claimed "first-match wins for tenant-wide queries" —
// a behaviour the code never had. Fixing the code to match the comment is
// only safe while a name is unambiguous; the ambiguous case is asserted
// below, because guessing there returns the WRONG project's environment,
// which is worse than the not-found being fixed.
package tenancy

import (
	"testing"

	apiv1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

func entries(pairs ...[2]string) []apiv1.TenantEnvironmentEntry {
	out := make([]apiv1.TenantEnvironmentEntry, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, apiv1.TenantEnvironmentEntry{Name: p[0], Project: p[1]})
	}
	return out
}

// TestResolveEnvEntry_BackfilledEntryFoundWithoutProject is the #139 RED.
//
// Exactly the reported state: the tenant's only `production` entry is
// project-scoped after the #124 backfill, and the caller (the CLI, which has
// no --project flag) asks with project="".
func TestResolveEnvEntry_BackfilledEntryFoundWithoutProject(t *testing.T) {
	es := entries(
		[2]string{"production", "loopzerokit-example"},
		[2]string{"preview", "loopzerokit-example"},
	)

	idx, ambiguous, err := resolveEnvEntry(es, "production", "")

	if err != nil {
		t.Fatalf("resolveEnvEntry returned error %v, want the unique match", err)
	}
	if len(ambiguous) != 0 {
		t.Fatalf("reported ambiguity %v for a name that exists in exactly one project", ambiguous)
	}
	if idx < 0 {
		t.Fatalf("`env inspect production` resolved to NOT FOUND while the environment exists\n" +
			"    as (production, loopzerokit-example) — issue #139.\n" +
			"    `isEnvMatch` requires entry.Project == project, and the CLI has no --project flag,\n" +
			"    so project is \"\". Before the #124 backfill that matched the legacy entry; after it,\n" +
			"    the only entry is project-scoped and strict equality can never match.")
	}
	if got := es[idx]; got.Name != "production" || got.Project != "loopzerokit-example" {
		t.Errorf("resolved to %+v, want (production, loopzerokit-example)", got)
	}
}

// TestResolveEnvEntry_ExactProjectStillWins keeps the M2 contract: when the
// caller DOES name a project, the tuple lookup is exact. Relaxing that would
// let `--project api` return project `web`'s environment.
func TestResolveEnvEntry_ExactProjectStillWins(t *testing.T) {
	es := entries(
		[2]string{"production", "web"},
		[2]string{"production", "api"},
	)

	idx, _, err := resolveEnvEntry(es, "production", "api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx < 0 {
		t.Fatalf("exact (production, api) lookup returned not-found")
	}
	if got := es[idx]; got.Project != "api" {
		t.Errorf("exact lookup resolved to project %q, want \"api\" — an exact tuple request must\n"+
			"    never fall back to another project", got.Project)
	}
}

// TestResolveEnvEntry_LegacyEntryPreferredWhenPresent preserves ADR D2: a
// tenant that still has the legacy shared entry resolves to IT for a
// project-less query, not to some other project's row.
func TestResolveEnvEntry_LegacyEntryPreferredWhenPresent(t *testing.T) {
	es := entries(
		[2]string{"production", "web"},
		[2]string{"production", ""}, // legacy shared entry (ADR D2)
	)

	idx, ambiguous, err := resolveEnvEntry(es, "production", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ambiguous) != 0 {
		t.Fatalf("legacy entry present, so the query is NOT ambiguous; got %v", ambiguous)
	}
	if idx < 0 || es[idx].Project != "" {
		t.Errorf("project-less query resolved to %+v, want the legacy entry (Project \"\") — ADR D2\n"+
			"    makes it the canonical answer whenever it still exists", es[idx])
	}
}

// TestResolveEnvEntry_AmbiguousNameRefusesToGuess is the reason the fix is
// not simply "first match wins".
//
// When `production` exists in two projects and the caller named none, ANY
// answer is a coin flip — and a wrong one hands back a different project's
// environment under the right-looking name. Returning an error the caller can
// act on ("pass --project") is the only honest outcome; silently picking one
// converts a visible not-found into an invisible wrong-answer.
func TestResolveEnvEntry_AmbiguousNameRefusesToGuess(t *testing.T) {
	es := entries(
		[2]string{"production", "web"},
		[2]string{"production", "api"},
	)

	idx, ambiguous, err := resolveEnvEntry(es, "production", "")

	if err == nil {
		t.Fatalf("ambiguous project-less lookup returned idx=%d without an error — picking one of\n"+
			"    two projects at random returns the WRONG environment under the right name, which is\n"+
			"    strictly worse than the not-found this fix removes", idx)
	}
	if len(ambiguous) != 2 {
		t.Errorf("error names %d candidate projects %v, want 2 — the message must tell the caller\n"+
			"    WHICH projects to disambiguate between, or it is not actionable", len(ambiguous), ambiguous)
	}
}

// TestResolveEnvEntry_GenuinelyMissingStaysNotFound proves the relaxation did
// not turn every miss into a match.
func TestResolveEnvEntry_GenuinelyMissingStaysNotFound(t *testing.T) {
	es := entries([2]string{"preview", "web"})

	idx, _, err := resolveEnvEntry(es, "production", "")
	if err != nil {
		t.Fatalf("unexpected error for a genuinely absent env: %v", err)
	}
	if idx >= 0 {
		t.Errorf("resolved absent environment %q to %+v — the fix must not manufacture matches",
			"production", es[idx])
	}
}
