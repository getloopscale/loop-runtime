#!/usr/bin/env bash
# check-infra.sh — Post-cleanroom validation that EVERY canonical component is
# in the RIGHT cluster + namespace + configured with REAL (non-placeholder)
# state, beyond what `task doctor` covers.
#
# Plan: 2026-06-04-dev-public-docr-fix-forward T6+ (sponsor request 2026-06-04).
#
# WHY this exists separately from `task doctor`:
#   - `doctor` checks "is the pod Running?" — fast smoke after setup.
#   - `check:infra` checks "is the pod in the RIGHT cluster + the secret
#      holds a REAL value, not the default placeholder?" — catches the
#      class of bugs that fooled cleanroom 2026-06-04:
#        (a) loop-runtime-cluster-kubeconfigs build slot pointing at mgmt
#            (kubernetes.default.svc) → Workflow CR submitted to
#            cluster that has no Workflow CRD.
#        (b) gitea-admin.token holding the 32-char chart default
#            instead of a 40-char real API token with required scopes.
#        (c) minio-credentials.secretkey out of sync with MinIO
#            rootPassword → SignatureDoesNotMatch.
#        (d) operator chart networkPolicy.extraEgress pointing at
#            namespace `gitea` when gitea is in `loop-runtime-system`.
#
# Each check has THREE possible outcomes:
#   PASS — placement + config canonical
#   FAIL — placement wrong OR config holds known-bad placeholder
#   WARN — placement OK but value cannot be verified (e.g. no live probe)
#
# Exit: 0 if all PASS or only WARN; 1 if any FAIL.
#
# Output: human-readable to stderr, JSON summary to stdout when --json passed.

set -uo pipefail

JSON_MODE=0
[[ "${1:-}" == "--json" ]] && JSON_MODE=1

MGMT_CTX="${MGMT_CTX:-k3d-loop-runtime-core}"
BUILD_CTX="${BUILD_CTX:-k3d-loop-runtime-build}"
WORKLOAD_CTX="${WORKLOAD_CTX:-k3d-loop-runtime-cell-0}"
RUNTIME2_CTX="${RUNTIME2_CTX:-k3d-loop-runtime-cell-1}"

PASS=0
FAIL=0
WARN=0
RESULTS=()

record() {
  local id="$1" status="$2" evidence="$3"
  case "$status" in
    PASS) PASS=$((PASS+1)); echo "  [PASS] $id — $evidence" >&2 ;;
    FAIL) FAIL=$((FAIL+1)); echo "  [FAIL] $id — $evidence" >&2 ;;
    WARN) WARN=$((WARN+1)); echo "  [WARN] $id — $evidence" >&2 ;;
  esac
  RESULTS+=("{\"id\":\"$id\",\"status\":\"$status\",\"evidence\":\"$(printf '%s' "$evidence" | sed 's/"/\\"/g')\"}")
}

heading() { echo "" >&2; echo "=== $1 ===" >&2; }

# -----------------------------------------------------------------------------
# C1 — Component placement (right cluster + right namespace)
# -----------------------------------------------------------------------------
heading "C1: Component placement"

check_in_cluster() {
  # $1 id, $2 ctx, $3 ns, $4 deploy_or_sts name, $5 kind (deploy|sts|svc)
  local id="$1" ctx="$2" ns="$3" name="$4" kind="${5:-deploy}"
  if kubectl --context "$ctx" -n "$ns" get "$kind" "$name" >/dev/null 2>&1; then
    record "$id" "PASS" "$kind/$name present in $ctx/$ns"
  else
    record "$id" "FAIL" "$kind/$name MISSING in $ctx/$ns (placement bug — check helmfile target context)"
  fi
}

