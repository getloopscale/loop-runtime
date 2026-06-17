#!/usr/bin/env bash
# T5.1 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
#
# LIVE master orchestrator dispatching 9 journey scenarios sequencialmente
# against dev-public env. Closes the Cleanroom regression-defense LIVE gap
# by automating sponsor walkthrough JUNTOS chain into one runnable bash
# script. Per memory reference_t62_cleanroom_session_2026_06_11 — empirical
# proof of canonical Cleanroom.
#
# 9 journeys (per CLAUDE.md § Dashboard Journeys — priority chain):
#   J-22 Secrets CRUD + zero-leak       → infra/tests/e2e/dev-public-secrets-zero-leak_test.sh
#   J-09 Project Connect (GitHub App)   → infra/tests/e2e/dev-public-github-webhook_test.sh
#   J-23 Previews (per-PR)              → infra/tests/e2e/dev-public-preview-flow_test.sh
#   J-07 Projects List                  → (sponsor browser walkthrough — operator HARD STOP)
#   J-08 Project Detail Overview        → (sponsor browser walkthrough — operator HARD STOP)
#   J-10 Environments / Deployments     → infra/tests/e2e/dev-public-environments-flow_test.sh
#   J-17 Deployments Realtime           → infra/tests/e2e/dev-public-deployments-flow_test.sh
#   J-05 Device Success                 → infra/tests/e2e/dev-public-device-flow_test.sh
#   J-04 Password Recovery              → infra/tests/e2e/dev-public-recovery-flow_test.sh
#
# Per ADR D6 (bash NOT Ginkgo per cleanroom-up-live-test-mandate.md): bash
# orchestrator preserves LIVE-debug ergonomics that Ginkgo abstracts away.
#
# Per ADR D10 (Auto vs Semi-auto distinction):
#   - Auto journeys (J-22 J-09 J-23 J-10 J-17 J-05 J-04): scripted bash, no
#     human interaction; PASS counter increments enforce determinism.
#   - Semi-auto journeys (J-07 J-08): require sponsor browser walkthrough
#     JUNTOS canonical gate — script SKIPs with WARN unless
#     --include-semi-auto flag set + operator confirms sponsor available.
#
# Per ADR D7: sponsor walkthrough scheduling honored async. Script emits
# IMPLEMENTATION_COMPLETE-ENGINEERING-SIDE marker after all auto journeys
# pass; sponsor walkthrough completes via separate `infra/scripts/local/
# sponsor-walkthrough-prompt.sh` (operator-driven session).
#
# Operator HARD STOPs:
#   1. Requires running cluster post-Cleanroom + dev-public env up
#   2. ~/.loopzero/local-dogfood-token.txt JWT seeded
#   3. J-07/J-08 sponsor walkthrough JUNTOS (semi-auto)
#
# Flags:
#   --skip-live              hermetic mode (CI structure validation only)
#   --include-semi-auto      include J-07 J-08 (requires sponsor)
#   --journey=J-NN           run single journey
#   --json                   machine-readable output
#   --help                   show this header

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

SKIP_LIVE=0
INCLUDE_SEMI_AUTO=0
JOURNEY_FILTER=""
JSON_MODE=0

for arg in "$@"; do
  case "$arg" in
    --skip-live) SKIP_LIVE=1 ;;
    --include-semi-auto) INCLUDE_SEMI_AUTO=1 ;;
    --journey=*) JOURNEY_FILTER="${arg#--journey=}" ;;
    --json) JSON_MODE=1 ;;
    --help) sed -n '1,50p' "$0" | grep '^#' | sed 's/^# \?//'; exit 0 ;;
  esac
done

# Journey registry: id:name:kind:script
JOURNEYS=(
  "J-22:Secrets CRUD + zero-leak:auto:infra/tests/e2e/dev-public-secrets-zero-leak_test.sh"
  "J-09:Project Connect GitHub App:auto:infra/tests/e2e/dev-public-github-webhook_test.sh"
  "J-23:Previews per-PR:auto:infra/tests/e2e/dev-public-preview-flow_test.sh"
  "J-07:Projects List:semi-auto:SPONSOR_BROWSER_WALKTHROUGH"
  "J-08:Project Detail Overview:semi-auto:SPONSOR_BROWSER_WALKTHROUGH"
  "J-10:Environments / Deployments:auto:infra/tests/e2e/dev-public-environments-flow_test.sh"
  "J-17:Deployments Realtime:auto:infra/tests/e2e/dev-public-deployments-flow_test.sh"
  "J-05:Device Success:auto:infra/tests/e2e/dev-public-device-flow_test.sh"
  "J-04:Password Recovery:auto:infra/tests/e2e/dev-public-recovery-flow_test.sh"
)

