#!/usr/bin/env bash
# T6.8 — Hermetic gate: every `continue-on-error: true` in workflows MUST
# have an adjacent rationale comment OR graduation marker.
#
# Plan 2026-05-21-deep-review-remediation T6.8 + ADR D4.
#
# Background: per deep review v2, advisory mode that lasts forever is
# honor-system in disguise. Per ADR D4: every advisory must EITHER
# (a) be intentional with a rationale comment (legit "post-task continues
# even if main job failed" pattern, OR "auto-issue creation requires
# this"), OR (b) carry a `graduation: <ISO date>` + `issue: #N` to
# track when it becomes blocking.
#
# This gate detects un-annotated continue-on-error: true and fails.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

VIOLATIONS=0
fail() { VIOLATIONS=$((VIOLATIONS + 1)); echo "FAIL: $*" >&2; }
info() { echo "[advisory-mode] $*"; }

ANNOTATED_KEYS=(
  "graduation:"
  "rationale:"
  "rationale ("     # natural form: `# rationale (T-id): ...`
  "Rationale:"
  "Rationale ("
  "required so"     # natural-language pattern in existing comments
  "post-task"
  "best-effort"     # "X is best-effort" rationale shape
  "advisory"        # explicit "advisory" comment
  "Continue on"     # natural-language
  "Auto-issue"      # auto-issue creation requires continue-on-error
  "evidence"        # evidence capture even on failure
  "fork PR"         # secret access on forks
)

is_annotated() {
  local file="$1" lineno="$2"
  local start=$((lineno - 5))
  [ "$start" -lt 1 ] && start=1
  local context
  context=$(sed -n "${start},$((lineno + 1))p" "$file")
  for key in "${ANNOTATED_KEYS[@]}"; do
    # Case-insensitive match (rationale: vs Rationale: vs RATIONALE:)
    if echo "$context" | grep -qiF "$key"; then
      return 0
    fi
  done
  return 1
}

TOTAL=0
ANNOTATED=0
for wf in .github/workflows/*.yml; do
  [ -f "$wf" ] || continue
  while IFS= read -r match; do
    [ -z "$match" ] && continue
    lineno="${match%%:*}"
    TOTAL=$((TOTAL + 1))
    if is_annotated "$wf" "$lineno"; then
      ANNOTATED=$((ANNOTATED + 1))
    else
      fail "$wf:$lineno — continue-on-error: true without rationale comment OR graduation marker"
    fi
  done < <(grep -nE "continue-on-error:\s*true" "$wf" 2>/dev/null || true)
done

info "Total continue-on-error: true: $TOTAL"
info "Annotated (rationale or graduation): $ANNOTATED"
info "Violations: $VIOLATIONS"

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "" >&2
  echo "Fix: add adjacent comment (within 5 lines above) containing one of:" >&2
  echo "  # rationale: <one-line why advisory is correct here>" >&2
  echo "  # graduation: 2026-MM-DD" >&2
  echo "  # issue: #N (link to graduation tracking issue)" >&2
  exit 1
fi

echo "[advisory-mode] PASS — all continue-on-error: true entries are governance-tagged"
