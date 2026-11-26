#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopRuntime Authors
#
# drill-jwt-diagnose.sh — Diagnose JWT 401 root cause between drill Hydra
# OAuth2 server and loop-runtime-api validator.
#
# Plan: 2026-05-19-drill-skills-rigor-restoration-plan T1.1
#
# READ-ONLY: does NOT mutate drill stack. Safe to run repeatedly.
# Idempotent: re-runs do not corrupt state.
#
# Assumes drill stack is LIVE (cleanroom-drill PASS or operator-provisioned).
# If API_URL is not set or loop-runtime-api unreachable, exits early with finding.
#
# Output: /tmp/drill-jwt-diag-<timestamp>/
#   header.json       — JWT header decoded
#   payload.json      — JWT payload decoded
#   api-config.txt    — loop-runtime-api JWT_ISSUER + JWKS_URL env vars
#   request.txt       — curl /projects status + correlation_id
#   api-logs.txt      — loop-runtime-api logs grep by correlation_id
#   jwks-probe.txt    — JWKS endpoint reachability from loop-runtime-api pod
#   diagnosis.md      — side-by-side comparison + category A-F classification
#
# Categories (from plan T1.2):
#   A — Issuer mismatch (payload.iss != loop-runtime-api JWT_ISSUER)
#   B — Audience mismatch (payload.aud != "loop-runtime-engine")
#   C — JWKS URL unreachable from loop-runtime-api pod
#   D — Token expired (exp < now)
#   E — Signature algorithm mismatch (RS256 vs ES256, etc.)
#   F — Combination or novel

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=infra/scripts/lib/drill-skill-common.sh
source "$SCRIPT_DIR/../lib/drill-skill-common.sh"

# Test-mode guard: when sourced for hermetic testing, skip top-level execution
# and only expose function definitions. Tests set this env var.
if [[ "${_DRILL_JWT_DIAGNOSE_TEST_MODE:-0}" == "1" ]]; then
  : # Skip top-level execution; functions below are still defined.
fi