# Mgmt cluster: control plane (loop-runtime-api, operators, gitea, kratos, hydra, postgres)
check_in_cluster C1.1  "$MGMT_CTX" loop-runtime-system loop-runtime-api                 deploy
check_in_cluster C1.2  "$MGMT_CTX" loop-runtime-system loop-runtime-tenant-operator     deploy
check_in_cluster C1.3  "$MGMT_CTX" loop-runtime-system loop-runtime-app-operator        deploy
check_in_cluster C1.4  "$MGMT_CTX" loop-runtime-system loop-runtime-platform-operator   deploy
check_in_cluster C1.5  "$MGMT_CTX" loop-runtime-system loop-runtime-preview-operator    deploy
check_in_cluster C1.6  "$MGMT_CTX" loop-runtime-system gitea                    deploy
check_in_cluster C1.7  "$MGMT_CTX" loop-runtime-system minio                    deploy
check_in_cluster C1.8  "$MGMT_CTX" loop-runtime-auth   ory-kratos               deploy
check_in_cluster C1.9  "$MGMT_CTX" loop-runtime-auth   ory-hydra                deploy
check_in_cluster C1.10 "$MGMT_CTX" loop-runtime-system postgresql               sts
check_in_cluster C1.11 "$MGMT_CTX" argocd      argocd-server            deploy

# Build cluster: argo-workflows + argo-events + buildkit (clone/packs/cosign/syft via templates)
check_in_cluster C1.12 "$BUILD_CTX" argo        argo-workflows-server                deploy
check_in_cluster C1.13 "$BUILD_CTX" argo        argo-workflows-workflow-controller   deploy
check_in_cluster C1.14 "$BUILD_CTX" argo-events argo-events-controller-manager       deploy

# Workload cluster: argo-rollouts + traefik + cert-manager + external-secrets + keda
check_in_cluster C1.15 "$WORKLOAD_CTX" argo-rollouts    argo-rollouts                       deploy
check_in_cluster C1.16 "$WORKLOAD_CTX" traefik          traefik                             deploy
check_in_cluster C1.17 "$WORKLOAD_CTX" cert-manager     cert-manager                        deploy
check_in_cluster C1.18 "$WORKLOAD_CTX" external-secrets external-secrets                    deploy
check_in_cluster C1.19 "$WORKLOAD_CTX" keda             keda-add-ons-http-controller-manager deploy

# Anti-placement: components NOT in wrong cluster (Argo Workflow CRD must be in build, NOT mgmt)
heading "C2: Anti-placement (CRD must NOT be in mgmt cluster)"
if kubectl --context "$MGMT_CTX" get crd workflows.argoproj.io >/dev/null 2>&1; then
  record "C2.1" "FAIL" "Argo workflows.argoproj.io CRD UNEXPECTEDLY in mgmt cluster (must be in build cluster only)"
else
  record "C2.1" "PASS" "Argo Workflow CRD correctly absent from mgmt cluster"
fi
if kubectl --context "$BUILD_CTX" get crd workflows.argoproj.io >/dev/null 2>&1; then
  record "C2.2" "PASS" "Argo Workflow CRD present in build cluster"
else
  record "C2.2" "FAIL" "Argo Workflow CRD MISSING in build cluster"
fi

# -----------------------------------------------------------------------------
# C3 — Secret + ConfigMap content validation (NOT just existence — REAL values)
# -----------------------------------------------------------------------------
heading "C3: Critical Secret/ConfigMap content (placeholder detection)"

# C3.1 — loop-runtime-cluster-kubeconfigs build slot must point at REAL build cluster
build_kc=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get secret loop-runtime-cluster-kubeconfigs \
  -o go-template='{{index .data "build.kubeconfig" | base64decode}}' 2>/dev/null || true)
if [[ -z "$build_kc" ]]; then
  record "C3.1" "FAIL" "loop-runtime-cluster-kubeconfigs.build.kubeconfig MISSING"
elif printf '%s' "$build_kc" | grep -q "kubernetes.default.svc"; then
  record "C3.1" "FAIL" "loop-runtime-cluster-kubeconfigs.build.kubeconfig holds default placeholder (kubernetes.default.svc) — run task k3d:heal"
else
  server=$(printf '%s' "$build_kc" | grep -oE "server: https://[^ ]+" | head -1)
  record "C3.1" "PASS" "loop-runtime-cluster-kubeconfigs.build.kubeconfig has real $server"
fi

# C3.2 — same for runtime slot
runtime_kc=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get secret loop-runtime-cluster-kubeconfigs \
  -o go-template='{{index .data "cell-0.kubeconfig" | base64decode}}' 2>/dev/null || true)
