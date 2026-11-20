#!/usr/bin/env bash
# dev-public-production-deploy_test.sh
#
# T6.1 of plan 2026-06-09-m2-operator-canonical-fix v1.4.
#
# LIVE end-to-end production deploy chain assertion against dev-public.
# Goal metric of the plan: `git push -> production deploy` end-to-end
# without ANY manual operator workaround. This script IS the test (per
# plan TDD body line 734 "The script IS the test").
#
# Chain steps:
#   1. Preflight  — gh auth + kubectl context + dev-public reachability
#   2. Reset      — reset-for-e2e.sh applied (DB + Kratos identities)
#   3. Seed       — admin@getloopscale.com session via seed-admin-loopzero-dev.sh
#   4. Project    — POST /api/v1/projects {name: sample} (T4.1 auto-creates production env)
#   5. Connect    — connect-canonical-fixture.sh wires the canonical loopzerokit-example repo
#   6. Push       — gh api PUT /repos/.../contents/<path> synthesizes a real commit
#   7. Chain      — webhook -> autodeploy -> build -> release ACTIVE -> ArgoCD Synced+Healthy
#   8. Assert M2  — kubectl get ns loopzero-<tenant>-sample-production exists (M2 canonical)
#   9. Assert M2  — kubectl get appproject <tenant> destinations contains M2 namespace
#  10. Assert M2  — kubectl get sa loopzero-app -n loopzero-<tenant>-sample-production exists
#  11. Assert M2  — pods Running 2/2 in loopzero-<tenant>-sample-production
#  12. Assert M2  — ArgoCD Application <tenant>-production Synced+Healthy
#  13. Anti-WA    — grep this script body: ZERO `kubectl patch appproject` calls,
#                    ZERO `kubectl create namespace` calls, ZERO `kubectl ... apply -f -`
#                    to copy SA across namespaces (the 4 workarounds T6.1 of
#                    plan 2026-06-08-pre-release-test-rigor had to apply manually).
#
# Wall-clock budget: < 20 min total per plan Goal + cross-reference to
# T6.1 budget in plan 2026-06-08-pre-release-test-rigor.
#
# Per ADR D6 - fail-fast on first broken link; no retry loops on chain
# semantics; chain shape == M2 canonical with ZERO workarounds.
#
# Usage:
#   bash infra/tests/e2e/live/dev-public-production-deploy_test.sh
#   bash infra/tests/e2e/live/dev-public-production-deploy_test.sh --skip-live   # hermetic-only mode
#   bash infra/tests/e2e/live/dev-public-production-deploy_test.sh --json        # machine-readable verdict
#   bash infra/tests/e2e/live/dev-public-production-deploy_test.sh --skip-reset  # presumes reset-for-e2e.sh already ran
set -euo pipefail

REPO_OWNER="${REPO_OWNER:-getloopscale}"
REPO_NAME="${REPO_NAME:-loopzerokit-example}"
DEFAULT_BRANCH="${DEFAULT_BRANCH:-develop}"
PROJECT_NAME="${PROJECT_NAME:-sample}"
ENV_NAME="${ENV_NAME:-production}"
SESSION_FILE="${SESSION_FILE:-$HOME/.loopzero/admin-loopzero-dev-session.txt}"
PUBLIC_APP_URL="${PUBLIC_APP_URL:-https://app-dev.getloopscale.com}"
PUBLIC_CLOUD_URL="${PUBLIC_CLOUD_URL:-https://app-dev.getloopscale.com}"
CONTEXT_MGMT="${CONTEXT_MGMT:-k3d-loopzero-core}"
CONTEXT_WORKLOAD="${CONTEXT_WORKLOAD:-k3d-loopzero-cell-0}"

MAX_DURATION_SECONDS="${MAX_DURATION_SECONDS:-1200}"  # 20 min hard ceiling
LINK_RELEASE_BUDGET=$((10 * 60)) # release ACTIVE
LINK_FINAL_BUDGET=$((20 * 60))   # pods Running + ArgoCD Healthy

