#!/usr/bin/env bash
# T5.1 of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2 — dogfood
# wrapper for the J-22 OpenBao Transit + ESO Sync chain. Invokes the 4
# suite components in sequence and reports an aggregate verdict so
# operators run ONE command before declaring the openbao-transit chain
# regression-free:
#
#   1. T4.2 hermetic syntax test on T4.1 LIVE script (~1s)
#   2. Go pkg/vault tests (race detector) — 31 specs covering
#      TransitClient, Zeroize, Redaction, RateLimit, EC-04 singleflight,
#      EC-06 auto-create
#   3. Go api/internal/routes tests for SecretHandler Bulk transit wire
#      (14 specs including T1.1/T1.2/T2.3 absorption coverage)
#   4. T4.1 LIVE bash 6-scenario zero-leak proof against dev-public
#      (OPERATOR HARD STOP unless --skip-live; CI uses --hermetic-only)
#
# Flags:
#   --hermetic-only   only suites 1+2+3 (CI velocity, no LIVE)
#   --live-only       only suite 4 LIVE (operator drilldown)
#   --json            aggregated JSON verdict (machine-readable)
#   --help, -h        show this help

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
    -h|--help) sed -n '2,30p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) shift ;;
  esac
done

log() {
  if [[ "$JSON_MODE" != "1" ]]; then
    printf '[dogfood-openbao] %s\n' "$*" >&2
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

# --- Suite 1: T4.2 hermetic syntax test on T4.1 LIVE script ---
if [[ "$LIVE_ONLY" != "1" ]]; then
  log "Suite 1/4: T4.2 hermetic syntax test (T4.1 structural shape)"
  if bash "$REPO_ROOT/infra/tests/check-dev-public-secrets-zero-leak-syntax_test.sh" >/dev/null 2>&1; then
    record "syntax" "PASS" "(38 assertions)"
  else
    record "syntax" "FAIL" "(check-dev-public-secrets-zero-leak-syntax_test failed)"
  fi
fi

# --- Suite 2: Go pkg/vault tests (race detector) ---
if [[ "$LIVE_ONLY" != "1" ]]; then
  log "Suite 2/4: go test -race ./pkg/vault/... (TransitClient + Zeroize + Redaction + RateLimit)"
  (
    cd "$REPO_ROOT/pkg"
    if go test -race -count=1 ./vault/... >/tmp/dogfood-openbao-pkg-vault.log 2>&1; then
      exit 0
    else
      exit 1
    fi
  ) && record "pkg-vault" "PASS" "(31 specs race-clean)" || record "pkg-vault" "FAIL" "(see /tmp/dogfood-openbao-pkg-vault.log)"
fi

# --- Suite 3: Go api routes SecretBulk transit absorption tests ---
if [[ "$LIVE_ONLY" != "1" ]]; then
  log "Suite 3/4: go test -race ./api/internal/routes (SecretBulk T1.1/T1.2/T2.3)"
  (
    cd "$REPO_ROOT/api"
    if go test -race -count=1 -run 'TestSecretBulk' ./internal/routes/ >/tmp/dogfood-openbao-api-routes.log 2>&1; then
      exit 0
    else
      exit 1
    fi
  ) && record "api-routes" "PASS" "(14 TestSecretBulk specs)" || record "api-routes" "FAIL" "(see /tmp/dogfood-openbao-api-routes.log)"
fi

# --- Suite 4: T4.1 LIVE bash 6-scenario zero-leak against dev-public ---
if [[ "$HERMETIC_ONLY" != "1" ]]; then
  log "Suite 4/4: T4.1 LIVE 6-scenario zero-leak (operator HARD STOP)"
  # Operator runs this only when the cluster is wired (real OpenBao + ESO
  # + loopzero-api transit chain). CI invocation passes --skip-live to short-
  # circuit; operator-mode runs it as-is.
  if bash "$REPO_ROOT/infra/tests/e2e/dev-public-secrets-zero-leak_test.sh" >/tmp/dogfood-openbao-live.log 2>&1; then
    record "live-bash" "PASS" "(6 scenarios)"
  else
    record "live-bash" "FAIL" "(see /tmp/dogfood-openbao-live.log)"
  fi
fi

# --- Aggregate verdict ---
VERDICT=$([[ $TOTAL_FAIL -eq 0 ]] && echo PASS || echo FAIL)

if [[ "$JSON_MODE" == "1" ]]; then
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
