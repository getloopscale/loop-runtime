#!/usr/bin/env bash
# T6.1 — Hermetic gate: every non-lib .sh under infra/scripts +
# infra/helmfile/scripts + infra/tests MUST have `set -euo pipefail`
# in the top 30 lines.
#
# Plan 2026-05-21-deep-review-remediation T6.1.
#
# Background: T2.1 swept 156 scripts to add set -euo pipefail (commit
# eabf5060). This gate prevents regression — any NEW .sh added in a
# future PR without the directive FAILS CI.
#
# Exempt:
#   - **/lib/*.sh — sourced libraries (set -e propagates to caller, bad)
#
# Allowed variants:
#   - set -euo pipefail (canonical)
#   - set -uo pipefail  (allowed when script handles errors explicitly)
#   - set -eo pipefail  (allowed for legacy reasons)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

VIOLATIONS=0
TOTAL=0
fail() { VIOLATIONS=$((VIOLATIONS + 1)); echo "FAIL: $*" >&2; }

while IFS= read -r path; do
  [ -f "$path" ] || continue
  # Exempt sourced libs
  case "$path" in
    */lib/*.sh) continue ;;
  esac
  TOTAL=$((TOTAL + 1))
  # Check first 60 lines for any set -[eu]+o? pipefail variant
  # (60 line window covers long docstring preambles that document
  # contract/usage before any code runs).
  if ! head -n 60 "$path" | grep -qE 'set -[eu][eu]?o?\s+pipefail'; then
    fail "$path missing 'set -euo pipefail' in top 60 lines"
  fi
done < <(
  find infra/scripts infra/helmfile/scripts infra/tests \
    -type f -name '*.sh' 2>/dev/null
)

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "" >&2
  echo "[shell-hygiene] FAIL: $VIOLATIONS / $TOTAL scripts missing set -euo pipefail in top 60 lines" >&2
  echo "" >&2
  echo "Add 'set -euo pipefail' near the top of each violating script." >&2
  echo "Sourced libs in **/lib/*.sh are exempt (set -e propagates to callers)." >&2
  exit 1
fi

echo "[shell-hygiene] PASS — $TOTAL scripts have set -euo pipefail (libs in **/lib/*.sh exempt)"