# --- decode_jwt_segment ---
# Extracts and decodes the Nth segment (0=header, 1=payload) of a JWT.
# Handles base64url -> base64 conversion + padding restoration.
# Stdin: JWT token (single line)
# Args:  segment_index (0 or 1)
# Stdout: decoded JSON (raw)
# Exit:   0 on success, 1 on invalid input
decode_jwt_segment() {
  local segment_index="$1"
  local jwt
  read -r jwt
  if [[ -z "$jwt" ]]; then
    return 1
  fi
  # Validate JWT shape: must have 2 dots (3 segments: header.payload.signature).
  # Defensive guard against malformed input (e.g., mint script failure producing
  # plain error string). Without this, decoder would emit binary garbage silently.
  if [[ "$jwt" != *.*.* ]]; then
    return 1
  fi

  local segment
  segment=$(echo -n "$jwt" | cut -d. -f$((segment_index + 1)))
  if [[ -z "$segment" ]]; then
    return 1
  fi

  # base64url -> base64: replace - with +, _ with /
  segment="${segment//-/+}"
  segment="${segment//_//}"

  # Restore padding (base64 requires length multiple of 4)
  local pad_needed=$(( (4 - ${#segment} % 4) % 4 ))
  local padding=""
  while [[ $pad_needed -gt 0 ]]; do
    padding+="="
    pad_needed=$((pad_needed - 1))
  done
  segment="${segment}${padding}"

  echo -n "$segment" | base64 -d 2>/dev/null
}

# --- decode_jwt_header ---
# Convenience wrapper for header (segment 0).
# Args: jwt_token
# Stdout: header JSON
decode_jwt_header() {
  echo -n "$1" | decode_jwt_segment 0
}

# --- decode_jwt_payload ---
# Convenience wrapper for payload (segment 1).
# Args: jwt_token
# Stdout: payload JSON
decode_jwt_payload() {
  echo -n "$1" | decode_jwt_segment 1
}

# --- extract_jwt_claim ---
# Extract a single claim from JWT payload using jq.
# Args: jwt_token, claim_name
# Stdout: claim value (raw, no quotes) or empty on error
extract_jwt_claim() {
  local jwt="$1"
  local claim="$2"
  if ! command -v jq >/dev/null 2>&1; then
    return 1
  fi
  decode_jwt_payload "$jwt" | jq -r ".${claim} // \"\"" 2>/dev/null
}

# --- get_loop_runtime_api_env_vars ---
# Read JWT_ISSUER and JWKS_URL from the running loop-runtime-api pod.
# Args: mgmt_context, namespace (default loop-runtime-system)
# Stdout: KEY=value lines
get_loop_runtime_api_env_vars() {
  local ctx="$1"
  local ns="${2:-loop-runtime-system}"
  local keys=("JWT_ISSUER" "JWKS_URL")
  local pod
  pod=$(kubectl --context "$ctx" get pod -n "$ns" -l app=loop-runtime-api \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null) || return 1
  if [[ -z "$pod" ]]; then
    return 1
  fi
  local key val
  for key in "${keys[@]}"; do
    val=$(kubectl --context "$ctx" exec -n "$ns" "$pod" -- printenv "$key" 2>/dev/null || echo "")
    echo "${key}=${val}"
  done
}

# --- probe_jwks ---
# Probe JWKS URL from inside the loop-runtime-api pod (validates DNS + reachability).
# Args: mgmt_context, namespace, jwks_url
# Stdout: HTTP status + body preview
probe_jwks() {
  local ctx="$1"
  local ns="$2"
  local url="$3"
  local pod
  pod=$(kubectl --context "$ctx" get pod -n "$ns" -l app=loop-runtime-api \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null) || return 1
  if [[ -z "$pod" ]]; then
    echo "ERROR: loop-runtime-api pod not found in $ns"
    return 1
  fi
  kubectl --context "$ctx" exec -n "$ns" "$pod" -- \
    curl -sS -o - -w "\nHTTP_STATUS=%{http_code}\n" --max-time 10 "$url" 2>&1 | head -50
}

# --- capture_api_logs ---
# Capture recent loop-runtime-api logs filtered by correlation_id.
# Args: mgmt_context, namespace, correlation_id, tail_lines (default 200)
# Stdout: matching log lines
capture_api_logs() {
  local ctx="$1"
  local ns="$2"
  local cid="$3"
  local tail_n="${4:-200}"
  kubectl --context "$ctx" logs -n "$ns" -l app=loop-runtime-api --tail="$tail_n" 2>/dev/null | \
    grep -F "$cid" 2>/dev/null || echo "(no log lines matched correlation_id=$cid)"
}

# --- classify_root_cause ---
# Compare payload claims against api config + JWKS probe + log signals.
# Args: payload_file, api_config_file, jwks_probe_file, api_logs_file
# Stdout: one of A/B/C/D/E/F + reasoning
classify_root_cause() {
  local payload_f="$1"
  local config_f="$2"
  local jwks_f="$3"
  local logs_f="$4"

  if ! command -v jq >/dev/null 2>&1; then
    echo "F: jq not available — cannot classify"
    return 0
  fi

  local payload_iss payload_aud payload_exp now
  payload_iss=$(jq -r '.iss // ""' < "$payload_f" 2>/dev/null)
  payload_aud=$(jq -r '.aud // "" | if type=="array" then .[0] else . end' < "$payload_f" 2>/dev/null)
  payload_exp=$(jq -r '.exp // 0' < "$payload_f" 2>/dev/null)
  now=$(date +%s)

  local api_issuer api_jwks
  api_issuer=$(grep "^JWT_ISSUER=" "$config_f" 2>/dev/null | cut -d= -f2-)
  api_jwks=$(grep "^JWKS_URL=" "$config_f" 2>/dev/null | cut -d= -f2-)

  # Category D — expired
  if [[ "$payload_exp" -gt 0 && "$payload_exp" -lt "$now" ]]; then
    echo "D: token expired (exp=$payload_exp now=$now)"
    return 0
  fi

  # Category A — issuer mismatch
  if [[ "$payload_iss" != "$api_issuer" ]]; then
    echo "A: issuer mismatch (jwt.iss='$payload_iss' vs loop-runtime-api.JWT_ISSUER='$api_issuer')"
    return 0
  fi

  # Category B — audience mismatch
  if [[ "$payload_aud" != "loop-runtime-engine" ]]; then
    echo "B: audience mismatch (jwt.aud='$payload_aud' vs expected 'loop-runtime-engine')"
    return 0
  fi

  # Category C — JWKS unreachable
  if grep -q "HTTP_STATUS=000\|HTTP_STATUS=5\|connection refused\|no route" "$jwks_f" 2>/dev/null; then
    echo "C: JWKS unreachable (JWKS_URL=$api_jwks) — see jwks-probe.txt"
    return 0
  fi

  # Category E — algorithm signal from logs
  if grep -qi "signature.*invalid\|alg.*not.*supported\|algorithm mismatch" "$logs_f" 2>/dev/null; then
    echo "E: signature algorithm mismatch — see api-logs.txt"
    return 0
  fi

  # Default
  echo "F: no single category matched — manual review required (see all output files)"
}

# --- emit_diagnosis_report ---
# Write structured diagnosis.md combining all captures.
# Args: output_dir, category, payload_file, config_file
emit_diagnosis_report() {
  local out_dir="$1"
  local category="$2"
  local payload_f="$3"
  local config_f="$4"
  local diag_f="$out_dir/diagnosis.md"

  {
    echo "# Drill JWT Diagnosis — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo ""
    echo "**Category:** $category"
    echo ""
    echo "## Inputs"
    echo ""
    echo "### JWT payload claims [MEASURED]"
    echo '```json'
    cat "$payload_f" 2>/dev/null || echo "(payload capture failed)"
    echo '```'
    echo ""
    echo "### loop-runtime-api env vars [MEASURED]"
    echo '```'
    cat "$config_f" 2>/dev/null || echo "(api config capture failed)"
    echo '```'
    echo ""
    echo "## Remediation path"
    echo ""
    echo "Refer to plan T2.1 file-mapping by category:"
    echo "- **A (issuer):** edit \`infra/helmfile/values-ory-hydra-drill.yaml.gotmpl\` (urls.issuer) OR \`values-loop-runtime-api-drill.yaml.gotmpl\` (JWT_ISSUER env)"
    echo "- **B (audience):** edit \`infra/scripts/drill/drill-mint-jwt.sh\` (audience param) — T3 if changing \`api/cmd/setup_auth.go:60\` hardcoded 'loop-runtime-engine'"
    echo "- **C (JWKS):** edit \`infra/helmfile/values-loop-runtime-api-drill.yaml.gotmpl\` (JWKS_URL with cross-namespace FQDN)"
    echo "- **D (expired):** edit \`infra/scripts/drill/drill-mint-jwt.sh\` (expires_in)"
    echo "- **E (signature):** T3 — auth contract change requires meeting"
    echo "- **F:** manual review"
    echo ""
    echo "## Captures in this run"
    echo ""
    echo "- header.json — JWT header decoded"
    echo "- payload.json — JWT payload decoded"
    echo "- api-config.txt — loop-runtime-api JWT_ISSUER + JWKS_URL"
    echo "- request.txt — curl /projects status + correlation_id"
    echo "- api-logs.txt — loop-runtime-api logs by correlation_id"
    echo "- jwks-probe.txt — JWKS reachability from loop-runtime-api pod"
  } > "$diag_f"
}

# --- main ---
main() {
  drill_skill_load_dotenv
  drill_skill_log "drill-jwt-diagnose: starting (read-only)"

  # Pre-flight (EC-8): drill stack must be LIVE
  if [[ -z "${API_URL:-}" ]]; then
    drill_skill_log "ERROR: API_URL not set — drill stack not provisioned (run /cleanroom-drill first)"
    exit 1
  fi

  local mgmt_ctx="${CORE_CONTEXT:-k8s-drill-mgmt}"
  local mgmt_ns="${MGMT_NAMESPACE:-loop-runtime-system}"

  local ts
  ts=$(date -u +%Y%m%dT%H%M%SZ)
  local out_dir="/tmp/drill-jwt-diag-${ts}"
  mkdir -p "$out_dir"
  drill_skill_log "output: $out_dir"

  # Step 1: mint JWT (no cache — fresh issuance)
  drill_skill_log "step 1: minting fresh JWT"
  rm -f ~/.loop-runtime/drill-token.txt ~/.loop-runtime/drill-token-cache.json 2>/dev/null || true
  local jwt
  jwt=$(bash "$SCRIPT_DIR/drill-mint-jwt.sh" 2>/dev/null) || {
    drill_skill_log "ERROR: drill-mint-jwt.sh failed"
    exit 1
  }
  if [[ -z "$jwt" || "$jwt" != *.*.* ]]; then
    drill_skill_log "ERROR: invalid JWT format"
    exit 1
  fi

  # Step 2: decode header + payload
  drill_skill_log "step 2: decoding JWT"
  decode_jwt_header "$jwt" > "$out_dir/header.json" || true
  decode_jwt_payload "$jwt" > "$out_dir/payload.json" || true

  # Step 3: capture loop-runtime-api env config
  drill_skill_log "step 3: capturing loop-runtime-api env config"
  get_loop_runtime_api_env_vars "$mgmt_ctx" "$mgmt_ns" > "$out_dir/api-config.txt" 2>&1 || \
    echo "(env capture failed)" > "$out_dir/api-config.txt"

  # Step 4: curl /projects + capture
  drill_skill_log "step 4: curl /projects with JWT"
  local req_body req_status cid
  req_body=$(mktemp)
  req_status=$(curl -sS -m 30 \
    -H "Authorization: Bearer $jwt" \
    -H "X-Loop-Runtime-Tenant: ${SEED_TENANT_ID:-tn-e2etest}" \
    -o "$req_body" -w "%{http_code}" \
    "${API_URL}/projects" 2>/dev/null || echo "000")
  cid=$(grep -oE 'correlation[-_]id["\:]*\s*[a-f0-9-]+' "$req_body" 2>/dev/null | head -1 | grep -oE '[a-f0-9-]{8,}' | head -1 || echo "")
  {
    echo "status=$req_status"
    echo "correlation_id=$cid"
    echo "--- body ---"
    cat "$req_body"
  } > "$out_dir/request.txt"
  rm -f "$req_body"

  # Step 5: capture api logs by correlation_id
  drill_skill_log "step 5: capturing api logs"
  if [[ -n "$cid" ]]; then
    capture_api_logs "$mgmt_ctx" "$mgmt_ns" "$cid" 200 > "$out_dir/api-logs.txt" 2>&1
  else
    echo "(no correlation_id captured — fetching last 50 lines)" > "$out_dir/api-logs.txt"
    kubectl --context "$mgmt_ctx" logs -n "$mgmt_ns" -l app=loop-runtime-api --tail=50 \
      >> "$out_dir/api-logs.txt" 2>&1 || true
  fi

  # Step 6: probe JWKS from loop-runtime-api pod
  drill_skill_log "step 6: probing JWKS reachability"
  local jwks_url
  jwks_url=$(grep "^JWKS_URL=" "$out_dir/api-config.txt" | cut -d= -f2-)
  if [[ -n "$jwks_url" ]]; then
    probe_jwks "$mgmt_ctx" "$mgmt_ns" "$jwks_url" > "$out_dir/jwks-probe.txt" 2>&1
  else
    echo "(JWKS_URL not set in api config)" > "$out_dir/jwks-probe.txt"
  fi

  # Step 7: classify + emit diagnosis.md
  drill_skill_log "step 7: classifying root cause"
  local category
  category=$(classify_root_cause "$out_dir/payload.json" "$out_dir/api-config.txt" \
    "$out_dir/jwks-probe.txt" "$out_dir/api-logs.txt")
  emit_diagnosis_report "$out_dir" "$category" "$out_dir/payload.json" "$out_dir/api-config.txt"

  drill_skill_log "diagnosis complete: $category"
  drill_skill_log "see: $out_dir/diagnosis.md"
}

if [[ "${_DRILL_JWT_DIAGNOSE_TEST_MODE:-0}" != "1" ]]; then
  main "$@"
fi
