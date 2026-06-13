#!/usr/bin/env bash
# T6.1 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
#
# Canonical chain runner: orchestrates the FULL post-Cleanroom integration
# validation chain from `task clean:all` through sponsor walkthrough JUNTOS.
# This is the EXECUTABLE form of Phase 6 of the plan.
#
# Per cleanroom-up-live-test-mandate.md INQUEBRÁVEL: SEMPRE Cleanroom TOTAL
# + UP + LIVE TEST. This runner enforces the canonical sequence.
#
# Chain:
#   1. Cleanroom TOTAL: task clean:all (~5min destroy)
#   2. Recreate infra: task setup (~25min)
#   3. Dev loop: tilt up background
#   4. Health gate: task doctor (PASS=38+ FAIL=0)
#   5. LoopZero canonical state: task check (T4.1 check-sprint canonical)
#   6. LIVE E2E 9-journeys auto: T5.1 with --include-semi-auto=false
#   7. Sponsor JUNTOS walkthrough: J-07 + J-08 (semi-auto HARD STOP)
#   8. Emit IMPLEMENTATION_COMPLETE only after sponsor verbatim approval
#
# Per ADR D7: sponsor walkthrough scheduling honored async. Engineering-side
# can emit IMPLEMENTATION_COMPLETE-ENGINEERING-SIDE after step 6; final
# IMPLEMENTATION_COMPLETE gated on step 7.
#
# Wall-clock budget: ~60-90min total (~30min Cleanroom + ~5min dev loop +
# ~5min doctor/check + ~15min LIVE auto + ~30min sponsor walkthrough).
#
# Flags:
#   --skip-cleanroom    skip steps 1-3 (env already up; debug iteration)
#   --skip-sponsor      stop at step 6 (engineering-side closure only)
#   --json              machine-readable verdict per step
#   --help              show usage
#
# Allowlist entry: KEEP-OPERATOR (live-cluster chain orchestration).

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

SKIP_CLEANROOM=0
SKIP_SPONSOR=0
JSON_MODE=0

for arg in "$@"; do
  case "$arg" in
    --skip-cleanroom) SKIP_CLEANROOM=1 ;;
    --skip-sponsor) SKIP_SPONSOR=1 ;;
    --json) JSON_MODE=1 ;;
    --help) sed -n '1,40p' "$0" | grep '^#' | sed 's/^# \?//'; exit 0 ;;
  esac
done

log() { [[ "$JSON_MODE" -eq 0 ]] && printf '[chain-runner] %s\n' "$*" >&2; }

# Step verdict tracking
declare -a STEP_RESULTS=()
record() {
  local step="$1" verdict="$2" detail="$3"
  STEP_RESULTS+=("$step:$verdict:$detail")
  log "[$verdict] step $step — $detail"
}

cd "$REPO_ROOT"

# === Step 1-3: Cleanroom TOTAL + dev loop (operator hard fail if absent) ===
if [[ "$SKIP_CLEANROOM" -eq 0 ]]; then
  log "Step 1: task clean:all (destroy ~5min)"
  if task clean:all >/tmp/cleanroom-clean.log 2>&1; then
    record 1 PASS "task clean:all completed"
  else
    record 1 FAIL "task clean:all failed — see /tmp/cleanroom-clean.log"
    exit 1
  fi

  log "Step 2: task setup (recreate ~25min)"
  if task setup >/tmp/cleanroom-setup.log 2>&1; then
    record 2 PASS "task setup completed"
  else
    record 2 FAIL "task setup failed — see /tmp/cleanroom-setup.log"
    exit 1
  fi

  log "Step 3: tilt up background"
  pkill -f "tilt up" 2>/dev/null || true  # kill stale per j17-aprendizado
  setsid tilt up >/tmp/cleanroom-tilt.log 2>&1 &
  # LIVE bug fix (T6.2 Cleanroom TOTAL 2026-06-12): Tilt boot + operator/api
  # build + deploy needs ~3-5min for healthy state. Previous sleep 30 fired
  # Step 4 task doctor before Tilt was ready → FAIL with 0 replicas + Vault
  # CrashLoopBackOff. New 240s sleep accommodates canonical boot time.
  sleep 240
  record 3 PASS "tilt up launched (PID $!) + 240s boot wait"
else
  log "Skip steps 1-3 (--skip-cleanroom)"
fi

# === Step 4: task doctor health gate ===
log "Step 4: task doctor (38+ PASS / 0 FAIL)"
if task doctor >/tmp/cleanroom-doctor.log 2>&1; then
  FAIL_COUNT=$(grep -c "^FAIL\|: FAIL" /tmp/cleanroom-doctor.log 2>/dev/null || echo 0)
  if [[ "$FAIL_COUNT" -eq 0 ]]; then
    record 4 PASS "task doctor 38+/0"
  else
    record 4 FAIL "task doctor has $FAIL_COUNT FAILs — abort"
    exit 1
  fi
else
  record 4 FAIL "task doctor non-zero exit"
  exit 1
fi

# === Step 5: task check canonical state (T4.1) ===
log "Step 5: bash infra/scripts/local/check-sprint.sh --fail-fast (T4.1 canonical)"
if bash infra/scripts/local/check-sprint.sh --fail-fast 2>&1 | tee /tmp/cleanroom-check.log | grep -q "FAIL=0"; then
  record 5 PASS "task check canonical state OK"
else
  record 5 FAIL "task check has FAILs — abort"
  exit 1
fi

# === Step 6: LIVE E2E 9-journeys (auto only) ===
log "Step 6: T5.1 LIVE E2E 9-journeys auto subset"
if bash infra/tests/e2e/dev-public-cleanroom-canonical-9-journeys_test.sh 2>&1 | tee /tmp/cleanroom-9journeys.log | grep -q "0 FAIL"; then
  record 6 PASS "7 auto-journeys GREEN"
else
  record 6 FAIL "auto-journey FAILs — abort"
  exit 1
fi

# === Engineering-side closure ===
log ""
log "════════════════════════════════════════════════════════════════"
log "  ENGINEERING-SIDE IMPLEMENTATION_COMPLETE-ENGINEERING-SIDE"
log "════════════════════════════════════════════════════════════════"

if [[ "$SKIP_SPONSOR" -eq 1 ]]; then
  log "Stop at step 6 per --skip-sponsor flag"
  exit 0
fi

# === Step 7: sponsor walkthrough JUNTOS (semi-auto HARD STOP) ===
log ""
log "Step 7: Sponsor walkthrough JUNTOS (J-07 + J-08)"
log ""
log "  HARD STOP per cleanroom-up-live-test-mandate.md INQUEBRÁVEL"
log "  This step requires sponsor browser presence + verbatim approval."
log ""
log "  Operator action: open browser to https://app-dev.loopzero.example"
log "    (a) Walk J-07 Projects List with sponsor"
log "    (b) Walk J-08 Project Detail Overview with sponsor"
log "    (c) Capture sponsor verbatim approval (e.g., 'Confirmado' or"
log "        'Ficou ótimo, parabens!')"
log "    (d) Record verbatim quote in commit message OR baseline doc"
log ""
log "  When sponsor approval recorded, re-run with --skip-sponsor-prompt"
log "  to emit final IMPLEMENTATION_COMPLETE."

# Without sponsor verbatim approval, NEVER emit final marker
log "  NOT emitting final IMPLEMENTATION_COMPLETE — sponsor JUNTOS pending."
exit 0
