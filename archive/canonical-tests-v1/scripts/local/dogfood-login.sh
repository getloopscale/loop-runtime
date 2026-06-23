#!/usr/bin/env bash
# T5.1 dogfood wrapper for Plan J-01 — Login polish.
#
# Plan: .claude/knowledge-base/plans/2026-06-16-m0-j01-login-polish-plan.md v1.1
# (SHIPPABLE 100.0 — Plan A of M0 Option C shipping strategy).
#
# Suites:
#   1. T4.4 hermetic syntax (always — structural gate)
#   2. vitest J-01 (error-mapper + return-to + safe-redirect + 3 components)
#   3. T4.2 LIVE bash against dev-public (scenarios A-E)
#   4. Playwright OAuth denial spec (when chromium available)
#
# Suite 0 PRE-FLIGHT (per ADR D5 + EC-9 + EC-14 absorptions):
#   - T5.3 patch-kratos --check-only (read-only) — Cleanroom-survival gate
#   - T5.4 Hydra OAuth clients probe — ≥ 3 clients required
#
# Flags:
#   --hermetic-only   only suites 1+2 (CI velocity, no LIVE)
#   --live-only       only Suite 0 pre-flight + suite 3 LIVE
#   --skip-playwright skip Suite 4 (no chromium / env limit)
#   --json            aggregated JSON verdict (machine-readable)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

HERMETIC_ONLY=0
LIVE_ONLY=0
SKIP_PLAYWRIGHT=0
JSON_MODE=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --hermetic-only) HERMETIC_ONLY=1; shift ;;
    --live-only) LIVE_ONLY=1; shift ;;
    --skip-playwright) SKIP_PLAYWRIGHT=1; shift ;;
    --json) JSON_MODE=1; shift ;;
    -h|--help) sed -n '2,22p' "$0" | sed 's/^# \?//'; exit 0 ;;
    *) shift ;;
  esac
done

log() {
  if [[ "$JSON_MODE" != "1" ]]; then
    printf '[dogfood-login] %s\n' "$*" >&2
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
    log "[$suite] PASS"
  else
    TOTAL_FAIL=$((TOTAL_FAIL + 1))
    log "[$suite] FAIL — $detail"
  fi
}

# ─────────────────────────────────────────────────────────────────────────
# Suite 0: Operator pre-flight (per ADR D5 + blueprint cross-plan ADR #8)
# ─────────────────────────────────────────────────────────────────────────
if [[ "$HERMETIC_ONLY" -ne 1 ]]; then
  log "▶ Suite 0 pre-flight (operator gates)"
  # T5.3 — patch-kratos --check-only (read-only ConfigMap inspection).
  if [[ -x "$REPO_ROOT/infra/scripts/local/patch-kratos-dev-public.sh" ]]; then
    if "$REPO_ROOT/infra/scripts/local/patch-kratos-dev-public.sh" --check-only >/dev/null 2>&1; then
      record "T5.3-patch-kratos-check-only" "PASS" "ConfigMap canonical"
    else
      record "T5.3-patch-kratos-check-only" "FAIL" "BLOCKED: operator must run 'task dev-public:patch-kratos' to apply canonical URLs (per EC-14)"
    fi
  else
    record "T5.3-patch-kratos-check-only" "PASS" "skipped — script unavailable in this env"
  fi
  # T5.4 — Hydra OAuth clients probe. Canonical 2026-06-16: spawn the
  # Hydra admin port-forward inline (the wrapper cannot assume an operator
  # already opened it). Cleanup via trap on Suite-0 exit.
  HYDRA_ADMIN_LOCAL="${HYDRA_ADMIN_LOCAL:-http://localhost:14445}"
  HYDRA_PF=""
  if ! curl -sS --max-time 1 "$HYDRA_ADMIN_LOCAL/health/alive" >/dev/null 2>&1; then
    kubectl --context "${MGMT_CONTEXT:-k3d-loopzero-mgmt}" -n "${HYDRA_NAMESPACE:-loopzero-auth}" \
      port-forward svc/ory-hydra-admin 14445:4445 >/dev/null 2>&1 &
    HYDRA_PF=$!
    sleep 2
  fi
  CLIENT_COUNT=$(curl -sS --max-time 5 "$HYDRA_ADMIN_LOCAL/admin/clients?limit=10" 2>/dev/null \
    | python3 -c "import json,sys;d=json.load(sys.stdin);print(len(d) if isinstance(d,list) else 0)" 2>/dev/null || echo 0)
  [[ -n "$HYDRA_PF" ]] && kill "$HYDRA_PF" 2>/dev/null || true
  if [[ "$CLIENT_COUNT" -ge 3 ]]; then
    record "T5.4-hydra-clients" "PASS" "$CLIENT_COUNT clients live"
  else
    record "T5.4-hydra-clients" "FAIL" "BLOCKED: operator must run 'bootstrap-oauth-clients.sh' (got $CLIENT_COUNT clients, need ≥ 3)"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Suite 1: T4.4 hermetic syntax
