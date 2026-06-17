#!/usr/bin/env bash
# T6.1 dogfood wrapper for Plan J-22 — Dashboard Journey Secrets CRUD.
# Plan: .claude/knowledge-base/plans/2026-06-06-dashboard-journey-j22-secrets-crud-plan.md
#
# Invokes the J-22 suite components in sequence and reports an aggregate
# verdict so operators run ONE command before declaring the journey
# regression-free:
#
#   1. T5.4 hermetic syntax test (always — structural gate, ~1s)
#   2. vitest J-22 (35 specs / 9 files covering parser + hooks + 5
#      components + page URL state)
#   3. Go tests for SecretHandler.Bulk (T1.1/T1.2/T1.3/T5.2 — 5 specs)
#   4. T5.3 LIVE bash against dev-public (scenarios A-D)
#
# Flags:
#   --hermetic-only   only suites 1+2+3 (CI velocity, no LIVE)
#   --live-only       only suite 4 LIVE (operator drilldown)
#   --json            aggregated JSON verdict (machine-readable)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"

HERMETIC_ONLY=0
LIVE_ONLY=0
JSON_MODE=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --hermetic-only) HERMETIC_ONLY=1; shift ;;
    --live-only) LIVE_ONLY=1; shift ;;
    --json) JSON_MODE=1; shift ;;
    -h|--help) sed -n '2,20p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) shift ;;
  esac
done

log() {
  if [[ "$JSON_MODE" != "1" ]]; then
    printf '[dogfood-secrets] %s\n' "$*" >&2
  fi
}

RESULTS=()
TOTAL_PASS=0
TOTAL_FAIL=0

record() {
  local suite="$1" verdict="$2" detail="${3:-}"
  RESULTS+=("$suite|$verdict|$detail")
  if [[ "$verdict" == "PASS" ]]; then
    TOTAL_PASS=$((TOTAL_PASS + 1))
    log "[$suite] PASS $detail"
  else
    TOTAL_FAIL=$((TOTAL_FAIL + 1))
    log "[$suite] FAIL $detail"
  fi
}

# --- Suite 1: T5.4 hermetic syntax test ---
if [[ "$LIVE_ONLY" != "1" ]]; then
  log "Suite 1/4: T5.4 hermetic syntax test"
  if bash "$REPO_ROOT/infra/tests/check-dev-public-secrets-flow-syntax_test.sh" >/dev/null 2>&1; then
    record "syntax" "PASS"
  else
    record "syntax" "FAIL"
  fi
fi

# --- Suite 2: vitest J-22 ---
if [[ "$LIVE_ONLY" != "1" ]]; then
  log "Suite 2/4: vitest J-22 (35 specs / 9 files)"
  (
    cd "$REPO_ROOT/cloud/dashboard"
    if npx vitest run \
        src/lib/dotenv.test.ts \
        src/hooks/use-secrets.test.tsx \
        src/hooks/mutations/use-secrets-mutations.test.tsx \
        src/components/secrets/ \
        src/pages/secrets.test.tsx >/tmp/dogfood-secrets-vitest.log 2>&1; then
      exit 0
    else
      exit 1
    fi
  ) && record "vitest" "PASS" "(35 specs)" || record "vitest" "FAIL" "(see /tmp/dogfood-secrets-vitest.log)"
fi

# --- Suite 3: Go SecretHandler.Bulk tests ---
if [[ "$LIVE_ONLY" != "1" ]]; then
  log "Suite 3/4: Go tests SecretHandler.Bulk"
  (
    cd "$REPO_ROOT/api"
    if go test -run 'TestSecretBulk' ./internal/routes/ >/tmp/dogfood-secrets-go.log 2>&1; then
      exit 0
    else
      exit 1
    fi
  ) && record "go-bulk" "PASS" "(5 specs)" || record "go-bulk" "FAIL" "(see /tmp/dogfood-secrets-go.log)"
fi

# --- Suite 4: T5.3 LIVE bash dev-public ---
if [[ "$HERMETIC_ONLY" != "1" ]]; then
  log "Suite 4/4: T5.3 LIVE bash dev-public-secrets-flow"
  if bash "$REPO_ROOT/infra/tests/e2e/dev-public-secrets-flow_test.sh" >/tmp/dogfood-secrets-live.log 2>&1; then
    record "live-bash" "PASS"
  else
    record "live-bash" "FAIL" "(see /tmp/dogfood-secrets-live.log)"
  fi
fi

# --- Aggregate verdict ---
VERDICT=$([[ $TOTAL_FAIL -eq 0 ]] && echo PASS || echo FAIL)

if [[ "$JSON_MODE" == "1" ]]; then
  # Emit a compact JSON object
  printf '{"verdict":"%s","pass":%d,"fail":%d,"suites":[' "$VERDICT" "$TOTAL_PASS" "$TOTAL_FAIL"
  first=1
  for r in "${RESULTS[@]}"; do
    IFS='|' read -r s v d <<<"$r"
    if [[ "$first" == "1" ]]; then first=0; else printf ','; fi
    printf '{"name":"%s","verdict":"%s","detail":"%s"}' "$s" "$v" "$d"
  done
  printf ']}\n'
else
  log "Summary: PASS=$TOTAL_PASS FAIL=$TOTAL_FAIL"
  log "Verdict: $VERDICT"
fi

[[ "$VERDICT" == "PASS" ]] || exit 1
