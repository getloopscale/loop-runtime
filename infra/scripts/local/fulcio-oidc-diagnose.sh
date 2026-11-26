#!/usr/bin/env bash
# fulcio-oidc-diagnose.sh — Diagnose Fulcio K8s OIDC trust chain (M07 keyless).
#
# Per plan v1.1 2026-05-28-M07-fulcio-oidc-trust-to-3-85-plan
# § T0.1. Identifies which scenario (A/B/C/D/GREEN/UNKNOWN) is blocking
# Fulcio /api/v2/signingCert validation in ≤30s.
#
# Verdict:
#   GREEN — all checks pass + Fulcio log "successful identity verification" → exit 0
#   A — anonymous discovery 403 → exit 0 (with verdict=A)
#   B — NetworkPolicy / connectivity → exit 1
#   C — Issuer claim mismatch (token iss not in OIDCIssuers) → exit 2
#   D — TLS CA mount missing (x509 unknown authority) → exit 3
#   UNKNOWN — diagnosis inconclusive → exit 4
#
# EC absorptions:
#   EC-2 (concurrent): flock + random pod-name suffix
#   EC-3 (empty SA token): probe1 validates non-empty before decode
#   EC-4 (cwd independent): BASH_SOURCE absolute path resolve

set -euo pipefail

# EC-4: resolve absolute paths via BASH_SOURCE (cwd-independent)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB_DIR="${SCRIPT_DIR}/lib"

# shellcheck source=lib/fulcio-probes.sh
source "${LIB_DIR}/fulcio-probes.sh"

# ─── Flag parsing ───────────────────────────────────────────────────
DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    --help|-h)
      sed -n '/^# fulcio-oidc-diagnose.sh/,/^$/p' "${BASH_SOURCE[0]}" | sed 's/^# \?//'
      exit 0
      ;;
  esac
done

# ─── EC-2: Concurrent safe via flock ───────────────────────────────
LOCK_FILE="/tmp/fulcio-diagnose.lock"
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  echo "ERROR: Another fulcio-diagnose is running (lock=$LOCK_FILE)" >&2
  exit 5
fi

# Random suffix for all spawned pod names
export PROBE_ID="${PROBE_ID:-$(date +%s)-$$}"
export PROBE_OUT_DIR="${PROBE_OUT_DIR:-/tmp/fulcio-diagnose-out}"
mkdir -p "$PROBE_OUT_DIR"

# ─── Trap cleanup (idempotent) ─────────────────────────────────────
cleanup_done=0
# Invoked via `trap on_exit EXIT INT TERM` — shellcheck does not see invocation by
# trap. BOTH codes because the diagnostic changed number between versions:
# SC2317 in 0.9.x (what the runner's Ubuntu 24.04 ships) and SC2329 in 0.10+.
# Suppressing only one leaves the other environment red.
# shellcheck disable=SC2317,SC2329
on_exit() {
  if [[ "$cleanup_done" -eq 0 && "$DRY_RUN" -eq 0 ]]; then
    cleanup_done=1
    fulcio_probes_cleanup
  fi
}
trap on_exit EXIT INT TERM

# ─── Dry-run: print plan and exit ──────────────────────────────────
if [[ "$DRY_RUN" -eq 1 ]]; then
  cat <<EOF
DRY RUN — would execute:
  1. flock $LOCK_FILE (acquired)
  2. probe1: spawn pod sa-token-probe-${PROBE_ID} in ${ARGO_NS:-argo} ns, read SA token, decode iss/aud
  3. probe2: spawn pod net-probe-${PROBE_ID} in ${SIGSTORE_NS:-sigstore-system} ns, curl discovery + jwks
  4. probe3: patch fulcio-server +--log-level=debug, capture last 200 log lines, classify error
  5. compute verdict (A/B/C/D/GREEN/UNKNOWN)
  6. cleanup: remove --log-level patch + delete probe pods

Output: ${PROBE_OUT_DIR}/{probe1,probe2,probe3}.json + verdict.json
EOF
  exit 0
fi

# ─── Auto-discover BUILD_KUBECONFIG if not set ────────────────────
if [[ -z "${BUILD_KUBECONFIG:-}" ]]; then
  if command -v k3d >/dev/null 2>&1; then
    BUILD_KUBECONFIG=$(k3d kubeconfig write loopruntime-build 2>/dev/null || true)
  fi
