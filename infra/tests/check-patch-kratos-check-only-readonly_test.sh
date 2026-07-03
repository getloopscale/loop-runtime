#!/usr/bin/env bash
# Hermetic gate for patch-kratos-dev-public.sh --check-only read-only mode
# (M0 J-01 T5.3 EC-9). The dogfood-login Suite 0 pre-flight runs
# `patch-kratos-dev-public.sh --check-only` as a Cleanroom-SURVIVAL gate — it
# MUST be read-only (no kubectl apply, no rollout restart), otherwise a pre-flight
# check would mutate the very Kratos config it is validating. This guard locks
# --check-only as an alias for --dry-run --skip-restart. NO network, NO kubectl.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TARGET="$REPO_ROOT/infra/scripts/local/patch-kratos-dev-public.sh"

PASS=0
FAIL=0
assert() {
  local desc="$1" cond="$2"
  if [[ "$cond" == "1" ]]; then printf '  PASS: %s\n' "$desc"; PASS=$((PASS + 1))
  else printf '  FAIL: %s\n' "$desc"; FAIL=$((FAIL + 1)); fi
}
contains() { grep -qF -- "$2" "$1" && echo 1 || echo 0; }
matches() { grep -qE -- "$2" "$1" && echo 1 || echo 0; }

printf '[patch-kratos-check-only-readonly] gates:\n'

[[ -f "$TARGET" ]] && exists=1 || exists=0
assert "target script exists" "$exists"
[[ "$exists" == "1" ]] || { printf 'ABORT: %s missing\n' "$TARGET"; exit 1; }

if bash -n "$TARGET" 2>/dev/null; then assert "bash -n syntax clean" 1; else assert "bash -n syntax clean" 0; fi

# --check-only exists and is documented
assert "--check-only flag present" "$(contains "$TARGET" '--check-only')"

# --check-only sets DRY_RUN=1 (no apply) AND SKIP_RESTART=1 (no rollout).
assert "--check-only sets DRY_RUN (read-only, no apply)" \
  "$(matches "$TARGET" '\-\-check-only\).*DRY_RUN=1')"
assert "--check-only sets SKIP_RESTART (no rollout restart)" \
  "$(matches "$TARGET" '\-\-check-only\).*SKIP_RESTART=1')"

# The kubectl apply / mutation MUST be gated behind the DRY_RUN check (i.e. NOT
# reached when DRY_RUN=1). Assert a DRY_RUN guard exists around the apply path.
assert "apply/mutation gated behind DRY_RUN guard" \
  "$(matches "$TARGET" 'DRY_RUN.*(-eq 1|= .?1)')"

printf '[patch-kratos-check-only-readonly] %d passed, %d failed\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]]
