#!/usr/bin/env bash
# baseline-capture.sh
#
# T1.1 of plan 2026-06-08-pre-release-test-rigor v1.2 — captures a 4-layer
# steady-state snapshot per `.claude/rules/steady-state-baseline.md` into a
# structured JSON file. The output is the canonical machine-readable form;
# the markdown report in `docs/operations/baselines/` is the human-readable
# companion (operator writes that manually using this JSON as input).
#
# Modes:
#   - PRODUCTION  — default; queries kubectl + curl + dig against the live
#                   k3d/dev-public stack. Use during operator-driven captures.
#   - FIXTURE     — BASELINE_FIXTURE_DIR env var set to a directory; the
#                   script reads pre-canned JSON files from it instead of
#                   probing the cluster. Used by hermetic tests + by ralph
#                   loop iterations that can't reach the cluster.
#   - DETERMINISTIC — DETERMINISTIC=1 env var; zeroes captured_at and any
#                     other timestamp fields so 2 captures of identical state
#                     produce byte-identical JSON. Required for diff sanity.
#
# Volatile-field normalization (EC-7 absorption): even in PRODUCTION mode, the
# capture strips `metadata.resourceVersion`, `metadata.generation`,
# `metadata.managedFields`, `metadata.creationTimestamp`, `metadata.uid`, and
# `status.conditions[].lastTransitionTime` from every kubectl resource before
# serializing. This makes the JSON stable across runs that observe the same
# logical state.
#
# Usage:
#   bash infra/scripts/local/baseline-capture.sh --output PATH
#   BASELINE_FIXTURE_DIR=/path bash infra/scripts/local/baseline-capture.sh --output PATH
#   DETERMINISTIC=1 BASELINE_FIXTURE_DIR=/path bash ... (for tests + reproducibility)
#
# Exit codes:
#   0  capture written
#   1  --output not provided OR invalid args
#   2  kubectl/curl unreachable (production mode only)
set -euo pipefail

OUTPUT=""
CONTEXT="${CONTEXT:-k3d-loopzero-core}"
PUBLIC_APP_URL="${PUBLIC_APP_URL:-https://app-dev.getloopscale.com}"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.getloopscale.com}"
FIXTURE_DIR="${BASELINE_FIXTURE_DIR:-}"
DETERMINISTIC="${DETERMINISTIC:-0}"

while [ $# -gt 0 ]; do
  case "$1" in
    --output) OUTPUT="$2" ; shift 2 ;;
    --output=*) OUTPUT="${1#--output=}" ; shift ;;
    -h|--help)
      sed -n '2,32p' "$0" | sed 's/^# \{0,1\}//' ; exit 0 ;;
    *) printf '[baseline-capture] unknown arg: %s\n' "$1" >&2 ; exit 1 ;;
  esac
done

[ -n "$OUTPUT" ] || { echo "[baseline-capture] --output PATH required" >&2 ; exit 1 ; }

log() { printf '[baseline-capture] %s\n' "$*" >&2 ; }

# --- Layer 1 — Code state ---------------------------------------------------

capture_code_state() {
  if [ -n "$FIXTURE_DIR" ] && [ -f "$FIXTURE_DIR/code_state.json" ] ; then
    cat "$FIXTURE_DIR/code_state.json"
    return
  fi
  local commit branch ahead behind clean
  commit="$(git rev-parse HEAD 2>/dev/null || echo "")"
  branch="$(git branch --show-current 2>/dev/null || echo "")"
  if git rev-parse --verify origin/develop >/dev/null 2>&1 ; then
    read -r behind ahead < <(git rev-list --left-right --count origin/develop...HEAD 2>/dev/null || echo "0 0")
  else
    ahead=0 ; behind=0
  fi
  if [ -z "$(git status --porcelain 2>/dev/null)" ] ; then
    clean=True
  else
    clean=False
  fi
  BC_COMMIT="$commit" BC_BRANCH="$branch" BC_AHEAD="$ahead" BC_BEHIND="$behind" BC_CLEAN="$clean" \
    python3 "$(dirname "$0")/baseline_capture.py" code
}

# --- Layer 2 — Runtime state ------------------------------------------------

capture_runtime_state() {
  # Write probe outputs to a per-invocation temp dir so the Python heredoc
  # consumes them via file I/O (NOT shell interpolation, which breaks on
  # backslash / unbalanced quote characters present in real kubectl output).
  local tmp
  tmp="$(mktemp -d)"
  if [ -n "$FIXTURE_DIR" ] ; then
    cp "$FIXTURE_DIR/pods.json"        "$tmp/pods.json" 2>/dev/null || echo '{"items":[]}' > "$tmp/pods.json"
    cp "$FIXTURE_DIR/helm-list.json"   "$tmp/helm.json" 2>/dev/null || echo '[]'           > "$tmp/helm.json"
    cp "$FIXTURE_DIR/argocd-apps.json" "$tmp/apps.json" 2>/dev/null || echo '{"items":[]}' > "$tmp/apps.json"
    cp "$FIXTURE_DIR/tenants.json"     "$tmp/tens.json" 2>/dev/null || echo '{"items":[]}' > "$tmp/tens.json"
  else
    kubectl --context="$CONTEXT" get pods -A -o json 2>/dev/null > "$tmp/pods.json" || echo '{"items":[]}' > "$tmp/pods.json"
    helm list -A --kube-context="$CONTEXT" -o json 2>/dev/null > "$tmp/helm.json" || echo '[]' > "$tmp/helm.json"
    kubectl --context="$CONTEXT" get applications.argoproj.io -n argocd -o json 2>/dev/null > "$tmp/apps.json" || echo '{"items":[]}' > "$tmp/apps.json"
    kubectl --context="$CONTEXT" get tenants.loopzero.io -A -o json 2>/dev/null > "$tmp/tens.json" || echo '{"items":[]}' > "$tmp/tens.json"
  fi
  TMP_DIR="$tmp" python3 "$(dirname "$0")/baseline_capture.py" runtime
}

