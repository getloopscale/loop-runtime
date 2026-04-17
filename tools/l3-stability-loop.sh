#!/usr/bin/env bash
# T-09 L3 stability loop — validates Gate #1 by running L3 N times
# consecutively, with `admin-evict-tenants.sh` cleanup between each.
#
# Gate #1 success criterion: 10 consecutive runs ≥ 95/100 apps live,
# delta p95 deploy time between min/max runs < 10%, zero manual
# intervention (no kubectl delete --type=json --force live-patches).
#
# Exits 0 only when ALL requested runs pass. Any run below target
# halts the loop immediately (fail-fast to avoid burning cluster
# resources on a broken baseline).
#
# Usage:
#   tools/l3-stability-loop.sh                 # default: 10 runs
#   tools/l3-stability-loop.sh --max-runs 3    # smoke-test the loop
#   tools/l3-stability-loop.sh --threshold 90  # lower bar temporarily
#   tools/l3-stability-loop.sh --no-cleanup    # skip eviction (dev use)
#
# Plan: docs/plans/50k-immediate-focus.md (T-09, Gate #1)
set -eu

MAX_RUNS="${MAX_RUNS:-10}"
THRESHOLD="${THRESHOLD:-95}"      # apps-live minimum as integer (0-100)
PATTERN="${PATTERN:-tn-lt-l3-%}"
DO_CLEANUP=1
RUN_LOG_DIR="${RUN_LOG_DIR:-/tmp/l3-stability}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --max-runs)   MAX_RUNS="$2"; shift 2 ;;
    --threshold)  THRESHOLD="$2"; shift 2 ;;
    --pattern)    PATTERN="$2"; shift 2 ;;
    --no-cleanup) DO_CLEANUP=0; shift ;;
    -h|--help)
      echo "Usage: $0 [--max-runs N] [--threshold PCT] [--pattern SQL-LIKE] [--no-cleanup]"
      echo "Env overrides: MAX_RUNS, THRESHOLD, PATTERN, RUN_LOG_DIR"
      exit 0
      ;;
    *) echo "Unknown arg: $1" >&2; exit 1 ;;
  esac
done

mkdir -p "$RUN_LOG_DIR"
SUMMARY="$RUN_LOG_DIR/summary.tsv"
: > "$SUMMARY"
echo -e "run\ttimestamp\tapps_live\tp95_ms\telapsed_s\tstatus" >> "$SUMMARY"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

passed=0
failed=0
for i in $(seq 1 "$MAX_RUNS"); do
  TS=$(date +%s)
  RUN_LOG="$RUN_LOG_DIR/run-$i-$TS.log"
  echo ""
  echo "=============================================="
  echo " L3 stability loop — run $i/$MAX_RUNS"
  echo " log: $RUN_LOG"
  echo "=============================================="

  # Cleanup between runs — fail-fast if eviction itself breaks.
  if [ "$DO_CLEANUP" -eq 1 ]; then
    echo "[cleanup] evicting pattern=$PATTERN"
    if ! "$SCRIPT_DIR/admin-evict-tenants.sh" --pattern "$PATTERN" --yes > "$RUN_LOG_DIR/cleanup-$i.log" 2>&1; then
      echo "FAIL: cleanup before run $i exited non-zero. See $RUN_LOG_DIR/cleanup-$i.log"
      exit 1
    fi
    # Brief settle for controllers to finish reconcileDelete.
    sleep 15
  fi

  # Run L3. The test hardcodes c=30 at real_deploy_test.go; honor that.
  STARTED=$(date +%s)
  if (cd "$REPO_ROOT" && go test ./infra/tests/load/ -run TestRealDeployL3 -v -timeout 60m) \
        > "$RUN_LOG" 2>&1; then
    STATUS="pass"
  else
    STATUS="fail"
  fi
  ELAPSED=$(( $(date +%s) - STARTED ))

  # Parse: lines end with "Apps Live: N / 100 (HTTP 200 verified)".
  APPS_LIVE=$(grep -oE "Apps Live:[[:space:]]+[0-9]+" "$RUN_LOG" | head -1 | grep -oE "[0-9]+" | tail -1 || echo "0")
  P95=$(grep -oE "p95[^0-9]*[0-9]+(\.[0-9]+)?ms" "$RUN_LOG" | head -1 | grep -oE "[0-9]+" | head -1 || echo "0")

  echo -e "$i\t$TS\t$APPS_LIVE\t$P95\t$ELAPSED\t$STATUS" >> "$SUMMARY"
  echo "run $i: apps_live=$APPS_LIVE/100 p95=${P95}ms elapsed=${ELAPSED}s status=$STATUS"

  if [ "$STATUS" = "fail" ] || [ "$APPS_LIVE" -lt "$THRESHOLD" ]; then
    failed=$((failed + 1))
    echo "HALT: run $i below threshold ($APPS_LIVE < $THRESHOLD) — fail-fast."
    echo ""
    echo "Summary so far (see $SUMMARY):"
    cat "$SUMMARY"
    exit 1
  fi
  passed=$((passed + 1))
done

echo ""
echo "=============================================="
echo " ALL $MAX_RUNS RUNS PASSED ≥ $THRESHOLD/100"
echo "=============================================="
cat "$SUMMARY"
echo ""
echo "Gate #1 evidence: $SUMMARY"
exit 0
