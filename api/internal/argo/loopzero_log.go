// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"fmt"
	"strings"

	"github.com/getloopscale/loopzero/pkg/events"
)

// Plan J-17 Layer 2 (2026-06-06) — pipeline emits structured logs
// intended for user view.
//
// Why: the dashboard's deploy detail "Logs" tab used to show every
// stdout/stderr line from every container, including Argo executor
// banners (level=INFO msg="Starting Workflow Executor" goVersion=...
// argo=true version=v4.0.3) and other infrastructure noise. That's
// (a) UX-noise users don't understand and (b) version disclosure that
// helps attackers target CVEs against our exact Argo/Go versions.
//
// How: each pipeline step wraps its main script with `::loopzero::`
// prefixed markers that Promtail extracts into a `loopzero_phase` label
// (see promtail relabel config). The API's default LogQL filter
// `{loopzero_phase=~".+"}` then shows only labeled lines; debug mode
// (`?mode=debug`, admin-only) shows raw output.
//
// Marker format (mirrors Mocha/GitHub Actions style for grep-friendly
// patterns, no JSON parsing needed in bash):
//
//	::loopzero:: phase=<group> step=<name> level=<info|warn|error> msg="..."
//
// Phase is derived via events.PhaseGroupForStep — same mapping the
// frontend BuildTimelineLive uses to group steps into Clone / Install /
// Build / Sign / Deliver / Other. Keeping the mapping in pkg/events as
// the single source of truth avoids drift between pipeline and UI.

// wrapWithLoopZeroMarkers wraps a bash script with start/success/error
// markers that Promtail extracts into loopzero_phase + loopzero_step + loopzero_level
// labels.
//
// The wrapper preserves the original script's exit code semantics AND
// captures its combined stdout/stderr to a temp log so that:
//   - Start marker prints unconditionally.
//   - Original script runs through a `tee` pipe — every line goes both
//     to the container stdout (where Promtail still picks it up for
//     debug mode) AND to a temp file so the trap can replay context.
//   - On success (exit 0): a "completed in Ns" marker prints (with the
//     measured duration so the LogsTab can show step timings).
//   - On failure (exit != 0): a "failed (exit N, ran Ns)" header marker
//     prints, then the LAST 50 LINES of the script's combined output
//     are replayed as `level=error` markers. Each replayed line is
//     truncated to 400 chars and has `"` + `\` escaped so it survives
//     the marker regex parser (Layer 4). The user finally sees WHY the
//     build failed in the LogsTab without needing operator debug mode.
//
// `set -o pipefail` is required so that a failing script inside the
// `... | tee` pipe propagates its exit code to the trap. Most build
// containers run on Alpine ash which supports pipefail; the wrapper
// degrades gracefully (`|| true`) if not supported, accepting that on
// pure POSIX dash the exit code may not propagate (rare in our stack).
func wrapWithLoopZeroMarkers(stepName, script string) string {
	phase := phaseForStepFuzzy(stepName)
	display := humanizeStepName(stepName)

	// Canonical 2026-06-16 (dash-compat shim): shellgen.SetE emits
	// `set -euo pipefail` which is NOT POSIX — Debian's /bin/sh (dash)
	// errors with exit 2 ("illegal option") on `set -o pipefail`. The
	// canonical solution would be to make shellgen emit POSIX-compatible
	// strict-mode (`set -e` + `set -u` + graceful pipefail). Since the
	// shellgen package is outside the current scope, transform the inner
	// script here to swap the offending `set -euo pipefail` line for the
	// dash-compatible 3-line equivalent. Alpine ash + bash continue to
	// work because they accept all three forms.
	script = strings.Replace(script,
		"set -euo pipefail",
		"set -e\nset -u\n(set -o pipefail) 2>/dev/null || true",
		1)

	return fmt.Sprintf(`echo '::loopzero:: phase=%[1]s step=%[2]s level=info msg="%[3]s starting"'
__loopzero_start=$(date +%%s)
__loopzero_log=$(mktemp 2>/dev/null || echo /tmp/loopzero-step.$$.log)
: > "$__loopzero_log"
__loopzero_on_exit() {
  __loopzero_rc=$?
  __loopzero_dur=$(( $(date +%%s) - __loopzero_start ))
  if [ "$__loopzero_rc" -eq 0 ]; then
    printf '::loopzero:: phase=%[1]s step=%[2]s level=info msg="%[3]s completed in %%ds"\n' "$__loopzero_dur"
  else
    printf '::loopzero:: phase=%[1]s step=%[2]s level=error msg="%[3]s failed (exit %%d after %%ds) — last output:"\n' "$__loopzero_rc" "$__loopzero_dur"
    tail -50 "$__loopzero_log" 2>/dev/null | while IFS= read -r __loopzero_line; do
      [ -z "$__loopzero_line" ] && continue
      __loopzero_esc=$(printf '%%s' "$__loopzero_line" | cut -c1-400 | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g')
      printf '::loopzero:: phase=%[1]s step=%[2]s level=error msg="%%s"\n' "$__loopzero_esc"
    done
  fi
  rm -f "$__loopzero_log" 2>/dev/null || true
}
trap __loopzero_on_exit EXIT
(set -o pipefail) 2>/dev/null || true
{
%[4]s
} 2>&1 | tee -a "$__loopzero_log"
`, phase, stepName, display, script)
}

// phaseForStepFuzzy resolves a phase group from a step name, accepting
// either the canonical full name (matches pkg/events.PhaseGroupForStep)
// or a buildContainer short name (e.g. "security-scan" vs canonical
// "security-scan-node"). When the canonical lookup misses, fall back
// to prefix matching against the canonical sets so phase labels stay
// correct in Loki even when the strategy uses a language-agnostic
// container name.
func phaseForStepFuzzy(name string) string {
	if p := events.PhaseGroupForStep(name); p != "other" {
		return p
	}
	switch {
	case strings.HasPrefix(name, "validate-workspace"), strings.HasPrefix(name, "clone-"):
		return "clone"
	case strings.HasPrefix(name, "restore-cache"),
		strings.HasPrefix(name, "install-deps"),
		strings.HasPrefix(name, "security-scan"):
		return "install"
	case strings.HasPrefix(name, "generate-dockerfile"),
		strings.HasPrefix(name, "dockerfile-lint"),
		strings.HasPrefix(name, "build-apps"),
		strings.HasPrefix(name, "build-image"),
		strings.HasPrefix(name, "save-cache"):
		return "build"
	case strings.HasPrefix(name, "sign-image"), strings.HasPrefix(name, "sbom"):
		return "sign"
	case strings.HasPrefix(name, "deliver-"):
		return "deliver"
	}
	return "other"
}

// humanizeStepName converts an internal step name into a sentence-style
// display name. "install-deps-node" → "Install deps", "build-apps-go"
// → "Build apps", "sign-image-api" → "Sign image". The language /
// app suffix is dropped because the dashboard already groups by step
// and the language is implicit in the app context.
func humanizeStepName(name string) string {
	// Strip known language/runtime suffix tokens that add noise. The
	// "-user-dockerfile" suffix is meaningful (user-supplied Dockerfile
	// path) and intentionally NOT stripped.
	for _, suf := range []string{"-node", "-go", "-python", "-ruby", "-java", "-php", "-rust", "-deno", "-bun"} {
		name = strings.TrimSuffix(name, suf)
	}
	parts := strings.Split(name, "-")
	if len(parts) == 0 || parts[0] == "" {
		return name
	}
	parts[0] = strings.ToUpper(parts[0][:1]) + parts[0][1:]
	return strings.Join(parts, " ")
}
