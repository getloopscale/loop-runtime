// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/api/internal/events"
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
// Build / Sign / Deliver / Other. Keeping the mapping in api/internal/events as
// the single source of truth avoids drift between pipeline and UI.

// pipefailRE matches every `set -<flags>o pipefail` form, not only shellgen's literal.
//
// B-153 (review): the sanitiser replaced the exact string "set -euo pipefail". But
// scripts.go writes "set -uo pipefail" by hand, without the e, and that form escaped. The
// Node image is Debian, whose /bin/sh is dash, and set is a special builtin: the illegal
// option KILLS the shell on the first line with status 2. While the status died in the tee
// pipe this was invisible; once the status propagated, every Node build would have failed
// at security-scan — trading "no step can fail" for "no Node build can pass".
var pipefailRE = regexp.MustCompile(`set -([a-zA-Z]*)o pipefail`)

// expandPipefail rewrites one match into the POSIX-safe equivalent, preserving the other
// flags. The pipefail attempt stays guarded because dash simply does not have it.
func expandPipefail(match string) string {
	m := pipefailRE.FindStringSubmatch(match)
	if m == nil {
		return match
	}
	var b strings.Builder
	for _, f := range m[1] {
		fmt.Fprintf(&b, "set -%c\n", f)
	}
	b.WriteString("(set -o pipefail) 2>/dev/null || true")
	return b.String()
}

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
// The step's status does NOT depend on pipefail. It is captured inside the block, before
// the pipe, and the wrapper exits with that value.
//
// This paragraph said the opposite until 2026-08-18: that pipefail was what carried the
// status through the pipe to tee, and that non-propagation under plain dash was "rare in
// our stack". Both statements were false, in the most expensive way — non-propagation hit
// 100% of steps, because the pipefail attempt sat in parentheses and died with the
// subshell. Measured live in B-153: five fail-closed steps reported Succeeded.
//
// The guarded attempt remains, for what it is actually worth: where the shell supports
// pipefail, a command failing mid-pipe INSIDE the user's script is detected.
func wrapWithLoopZeroMarkers(stepName, script string) string {
	phase := events.PhaseGroupForStep(stepName)
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
	script = pipefailRE.ReplaceAllStringFunc(script, expandPipefail)

	return fmt.Sprintf(`echo '::loopzero:: phase=%[1]s step=%[2]s level=info msg="%[3]s starting"'
__loopzero_start=$(date +%%s)
__loopzero_log=$(mktemp 2>/dev/null || echo /tmp/loopzero-step.$$.log)
__loopzero_rc_file=$(mktemp 2>/dev/null || echo /tmp/loopzero-step.$$.rc)
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
# The step's status is captured INSIDE the block, before the pipe.
#
# B-153: the script used to run as a pipeline into tee, with a set -o pipefail inside
# parentheses. Two things were wrong at once: a POSIX pipeline's status is the LAST
# command's (tee, which always succeeds), and a pipefail set in a subshell is discarded
# with it. Measured live: EVERY step built by buildContainer exited 0 whatever it did —
# the five fail-closed steps of an unsupported runtime reported Succeeded, and the build
# went on without a Dockerfile until it died with a message naming none of this.
#
# The form below is POSIX (ash/dash/bash): the script runs in a subshell, its status is
# recorded by an echo in the outer block — not aborted, because the script's set -e is
# confined to the subshell — and the shell exits with that status.
{
  (
%[4]s
  )
  echo $? > "$__loopzero_rc_file"
} 2>&1 | tee -a "$__loopzero_log"
__loopzero_status=$(cat "$__loopzero_rc_file" 2>/dev/null || true)
# An EMPTY file makes cat exit 0, so the error fallback does not cover that case — and the
# shell rejects an exit with no number with 2. That still fails closed, but with a value
# this code does not declare, sending whoever debugs it after a step that does not exist.
case "$__loopzero_status" in ''|*[!0-9]*) __loopzero_status=1 ;; esac
rm -f "$__loopzero_rc_file" 2>/dev/null || true
exit "$__loopzero_status"
`, phase, stepName, display, script)
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