if [[ -z "$runtime_kc" ]]; then
  record "C3.2" "FAIL" "loop-runtime-cluster-kubeconfigs.cell-0.kubeconfig MISSING"
elif printf '%s' "$runtime_kc" | grep -q "kubernetes.default.svc"; then
  record "C3.2" "FAIL" "loop-runtime-cluster-kubeconfigs.cell-0.kubeconfig holds default placeholder — run task k3d:heal"
else
  server=$(printf '%s' "$runtime_kc" | grep -oE "server: https://[^ ]+" | head -1)
  record "C3.2" "PASS" "loop-runtime-cluster-kubeconfigs.cell-0.kubeconfig has real $server"
fi

# C3.3 — gitea-admin.token must be a REAL 40-char API token with scopes (not 32-char placeholder)
gitea_tok=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get secret gitea-admin \
  -o go-template='{{.data.token | base64decode}}' 2>/dev/null || true)
gitea_user=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get secret gitea-admin \
  -o go-template='{{.data.username | base64decode}}' 2>/dev/null || true)
if [[ -z "$gitea_tok" ]]; then
  record "C3.3" "FAIL" "gitea-admin.token MISSING"
elif [[ ${#gitea_tok} -lt 40 ]]; then
  record "C3.3" "FAIL" "gitea-admin.token is ${#gitea_tok} chars (need >=40 for real Gitea API token with scopes)"
else
  # Live probe: token actually authenticates against Gitea. `kubectl run`
  # interleaves probe stdout with its own "pod ... deleted" message; first
  # extract only the 3-digit HTTP code (curl's `-w "%{http_code}"`).
  raw=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system run gitea-tok-probe-$$ \
    --rm -i --image=curlimages/curl --restart=Never --timeout=10s -- \
    curl -sS -o /dev/null -w "%{http_code}" -H "Authorization: token $gitea_tok" \
    http://gitea-http.loop-runtime-system.svc.cluster.local:3000/api/v1/users/$gitea_user 2>/dev/null || true)
  http_code=$(printf '%s' "$raw" | grep -oE '^[0-9]{3}' | head -1)
  if [[ "$http_code" == "200" ]]; then
    record "C3.3" "PASS" "gitea-admin.token (${#gitea_tok} chars) authenticates against Gitea API"
  else
    record "C3.3" "FAIL" "gitea-admin.token rejected by Gitea API (HTTP ${http_code:-???})"
  fi
fi

# C3.4 — MinIO rootPassword + minio-credentials.secretkey must MATCH
minio_root=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get secret minio \
  -o go-template='{{.data.rootPassword | base64decode}}' 2>/dev/null || true)
minio_cred=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get secret minio-credentials \
  -o go-template='{{.data.secretkey | base64decode}}' 2>/dev/null || true)
if [[ -z "$minio_root" || -z "$minio_cred" ]]; then
  record "C3.4" "FAIL" "MinIO root password OR minio-credentials.secretkey MISSING"
elif [[ "$minio_root" != "$minio_cred" ]]; then
  record "C3.4" "FAIL" "MinIO rootPassword (${#minio_root}c) != minio-credentials.secretkey (${#minio_cred}c) — SignatureDoesNotMatch will fire"
else
  record "C3.4" "PASS" "MinIO rootPassword matches minio-credentials.secretkey (${#minio_root} chars)"
fi

# C3.5 — Kratos ConfigMap OIDC clients must have REAL client_id (not REPLACE_VIA_PULUMI)
kratos_cm=$(kubectl --context "$MGMT_CTX" -n loop-runtime-auth get cm ory-kratos-config -o yaml 2>/dev/null || true)
if [[ -z "$kratos_cm" ]]; then
  record "C3.5" "WARN" "ory-kratos-config ConfigMap not present (loop-runtime-auth namespace check)"
elif printf '%s' "$kratos_cm" | grep -q "client_id: REPLACE_VIA_PULUMI"; then
  record "C3.5" "FAIL" "Kratos OIDC client_id holds REPLACE_VIA_PULUMI — .env not exported when generate-secrets ran"
else
  record "C3.5" "PASS" "Kratos OIDC client_id contains real value (no REPLACE_VIA_PULUMI placeholder)"
fi

# -----------------------------------------------------------------------------
# C4 — NetworkPolicy cross-namespace egress correctness
# -----------------------------------------------------------------------------
heading "C4: NetworkPolicy egress targets (loop-runtime-system ns where gitea/minio actually live)"

for op in loop-runtime-tenant-operator loop-runtime-app-operator loop-runtime-platform-operator loop-runtime-preview-operator; do
  np=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get netpol "$op" -o yaml 2>/dev/null || true)
  if [[ -z "$np" ]]; then
    record "C4.${op#loop-runtime-}" "WARN" "NetworkPolicy $op not present (chart networkPolicy.enabled=false?)"
  else
    # Egress must allow port 3000 to namespace loop-runtime-system (where gitea lives)
    if printf '%s' "$np" | grep -A6 "port: 3000" | grep -q "kubernetes.io/metadata.name: loop-runtime-system"; then
      record "C4.${op#loop-runtime-}" "PASS" "$op NetworkPolicy allows egress to loop-runtime-system:3000 (gitea)"
    elif printf '%s' "$np" | grep -A6 "port: 3000" | grep -q "kubernetes.io/metadata.name: gitea"; then
      record "C4.${op#loop-runtime-}" "FAIL" "$op NetworkPolicy targets ns 'gitea' but gitea is in 'loop-runtime-system' (env override missing)"
    else
      record "C4.${op#loop-runtime-}" "WARN" "$op NetworkPolicy gitea egress rule shape not recognized"
    fi
  fi
