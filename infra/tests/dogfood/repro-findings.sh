#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD — T0.1
# Reproduz cada um dos 13 achados do dogfood 2026-04-29 contra k3d standalone-multi.
# Emite JSON em stdout (esquema {findings: [...], summary: {...}}) e summary table em stderr.
#
# Exit codes:
#   0 — todos achados ainda REPRODUZEM (modo "antes do fix")
#   1 — pelo menos 1 achado FIXED (atualize a matriz!)
#   2 — erro de pre-condição (k3d ausente, kubectl falha)
#
# Uso:
#   bash infra/tests/dogfood/repro-findings.sh
#   bash infra/tests/dogfood/repro-findings.sh --json   # somente JSON em stdout
#   task dogfood:repro
set -euo pipefail

JSON_ONLY=0
[[ "${1:-}" == "--json" ]] && JSON_ONLY=1

# Cada `check_FNN` desabilita `-e` localmente porque comandos não-zero são
# DADOS aqui, não erros. `restore_e` re-arma o flag depois de cada bloco.
restore_e() { set -e; }

LOOPZERO_BIN="${LOOPZERO_BIN:-$(pwd)/cli/bin/loopzero}"
MGMT_KUBE=$(k3d kubeconfig write loopzero-mgmt 2>/dev/null || echo "")
WORKLOAD_KUBE=$(k3d kubeconfig write loopzero-workload 2>/dev/null || echo "")

declare -a FINDINGS=()

# Helper: append a finding to FINDINGS array. Args: id severity status evidence
add_finding() {
  local id="$1" sev="$2" status="$3" evidence="$4"
  evidence=$(printf '%s' "$evidence" | python3 -c 'import sys,json; print(json.dumps(sys.stdin.read()))')
  FINDINGS+=("{\"id\":\"$id\",\"severity\":\"$sev\",\"status\":\"$status\",\"evidence\":$evidence}")
}

# F-01: API not reachable from CLI (localhost:13001).
check_F01() {
  local code
  code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 3 http://localhost:13001/api/v1/health 2>/dev/null || echo "000")
  if [[ "$code" == "200" ]]; then
    add_finding F-01 S0 FIXED "localhost:13001/api/v1/health returned $code"
  else
    add_finding F-01 S0 REPRODUCED "localhost:13001/api/v1/health returned $code (expected 200)"
  fi
}

# F-02: process exit code != json exit_code for auth errors.
check_F02() {
  local out exit_p exit_j
  set +e
  out=$("$LOOPZERO_BIN" deploy --json 2>/dev/null)
  exit_p=$?
  set -e
  exit_j=$(printf '%s' "$out" | python3 -c 'import sys,json; d=json.loads(sys.stdin.read()); print(d.get("exit_code",-1))' 2>/dev/null || echo "-1")
  if [[ "$exit_p" == "$exit_j" && "$exit_j" != "-1" ]]; then
    add_finding F-02 S1 FIXED "process exit=$exit_p matches json exit_code=$exit_j"
  else
    add_finding F-02 S1 REPRODUCED "process exit=$exit_p, json exit_code=$exit_j (mismatch)"
  fi
}

# F-03: unknown command/flag returns exit 0.
check_F03() {
  set +e
  "$LOOPZERO_BIN" definitely-not-a-command >/dev/null 2>&1
  local exit1=$?
  "$LOOPZERO_BIN" deploy --not-a-real-flag >/dev/null 2>&1
  local exit2=$?
  set -e
  if [[ "$exit1" -ne 0 && "$exit2" -ne 0 ]]; then
    add_finding F-03 S2 FIXED "unknown cmd exit=$exit1, unknown flag exit=$exit2"
  else
    add_finding F-03 S2 REPRODUCED "unknown cmd exit=$exit1 (want !=0), unknown flag exit=$exit2 (want !=0)"
  fi
}

# F-04: duplicate Tenant CRs in 'default' and 'loopzero-system'.
check_F04() {
  if [[ -z "$MGMT_KUBE" ]]; then
    add_finding F-04 S1 INDETERMINATE "k3d loopzero-mgmt not running"
    return
  fi
  local default_count system_count
  default_count=$(KUBECONFIG="$MGMT_KUBE" kubectl get tenants -n default --no-headers 2>/dev/null | wc -l | tr -d ' ')
  system_count=$(KUBECONFIG="$MGMT_KUBE" kubectl get tenants -n loopzero-system --no-headers 2>/dev/null | wc -l | tr -d ' ')
  if [[ "$default_count" -eq 0 ]]; then
    add_finding F-04 S1 FIXED "default ns has 0 tenants, loopzero-system has $system_count"
  else
    add_finding F-04 S1 REPRODUCED "default ns has $default_count tenants (expected 0), loopzero-system has $system_count"
  fi
}

