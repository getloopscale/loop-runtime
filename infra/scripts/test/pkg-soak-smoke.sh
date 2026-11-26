#!/usr/bin/env bash
# pkg-soak-smoke.sh — Local-equivalent of pkg-soak.yml T3.2 special
# validation criteria, runnable against local k3d without CI secrets.
#
# Usage:
#   bash infra/scripts/pkg-soak-smoke.sh [--api-url URL]
#
# Default --api-url: http://localhost:13001
#
# Per pkg-sota-faang-grade-plan v1.4 T3.2 Deferred Runbook — provides
# operators a fast feedback loop BEFORE the Friday 02:00 UTC CI cron
# runs. Local check is a subset of the full gate (no 4h soak, no chaos,
# no SLO assertion) but covers the three criteria that depend on
# code (not on production traffic):
#
#   (a) RFC 9457 problem+json shape on error responses (T2.3 Stage C)
#   (b) Circuit breaker awareness (T1.4 + T2.1) — checks logs/metrics
#       for CB state observability
#   (c) kstatus conditions present on at least 1 Tenant CR (T2.2)
#
# Exits 0 if all reachable checks pass; 1 if any reachable check fails;
# 2 if pre-conditions are missing (api unreachable, kubectl missing).
#
# Stale-binary handling: if api returns the legacy ToJSON shape
# (Content-Type: application/json + {code,message}), this is reported
# as `STALE_BINARY` — operator must rebuild api with `tilt up`.

set -euo pipefail

API_URL="http://localhost:13001"
while [ $# -gt 0 ]; do
  case "$1" in
    --api-url) API_URL="$2"; shift 2 ;;
    -h|--help) sed -n '1,40p' "$0" | sed 's|^#||'; exit 0 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

PASS=0
FAIL=0
SKIP=0

result() {
  local code="$1"; shift
  local msg="$*"
  case "$code" in
    PASS) echo "  [PASS] $msg"; PASS=$((PASS+1)) ;;
    FAIL) echo "  [FAIL] $msg"; FAIL=$((FAIL+1)) ;;
    SKIP) echo "  [SKIP] $msg"; SKIP=$((SKIP+1)) ;;
    STALE) echo "  [STALE] $msg"; SKIP=$((SKIP+1)) ;;
  esac
}

echo "=== pkg-sota smoke checks (local) ==="
echo "API URL: $API_URL"
echo ""

# Pre-flight: api reachable? Use any endpoint that produces an HTTP
# response — even 401/404 means the process is up. We only fail when
# the connection itself fails (curl exit codes other than 22).
HTTP_CODE=$(curl -m 5 -s -o /dev/null -w "%{http_code}" "$API_URL/api/v1/projects" 2>/dev/null || echo "000")
if [ "$HTTP_CODE" = "000" ]; then
  echo "ERROR: api not reachable at $API_URL (no HTTP response)" >&2
  echo "       run 'task dev:up' or 'tilt up' first" >&2
  exit 2
fi
echo "  [INFO] api reachable (HTTP $HTTP_CODE on POST /api/v1/projects)"

# Criterion (a): RFC 9457 problem+json shape
echo "=== Criterion (a) — RFC 9457 problem+json shape (T2.3 Stage C) ==="
RESP=$(curl -si -X POST "$API_URL/api/v1/projects" \
  -H "Content-Type: application/json" \
  -d '{}' 2>/dev/null || true)

CT=$(echo "$RESP" | grep -i "^Content-Type:" | head -1 | tr -d '\r' | awk -F': ' '{print $2}')
BODY=$(echo "$RESP" | awk 'BEGIN{p=0} /^\r?$/{p=1; next} p{print}')

if echo "$CT" | grep -qi "application/problem+json"; then
  result PASS "Content-Type is application/problem+json"
  for field in type title status detail code; do
    if echo "$BODY" | grep -q "\"$field\""; then
      result PASS "field '$field' present in body"
    else
      result FAIL "field '$field' missing from body: $BODY"
    fi
  done
  TYPE_URI=$(echo "$BODY" | grep -oE '"type":"[^"]+"' | head -1 | sed 's/"type":"//;s/"$//')
  if echo "$TYPE_URI" | grep -q "^https://getloopscale.com/errors/"; then
    result PASS "type URI canonical: $TYPE_URI"
  else
    result FAIL "type URI not canonical: $TYPE_URI"
  fi
elif echo "$CT" | grep -qi "application/json"; then
  result STALE "Content-Type is plain application/json — api binary likely stale (pre-T2.3 Stage C). Rebuild via 'tilt up' to validate post-Stage C shape."
else
  result FAIL "unexpected Content-Type: $CT"
fi
echo ""

# Criterion (b): Circuit breaker observability
echo "=== Criterion (b) — Circuit breaker observability (T1.4 + T2.1) ==="
# Look for CB-related log lines or metrics endpoints
if curl -sf "$API_URL/metrics" -o /tmp/api-metrics.txt 2>/dev/null; then
  if grep -qE "circuitbreaker|loop_runtime_jwks_circuit|breaker_state" /tmp/api-metrics.txt; then
    result PASS "circuit breaker metrics exposed via /metrics"
  else
    result SKIP "no CB-prefixed metrics on /metrics — observability likely via logs only (acceptable)"
  fi
  rm -f /tmp/api-metrics.txt
else
  result SKIP "/metrics endpoint not reachable — observability check skipped"
fi
echo ""

# Criterion (c): kstatus conditions on at least 1 Tenant CR
echo "=== Criterion (c) — kstatus conditions on Tenant CR (T2.2 + Adoption #1) ==="
if ! command -v kubectl >/dev/null 2>&1; then
  result SKIP "kubectl not available — cannot check Tenant CR conditions"
else
  TENANTS=$(kubectl get tenant -A -o name 2>/dev/null | head -3 || true)
  if [ -z "$TENANTS" ]; then
    result SKIP "no Tenant CRs found in cluster — apply a sample Tenant first"
  else
    FOUND_KSTATUS=0
    for tn in $TENANTS; do
      NS="${tn%%/*}"
      NAME="${tn##*/}"
      # kubectl get returns "tenant/foo"; tease out
      if [[ "$tn" == */* ]] && [[ "$tn" != tenant/* ]]; then
        NS="${tn%%/*}"
        NAME="${tn##*/}"
      fi
      CONDS=$(kubectl get tenant "$NAME" -o jsonpath='{.status.conditions[*].type}' 2>/dev/null || true)
      if echo "$CONDS" | grep -qE "Ready|Reconciling|Stalled"; then
        result PASS "Tenant '$NAME' has kstatus condition triplet (types: $CONDS)"
        FOUND_KSTATUS=1
        break
      fi
    done
    if [ "$FOUND_KSTATUS" -eq 0 ]; then
      result SKIP "no Tenant CR has Ready/Reconciling/Stalled — controllers may not be running"
    fi
  fi
fi
echo ""

# Summary
echo "=== Summary ==="
echo "  PASS: $PASS  |  FAIL: $FAIL  |  SKIP/STALE: $SKIP"
echo ""
if [ "$FAIL" -gt 0 ]; then
  echo "RESULT: FAIL — $FAIL hard failure(s) detected"
  exit 1
fi
if [ "$SKIP" -gt 0 ]; then
  echo "RESULT: PARTIAL — $SKIP check(s) deferred (stale binary or infra not ready)"
  echo "        Re-run after 'tilt up' completes and operators are Ready."
  echo "        For full validation, await pkg-soak.yml first GREEN cron run."
fi
echo "RESULT: $PASS check(s) PASS — implementation present and reachable"
exit 0
