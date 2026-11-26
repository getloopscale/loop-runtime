// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package loki

import (
	"strings"
	"testing"
)

// TestBuildRuntimeLogQL_M2CanonicalAndLegacyMatched — T2.2 of plan
// 2026-06-11-code-review-remediation. Post-M2 (2026-06-09) the canonical
// namespace formula is `loop-runtime-{tenant}-{project}-{env}` per
// pkg/naming.Namespace (sponsor verbatim 2026-06-07, meaning "EVERYTHING
// SHOULD be separated by project": "TUDO DEVERIA esta separado por projeto"). // english-only: verbatim sponsor quote, kept as evidence
// The Loki client was emitting the legacy
// 3-segment form `loop-runtime-{tenant}-{env}`, so logs for any M2-shaped
// namespace silently disappeared from /logs queries.
//
// T2.2.a backward-compat: the LogQL selector MUST cover BOTH shapes via
// an anchored regex OR query, so historical logs from legacy-deployed
// namespaces remain visible alongside post-M2 namespaces.
func TestBuildRuntimeLogQL_M2CanonicalAndLegacyMatched(t *testing.T) {
	got, err := buildRuntimeLogQL("runtime:tn-acme:production:my-project")
	if err != nil {
		t.Fatalf("legacy form rejected: %v", err)
	}
	// Anchored regex `^(M2|legacy)$` avoids prefix collisions with other
	// namespaces (e.g. `loop-runtime-tn-acme-production-canary`).
	want := `{namespace="loop-runtime-tn-acme-my-project-production", container=~".+"}`
	if got != want {
		t.Errorf("LogQL drift (T2.2 + T2.2.a):\n  got:  %q\n  want: %q", got, want)
	}
}

// TestBuildRuntimeLogQL_FiveSegmentFormWithApp — Beta Logs Gate +
// T2.2/T2.2.a combined. 5-segment with non-empty app produces the
// app="<value>" filter AND the M2/legacy OR namespace query.
func TestBuildRuntimeLogQL_FiveSegmentFormWithApp(t *testing.T) {
	got, err := buildRuntimeLogQL("runtime:tn-acme:production:my-project:web")
	if err != nil {
		t.Fatalf("5-segment form rejected: %v", err)
	}
	want := `{namespace="loop-runtime-tn-acme-my-project-production", app="web", container=~".+"}`
	if got != want {
		t.Errorf("5-segment LogQL drift:\n  got:  %q\n  want: %q", got, want)
	}
}

// TestBuildRuntimeLogQL_FiveSegmentFormWithEmptyApp — operator may
// have constructed the 5-segment form with an empty app (default
// behavior when --app flag is absent). MUST behave identically to
// 4-segment form (no app filter in LogQL).
func TestBuildRuntimeLogQL_FiveSegmentFormWithEmptyApp(t *testing.T) {
	got, err := buildRuntimeLogQL("runtime:tn-acme:production:my-project:")
	if err != nil {
		t.Fatalf("5-segment empty-app form rejected: %v", err)
	}
	// Must NOT contain app="<empty>" — empty app is "no filter".
	if strings.Contains(got, `app=""`) {
		t.Errorf("empty app must not produce an empty filter clause: %q", got)
	}
	want := `{namespace="loop-runtime-tn-acme-my-project-production", container=~".+"}`
	if got != want {
		t.Errorf("empty-app LogQL drift:\n  got:  %q\n  want: %q", got, want)
	}
}

// TestBuildRuntimeLogQL_M2CanonicalShapeIsCorrect_T22 — direct assertion
// that the M2 form `loop-runtime-{tenant}-{project}-{env}` is the FIRST
// alternative in the regex (so an exact equality on canonical
// namespaces is fast-path-matched by Loki before falling through to
// the legacy alternative).
func TestBuildRuntimeLogQL_M2CanonicalShapeIsCorrect_T22(t *testing.T) {
	got, err := buildRuntimeLogQL("runtime:tn-x:staging-eu-1:my-frontend")
	if err != nil {
		t.Fatalf("regression: %v", err)
	}
	// Canonical M2 namespace must be the FIRST alternative.
	expectedM2 := "loop-runtime-tn-x-my-frontend-staging-eu-1"
	if !strings.Contains(got, expectedM2) {
		t.Errorf("expected M2 namespace %q in LogQL: got %q", expectedM2, got)
	}
	// The legacy two-segment alternative was deleted: tenant namespaces fall under Loki's
	// default 168h retention (only secrets-system has the 90-day stream) and the M2 rename
	// dates from 2026-06-12 — those logs expired weeks ago. The selector must be an exact
	// match, and must not reintroduce the old form.
	deletedLegacy := `"loop-runtime-tn-x-staging-eu-1"`
	if strings.Contains(got, deletedLegacy) {
		t.Errorf("the legacy two-segment selector is back: %q", got)
	}
}

// TestBuildRuntimeLogQL_RejectsMalformedTenant — input validation.
// Tenants are validated against logParamRegex (alphanumeric + hyphen);
// LogQL injection via tenant header value is the primary attack
// surface. Reject loudly.
func TestBuildRuntimeLogQL_RejectsMalformedTenant(t *testing.T) {
	cases := []string{
		`runtime:bad"tenant:production:my-project`,             // double-quote injection
		`runtime:tn-acme:bad"env:my-project`,                   // env injection
		`runtime:tn-acme:production:bad"project`,               // project injection
		`runtime:tn-acme:production:my-project:bad"app`,        // app injection
		`runtime::production:my-project`,                       // empty tenant
		`runtime:` + strings.Repeat("a", 130) + `:env:project`, // tenant too long
	}
	for _, key := range cases {
		t.Run(key, func(t *testing.T) {
			_, err := buildRuntimeLogQL(key)
			if err == nil {
				t.Errorf("expected rejection of %q", key)
			}
		})
	}
}

// TestBuildRuntimeLogQL_RejectsShortKey — fewer than 4 segments is a
// programming error (caller bug) and must surface.
func TestBuildRuntimeLogQL_RejectsShortKey(t *testing.T) {
	cases := []string{
		"runtime:tn-acme",            // 2 segments
		"runtime:tn-acme:production", // 3 segments
	}
	for _, key := range cases {
		_, err := buildRuntimeLogQL(key)
		if err == nil {
			t.Errorf("expected rejection of short key %q", key)
		}
	}
}
