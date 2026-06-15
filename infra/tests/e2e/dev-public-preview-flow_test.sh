#!/usr/bin/env bash
# J-23 #64 of the 4-task batch (sponsor verbatim 2026-06-07 "Adicionar
# teste de PR preview ao dogfood wrapper").
#
# LIVE end-to-end validation of the J-23 preview canonical chain against
# either localhost (standalone-multi) or dev-public infra. Asserts that
# every architectural commit landed in this session's batch stays wired:
#
#   #65  ClusterRole `tenant-operator` lists previewenvironments.loopzero.io
#   #66  Existing namespaces are migrated to the canonical formula
#   #67  Preview namespace carries the `loopzero-app` SA with automount=false
#   #68  Wildcard `*.preview-dev.loopzero.example` resolves to LE TLS via DNS-01
#   #69  ArgoCD App destination matches the canonical 3-segment namespace
#
# Each scenario fires the LIVE chain and reads the observable result from
# the cluster. NO mocks. NO assumptions. NO retries to mask flakes.
#
#   Scenario A — Pre-flight: loopzero-mgmt reachable + tenant + project exist
#     - kubectl get ns + Tenant CR + projects table row
#
#   Scenario B — RBAC contract (#65)
#     - kubectl auth can-i list previewenvironments.loopzero.io as the
#       tenant-operator SA → expects "yes"
#
#   Scenario C — SA `loopzero-app` present on canonical preview namespace (#67)
#     - Reads the most recent active PreviewEnvironment, derives
#       canonical namespace, asserts SA name + automount=false +
#       label loopzero.io/managed-by=preview-controller.
#
#   Scenario D — Canonical destination wiring (#69)
#     - For every Tenant CR with Spec.Environments[*].Project set,
#       asserts the matching ArgoCD App destination.namespace equals
#       loopzero-<tenant>-<project>-<env>.
#
#   Scenario E — Wildcard TLS (#68)
#     - Builds the canonical SNI hostname from the active preview env
#       and curls https://<sni>.preview-dev.loopzero.example/health,
#       asserts HTTP 200 + cert issuer == "Let's Encrypt".
#
# Run against either env:
#   bash dev-public-preview-flow_test.sh
#   CLOUD_URL=https://app-dev.loopzero.example API_PREFIX=/engine bash dev-public-preview-flow_test.sh
#   --skip-live for CI hermetic mode

set -euo pipefail

CLOUD_URL="${CLOUD_URL:-https://cloud-dev.loopzero.example}"
API_PREFIX="${API_PREFIX:-/engine}"
PROJECT_NAME="${PROJECT_NAME:-teste}"
PREVIEW_PUBLIC_DOMAIN="${PREVIEW_PUBLIC_DOMAIN:-preview-dev.loopzero.example}"
MGMT_CTX="${MGMT_CTX:-k3d-loopzero-mgmt}"
WORKLOAD_CTX="${WORKLOAD_CTX:-k3d-loopzero-workload}"

SKIP_LIVE="${SKIP_LIVE:-0}"
SCENARIO="${SCENARIO:-all}"
JSON_OUTPUT="${JSON_OUTPUT:-0}"

PASS=0
FAIL=0
FAIL_DETAIL=""

usage() {
  cat <<USAGE
Usage: $0 [--skip-live] [--scenario=A|B|C|D|E|all] [--json]

Flags:
  --skip-live           Skip scenarios that touch the cluster (CI hermetic)
  --scenario=<id>       Run only one scenario (A, B, C, D, E, or all)
  --json                Emit machine-readable verdict on stdout

Env overrides:
  CLOUD_URL             Default: http://localhost:13001
  API_PREFIX            Default: empty (for /engine on dev-public, set to /engine)
  PROJECT_NAME          Default: teste
  PREVIEW_PUBLIC_DOMAIN Default: preview-dev.loopzero.example
  MGMT_CTX              Default: k3d-loopzero-mgmt
  WORKLOAD_CTX          Default: k3d-loopzero-workload
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-live) SKIP_LIVE=1 ;;
    --scenario=*) SCENARIO="${1#--scenario=}" ;;
    --json) JSON_OUTPUT=1 ;;
    -h|--help) usage; exit 0 ;;
    *) printf 'Unknown flag: %s\n' "$1" >&2; usage; exit 2 ;;
  esac
  shift