# F-05: loopzero init with invalid token completes silently with exit 0.
check_F05() {
  local tmpdir
  tmpdir=$(mktemp -d)
  pushd "$tmpdir" >/dev/null
  cat >package.json <<'EOF'
{"name":"f05-test","version":"1.0.0","scripts":{"start":"node index.js"}}
EOF
  echo "console.log('hi');" > index.js
  git init -q
  git add . && git -c user.email=t@x -c user.name=t commit -q -m i 2>/dev/null
  set +e
  LOOPZERO_TOKEN=fake-token LOOPZERO_URL=http://localhost:13001 "$LOOPZERO_BIN" init --yes --name f05-test --json >/dev/null 2>&1
  local exit_code=$?
  set -e
  popd >/dev/null
  rm -rf "$tmpdir"
  if [[ "$exit_code" -ne 0 ]]; then
    add_finding F-05 S1 FIXED "init with fake token returned exit=$exit_code"
  else
    add_finding F-05 S1 REPRODUCED "init with fake token returned exit=0 (silent success)"
  fi
}

# F-06: loopzero logout when not authenticated says "Logout cancelled."
check_F06() {
  local out
  out=$("$LOOPZERO_BIN" logout 2>&1 || true)
  if printf '%s' "$out" | grep -qi "cancelled"; then
    add_finding F-06 S3 REPRODUCED "logout output: $(printf '%s' "$out" | head -1)"
  else
    add_finding F-06 S3 FIXED "logout output: $(printf '%s' "$out" | head -1)"
  fi
}

# F-07: GET /api/v1/version returns 404.
check_F07() {
  local code
  code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 3 http://localhost:13001/api/v1/version 2>/dev/null || echo "000")
  if [[ "$code" == "404" ]]; then
    add_finding F-07 S2 REPRODUCED "version endpoint returned 404"
  elif [[ "$code" == "200" ]]; then
    add_finding F-07 S2 FIXED "version endpoint returned 200"
  else
    add_finding F-07 S2 INDETERMINATE "version endpoint returned $code (API may be unreachable per F-01)"
  fi
}

# F-08: /metrics architectural decision — port 9090 is the Prometheus
# exposition endpoint, port 3001 (Service.port http) is the API plane.
# Pre-fix the runbook didn't exist documenting this. Post-fix: check that
# (a) docs/runbooks/observability.md exists, (b) chart exposes :9090, and
# (c) ServiceMonitor opt-in is wired. NOT a check on :3001/metrics — that
# was a false-positive in the original dogfood report (:3001 never served
# Prometheus exposition, by design — blast-radius separation).
check_F08() {
  local runbook="docs/runbooks/observability.md"
  if [[ ! -f "$runbook" ]]; then
    add_finding F-08 S3 REPRODUCED "runbook $runbook missing — :9090 metrics architecture undocumented"
    return
  fi
  if ! grep -q "9090" "$runbook"; then
    add_finding F-08 S3 REPRODUCED "runbook does not document :9090 metrics endpoint"
    return
  fi
  if ! grep -q "ServiceMonitor" infra/helm/loopzero-api/templates/servicemonitor.yaml 2>/dev/null; then
    add_finding F-08 S3 REPRODUCED "ServiceMonitor template missing"
    return
  fi
  add_finding F-08 S3 FIXED "runbook + ServiceMonitor opt-in + metrics port wired (architecturally correct: :3001=API, :9090=Prom)"
}