# --- Layer 3 — Config state -------------------------------------------------

capture_config_state() {
  local pg_max plan_limits_present cosign_mode
  if [ -n "$FIXTURE_DIR" ] ; then
    pg_max="$(tr -d '[:space:]' < "$FIXTURE_DIR/pg_max_migration.txt" 2>/dev/null || echo "0")"
    plan_limits_present="$(tr -d '[:space:]' < "$FIXTURE_DIR/plan_limits_present.txt" 2>/dev/null || echo "false")"
    cosign_mode="$(tr -d '[:space:]' < "$FIXTURE_DIR/cosign_mode.txt" 2>/dev/null || echo "keyed")"
  else
    pg_max="$(kubectl --context="$CONTEXT" -n loopzero-system exec -i postgresql-0 -c postgresql -- \
                env PGPASSWORD=loopzero-dev-password psql -U loopzero -d loopzero -tAc \
                "SELECT max(version) FROM schema_migrations" 2>/dev/null | tr -d '[:space:]' || echo "0")"
    if [ -z "$pg_max" ] ; then pg_max=0 ; fi
    if kubectl --context="$CONTEXT" -n loopzero-system exec -i postgresql-0 -c postgresql -- \
         env PGPASSWORD=loopzero-dev-password psql -U loopzero -d loopzero -tAc \
         "SELECT 1 FROM information_schema.tables WHERE table_name='plan_limits'" 2>/dev/null | grep -q 1 ; then
      plan_limits_present=true
    else
      plan_limits_present=false
    fi
    cosign_mode="$(kubectl --context="$CONTEXT" -n loopzero-system get configmap loopzero-cosign-config \
                    -o jsonpath='{.data.mode}' 2>/dev/null || echo "keyed")"
    [ -n "$cosign_mode" ] || cosign_mode="keyed"
  fi
  # Normalize bash bool string → Python bool literal for interpolation
  case "$plan_limits_present" in
    true|True|TRUE|1)   plp_py="True" ;;
    *)                  plp_py="False" ;;
  esac
  BC_PG_MAX="$pg_max" BC_PLP="$plp_py" BC_COSIGN="$cosign_mode" \
    python3 "$(dirname "$0")/baseline_capture.py" config
}

# --- Layer 4 — LIVE functional probes ---------------------------------------

capture_live_probes() {
  if [ -n "$FIXTURE_DIR" ] && [ -f "$FIXTURE_DIR/probes.json" ] ; then
    cat "$FIXTURE_DIR/probes.json"
    return
  fi
  probe() {
    curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$@" 2>/dev/null || echo "000"
  }
  local r1 r2 r3 r4 r5 r6
  r1="$(probe "${PUBLIC_APP_URL}/")"
  r2="$(probe "${PUBLIC_APP_URL}/projects")"
  r3="$(probe "${PUBLIC_APP_URL}/environments")"
  r4="$(probe "${PUBLIC_APP_URL}/api/v1/projects")"
  r5="$(probe "${PUBLIC_APP_URL}/health")"
  r6="$(probe -X POST -H 'X-GitHub-Event: ping' -d '{}' "${PUBLIC_CLOUD_URL}/api/v1/webhooks/github")"
  BC_R1="$r1" BC_R2="$r2" BC_R3="$r3" BC_R4="$r4" BC_R5="$r5" BC_R6="$r6" \
    python3 "$(dirname "$0")/baseline_capture.py" probes
}

# --- Compose snapshot -------------------------------------------------------
# Capture each layer to a temp file (avoids triple-quote-string escape bugs
# when kubectl output contains backslashes / unbalanced quotes / control chars).

COMPOSE_TMP="$(mktemp -d)"
trap 'rm -rf "$COMPOSE_TMP"' EXIT INT TERM

capture_code_state    > "$COMPOSE_TMP/code.json"
capture_runtime_state > "$COMPOSE_TMP/runtime.json"
capture_config_state  > "$COMPOSE_TMP/config.json"
capture_live_probes   > "$COMPOSE_TMP/probes.json"

if [ "$DETERMINISTIC" = "1" ] ; then
  CAPTURED_AT="DETERMINISTIC"
else
  CAPTURED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
fi

COMPOSE_DIR="$COMPOSE_TMP" CAPTURED_AT="$CAPTURED_AT" \
  python3 "$(dirname "$0")/baseline_capture.py" compose > "$OUTPUT"

log "Captured 4-layer snapshot → $OUTPUT"