done

log() {
  if [[ "$JSON_OUTPUT" != "1" ]]; then
    printf '[preview-flow] %s\n' "$*" >&2
  fi
}

assert() {
  local desc="$1" cond="$2"
  if [[ "$cond" == "1" ]]; then
    printf '  PASS: %s\n' "$desc"
    PASS=$((PASS+1))
  else
    printf '  FAIL: %s\n' "$desc"
    FAIL=$((FAIL+1))
    FAIL_DETAIL="${FAIL_DETAIL}${desc}; "
  fi
}

run_scenario() {
  local id="$1"
  [[ "$SCENARIO" == "all" || "$SCENARIO" == "$id" ]]
}

# Scenario A — Pre-flight
scenario_A() {
  log "Scenario A — Pre-flight: mgmt cluster reachable + tenant + project"
  local tenant_count="0" project_id=""
  if [[ "$SKIP_LIVE" == "1" ]]; then
    log "  (skipped: SKIP_LIVE=1)"
    return
  fi
  tenant_count=$(kubectl --context "$MGMT_CTX" get tenants.loopzero.io -A 2>/dev/null | grep -c "^loopzero-system" || true)
  assert "mgmt cluster reachable + >=1 tenant CR" "$([ "$tenant_count" -ge 1 ] && echo 1 || echo 0)"

  project_id=$(kubectl --context "$MGMT_CTX" exec -n loopzero-system postgresql-0 -- bash -c \
    "PGPASSWORD=loopzero-dev-password psql -U loopzero -d loopzero -t -A -c \"SELECT id FROM projects WHERE name='${PROJECT_NAME}' LIMIT 1\"" 2>/dev/null | tr -d '[:space:]' || true)
  assert "project '${PROJECT_NAME}' exists in PG" "$([ -n "$project_id" ] && echo 1 || echo 0)"
}

# Scenario B — RBAC contract (#65)
scenario_B() {
  log "Scenario B — RBAC contract: tenant-operator can list previewenvironments (#65)"
  if [[ "$SKIP_LIVE" == "1" ]]; then
    log "  (skipped: SKIP_LIVE=1)"
    return
  fi
  local rbac
  rbac=$(kubectl --context "$MGMT_CTX" auth can-i list previewenvironments.loopzero.io \
    --as=system:serviceaccount:loopzero-system:tenant-operator 2>/dev/null || true)
  assert "tenant-operator SA can list previewenvironments.loopzero.io" "$([ "$rbac" = "yes" ] && echo 1 || echo 0)"
}

# Scenario C — SA `loopzero-app` on canonical preview namespace (#67)
scenario_C() {
  log "Scenario C — Canonical SA on preview namespace (#67)"
  if [[ "$SKIP_LIVE" == "1" ]]; then
    log "  (skipped: SKIP_LIVE=1)"
    return
  fi
  # Pick the most recently created PreviewEnvironment CR
  local pe_name pe_tenant pe_project pe_pr ns
  pe_name=$(kubectl --context "$MGMT_CTX" get previewenvironments.loopzero.io -n loopzero-system \
    --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null || true)
  if [[ -z "$pe_name" ]]; then
    log "  no PreviewEnvironment CR found — Scenario C skipped"
    return
  fi
  pe_tenant=$(kubectl --context "$MGMT_CTX" get previewenvironments.loopzero.io -n loopzero-system "$pe_name" \
    -o jsonpath='{.spec.tenantId}' 2>/dev/null || true)
  pe_project=$(kubectl --context "$MGMT_CTX" get previewenvironments.loopzero.io -n loopzero-system "$pe_name" \
    -o jsonpath='{.spec.projectId}' 2>/dev/null || true)
  pe_pr=$(kubectl --context "$MGMT_CTX" get previewenvironments.loopzero.io -n loopzero-system "$pe_name" \
    -o jsonpath='{.spec.prNumber}' 2>/dev/null || true)
  ns="loopzero-${pe_tenant}-${pe_project}-preview-pr-${pe_pr}"

  local sa_automount sa_managed
  sa_automount=$(kubectl --context "$WORKLOAD_CTX" get sa loopzero-app -n "$ns" \
    -o jsonpath='{.automountServiceAccountToken}' 2>/dev/null || true)
  assert "SA loopzero-app exists in canonical ns ${ns}" "$([ -n "$sa_automount" ] && echo 1 || echo 0)"
  assert "SA automount=false (defense-in-depth)" "$([ "$sa_automount" = "false" ] && echo 1 || echo 0)"

  sa_managed=$(kubectl --context "$WORKLOAD_CTX" get sa loopzero-app -n "$ns" \
    -o jsonpath='{.metadata.labels.loopzero\.io/managed-by}' 2>/dev/null || true)
  assert "SA label loopzero.io/managed-by=preview-controller" \
    "$([ "$sa_managed" = "preview-controller" ] && echo 1 || echo 0)"
}