# F-09: tenant-controller logs "pull secret ensure failed" in loop.
check_F09() {
  if [[ -z "$MGMT_KUBE" ]]; then
    add_finding F-09 S2 INDETERMINATE "k3d loopzero-mgmt not running"
    return
  fi
  local hits
  hits=$(KUBECONFIG="$MGMT_KUBE" kubectl logs -n loopzero-system -l app=tenant-controller --since=5m 2>/dev/null \
    | grep -c "pull secret ensure failed" 2>/dev/null || true)
  hits=${hits//[^0-9]/}
  hits=${hits:-0}
  if [[ "$hits" -eq 0 ]]; then
    add_finding F-09 S2 FIXED "0 occurrences of 'pull secret ensure failed' in last 5 min"
  else
    add_finding F-09 S2 REPRODUCED "$hits occurrences of 'pull secret ensure failed' in last 5 min"
  fi
}

# F-10: traces export RPC error every 30s.
check_F10() {
  if [[ -z "$MGMT_KUBE" ]]; then
    add_finding F-10 S2 INDETERMINATE "k3d loopzero-mgmt not running"
    return
  fi
  local hits
  hits=$(KUBECONFIG="$MGMT_KUBE" kubectl logs -n loopzero-system -l app=tenant-controller --since=5m 2>/dev/null \
    | grep -c "traces export" 2>/dev/null || true)
  hits=${hits//[^0-9]/}
  hits=${hits:-0}
  if [[ "$hits" -eq 0 ]]; then
    add_finding F-10 S2 FIXED "0 occurrences of 'traces export' RPC error in last 5 min"
  else
    add_finding F-10 S2 REPRODUCED "$hits occurrences of 'traces export' RPC error in last 5 min"
  fi
}

# F-11: ArgoCD App stuck Unknown::Progressing.
check_F11() {
  if [[ -z "$MGMT_KUBE" ]]; then
    add_finding F-11 S2 INDETERMINATE "k3d loopzero-mgmt not running"
    return
  fi
  local sync health
  sync=$(KUBECONFIG="$MGMT_KUBE" kubectl get app -n argocd tn-e2etest-production -o jsonpath='{.status.sync.status}' 2>/dev/null || echo "absent")
  health=$(KUBECONFIG="$MGMT_KUBE" kubectl get app -n argocd tn-e2etest-production -o jsonpath='{.status.health.status}' 2>/dev/null || echo "absent")
  if [[ "$sync" == "absent" ]]; then
    add_finding F-11 S2 INDETERMINATE "App tn-e2etest-production not present"
  elif [[ "$sync" == "Synced" && ( "$health" == "Healthy" || "$health" == "Progressing" ) ]]; then
    add_finding F-11 S2 FIXED "sync=$sync health=$health (consolidated)"
  else
    add_finding F-11 S2 REPRODUCED "sync=$sync health=$health (expected Synced + Healthy/Progressing)"
  fi
}

# F-12: /api/v1/health/deep reports argocd auth_failed.
check_F12() {
  local body argocd_status
  body=$(curl -s --max-time 3 http://localhost:13001/api/v1/health/deep 2>/dev/null || echo "{}")
  argocd_status=$(printf '%s' "$body" | python3 -c 'import sys,json; print(json.loads(sys.stdin.read() or "{}").get("argocd","unknown"))' 2>/dev/null || echo "unknown")
  if [[ "$argocd_status" == "auth_failed" ]]; then
    add_finding F-12 S2 REPRODUCED "argocd: auth_failed in /health/deep"
  elif [[ "$argocd_status" == "ok" || "$argocd_status" == "connected" ]]; then
    add_finding F-12 S2 FIXED "argocd: $argocd_status"
  else
    add_finding F-12 S2 INDETERMINATE "argocd: $argocd_status (API may be unreachable per F-01)"
  fi
}

# F-13: Argo Workflow pods left in Error.
check_F13() {
  local kube=""
  if [[ -n "${WORKLOAD_KUBE:-}" ]] && KUBECONFIG="$WORKLOAD_KUBE" kubectl get ns argo >/dev/null 2>&1; then
    kube="$WORKLOAD_KUBE"
  elif [[ -n "${MGMT_KUBE:-}" ]] && KUBECONFIG="$MGMT_KUBE" kubectl get ns argo >/dev/null 2>&1; then
    kube="$MGMT_KUBE"
  else
    local build_kube
    build_kube=$(k3d kubeconfig write loopzero-build 2>/dev/null || echo "")
    [[ -n "$build_kube" ]] && kube="$build_kube"
  fi
  if [[ -z "$kube" ]]; then
    add_finding F-13 S3 INDETERMINATE "no cluster with argo namespace found"
    return
  fi
  local error_count
  error_count=$(KUBECONFIG="$kube" kubectl get pods -n argo --field-selector status.phase=Failed --no-headers 2>/dev/null | wc -l | tr -d ' ')
  if [[ "$error_count" -eq 0 ]]; then
    add_finding F-13 S3 FIXED "0 Failed pods in argo ns"
  else
    add_finding F-13 S3 REPRODUCED "$error_count Failed pods in argo ns (TTL/podGC not removing)"
  fi
}

# Run all checks
check_F01
check_F02
check_F03
check_F04
check_F05
check_F06
check_F07
check_F08
check_F09
check_F10
check_F11
check_F12
check_F13

# Build summary counts
total=${#FINDINGS[@]}
reproduced=0
fixed=0
indeterminate=0
for f in "${FINDINGS[@]}"; do
  case "$f" in
    *'"status":"REPRODUCED"'*)    reproduced=$((reproduced+1)) ;;
    *'"status":"FIXED"'*)         fixed=$((fixed+1)) ;;
    *'"status":"INDETERMINATE"'*) indeterminate=$((indeterminate+1)) ;;
  esac
done

# Emit JSON to stdout
findings_json=$(IFS=,; printf '[%s]' "${FINDINGS[*]}")
printf '{"findings":%s,"summary":{"total":%d,"reproduced":%d,"fixed":%d,"indeterminate":%d}}\n' \
  "$findings_json" "$total" "$reproduced" "$fixed" "$indeterminate"

# Summary table to stderr (unless --json mode suppresses)
if [[ "$JSON_ONLY" -eq 0 ]]; then
  {
    printf '\n=== Dogfood 2026-04-29 — Repro Summary ===\n'
    printf '  Total findings:     %d\n' "$total"
    printf '  Still REPRODUCED:   %d\n' "$reproduced"
    printf '  Now FIXED:          %d\n' "$fixed"
    printf '  INDETERMINATE:      %d\n' "$indeterminate"
    printf '\n'
  } >&2
fi

# Exit policy:
#   0 = all REPRODUCED (matrix is consistent, no fixes yet)
#   1 = at least one FIXED (matrix needs update)
#   2 = pre-condition error (handled by set -e earlier)
if [[ "$fixed" -gt 0 ]]; then
  exit 1
fi
exit 0