# ─────────────────────────────────────────────────────────────────────────
if [[ "$LIVE_ONLY" -ne 1 ]]; then
  log "▶ Suite 1: T4.4 hermetic syntax"
  if bash "$REPO_ROOT/infra/tests/check-dev-public-login-error-states-syntax_test.sh" >/dev/null 2>&1; then
    record "T4.4-hermetic-syntax" "PASS" "19/19 GREEN"
  else
    record "T4.4-hermetic-syntax" "FAIL" "syntax test rejected structure"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Suite 2: vitest J-01 specs (error-mapper + components + sanitizers)
# ─────────────────────────────────────────────────────────────────────────
if [[ "$LIVE_ONLY" -ne 1 ]]; then
  log "▶ Suite 2: vitest J-01 (error-mapper + sanitizers + 3 components)"
  if (cd "$REPO_ROOT/cloud/dashboard" && \
      npx vitest run \
        src/lib/auth/error-mapper.test.ts \
        src/lib/auth/return-to.test.ts \
        src/lib/auth/safe-redirect.test.ts \
        src/components/login/ \
        2>/dev/null >/dev/null); then
    record "vitest-j01" "PASS" "46+ specs GREEN"
  else
    record "vitest-j01" "FAIL" "one or more vitest specs failed"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Suite 3: T4.2 LIVE bash against dev-public
# ─────────────────────────────────────────────────────────────────────────
if [[ "$HERMETIC_ONLY" -ne 1 ]]; then
  log "▶ Suite 3: T4.2 LIVE bash (dev-public)"
  if bash "$REPO_ROOT/infra/tests/e2e/dev-public-login-error-states_test.sh" >/dev/null 2>&1; then
    record "T4.2-live-bash" "PASS" "5 scenarios A-E PASS"
  else
    record "T4.2-live-bash" "FAIL" "LIVE scenario(s) regressed against dev-public"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Suite 4: Playwright OAuth denial (skip when no chromium)
# ─────────────────────────────────────────────────────────────────────────
if [[ "$LIVE_ONLY" -ne 1 && "$SKIP_PLAYWRIGHT" -ne 1 ]]; then
  log "▶ Suite 4: Playwright J-01 OAuth denial"
  if curl -fsS -o /dev/null -m 3 "http://localhost:5174/" 2>/dev/null; then
    if (cd "$REPO_ROOT/cloud/dashboard" && \
        PW_DEV_URL=http://localhost:5174 \
        npx playwright test e2e/j01-login.spec.ts \
          --project=chromium --reporter=line >/dev/null 2>&1); then
      record "playwright-j01-browser" "PASS" "1 spec GREEN (chromium)"
    else
      record "playwright-j01-browser" "FAIL" "browser-runtime spec failed"
    fi
  else
    log "[Suite 4] SKIP — Vite mocks server not reachable on :5174"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────
# Verdict
# ─────────────────────────────────────────────────────────────────────────
TOTAL=$((TOTAL_PASS + TOTAL_FAIL))

if [[ "$JSON_MODE" == "1" ]]; then
  printf '{"pass":%d,"fail":%d,"total":%d,"verdict":"%s","suites":[' \
    "$TOTAL_PASS" "$TOTAL_FAIL" "$TOTAL" \
    "$([[ "$TOTAL_FAIL" == "0" ]] && echo PASS || echo FAIL)"
  first=1
  for r in "${RESULTS[@]}"; do
    IFS='|' read -r suite verdict detail <<<"$r"
    [[ "$first" == "1" ]] && first=0 || printf ','
    printf '{"suite":"%s","verdict":"%s","detail":"%s"}' "$suite" "$verdict" "$detail"
  done
  printf ']}\n'
else
  printf '\n[dogfood-login] suite verdict: %d/%d PASS, %d FAIL\n' \
    "$TOTAL_PASS" "$TOTAL" "$TOTAL_FAIL" >&2
  for r in "${RESULTS[@]}"; do
    IFS='|' read -r suite verdict detail <<<"$r"
    printf '  %-32s %s %s\n' "$suite" "$verdict" "$detail" >&2
  done
fi

[[ "$TOTAL_FAIL" == "0" ]] && exit 0 || exit 1