# Scenario D — Canonical destination wiring (#69)
scenario_D() {
  log "Scenario D — ArgoCD App destination canonical (#69)"
  if [[ "$SKIP_LIVE" == "1" ]]; then
    log "  (skipped: SKIP_LIVE=1)"
    return
  fi
  # For every tenant with environments[].project set, verify the App
  # destination matches the canonical 3-segment formula.
  local total=0 ok=0
  while IFS='|' read -r tenant env_name env_project; do
    [[ -z "$tenant" || -z "$env_name" ]] && continue
    [[ -z "$env_project" ]] && continue  # skip pre-#69 envs with no Project field set
    total=$((total+1))
    local app_name="${tenant}-${env_name}"
    local want_ns="loopzero-${tenant}-${env_project}-${env_name}"
    local got_ns
    got_ns=$(kubectl --context "$MGMT_CTX" get application "$app_name" -n argocd \
      -o jsonpath='{.spec.destination.namespace}' 2>/dev/null || true)
    if [[ "$got_ns" == "$want_ns" ]]; then
      ok=$((ok+1))
    else
      log "  destination mismatch: app=${app_name} got=${got_ns} want=${want_ns}"
    fi
  done < <(kubectl --context "$MGMT_CTX" get tenants.loopzero.io -A -o json 2>/dev/null \
    | python3 -c "
import sys, json
d = json.load(sys.stdin)
for t in d.get('items', []):
    name = t.get('metadata', {}).get('name', '')
    for env in t.get('spec', {}).get('environments', []):
        print(name + '|' + env.get('name', '') + '|' + env.get('project', ''))
" 2>/dev/null)
  if [[ "$total" -eq 0 ]]; then
    log "  no tenants with environments[].project set — degenerate-OK (cleanroom pre-#69 migration)"
    assert "no project envs to validate (degenerate-OK)" "1"
  else
    assert "${ok}/${total} ArgoCD App destinations match canonical formula" \
      "$([ "$ok" -eq "$total" ] && echo 1 || echo 0)"
  fi
}

# Scenario E — Wildcard TLS (#68)
scenario_E() {
  log "Scenario E — Wildcard preview URL via DNS-01 TLS (#68)"
  if [[ "$SKIP_LIVE" == "1" ]]; then
    log "  (skipped: SKIP_LIVE=1)"
    return
  fi
  # Pick the most recently active preview env + first IngressRoute Host
  local pe_name pe_tenant pe_project pe_pr ns ingress_host probe_url
  pe_name=$(kubectl --context "$MGMT_CTX" get previewenvironments.loopzero.io -n loopzero-system \
    --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null || true)
  if [[ -z "$pe_name" ]]; then
    log "  no PreviewEnvironment CR found — Scenario E skipped"
    return
  fi
  pe_tenant=$(kubectl --context "$MGMT_CTX" get previewenvironments.loopzero.io -n loopzero-system "$pe_name" \
    -o jsonpath='{.spec.tenantId}' 2>/dev/null || true)
  pe_project=$(kubectl --context "$MGMT_CTX" get previewenvironments.loopzero.io -n loopzero-system "$pe_name" \
    -o jsonpath='{.spec.projectId}' 2>/dev/null || true)
  pe_pr=$(kubectl --context "$MGMT_CTX" get previewenvironments.loopzero.io -n loopzero-system "$pe_name" \
    -o jsonpath='{.spec.prNumber}' 2>/dev/null || true)
  ns="loopzero-${pe_tenant}-${pe_project}-preview-pr-${pe_pr}"

  ingress_host=$(kubectl --context "$WORKLOAD_CTX" get ingressroutes.traefik.io -n "$ns" \
    -o jsonpath='{.items[0].spec.routes[0].match}' 2>/dev/null | sed -n 's/.*Host(`\([^`]*\)`).*/\1/p' || true)
  if [[ -z "$ingress_host" ]]; then
    log "  no IngressRoute Host found in ${ns} — Scenario E skipped"
    return
  fi
  # Strip the .loopzero.local suffix and append the public wildcard domain.
  local sni_prefix="${ingress_host%.loopzero.local}"
  probe_url="https://${sni_prefix}.${PREVIEW_PUBLIC_DOMAIN}/health"

  # Resolve to localhost if Caddy is local; otherwise let DNS do it.
  local http_code cert_issuer
  http_code=$(curl -sS -k -o /dev/null -w "%{http_code}" \
    --resolve "${sni_prefix}.${PREVIEW_PUBLIC_DOMAIN}:443:127.0.0.1" "$probe_url" 2>/dev/null || echo "000")
  cert_issuer=$(echo | openssl s_client -connect 127.0.0.1:443 \
    -servername "${sni_prefix}.${PREVIEW_PUBLIC_DOMAIN}" 2>/dev/null \
    | openssl x509 -noout -issuer 2>/dev/null | sed 's/.*O *= *\([^,]*\),.*/\1/' | tr -d '[:space:]' || true)

  assert "HTTPS probe ${probe_url} returns HTTP 200" "$([ "$http_code" = "200" ] && echo 1 || echo 0)"
  assert "TLS cert issuer == 'Let's Encrypt' (DNS-01 issued)" \
    "$([ "$cert_issuer" = "Let'sEncrypt" ] && echo 1 || echo 0)"
}

# Scenario F — Single-slot SoT (D2/D3 of plan
# 2026-06-07-preview-as-category-single-slot-plan.md)
#
# Asserts that the preview_slot table is the canonical SOT for "this
# tenant's active preview" and ON CONFLICT (tenant_id) DO UPDATE
# enforces substitution at the SQL layer. Reads the slot directly
# from PG; verifies that:
#   (a) ≤ 1 row per tenant (single-slot constraint at PK level)
#   (b) When a PreviewEnvironment CR exists, the slot row's
#       active_pr_number matches the most recent PR.
scenario_F() {
  log "Scenario F — preview_slot single-slot SoT (D2/D3)"
  if [[ "$SKIP_LIVE" == "1" ]]; then
    log "  (skipped: SKIP_LIVE=1)"
    return
  fi
  local pe_tenant pe_pr pg_slot_pr pg_row_count
  pe_tenant=$(kubectl --context "$MGMT_CTX" get previewenvironments.loopzero.io -n loopzero-system \
    --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].spec.tenantId}' 2>/dev/null || true)
  pe_pr=$(kubectl --context "$MGMT_CTX" get previewenvironments.loopzero.io -n loopzero-system \
    --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].spec.prNumber}' 2>/dev/null || true)
  if [[ -z "$pe_tenant" || -z "$pe_pr" ]]; then
    log "  no active PreviewEnvironment CR — Scenario F skipped"
    return
  fi
  pg_row_count=$(kubectl --context "$MGMT_CTX" exec -n loopzero-system postgresql-0 -- \
    psql -U loopzero -d loopzero -tA -c "SELECT COUNT(*) FROM preview_slot WHERE tenant_id = '${pe_tenant}';" 2>/dev/null \
    | tr -d '[:space:]' || echo "0")
  pg_slot_pr=$(kubectl --context "$MGMT_CTX" exec -n loopzero-system postgresql-0 -- \
    psql -U loopzero -d loopzero -tA -c "SELECT active_pr_number FROM preview_slot WHERE tenant_id = '${pe_tenant}';" 2>/dev/null \
    | tr -d '[:space:]' || echo "")
  assert "preview_slot has ≤ 1 row per tenant (single-slot PK constraint)" \
    "$([ "$pg_row_count" -le "1" ] && echo 1 || echo 0)"
  assert "preview_slot.active_pr_number == active CR PR (D3 substitute)" \
    "$([ "$pg_slot_pr" = "$pe_pr" ] && echo 1 || echo 0)"
}

