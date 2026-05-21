#!/usr/bin/env bash
# T6.2 — Hermetic gate: scripts that perform destructive state writes
# (kubectl apply/create/delete, helm install/uninstall, pulumi up/destroy,
# doctl <verb>, git push) MUST support --dry-run flag (DRY_RUN env var
# OR --dry-run / -n argument).
#
# Plan 2026-05-21-deep-review-remediation T6.2 + memory
# feedback_dry_run_flag_mandatory_for_state_writers.
#
# Background: T2.2 shipped infra/scripts/lib/dry-run.sh providing the
# canonical `apply` wrapper. This gate enforces adoption: a script that
# wraps destructive calls MUST also support the flag (either via the
# shared lib OR via its own DRY_RUN/--dry-run handling).
#
# Detection heuristic:
#   - State-writer = script that contains `kubectl apply|create|delete`,
#     `helm install|uninstall|upgrade`, `pulumi up|destroy`,
#     `doctl <delete|create>`, OR `git push`.
#   - Dry-run support = `DRY_RUN` referenced OR `--dry-run` referenced OR
#     `parse_dry_run_flag` referenced OR `lib/dry-run.sh` sourced.
#
# Exempt:
#   - **/lib/*.sh — sourced libs delegate dry-run to callers
#   - **/test*.sh, **/test_helpers/ — test scaffolding
#   - infra/tests/** — hermetic test files
#   - scripts that emit a clear DRY-RUN handling via kubectl --dry-run=client
#
# Baseline-aware: 16 known state-writers grandfathered (per plan T2.2
# follow-up) in infra/quality/dry-run-baseline.txt.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

BASELINE_FILE="$REPO_ROOT/infra/quality/dry-run-baseline.txt"
IGNORE_BASELINE="${IGNORE_BASELINE:-0}"

CURRENT_VIOLATIONS=$(mktemp)
trap 'rm -f "$CURRENT_VIOLATIONS"' EXIT

is_exempt_path() {
  local p="$1"
  case "$p" in
    */lib/*.sh) return 0 ;;
    *test_helpers/*) return 0 ;;
    infra/tests/*) return 0 ;;
    *_test.sh) return 0 ;;
    *.test.sh) return 0 ;;
  esac
  return 1
}

STATE_WRITER_PATTERN='(kubectl[[:space:]]+(apply|create|delete|patch)|helm[[:space:]]+(install|uninstall|upgrade)|pulumi[[:space:]]+(up|destroy)|doctl[[:space:]]+(databases|kubernetes|registry)[[:space:]]+(delete|create)|git[[:space:]]+push)'
DRY_RUN_SUPPORT_PATTERN='(DRY_RUN|--dry-run|parse_dry_run_flag|lib/dry-run\.sh|kubectl[[:space:]]+--dry-run=client)'

while IFS= read -r path; do
  [ -f "$path" ] || continue
  if is_exempt_path "$path"; then continue; fi

  # Has destructive pattern?
  if ! grep -qE "$STATE_WRITER_PATTERN" "$path" 2>/dev/null; then
    continue  # not a state-writer
  fi

  # Has dry-run support?
  if grep -qE "$DRY_RUN_SUPPORT_PATTERN" "$path" 2>/dev/null; then
    continue  # supported
  fi

  rel="${path#"$REPO_ROOT"/}"
  rel="${rel#./}"
  echo "$rel" >> "$CURRENT_VIOLATIONS"
done < <(
  find infra/scripts infra/helmfile/scripts -type f -name '*.sh' 2>/dev/null
)

VIOLATIONS=0
fail() { VIOLATIONS=$((VIOLATIONS + 1)); echo "FAIL: $*" >&2; }

# Baseline filter
if [ "$IGNORE_BASELINE" != "1" ] && [ -f "$BASELINE_FILE" ]; then
  BASELINE_KEYS=$(grep -vE '^[[:space:]]*(#|$)' "$BASELINE_FILE" || true)
  while IFS= read -r viol; do
    [ -z "$viol" ] && continue
    if echo "$BASELINE_KEYS" | grep -Fxq "$viol"; then
      continue
    fi
    fail "$viol writes state but does not support --dry-run (NEW — not in baseline)"
  done < "$CURRENT_VIOLATIONS"
else
  while IFS= read -r viol; do
    [ -z "$viol" ] && continue
    fail "$viol writes state but does not support --dry-run"
  done < "$CURRENT_VIOLATIONS"
fi

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "" >&2
  echo "[dry-run-support] FAIL: $VIOLATIONS state-writing script(s) without --dry-run support" >&2
  echo "" >&2
  echo "Fix one of:" >&2
  echo "  1. Source infra/scripts/lib/dry-run.sh + use apply <cmd> wrapper" >&2
  echo "  2. Add DRY_RUN env var handling + gate destructive calls" >&2
  echo "  3. Add path to ALLOWLIST in this test (with one-line justification)" >&2
  exit 1
fi

GRANDFATHERED=0
if [ -f "$BASELINE_FILE" ]; then
  GRANDFATHERED=$(grep -vcE '^[[:space:]]*(#|$)' "$BASELINE_FILE" || echo 0)
fi
echo "[dry-run-support] PASS — all state-writing scripts support --dry-run ($GRANDFATHERED grandfathered in baseline)"
