#!/usr/bin/env bash
# T4.1 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
#
# Master orchestrator for `task check` sprint canonical. Aggregates 8 check
# categories (~44 probes total) into one structured PASS/WARN/FAIL report.
# Closes operator-friction gap: without this, post-Cleanroom validation
# requires hand-running 8+ separate diagnostic scripts.
#
# Per ADR D3: COMPLEMENTAR a `task doctor`, NOT substituto. doctor validates
# K8s infra; check sprint validates LoopZero canonical state (helmfile rendered
# correctly, configmaps match contract, networking cross-cluster, auth
# chain wired, webhook secrets present, observability stack alive,
# integrity gates green, cleanroom guards healthy).
#
# Per ADR D4: --fail-fast vs continue. Default continue-with-warnings;
# Cleanroom Phase 4.5 invokes with --fail-fast (canonical).
#
# Per ADR D9: setup chain wraps `task check || echo "[WARN] ...; true"`
# (non-fatal); Cleanroom skill invokes diretamente (fail-fast canonical).
#
# Flags:
#   --hermetic         skip C2-C8 (require kubectl); C1 only (CI mode)
#   --fail-fast        exit immediately on first FAIL
#   --json             emit machine-readable JSON to stdout
#   --category=NAME    run only one category (c1..c8)
#   --help             show usage
#
# Allowlist entry: KEEP-OPERATOR (live-cluster orchestration).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CHECK_DIR="$SCRIPT_DIR/check-sprint"

HERMETIC=0
FAIL_FAST=0
JSON_MODE=0
ONLY_CATEGORY=""

for arg in "$@"; do
  case "$arg" in
    --hermetic) HERMETIC=1 ;;
    --fail-fast) FAIL_FAST=1 ;;
    --json) JSON_MODE=1 ;;
    --category=*) ONLY_CATEGORY="${arg#--category=}" ;;
    --help)
      sed -n '1,30p' "$0" | grep '^#' | sed 's/^# \?//'
      exit 0
      ;;
  esac
done

# 8 canonical check categories per plan T4.1
CATEGORIES=(
  "c1:helmfile-rendering:hermetic:Validate per-env Helmfile overlays render canonical state"
  "c2:configmaps:kubectl:Validate Kratos+Hydra+loopzero-cloud ConfigMaps match canonical contract"
  "c3:networking:kubectl:Validate cross-cluster DNS + ingress + port-forwards"
  "c4:auth:kubectl:Validate Kratos+Hydra running + JWT issuer reachable"
  "c5:webhook:kubectl:Validate loopzero-github-webhook Secret + cloud handler wiring"
  "c6:observability:kubectl:Validate Promtail→Loki pipeline + Prometheus scraping"
  "c7:integrity:hermetic:Validate hermetic CI gates GREEN (T3.x)"
  "c8:cleanroom-guards:kubectl:Validate Cleanroom anti-pattern guards (Tilt stale, etc)"
)

# Aggregate counters
TOTAL_PASS=0
TOTAL_WARN=0
TOTAL_FAIL=0
TOTAL_SKIP=0
declare -a RESULTS=()

log() { [[ "$JSON_MODE" -eq 0 ]] && printf '[check-sprint] %s\n' "$*" >&2; }

run_category() {
  local cat_id="$1" cat_name="$2" cat_kind="$3" cat_desc="$4"
  local sub_script="$CHECK_DIR/${cat_id}-${cat_name}.sh"

  # EC-11: kubectl reachability gate
  if [[ "$cat_kind" == "kubectl" && "$HERMETIC" -eq 1 ]]; then
    log "[SKIP] $cat_id ($cat_name) — hermetic mode, kubectl-required"
    TOTAL_SKIP=$((TOTAL_SKIP + 1))
    RESULTS+=("$cat_id:SKIP:hermetic mode")
    return
  fi

  if [[ "$cat_kind" == "kubectl" ]]; then
    if ! kubectl --context "${CHECK_CONTEXT:-k3d-loopzero-mgmt}" get nodes >/dev/null 2>&1; then
      log "[SKIP-WARN] $cat_id ($cat_name) — kubectl context unreachable"
      TOTAL_WARN=$((TOTAL_WARN + 1))
      RESULTS+=("$cat_id:WARN:kubectl context unreachable")
      return
    fi
  fi

  if [[ ! -f "$sub_script" ]]; then
    log "[WARN] $cat_id ($cat_name) — sub-script $sub_script missing"
    TOTAL_WARN=$((TOTAL_WARN + 1))
    RESULTS+=("$cat_id:WARN:sub-script missing")
    return
  fi

  log "[RUN] $cat_id $cat_name — $cat_desc"
  if bash "$sub_script" 2>&1 | tail -1 | grep -q "^PASS"; then
    log "[PASS] $cat_id"
    TOTAL_PASS=$((TOTAL_PASS + 1))
    RESULTS+=("$cat_id:PASS:OK")
  else
    log "[FAIL] $cat_id"
    TOTAL_FAIL=$((TOTAL_FAIL + 1))
    RESULTS+=("$cat_id:FAIL:see logs")
    if [[ "$FAIL_FAST" -eq 1 ]]; then
      log "[FAIL-FAST] aborting after $cat_id failure"
      # `return 2`, e nao `break`: nao ha loop DENTRO desta funcao. O `break`
      # anterior quebrava o loop do CHAMADOR — o bash permite, e por isso o
      # --fail-fast funcionava, mas por acidente: chamar run_category fora de um
      # loop tornaria o break um erro em runtime. SC2104 (severidade error).
      # 2 e' o codigo de "abortar"; quem decide parar e' o loop chamador.
      return 2
    fi
  fi
  return 0
}

# === Main execution ===
START=$(date +%s)

for spec in "${CATEGORIES[@]}"; do
  IFS=: read -r cat_id cat_name cat_kind cat_desc <<< "$spec"
  if [[ -n "$ONLY_CATEGORY" && "$ONLY_CATEGORY" != "$cat_id" ]]; then continue; fi
  # rc 2 = run_category pediu abortar (--fail-fast). Sem este teste o
  # --fail-fast deixaria de funcionar EM SILENCIO ao trocar o break por return —
  # o loop seguiria para a proxima categoria e a flag viraria decoracao.
  run_category "$cat_id" "$cat_name" "$cat_kind" "$cat_desc" && rc=0 || rc=$?
  if [[ "$rc" -eq 2 ]]; then break; fi
done

END=$(date +%s)
ELAPSED=$((END - START))

# === Report ===
if [[ "$JSON_MODE" -eq 1 ]]; then
  CS_TMP="$(mktemp)"
  for r in "${RESULTS[@]}"; do printf '%s\n' "$r"; done > "$CS_TMP"
  CS_RESULTS_FILE="$CS_TMP" CS_PASS="$TOTAL_PASS" CS_WARN="$TOTAL_WARN" \
    CS_FAIL="$TOTAL_FAIL" CS_SKIP="$TOTAL_SKIP" CS_ELAPSED="$ELAPSED" \
    python3 "$(dirname "$0")/check_sprint.py"
  rm -f "$CS_TMP"
else
  log ""
  log "════════════════════════════════════════════════════════"
  log "  task check sprint — PASS=$TOTAL_PASS WARN=$TOTAL_WARN FAIL=$TOTAL_FAIL SKIP=$TOTAL_SKIP (${ELAPSED}s)"
  log "════════════════════════════════════════════════════════"
fi

# Exit code: 0 only if FAIL=0 per acceptance criteria
[[ "$TOTAL_FAIL" -eq 0 ]]