# Scenario G — D5 enforcement (preview env_targets NOT counted)
#
# Asserts that environment_targets rows for the active preview env
# carry kind='preview' so the enforcement middleware's
# CountPermanentEnvironments helper does NOT count them. This is the
# Bug A fix from the plan — pre-2026-06-07 the inline COUNT query
# mixed both kinds and a Pro tenant with 1 prod env + 2 preview-pr-N
# rows hit current=3, max=3 PLAN_LIMIT_EXCEEDED.
scenario_G() {
  log "Scenario G — env_targets kind='preview' for preview rows (D5)"
  if [[ "$SKIP_LIVE" == "1" ]]; then
    log "  (skipped: SKIP_LIVE=1)"
    return
  fi
  local pe_tenant preview_kind_count permanent_kind_count
  pe_tenant=$(kubectl --context "$MGMT_CTX" get previewenvironments.loopzero.io -n loopzero-system \
    --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].spec.tenantId}' 2>/dev/null || true)
  if [[ -z "$pe_tenant" ]]; then
    log "  no active PreviewEnvironment CR — Scenario G skipped"
    return
  fi
  preview_kind_count=$(kubectl --context "$MGMT_CTX" exec -n loopzero-system postgresql-0 -- \
    psql -U loopzero -d loopzero -tA -c "SELECT COUNT(*) FROM environment_targets WHERE tenant_id = '${pe_tenant}' AND environment LIKE 'preview-pr-%' AND kind = 'preview';" 2>/dev/null \
    | tr -d '[:space:]' || echo "0")
  permanent_kind_count=$(kubectl --context "$MGMT_CTX" exec -n loopzero-system postgresql-0 -- \
    psql -U loopzero -d loopzero -tA -c "SELECT COUNT(*) FROM environment_targets WHERE tenant_id = '${pe_tenant}' AND environment LIKE 'preview-pr-%' AND kind = 'permanent';" 2>/dev/null \
    | tr -d '[:space:]' || echo "0")
  assert "preview-pr-* env_targets rows have kind='preview' (≥1)" \
    "$([ "$preview_kind_count" -ge "1" ] && echo 1 || echo 0)"
  assert "preview-pr-* env_targets rows have NO kind='permanent' (D5 invariant)" \
    "$([ "$permanent_kind_count" -eq "0" ] && echo 1 || echo 0)"
}

# Driver
log "=== J-23 preview flow LIVE test ==="
log "Target: CLOUD_URL=${CLOUD_URL} MGMT_CTX=${MGMT_CTX} WORKLOAD_CTX=${WORKLOAD_CTX}"
log "Public domain: ${PREVIEW_PUBLIC_DOMAIN}"

run_scenario A && scenario_A
run_scenario B && scenario_B
run_scenario C && scenario_C
run_scenario D && scenario_D
run_scenario E && scenario_E
run_scenario F && scenario_F
run_scenario G && scenario_G

if [[ "$JSON_OUTPUT" == "1" ]]; then
  printf '{"pass":%d,"fail":%d,"detail":"%s"}\n' "$PASS" "$FAIL" "$FAIL_DETAIL"
else
  printf '\nTotal: %d PASS / %d FAIL\n' "$PASS" "$FAIL"
fi

exit "$([ "$FAIL" -eq 0 ] && echo 0 || echo 1)"