done

# -----------------------------------------------------------------------------
# C5 — Multi-cell topology (ClusterCredentials connected + map to live clusters)
# -----------------------------------------------------------------------------
heading "C5: Multi-cell topology"

for cell in cell-0 cell-1; do
  status=$(kubectl --context "$MGMT_CTX" -n loop-runtime-system get clustercredential "$cell" \
    -o jsonpath='{.status.connected}' 2>/dev/null || echo "")
  if [[ "$status" == "true" ]]; then
    record "C5.${cell#cell-}" "PASS" "ClusterCredential $cell connected=true"
  else
    record "C5.${cell#cell-}" "FAIL" "ClusterCredential $cell connected='$status' (expected 'true')"
  fi
done

# -----------------------------------------------------------------------------
# C6 — loop-runtime-api deep health (live)
# -----------------------------------------------------------------------------
heading "C6: loop-runtime-api deep health"

# Find local probe port (k3d LB host port mapping)
for port in 13001 3001; do
  body=$(curl -fsS -o - -w "\n%{http_code}" --max-time 5 \
    "http://localhost:$port/api/v1/health/deep" 2>/dev/null || true)
  http_code=$(printf '%s' "$body" | tail -1)
  if [[ "$http_code" == "200" ]] || [[ "$http_code" == "503" ]]; then
    json_body=$(printf '%s' "$body" | sed '$d')
    if [[ "$http_code" == "200" ]]; then
      record "C6.1" "PASS" "loop-runtime-api /health/deep HTTP 200: $json_body"
    else
      record "C6.1" "FAIL" "loop-runtime-api /health/deep HTTP 503 (degraded): $json_body"
    fi
    break
  fi
done
[[ "${http_code:-000}" != "200" && "${http_code:-000}" != "503" ]] && \
  record "C6.1" "WARN" "loop-runtime-api /health/deep not reachable on localhost:13001 or :3001 (no port-forward / NodePort)"

# =============================================================================
# Summary
# =============================================================================
echo "" >&2
echo "=== Summary ===" >&2
echo "  PASS=$PASS  FAIL=$FAIL  WARN=$WARN" >&2

if [[ $JSON_MODE -eq 1 ]]; then
  printf '{"summary":{"pass":%d,"fail":%d,"warn":%d},"results":[' "$PASS" "$FAIL" "$WARN"
  for i in "${!RESULTS[@]}"; do
    [[ $i -gt 0 ]] && printf ','
    printf '%s' "${RESULTS[$i]}"
  done
  printf ']}\n'
fi

[[ $FAIL -gt 0 ]] && exit 1
exit 0
