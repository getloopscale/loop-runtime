// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scrub

import (
	"os"
	"strings"
	"testing"
)

// TestScrubEvent_PreservesCleanInput — pure inputs pass through unchanged.
func TestScrubEvent_PreservesCleanInput(t *testing.T) {
	in := PhaseEvent{Phase: "build", Status: "in_progress"}
	out := scrubEvent(in)
	if out != in {
		t.Fatalf("clean input should round-trip; got %+v want %+v", out, in)
	}
}

// TestScrubEventWithCount_ReturnsAccurateReplacementCount locks the
// counter contract per T5.2 wiring + H2 review fix.
func TestScrubEventWithCount_ReturnsAccurateReplacementCount(t *testing.T) {
	tests := []struct {
		name    string
		in      PhaseEvent
		wantMin int
	}{
		{"no replacements on clean input", PhaseEvent{Phase: "build", Status: "in_progress"}, 0},
		{"one replacement on ArgoCD term", PhaseEvent{DegradedReason: "ArgoCD sync failed"}, 1},
		{"two replacements across fields", PhaseEvent{
			DegradedReason:  "cosign verify failed in cell-0",
			SubstateMessage: "Kaniko build failed",
		}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, n := ScrubEventWithCount(tt.in)
			if n < tt.wantMin {
				t.Errorf("ScrubEventWithCount count = %d; want ≥ %d", n, tt.wantMin)
			}
		})
	}
}

// TestScrubEvent_RemovesArgoCDTerm — exact-match canonical case.
func TestScrubEvent_RemovesArgoCDTerm(t *testing.T) {
	in := PhaseEvent{Phase: "deploy", Status: "in_progress", DegradedReason: "ArgoCD application sync timeout"}
	out := scrubEvent(in)
	if strings.Contains(out.DegradedReason, "ArgoCD") {
		t.Fatalf("scrubbed text still contains 'ArgoCD': %q", out.DegradedReason)
	}
	if !strings.Contains(out.DegradedReason, "deployment system") {
		t.Fatalf("expected product-language replacement 'deployment system' in %q", out.DegradedReason)
	}
}

// TestScrubEvent_RemovesCosignTerm — Sign-phase canonical leak.
func TestScrubEvent_RemovesCosignTerm(t *testing.T) {
	in := PhaseEvent{Phase: "sign", Status: "completed", DegradedReason: "cosign verify failed: no signature"}
	out := scrubEvent(in)
	if strings.Contains(out.DegradedReason, "cosign") {
		t.Fatalf("scrubbed text still contains 'cosign': %q", out.DegradedReason)
	}
}

// TestScrubEvent_HandlesAllDenyListEntries — covers the canonical denylist
// from blueprint § Q5 (12 mappings).
func TestScrubEvent_HandlesAllDenyListEntries(t *testing.T) {
	denylist := []string{
		"ArgoCD", "cosign", "BuildKit", "Kaniko", "Argo Rollouts",
		"cell-0", "cell-0", "Gitea", "Webpack", "Turbopack",
		"esbuild", "Lambda",
	}
	for _, term := range denylist {
		in := PhaseEvent{Phase: "deploy", DegradedReason: "test: " + term + " failed"}
		out := scrubEvent(in)
		if strings.Contains(out.DegradedReason, term) {
			t.Errorf("denylist term %q leaked: got %q", term, out.DegradedReason)
		}
	}
}

// EC-1 absorption: cell topology variants (`cell-1`, `cell-runtime-N`).
func TestScrubEvent_HandlesAllCellTopologyVariants(t *testing.T) {
	variants := []string{
		"cell-0", "cell-0", "cell-1", "cell-3",
		"cell-runtime-2", "cell-runtime-default", "cell-mgmt",
	}
	for _, v := range variants {
		in := PhaseEvent{Phase: "deploy", DegradedReason: "sync to " + v + " failed"}
		out := scrubEvent(in)
		if strings.Contains(out.DegradedReason, v) {
			t.Errorf("cell variant %q leaked: got %q", v, out.DegradedReason)
		}
		if !strings.Contains(out.DegradedReason, "your team's runtime") {
			t.Errorf("expected product mapping 'your team's runtime' for %q; got %q", v, out.DegradedReason)
		}
	}
}

// EC-1 absorption: case-insensitive ArgoCD variants.
func TestScrubEvent_CaseInsensitiveArgoCD(t *testing.T) {
	variants := []string{"ArgoCD", "argocd", "argo-cd", "Argo CD"}
	for _, v := range variants {
		in := PhaseEvent{Phase: "deploy", DegradedReason: v + " refused: 401"}
		out := scrubEvent(in)
		if strings.Contains(out.DegradedReason, v) {
			t.Errorf("argocd variant %q leaked: got %q", v, out.DegradedReason)
		}
	}
}

// EC-1 absorption: case-insensitive Argo Rollouts variants.
func TestScrubEvent_CaseInsensitiveArgoRollouts(t *testing.T) {
	variants := []string{"Argo Rollouts", "argo-rollouts", "argorollouts"}
	for _, v := range variants {
		in := PhaseEvent{Phase: "deploy", DegradedReason: v + " aborted"}
		out := scrubEvent(in)
		if strings.Contains(out.DegradedReason, v) {
			t.Errorf("argo-rollouts variant %q leaked: got %q", v, out.DegradedReason)
		}
	}
}

// EC-7 SHOULD TEST: scrub.go source file MUST contain denylist terms only
// inside the canonicalScrubMap declaration block — not as stringliterals in
// other code paths.
func TestScrub_SourceFileContainsNoLeakedTerms(t *testing.T) {
	src, err := os.ReadFile("scrub.go")
	if err != nil {
		t.Fatalf("read scrub.go: %v", err)
	}
	text := string(src)
	// Strip everything inside the canonicalScrubMap declaration.
	mapStart := strings.Index(text, "canonicalScrubRules = []scrubRule{")
	if mapStart < 0 {
		t.Fatalf("canonicalScrubRules declaration not found in scrub.go")
	}
	// Find matching closing brace at end of slice.
	mapEnd := strings.Index(text[mapStart:], "\n}\n")
	if mapEnd < 0 {
		t.Fatalf("canonicalScrubRules closing brace not found")
	}
	stripped := text[:mapStart] + text[mapStart+mapEnd:]
	// Now stripped should NOT contain any denylist term as a literal.
	forbidden := []string{"ArgoCD", "argo-cd", "cosign", "BuildKit", "Kaniko",
		"Argo Rollouts", "argo-rollouts", "cell-0", "Gitea",
		"Webpack", "Turbopack", "esbuild"}
	for _, term := range forbidden {
		if strings.Contains(stripped, term) {
			t.Errorf("denylist term %q found OUTSIDE canonicalScrubRules block — possible leak", term)
		}
	}
}

// scrubEvent is ScrubEventWithCount without the count, for assertions that
// only read the rewritten event.
func scrubEvent(in PhaseEvent) PhaseEvent {
	out, _ := ScrubEventWithCount(in)
	return out
}
