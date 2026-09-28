// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
)

// TestExtractReaperReason_PopulatedFromSideEffects asserts that the reaper_reason
// field is extracted from release.side_effects when present.
// Plan T2.1 RED #1.
func TestExtractReaperReason_PopulatedFromSideEffects(t *testing.T) {
	sideEffects, _ := json.Marshal(map[string]any{
		"reaper_reason":    "Release was READY for more than 5 minutes. Likely cause: repository write is slow. Next: run `loopzero deploy` to retry.",
		"reaper_event":     "GIT_PUSH_TIMEOUT",
		"reaper_from":      "READY",
		"reaper_at":        "2026-05-15T13:55:18Z",
		"reaper_timeout_s": 300,
	})
	rel := &db.Release{ID: "rel-x", SideEffects: sideEffects}

	got := ExtractReaperReason(rel)
	if !strings.Contains(got, "Release was READY") {
		t.Errorf("expected reaper text, got %q", got)
	}
	if !strings.Contains(got, "Next:") {
		t.Errorf("expected WHAT/CAUSE/NEXT pattern, got %q", got)
	}
}

// TestExtractReaperReason_EmptyOnLegacyRelease asserts empty string when
// side_effects is absent (legacy releases pre-reaper instrumentation).
// Plan T2.1 RED #2.
func TestExtractReaperReason_EmptyOnLegacyRelease(t *testing.T) {
	rel := &db.Release{ID: "rel-old"}
	if got := ExtractReaperReason(rel); got != "" {
		t.Errorf("expected empty for missing side_effects, got %q", got)
	}
}

// TestExtractReaperReason_EmptyOnNil asserts safe behavior when release is nil.
func TestExtractReaperReason_EmptyOnNil(t *testing.T) {
	if got := ExtractReaperReason(nil); got != "" {
		t.Errorf("expected empty for nil release, got %q", got)
	}
}

// TestFormatBuildFailureReason_PrefersReaperReason asserts that when
// reaper_reason is present, it's used as the build error_message.
// Plan T2.1 RED #3.
func TestFormatBuildFailureReason_PrefersReaperReason(t *testing.T) {
	sideEffects, _ := json.Marshal(map[string]any{
		"reaper_reason": "Release was PENDING for more than 10 minutes. Likely cause: app /health not 200. Next: check `loopzero logs --runtime`.",
	})
	rel := &db.Release{ID: "rel-y", Status: "SYNC_FAILED", SideEffects: sideEffects}

	got := FormatBuildFailureReason(rel)
	if !strings.Contains(got, "Release was PENDING") {
		t.Errorf("expected reaper text, got %q", got)
	}
	// Ensures we did NOT use the legacy "Release X failed: Y" format.
	if strings.HasPrefix(got, "Release rel-y failed:") {
		t.Errorf("should not use legacy format when reaper_reason available, got %q", got)
	}
}

// TestFormatBuildFailureReason_FallbackOnLegacy asserts that without
// reaper_reason, the message still has structured content (not "unknown error").
// Plan T2.1 RED #4.
func TestFormatBuildFailureReason_FallbackOnLegacy(t *testing.T) {
	rel := &db.Release{ID: "rel-z", Status: "SYNC_FAILED"}
	got := FormatBuildFailureReason(rel)
	if got == "" {
		t.Errorf("expected non-empty fallback, got empty")
	}
	if strings.Contains(strings.ToLower(got), "unknown error") {
		t.Errorf("must NEVER emit 'unknown error' even in fallback, got %q", got)
	}
	if !strings.Contains(got, "rel-z") {
		t.Errorf("fallback should mention release ID, got %q", got)
	}
}

// TestFormatBuildFailureReason_NilReleaseGivesActionableMessage asserts nil
// release still produces an actionable message (not silent empty).
func TestFormatBuildFailureReason_NilReleaseGivesActionableMessage(t *testing.T) {
	got := FormatBuildFailureReason(nil)
	if got == "" {
		t.Errorf("expected non-empty for nil release, got empty")
	}
	if strings.Contains(strings.ToLower(got), "unknown error") {
		t.Errorf("must NEVER emit 'unknown error', got %q", got)
	}
}

// TestSanitizeReaperText_StripsCRLFAndTabs (EC-7) asserts terminal-mangling
// characters are replaced with single spaces.
func TestSanitizeReaperText_StripsCRLFAndTabs(t *testing.T) {
	in := "Line one\r\nLine two\twith tab\nLine three"
	want := "Line one Line two with tab Line three"
	if got := sanitizeReaperText(in); got != want {
		t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
	}
}

// TestSanitizeReaperText_CollapsesWhitespace asserts double spaces collapse.
func TestSanitizeReaperText_CollapsesWhitespace(t *testing.T) {
	in := "  too    many   spaces  "
	want := "too many spaces"
	if got := sanitizeReaperText(in); got != want {
		t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
	}
}