fi
if [[ -n "$BUILD_KUBECONFIG" ]]; then
  export KUBECONFIG="$BUILD_KUBECONFIG"
fi

# ─── Execute probes sequentially ──────────────────────────────────
echo "▶ Probe 1: SA token iss claim..." >&2
probe_sa_token_iss

echo "▶ Probe 2: Network discovery (sigstore-system → kube-apiserver)..." >&2
probe_network_discovery

echo "▶ Probe 3: Fulcio debug log capture..." >&2
probe_fulcio_logs

# ─── Compute verdict ──────────────────────────────────────────────
P1="$PROBE_OUT_DIR/probe1-sa-token.json"
P2="$PROBE_OUT_DIR/probe2-network.json"
P3="$PROBE_OUT_DIR/probe3-fulcio-log.json"

match_oidc=$(jq -r '.match_oidcissuer' < "$P1")
discovery=$(jq -r '.discovery_status' < "$P2")
jwks=$(jq -r '.jwks_status' < "$P2")
err_class=$(jq -r '.matched_error_class' < "$P3")

VERDICT="UNKNOWN"
EXIT_CODE=4

# GREEN check: all probes show healthy
if [[ "$match_oidc" == "true" && "$discovery" == "200" && "$jwks" == "200" && "$err_class" == "green" ]]; then
  VERDICT="GREEN"
  EXIT_CODE=0
elif [[ "$match_oidc" == "false" ]]; then
  VERDICT="C"
  EXIT_CODE=2
elif [[ "$discovery" == "403" || "$discovery" == "401" ]]; then
  # Scenario A: anonymous discovery blocked.
  #   - 403 Forbidden = K3s explicit anonymous-auth=false
  #   - 401 Unauthorized = K3s default behavior (system:service-account-issuer-discovery
  #     ClusterRole bound only to system:authenticated, NOT system:unauthenticated).
  #     Fix path identical: add ClusterRoleBinding for system:unauthenticated.
  #     Discovered LIVE 2026-05-28 Ralph iter 4.
  VERDICT="A"
  EXIT_CODE=0
elif [[ "$discovery" != "200" || "$jwks" != "200" ]]; then
  VERDICT="B"
  EXIT_CODE=1
elif [[ "$err_class" == "verify_fail" || "$err_class" == "jwks_fail" ]]; then
  VERDICT="D"
  EXIT_CODE=3
fi

# Write verdict.json with full diagnostic data
jq -n \
  --arg verdict "$VERDICT" \
  --argjson exit "$EXIT_CODE" \
  --slurpfile p1 "$P1" \
  --slurpfile p2 "$P2" \
  --slurpfile p3 "$P3" \
  '{
    scenario: $verdict,
    exit_code: $exit,
    probe1_sa_token: $p1[0],
    probe2_network: $p2[0],
    probe3_fulcio_log: $p3[0]
  }' > "$PROBE_OUT_DIR/verdict.json"

# ─── Report verdict ────────────────────────────────────────────────
echo "" >&2
echo "═══════════════════════════════════════════════════════════" >&2
echo "  VERDICT: ${VERDICT}  (exit ${EXIT_CODE})" >&2
echo "═══════════════════════════════════════════════════════════" >&2
case "$VERDICT" in
  GREEN)   echo "  ✓ Fulcio OIDC trust chain healthy — no fix needed" >&2 ;;
  A)       echo "  → Scenario A: anonymous discovery 403 — create ClusterRoleBinding system:service-account-issuer-discovery" >&2 ;;
  B)       echo "  → Scenario B: NetworkPolicy/connectivity — open egress sigstore-system→kube-apiserver" >&2 ;;
  C)       echo "  → Scenario C: issuer mismatch — add token iss to Fulcio OIDCIssuers config" >&2 ;;
  D)       echo "  → Scenario D: TLS CA mount missing — mount kube-root-ca.crt in Fulcio pod" >&2 ;;
  UNKNOWN) echo "  ? Inconclusive — review $PROBE_OUT_DIR/verdict.json + escalate" >&2 ;;
esac
echo "  Full report: $PROBE_OUT_DIR/verdict.json" >&2

exit "$EXIT_CODE"
