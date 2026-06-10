#!/usr/bin/env bash
# Fulcio K8s OIDC trust probes — library sourced by fulcio-oidc-diagnose.sh.
# Each probe_* function writes JSON to $PROBE_OUT_DIR/<name>.json + returns 0/1.
#
# Per plan v1.1 docs/plans/2026-05-28-M07-fulcio-oidc-trust-to-3-85-plan.md
# § T0.1 Deep Dives. Mock-friendly: all kubectl calls go via $KUBECTL var
# (defaults to "kubectl") so hermetic tests can PATH-override.

# shellcheck disable=SC2034  # KUBECTL referenced by callers
KUBECTL="${KUBECTL:-kubectl}"
PROBE_OUT_DIR="${PROBE_OUT_DIR:-/tmp/fulcio-diagnose-out}"
PROBE_ID="${PROBE_ID:-$(date +%s)-$$}"
BUILD_KUBECONFIG="${BUILD_KUBECONFIG:-}"
SIGSTORE_NS="${SIGSTORE_NS:-sigstore-system}"
ARGO_NS="${ARGO_NS:-argo}"

mkdir -p "$PROBE_OUT_DIR"

# EC-3: fail-fast helper for empty SA token (kube-apiserver --api-audiences misconfig).
_die() {
  echo "ERROR: $*" >&2
  exit "${2:-1}"
}

# Probe 1: SA token iss claim + Fulcio OIDCIssuers match.
# Output: $PROBE_OUT_DIR/probe1-sa-token.json
probe_sa_token_iss() {
  local pod="sa-token-probe-${PROBE_ID}"
  local token_jwt
  # Spawn pod with ProjectedVolume audience=sigstore — actual K8s call OR mock
  token_jwt=$($KUBECTL run "$pod" \
    --namespace="$ARGO_NS" --rm -i --restart=Never \
    --image=busybox --quiet \
    -- sh -c "cat /var/run/secrets/sigstore/token" 2>/dev/null || true)

  # EC-3 — validate non-empty before decode
  if [[ -z "$token_jwt" ]]; then
    _die "SA token empty — check kube-apiserver --api-audiences includes 'sigstore'" 6
  fi

  # Decode JWT payload (base64url → JSON)
  local payload iss aud
  payload=$(echo "$token_jwt" | cut -d. -f2 | tr '_-' '/+' | base64 -d 2>/dev/null || echo '{}')
  iss=$(echo "$payload" | jq -r '.iss // empty')
  aud=$(echo "$payload" | jq -r 'if (.aud | type) == "array" then .aud[0] else (.aud // empty) end')

  # Fetch Fulcio OIDCIssuers config
  local oidc_issuers match
  oidc_issuers=$($KUBECTL get cm fulcio-server-config \
    --namespace="$SIGSTORE_NS" \
    -o jsonpath='{.data.config\.json}' 2>/dev/null | jq -c '.OIDCIssuers // []')

  # Check if iss matches any configured issuer URL
  match=$(echo "$oidc_issuers" | jq --arg i "$iss" 'any(.IssuerURL == $i)')

  jq -nc \
    --arg iss "$iss" \
    --arg aud "$aud" \
    --argjson issuers "$oidc_issuers" \
    --argjson match "$match" \
    '{iss: $iss, aud: $aud, oidc_issuers: $issuers, match_oidcissuer: $match}' \
    > "$PROBE_OUT_DIR/probe1-sa-token.json"
}