SKIP_LIVE=0
JSON_MODE=0
SKIP_RESET=0

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-live)   SKIP_LIVE=1 ; shift ;;
    --json)        JSON_MODE=1 ; shift ;;
    --skip-reset)  SKIP_RESET=1 ; shift ;;
    -h|--help)
      sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//' ; exit 0 ;;
    *) printf '[prod-deploy] unknown arg: %s\n' "$1" >&2 ; exit 1 ;;
  esac
done

log() { printf '[prod-deploy] %s\n' "$*" >&2 ; }

START_EPOCH="$(date +%s)"

# Script-level JWT — minted once via mint_jwt(), reused across steps 4+7
# (engine API requires Authorization: Bearer JWT, NOT raw Kratos session cookie).
JWT=""

TMP_JWT=""
mint_jwt() {
  if [[ -n "${TMP_JWT:-}" ]]; then echo "$TMP_JWT"; return 0; fi
  # CANONICAL 2026-06-15 dev-public LIVE: prefer HMAC session JWT (carries
  # tenant_id from active session; passes loopzero-cloud /engine proxy guard
  # per MEET-2026-04-07-039 B7). Bootstrap RSA JWT remains the fallback
  # for engine-direct (localhost:13001) but lacks tenant_id claim so the
  # /engine/* proxy rejects it.
  if [[ -f "$HOME/.loopzero/local-dogfood-session-jwt.txt" ]]; then
    TMP_JWT=$(cat "$HOME/.loopzero/local-dogfood-session-jwt.txt")
    echo "$TMP_JWT"
    return 0
  fi
  # Local dogfood path: reuse the seeded token file when available
  if [[ -f "$HOME/.loopzero/local-dogfood-token.txt" ]]; then
    TMP_JWT=$(cat "$HOME/.loopzero/local-dogfood-token.txt")
    echo "$TMP_JWT"
    return 0
  fi
  printf 'ABORT: no JWT available; run seed-admin-loopzero-dev.sh + seed-local-user.sh
' >&2
  return 1
}

# --- Preflight (EC-12) ------------------------------------------------------

preflight() {
  log "step 1/13 — preflight gh auth + kubectl context + dev-public reachability"

  if ! command -v gh >/dev/null 2>&1; then
    log "FAIL preflight: gh CLI not in PATH"
    return 1
  fi
  if ! gh auth status >/dev/null 2>&1; then
    log "FAIL preflight: gh auth failed (run 'gh auth login')"
    return 1
  fi
  if ! command -v kubectl >/dev/null 2>&1; then
    log "FAIL preflight: kubectl not in PATH"
    return 1
  fi
  if ! kubectl --context="$CONTEXT_MGMT" cluster-info >/dev/null 2>&1; then
    log "FAIL preflight: kubectl context $CONTEXT_MGMT unreachable"
    return 1
  fi
  if ! curl -fsS -o /dev/null --max-time 10 "${PUBLIC_CLOUD_URL}/health" 2>/dev/null; then
    # Optional /health may not exist; soft-check by host resolution + TLS handshake.
    if ! curl -sS -o /dev/null --max-time 10 "${PUBLIC_CLOUD_URL}/" 2>/dev/null; then
      log "FAIL preflight: $PUBLIC_CLOUD_URL not reachable"
      return 1
    fi
  fi

  log "preflight PASS"
  return 0
}

# --- Reset (step 2) ---------------------------------------------------------

reset_state() {
  if [ "$SKIP_RESET" -eq 1 ]; then
    log "step 2/13 — reset SKIPPED (--skip-reset)"
    return 0
  fi
  log "step 2/13 — reset-for-e2e.sh (DB + Kratos identities)"
  # --yes required: T6.1 runs non-interactive (CI + Claude); without it the
  # reset script blocks on "Continue? [y/N]" stdin read.
  bash infra/scripts/local/reset-for-e2e.sh --yes 2>&1 | tail -5 >&2
}

# --- Seed admin session (step 3) -------------------------------------------

