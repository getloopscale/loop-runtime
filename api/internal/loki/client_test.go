// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package loki

import (
	"strings"
	"testing"
)

// TestBuildRuntimeLogQL_LegacyFourSegmentForm — backward compat.
// Callers still using the 4-segment form (runtime:tenant:env:project)
// MUST produce the same LogQL as before (no app filter).
func TestBuildRuntimeLogQL_LegacyFourSegmentForm(t *testing.T) {
	got, err := buildRuntimeLogQL("runtime:tn-acme:production:my-project")
	if err != nil {
		t.Fatalf("legacy form rejected: %v", err)
	}
	want := `{namespace="loopzero-tn-acme-production", container=~".+"}`
	if got != want {
		t.Errorf("legacy LogQL drift:\n  got:  %q\n  want: %q", got, want)
	}
}

// TestBuildRuntimeLogQL_FiveSegmentFormWithApp — new Beta Logs Gate
// shape. 5-segment with non-empty app produces a LogQL selector
// containing `app="<value>"`. Drift here breaks per-app filtering.
func TestBuildRuntimeLogQL_FiveSegmentFormWithApp(t *testing.T) {
	got, err := buildRuntimeLogQL("runtime:tn-acme:production:my-project:web")
	if err != nil {
		t.Fatalf("5-segment form rejected: %v", err)
	}
	want := `{namespace="loopzero-tn-acme-production", app="web", container=~".+"}`
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
	want := `{namespace="loopzero-tn-acme-production", container=~".+"}`
	if got != want {
		t.Errorf("empty-app LogQL drift:\n  got:  %q\n  want: %q", got, want)
	}
}

// TestBuildRuntimeLogQL_RejectsMalformedTenant — input validation.
// Tenants are validated against logParamRegex (alphanumeric + hyphen);
// LogQL injection via tenant header value is the primary attack
// surface. Reject loudly.
func TestBuildRuntimeLogQL_RejectsMalformedTenant(t *testing.T) {
	cases := []string{
		`runtime:bad"tenant:production:my-project`,            // double-quote injection
		`runtime:tn-acme:bad"env:my-project`,                  // env injection
		`runtime:tn-acme:production:bad"project`,              // project injection
		`runtime:tn-acme:production:my-project:bad"app`,       // app injection
		`runtime::production:my-project`,                      // empty tenant
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
		"runtime:tn-acme",                  // 2 segments
		"runtime:tn-acme:production",       // 3 segments
	}
	for _, key := range cases {
		_, err := buildRuntimeLogQL(key)
		if err == nil {
			t.Errorf("expected rejection of short key %q", key)
		}
	}
}