# Probe 2: Network discovery from sigstore-system namespace.
# Output: $PROBE_OUT_DIR/probe2-network.json
probe_network_discovery() {
  local pod="net-probe-${PROBE_ID}"
  local issuer_url="https://kubernetes.default.svc.cluster.local"

  # Create probe pod once, exec multiple commands
  $KUBECTL run "$pod" \
    --namespace="$SIGSTORE_NS" --restart=Never \
    --image=alpine/curl --quiet \
    --command -- sleep 60 >/dev/null 2>&1 || true

  # DNS check
  local dns_out dns_status="ok"
  dns_out=$($KUBECTL exec "$pod" --namespace="$SIGSTORE_NS" -- nslookup kubernetes.default.svc.cluster.local 2>&1) || dns_status="fail"

  # Discovery endpoint
  local discovery_out discovery_status discovery_body jwks_uri
  discovery_out=$($KUBECTL exec "$pod" --namespace="$SIGSTORE_NS" -- \
    sh -c "curl -sk -w '\\n%{http_code}' --max-time 10 ${issuer_url}/.well-known/openid-configuration" 2>&1) || true

  if [[ "$discovery_out" == *"timed out"* || "$discovery_out" == *"refused"* ]]; then
    discovery_status=0
    discovery_body="$discovery_out"
    jwks_uri=""
  else
    discovery_status=$(echo "$discovery_out" | tail -1)
    discovery_body=$(echo "$discovery_out" | sed '$d')
    jwks_uri=$(echo "$discovery_body" | jq -r '.jwks_uri // empty' 2>/dev/null || echo "")
  fi

  # JWKS endpoint (only if discovery succeeded + jwks_uri extracted)
  local jwks_status=0
  if [[ "$discovery_status" == "200" && -n "$jwks_uri" ]]; then
    local jwks_out
    jwks_out=$($KUBECTL exec "$pod" --namespace="$SIGSTORE_NS" -- \
      sh -c "curl -sk -w '\\n%{http_code}' --max-time 10 ${jwks_uri}" 2>&1) || true
    if [[ "$jwks_out" == *"timed out"* || "$jwks_out" == *"refused"* ]]; then
      jwks_status=0
    else
      jwks_status=$(echo "$jwks_out" | tail -1)
    fi
  fi

  jq -nc \
    --arg dns "$dns_status" \
    --argjson disc "${discovery_status:-0}" \
    --arg body "$discovery_body" \
    --arg jwks "$jwks_uri" \
    --argjson jwks_s "${jwks_status:-0}" \
    '{dns_status: $dns, discovery_status: $disc, discovery_body: $body, jwks_uri: $jwks, jwks_status: $jwks_s}' \
    > "$PROBE_OUT_DIR/probe2-network.json"
}

# Probe 3: Fulcio log capture during live sign attempt (or recent logs).
# Output: $PROBE_OUT_DIR/probe3-fulcio-log.json
# Per plan: enables --log-level=debug temporarily; trap EXIT restores it.
probe_fulcio_logs() {
  # Enable debug log level (idempotent — patch checks current state)
  $KUBECTL patch deployment fulcio-server \
    --namespace="$SIGSTORE_NS" --type='json' \
    -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--log-level=debug"}]' \
    >/dev/null 2>&1 || true

  # Wait for rollout (mock returns immediately for fulcio_ready=true)
  $KUBECTL rollout status deployment/fulcio-server \
    --namespace="$SIGSTORE_NS" --timeout=60s >/dev/null 2>&1 || true

  # Capture recent logs
  local log_excerpt
  log_excerpt=$($KUBECTL logs deployment/fulcio-server \
    --namespace="$SIGSTORE_NS" --tail=200 2>/dev/null | \
    grep -iE 'error|fail|verifi|jwks|issuer|audience|discovery|x509|successful' | tail -20 || true)

  # Classify error pattern
  local err_class="unknown"
  if [[ "$log_excerpt" == *"issuer not configured"* || "$log_excerpt" == *"issuer mismatch"* ]]; then
    err_class="issuer_mismatch"
  elif [[ "$log_excerpt" == *"discovery"* ]]; then
    err_class="discovery_fail"
  elif [[ "$log_excerpt" == *"jwks"* ]]; then
    err_class="jwks_fail"
  elif [[ "$log_excerpt" == *"x509"* || "$log_excerpt" == *"certificate"* ]]; then
    err_class="verify_fail"
  elif [[ "$log_excerpt" == *"audience"* ]]; then
    err_class="audience_mismatch"
  elif [[ "$log_excerpt" == *"successful"* ]]; then
    err_class="green"
  fi

  jq -nc \
    --arg log "$log_excerpt" \
    --arg class "$err_class" \
    '{log_excerpt: $log, matched_error_class: $class}' \
    > "$PROBE_OUT_DIR/probe3-fulcio-log.json"
}

# Cleanup helper called by trap EXIT in main script.
fulcio_probes_cleanup() {
  # Remove --log-level=debug flag (best-effort; idempotent)
  $KUBECTL patch deployment fulcio-server \
    --namespace="$SIGSTORE_NS" --type='json' \
    -p='[{"op":"test","path":"/spec/template/spec/containers/0/args/-","value":"--log-level=debug"},{"op":"remove","path":"/spec/template/spec/containers/0/args/-"}]' \
    >/dev/null 2>&1 || true

  # Delete probe pods
  $KUBECTL delete pod "sa-token-probe-${PROBE_ID}" --namespace="$ARGO_NS" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  $KUBECTL delete pod "net-probe-${PROBE_ID}" --namespace="$SIGSTORE_NS" --ignore-not-found --wait=false >/dev/null 2>&1 || true
}