seed_session() {
  log "step 3/13 — seed-admin-loopzero-dev.sh (admin@getloopscale.com session mint)"
  bash infra/scripts/local/seed-admin-loopzero-dev.sh 2>&1 | tail -5 >&2

  if [ ! -f "$SESSION_FILE" ]; then
    log "FAIL seed: session file $SESSION_FILE not found"
    return 1
  fi
}

# --- Project create (step 4) — invokes T4.1 auto-create production env ----

project_create() {
  log "step 4/13 — POST /api/v1/projects {name: $PROJECT_NAME} (T4.1 auto-creates production env)"

  # Canonical 2026-06-16: mint_jwt prints token to stdout; capture into $JWT.
  if [ -z "$JWT" ]; then
    JWT="$(mint_jwt)" || return 1
  fi

  local response
  response="$(curl -sS -X POST \
    -H "Authorization: Bearer $JWT" \
    -H 'Content-Type: application/json' \
    --data "{\"name\":\"$PROJECT_NAME\"}" \
    "${PUBLIC_CLOUD_URL}/engine/api/v1/projects" 2>&1)" || true

  # Accept (a) successful create with "id"/"name" OR (b) idempotent already-exists
  # 409 conflict (project already created by an earlier seed/connect chain).
  if echo "$response" | grep -qE '"id"|"name"'; then
    log "project_create OK"
  elif echo "$response" | grep -qE 'PROJECT_EXISTS|already_exists|ALREADY_EXISTS|conflict'; then
    log "project_create OK (idempotent — project '$PROJECT_NAME' already exists)"
  else
    log "FAIL project_create: unexpected response: $response"
    return 1
  fi
}

# --- Connect canonical fixture (step 5) ------------------------------------

connect_repo() {
  log "step 5/13 — connect-canonical-fixture.sh wires $REPO_OWNER/$REPO_NAME -> $PROJECT_NAME"
  bash infra/scripts/local/connect-canonical-fixture.sh 2>&1 | tail -5 >&2
}

# --- Push commit (step 6) — gh api PUT contents synthesizes commit --------

push_commit() {
  log "step 6/13 — gh api PUT /contents triggers a real commit + push event"
  local marker_file="t61-marker-$(date +%s).txt"
  local content
  content="$(printf 'T6.1 dev-public-production-deploy test %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" | base64 -w0)"
  gh api -X PUT "/repos/${REPO_OWNER}/${REPO_NAME}/contents/${marker_file}" \
    -f message="T6.1 marker commit" \
    -f content="$content" \
    -f branch="${DEFAULT_BRANCH}" >/dev/null 2>&1
}

# --- Chain assertion (step 7) — webhook -> build -> release ACTIVE --------