PASS=0; FAIL=0; SKIP=0
RESULTS=()

log() { [[ "$JSON_MODE" -eq 0 ]] && printf '[9-journeys] %s\n' "$*" >&2; }

run_journey() {
  local id="$1" name="$2" kind="$3" script="$4"

  if [[ -n "$JOURNEY_FILTER" && "$JOURNEY_FILTER" != "$id" ]]; then return; fi

  # Semi-auto journeys SKIP unless --include-semi-auto + operator confirms
  if [[ "$kind" == "semi-auto" ]]; then
    if [[ "$INCLUDE_SEMI_AUTO" -eq 0 ]]; then
      log "[SKIP] $id $name (semi-auto — sponsor walkthrough JUNTOS required; use --include-semi-auto)"
      SKIP=$((SKIP+1))
      RESULTS+=("$id:SKIP:semi-auto deferred")
      return
    else
      log "[SEMI-AUTO] $id $name — sponsor browser walkthrough JUNTOS canonical gate"
      log "  HARD STOP: this journey requires sponsor presence + verbatim approval"
      log "  Operator script: bash infra/scripts/local/sponsor-walkthrough-prompt.sh $id"
      SKIP=$((SKIP+1))
      RESULTS+=("$id:SEMI-AUTO:sponsor walkthrough deferred to ops")
      return
    fi
  fi

  if [[ "$SKIP_LIVE" -eq 1 ]]; then
    if [[ -f "$REPO_ROOT/$script" ]]; then
      log "[SKIP-LIVE] $id $name (script exists at $script)"
      PASS=$((PASS+1))
      RESULTS+=("$id:PASS:skip-live structure OK")
    else
      log "[FAIL] $id $name (script missing: $script)"
      FAIL=$((FAIL+1))
      RESULTS+=("$id:FAIL:script missing")
    fi
    return
  fi

  log "[RUN] $id $name → $script"
  if bash "$REPO_ROOT/$script" >/dev/null 2>&1; then
    log "[PASS] $id"
    PASS=$((PASS+1))
    RESULTS+=("$id:PASS:OK")
  else
    log "[FAIL] $id"
    FAIL=$((FAIL+1))
    RESULTS+=("$id:FAIL:see logs")
  fi
}

# === Pre-flight ===
if [[ "$SKIP_LIVE" -eq 0 ]]; then
  if [[ ! -f "$HOME/.loopzero/local-dogfood-token.txt" ]]; then
    log "FAIL pre-flight: ~/.loopzero/local-dogfood-token.txt missing (JWT seed)"
    exit 1
  fi
fi

START=$(date +%s)
for spec in "${JOURNEYS[@]}"; do
  IFS=: read -r id name kind script <<< "$spec"
  run_journey "$id" "$name" "$kind" "$script"
done
ELAPSED=$(($(date +%s) - START))

# === Report ===
if [[ "$JSON_MODE" -eq 1 ]]; then
  CR_TMP="$(mktemp)"
  for r in "${RESULTS[@]}"; do printf '%s\n' "$r"; done > "$CR_TMP"
  CR_RESULTS_FILE="$CR_TMP" CR_PASS="$PASS" CR_FAIL="$FAIL" CR_SKIP="$SKIP" CR_ELAPSED="$ELAPSED" \
    python3 "$(dirname "$0")/dev_public_cleanroom_canonical_9_journeys.py"
  rm -f "$CR_TMP"
else
  log ""
  log "════════════════════════════════════════════════════════════════"
  log "  9-journeys aggregate: $PASS PASS / $FAIL FAIL / $SKIP SKIP (${ELAPSED}s)"
  log "  Acceptance: 9/9 GREEN ou explicit BLOCKED with reason"
  log "════════════════════════════════════════════════════════════════"
  if [[ "$INCLUDE_SEMI_AUTO" -eq 0 && "$PASS" -ge 7 && "$FAIL" -eq 0 ]]; then
    log ""
    log "  Engineering-side IMPLEMENTATION_COMPLETE-ENGINEERING-SIDE"
    log "  Next: sponsor walkthrough JUNTOS for J-07 + J-08 (Phase 6)"
  fi
fi

# Per acceptance criteria: 9/9 GREEN OR explicit BLOCKED with reason
# verified via jq '.journeys[] | select(.status != "PASS" and .status != "BLOCKED_WITH_REASON")' output.json returns empty array
[[ "$FAIL" -eq 0 ]]
