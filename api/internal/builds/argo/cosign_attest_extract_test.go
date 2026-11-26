// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argo

import (
	"bytes"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// T2.3 Test 3 (cosign extract) of plan
// 2026-05-28-meet-001-engineering-closure-plan.md.
//
// The Rekor URL extraction in cosign_attest.go is shell sed that runs
// inside the attest container. The LIVE 2026-05-28 3-layer bug taught
// us the extraction is the source of "unknown" sentinels and silent
// drops. These tests exercise the exact sed pipeline against fixture
// stdouts to assert:
//
//   - "tlog entry created with index: N" → reconstructed URL via $REKOR_URL
//   - literal "https://rekor..." in stdout → returned verbatim (preferred)
//   - both present → literal URL wins (cosign explicit > index reconstruct)
//   - neither → empty (NEVER "unknown" — that sentinel was removed in
//     M07 Ralph iter 5; downstream code gates on empty, not the sentinel)
//
// These shell tests rely on /bin/sh + sed (POSIX); the test Skips on
// Windows where /bin/sh isn't available.

// extractRekorURL runs the canonical shell extraction pipeline from
// cosign_attest.go against the supplied attest stdout, with the
// supplied REKOR_URL env. Returns the resulting REKOR_ENTRY_URL.
func extractRekorURL(t *testing.T, attestOutput, rekorURL string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sed pipeline requires POSIX shell")
	}
	// Mirror the exact extraction logic from cosign_attest.go
	// (lines ~181-187). Keep this in sync with the script template.
	script := `
set -eu
ATTEST_OUTPUT="$1"
REKOR_URL="$2"
REKOR_ENTRY_URL=$(echo "$ATTEST_OUTPUT" | sed -n 's/.*\(https*:\/\/rekor[^ ]*\).*/\1/p' | head -1)
if [ -z "$REKOR_ENTRY_URL" ]; then
  REKOR_INDEX=$(echo "$ATTEST_OUTPUT" | sed -n 's/.*tlog entry created with index:[[:space:]]*\([0-9][0-9]*\).*/\1/p' | head -1)
  if [ -n "$REKOR_INDEX" ] && [ -n "${REKOR_URL:-}" ]; then
    REKOR_ENTRY_URL="${REKOR_URL%/}/api/v1/log/entries?logIndex=${REKOR_INDEX}"
  fi
fi
printf '%s' "$REKOR_ENTRY_URL"
`
	cmd := exec.Command("/bin/sh", "-c", script, "_", attestOutput, rekorURL)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("shell pipeline failed: %v\nstderr: %s", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

func TestRekorExtract_LiteralURL_Returned(t *testing.T) {
	out := extractRekorURL(t,
		"tlog entry created\n"+
			"Logged to https://rekor.loopruntime.test/api/v1/log/entries?uuid=abc123\n"+
			"All set.",
		"https://rekor.fallback.test")
	want := "https://rekor.loopruntime.test/api/v1/log/entries?uuid=abc123"
	if out != want {
		t.Errorf("literal URL extraction: got %q want %q", out, want)
	}
}

func TestRekorExtract_TlogIndex_ReconstructsURL(t *testing.T) {
	out := extractRekorURL(t,
		"DEBUG: starting attest\n"+
			"tlog entry created with index: 42\n"+
			"done.",
		"https://rekor.loopruntime.test")
	want := "https://rekor.loopruntime.test/api/v1/log/entries?logIndex=42"
	if out != want {
		t.Errorf("tlog reconstruct: got %q want %q", out, want)
	}
}

func TestRekorExtract_TlogIndex_TrailingSlashStripped(t *testing.T) {
	// REKOR_URL with trailing slash — shell `${REKOR_URL%/}` strips it.
	out := extractRekorURL(t,
		"tlog entry created with index: 7\n",
		"https://rekor.loopruntime.test/")
	want := "https://rekor.loopruntime.test/api/v1/log/entries?logIndex=7"
	if out != want {
		t.Errorf("trailing slash strip: got %q want %q", out, want)
	}
}

func TestRekorExtract_BothLiteralAndIndex_LiteralWins(t *testing.T) {
	// Cosign sometimes emits both forms in the same output. The literal
	// URL has the explicit log-entry identifier; the index would resolve
	// to a less-specific lookup. The script prefers the literal.
	out := extractRekorURL(t,
		"tlog entry created with index: 99\n"+
			"Verified entry at https://rekor.loopruntime.test/api/v1/log/entries?uuid=xyz\n",
		"https://rekor.loopruntime.test")
	want := "https://rekor.loopruntime.test/api/v1/log/entries?uuid=xyz"
	if out != want {
		t.Errorf("literal-over-index: got %q want %q", out, want)
	}
}

func TestRekorExtract_NoMatchableOutput_EmptyResult(t *testing.T) {
	// LIVE 2026-05-28 bug class: cosign output without recognizable
	// Rekor footprint must yield empty — NOT "unknown" sentinel.
	// downstream `enrichAppReleasesWithAttestation` filters empty +
	// "unknown" alike, but the shell extractor must not fabricate.
	out := extractRekorURL(t,
		"DEBUG: unrelated cosign trace\n"+
			"Image signed successfully\n",
		"https://rekor.loopruntime.test")
	if out != "" {
		t.Errorf("expected empty when no Rekor footprint, got %q", out)
	}
}

func TestRekorExtract_TlogIndex_RekorURLEmpty_NoFakeURL(t *testing.T) {
	// Defensive: index present, but REKOR_URL env is empty. The script
	// must NOT emit a malformed URL like "/api/v1/log/entries?logIndex=N".
	out := extractRekorURL(t,
		"tlog entry created with index: 5\n",
		"")
	if out != "" {
		t.Errorf("expected empty when REKOR_URL empty, got %q", out)
	}
}

func TestRekorExtract_MultiTlogEntries_HeadOneTaken(t *testing.T) {
	// `head -1` defends against multi-app workflows whose attest steps
	// share stdout. We pin the first-match rule so test failures are
	// loud if someone reverts to last-match (would conflate app indices).
	out := extractRekorURL(t,
		"tlog entry created with index: 1\n"+
			"tlog entry created with index: 2\n"+
			"tlog entry created with index: 3\n",
		"https://rekor.loopruntime.test")
	want := "https://rekor.loopruntime.test/api/v1/log/entries?logIndex=1"
	if out != want {
		t.Errorf("head -1 first-match: got %q want %q", out, want)
	}
}