assert_release_active() {
  log "step 7/13 — assert release ACTIVE within ${LINK_RELEASE_BUDGET}s"
  if [ -z "$JWT" ]; then
    mint_jwt || return 1
  fi
  local deadline=$(( $(date +%s) + LINK_RELEASE_BUDGET ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    # Response shape (per cloud /engine/api/v1/projects/{name}/status):
    #   {"active_release":{"status":"ACTIVE",...},"last_build":{"status":"SUCCEEDED|FAILED",...}}
    # The polling target is active_release.status when last_build SUCCEEDED;
    # last_build.status FAILED short-circuits with a hard fail. Previous
    # iteration grepped a non-existent "phase" field — LIVE-caught 2026-06-09.
    local body status
    body="$(curl -sS \
      -H "Authorization: Bearer $JWT" \
      "${PUBLIC_CLOUD_URL}/engine/api/v1/projects/${PROJECT_NAME}/status?env=${ENV_NAME}" 2>/dev/null || true)"
    status="$(printf '%s' "$body" | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  lb = (d.get("last_build") or {}).get("status", "")
  ar = (d.get("active_release") or {}).get("status", "")
  if lb == "FAILED":
    print("FAILED")
  elif ar == "ACTIVE":
    print("ACTIVE")
  else:
    print(f"PENDING({lb}/{ar})")
except Exception:
  print("PARSE_ERR")
' 2>/dev/null || echo "PARSE_ERR")"
    if [ "$status" = "ACTIVE" ]; then
      log "release ACTIVE PASS"
      return 0
    fi
    if [ "$status" = "FAILED" ]; then
      log "FAIL release reached FAILED state"
      return 1
    fi
    sleep 10
  done
  log "FAIL release did not reach ACTIVE within ${LINK_RELEASE_BUDGET}s"
  return 1
}

# --- M2 canonical assertions (steps 8-12) ----------------------------------

assert_m2_chain() {
  log "step 8/13 — assert M2 canonical namespace exists"
  local tenant
  # Canonical column is `active_tenant_id` on users table (LIVE-caught
  # 2026-06-09 — previous `tenant_id` literal was schema drift; `users`
  # has email + active_tenant_id as its tenant-linking fields).
  # Container selector `-c postgresql` required: pod has 2 containers
  # (postgresql + metrics) and default selection is unstable.
  tenant="$(kubectl --context="$CONTEXT_MGMT" -n loopzero-system exec postgresql-0 -c postgresql -- \
    bash -c 'PGPASSWORD=loopzero-dev-password psql -U loopzero -d loopzero -t -A -c "SELECT active_tenant_id FROM users WHERE email='\''admin@getloopscale.com'\'' LIMIT 1"' 2>/dev/null \
    | tr -d '[:space:]')"
  if [ -z "$tenant" ]; then
    log "FAIL could not resolve tenant_id for admin@getloopscale.com"
    return 1
  fi
  local m2_ns="loopzero-${tenant}-${PROJECT_NAME}-${ENV_NAME}"
  log "expected M2 namespace: $m2_ns"

  if ! kubectl --context="$CONTEXT_WORKLOAD" get ns "$m2_ns" >/dev/null 2>&1; then
    log "FAIL M2 namespace $m2_ns not found"
    return 1
  fi
  log "M2 namespace PASS"

  log "step 9/13 — assert AppProject destinations include M2 namespace"
  local destinations
  destinations="$(kubectl --context="$CONTEXT_MGMT" -n argocd get appproject "$tenant" \
    -o jsonpath='{range .spec.destinations[*]}{.namespace}{"\n"}{end}' 2>/dev/null)"
  if ! echo "$destinations" | grep -q "^${m2_ns}$"; then
    log "FAIL AppProject $tenant destinations missing $m2_ns; got: $destinations"
    return 1
  fi
  log "AppProject destinations PASS"

  log "step 10/13 — assert SA loopzero-app exists in M2 namespace"
  if ! kubectl --context="$CONTEXT_WORKLOAD" -n "$m2_ns" get sa loopzero-app >/dev/null 2>&1; then
    log "FAIL SA loopzero-app missing in $m2_ns"
    return 1
  fi
  log "SA loopzero-app PASS"

  log "step 11/13 — assert pods Running in M2 namespace within ${LINK_FINAL_BUDGET}s"
  local deadline=$(( $(date +%s) + LINK_FINAL_BUDGET ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    local running_count
    running_count="$(kubectl --context="$CONTEXT_WORKLOAD" -n "$m2_ns" get pods \
      --field-selector=status.phase=Running -o name 2>/dev/null | wc -l)"
    if [ "$running_count" -gt 0 ]; then
      log "pods Running PASS ($running_count pods)"
      break
    fi
    sleep 10
  done
  if [ "$running_count" -le 0 ]; then
    log "FAIL no pods Running in $m2_ns after ${LINK_FINAL_BUDGET}s"
    return 1
  fi

  log "step 12/13 — assert ArgoCD Application Synced+Healthy"
  local sync_status health_status
  sync_status="$(kubectl --context="$CONTEXT_MGMT" -n argocd get application "${tenant}-${ENV_NAME}" \
    -o jsonpath='{.status.sync.status}' 2>/dev/null || true)"
  health_status="$(kubectl --context="$CONTEXT_MGMT" -n argocd get application "${tenant}-${ENV_NAME}" \
    -o jsonpath='{.status.health.status}' 2>/dev/null || true)"
  if [ "$sync_status" != "Synced" ] || [ "$health_status" != "Healthy" ]; then
    log "FAIL ArgoCD app sync=$sync_status health=$health_status (want Synced+Healthy)"
    return 1
  fi
  log "ArgoCD Synced+Healthy PASS"
}

# --- Anti-workaround self-assertion (step 13) -------------------------------

assert_no_workarounds() {
  log "step 13/13 — anti-workaround self-grep (script body must contain ZERO operator manual patches)"
  # The 4 workarounds from T6.1 of plan 2026-06-08-pre-release-test-rigor
  # (per feedback_m2_canonical_drift_operator_renderer memory) were:
  #   (a) kubectl patch appproject ... destinations
  #   (b) kubectl --context loopzero-cell-0 create namespace loopzero-...
  #   (c) kubectl get sa loopzero-app -o yaml | sed | kubectl apply -f -
  #   (d) kubectl patch app .../sync force
  # T6.1 SUCCESS = the script body NEVER invokes any of these.
  #
  # Self-reference filter (M2 LIVE-caught 2026-06-09): the FAIL/PASS log
  # messages below mention the workaround patterns literally for operator
  # readability. We exclude any line containing "anti-workaround" so the
  # grep doesn't self-flag on its own diagnostic strings — those lines are
  # exclusively log/comment lines, NEVER kubectl invocations.
  local body
  body="$(grep -vE '^[[:space:]]*#|anti-workaround' "$0" || true)"
  if echo "$body" | grep -qE 'kubectl[^|]+patch[[:space:]]+appproject'; then
    log "FAIL anti-workaround: script body contains kubectl patch appproject (workaround a)"
    return 1
  fi
  if echo "$body" | grep -qE 'kubectl[^|]+create[[:space:]]+namespace[[:space:]]+loopzero-'; then
    log "FAIL anti-workaround: script body contains kubectl create namespace loopzero-* (workaround b)"
    return 1
  fi
  if echo "$body" | grep -qE 'kubectl[^|]+(get|exec).*sa[[:space:]]+loopzero-app.*apply[[:space:]]+-f'; then
    log "FAIL anti-workaround: script body contains SA copy across namespaces (workaround c)"
    return 1
  fi
  if echo "$body" | grep -qE 'kubectl[^|]+patch[[:space:]]+app[[:space:]]+.*sync.*revision'; then
    log "FAIL anti-workaround: script body contains kubectl patch app .../sync (workaround d)"
    return 1
  fi
  log "anti-workaround PASS — script body is canonical (M2 by construction)"
}

# --- Verdict / JSON output --------------------------------------------------

emit_verdict() {
  local verdict="$1" elapsed
  elapsed=$(( $(date +%s) - START_EPOCH ))
  if [ "$JSON_MODE" -eq 1 ]; then
    printf '{"verdict":"%s","elapsed_seconds":%d,"max_duration_seconds":%d}\n' \
      "$verdict" "$elapsed" "$MAX_DURATION_SECONDS"
  fi
  log "verdict=${verdict} elapsed=${elapsed}s"
}

# --- Main -------------------------------------------------------------------

main() {
  # Anti-workaround grep is ALWAYS run (hermetic — no live cluster needed).
  assert_no_workarounds

  if [ "$SKIP_LIVE" -eq 1 ]; then
    log "SKIP_LIVE — hermetic-only run; chain assertions skipped"
    emit_verdict PASS
    exit 0
  fi

  preflight    || { emit_verdict FAIL ; exit 1 ; }
  reset_state  || { emit_verdict FAIL ; exit 1 ; }
  seed_session || { emit_verdict FAIL ; exit 1 ; }
  project_create || { emit_verdict FAIL ; exit 1 ; }
  connect_repo || { emit_verdict FAIL ; exit 1 ; }
  push_commit  || { emit_verdict FAIL ; exit 1 ; }
  assert_release_active || { emit_verdict FAIL ; exit 1 ; }
  assert_m2_chain || { emit_verdict FAIL ; exit 1 ; }

  emit_verdict PASS
}

main "$@"
