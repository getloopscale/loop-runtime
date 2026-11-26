// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// ExtractReaperReason pulls the rich reaper_reason text from a release's
// side_effects JSON column. Returns empty string when no reaper reason is
// available (legacy releases, non-reaper failures).
//
// Plan T2.1 ADR D4 — build.error_message must propagate this rich text
// (WHAT timed out + LIKELY CAUSE + NEXT STEP) instead of the legacy
// "Release X failed: Y" technical string.
//
// EC-7 sanitization: the reaper text is human-authored in release_reaper.go.
// Defensive sanitization replaces CRLF/tabs with single spaces and trims
// excessive whitespace, preventing terminal mangling if reaper text changes.
func ExtractReaperReason(release *db.Release) string {
	if release == nil || len(release.SideEffects) == 0 {
		return ""
	}
	var sideEffects map[string]any
	if err := json.Unmarshal(release.SideEffects, &sideEffects); err != nil {
		return ""
	}
	raw, ok := sideEffects["reaper_reason"]
	if !ok {
		return ""
	}
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	return sanitizeReaperText(s)
}

// FormatBuildFailureReason builds the build.error_message text. Prefers the
// rich reaper_reason from release.side_effects when present; falls back to
// the legacy technical message otherwise.
//
// Plan T2.1 — closes F-S1-01 "Build failed: unknown error" symptom by giving
// the CLI a non-empty actionable message in all failure paths.
func FormatBuildFailureReason(release *db.Release) string {
	if release == nil {
		return "Deploy failed — no release context available. Run `loopruntime logs --build <id>` for details."
	}
	if reason := ExtractReaperReason(release); reason != "" {
		return reason
	}
	// Fallback when no reaper_reason (e.g., legacy releases pre-reaper or
	// non-timeout failures). Stays informative — uses release ID + status.
	return fmt.Sprintf("Release %s failed: %s. Run `loopruntime logs --build <id>` for application context.",
		release.ID, release.Status)
}

// sanitizeReaperText replaces CRLF/tabs with single spaces and collapses
// consecutive whitespace. EC-7 defensive: prevents terminal mangling if a
// future reaper change introduces embedded control characters.
func sanitizeReaperText(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\t", " ").Replace(s)
	// Collapse multiple spaces
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}
