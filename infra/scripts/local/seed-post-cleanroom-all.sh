#!/usr/bin/env bash
# Canonical POST-CLEANROOM master seed — orchestrates the 7-step chain
# required between `task setup` completion and dogfood-all --live-only.
#
# Per `.claude/rules/cleanroom-up-live-test-mandate.md` UNBREAKABLE +
# memory `feedback_post_cleanroom_seed_admin_canonical` /
# `feedback_post_cleanroom_kratos_patch_dev_public` /
# `feedback_post_cleanroom_seed_github_app_canonical`: a fresh Cleanroom
# leaves the system in `infra installed, fixtures empty` state. This
# script aggregates the 9 LIVE-validated canonical seeds discovered in
# 2026-06-15 dogfood-all 12/12 PASS session into a single idempotent
# chain ordered by dependency.
#
# Steps (canonical order):
#
# 1. vault-bootstrap-dev.sh        — OpenBao init + unseal + AppRole +
#                                    transit chain + SPKI pin
# 2. loopzero-ops seed minio (M15 W1)        — MinIO password reset (mgmt + build)
# 3. seed-admin-loopzero-dev.sh     — Kratos identity + tenant + session JWT
# 5. loopzero-ops seed github-app-dev (M15 W1)         — GitHub App PEM + webhook secret
# 5b. seed-github-webhook-fixture-canonical.sh — repo-level webhook config
# 6. loopzero-ops seed gitea-token (M15 W2)  — Gitea API token mint + 3-way sync
# 7. loopzero-ops seed broker (M15 W3)       — loopzero-broker token (the store is the
#                                    cell helmfile's release loopzero-broker)
# 9. seed-tenant-fixture-canonical.sh — subscription plan + GH installation
# 17. loopzero-ops seed data-plane-admin-keys — loopzero-cloud's data-plane admin keys (#470)
# 17g. loopzero-ops seed data-plane-edge      — the edge authorises the data plane (#470)
#
# A step seeds secret material or data no helmfile can declare. The four that
# patched objects helmfile renders — patch-kratos, hydra-urls, loki-cross-cluster
# and vault-cross-cluster-addr — were removed (principle_violations#67): the Kratos
# and Hydra URLs, LOKI_URL and its NodePort bridge, OPENBAO_VAULT_ADDR and the
# Promtail push URL are environment values that `helmfile sync` renders
# (infra/tests/charts/seeded_wiring_is_rendered_test.go).
#
# Per cleanroom-up-live-test-mandate.md UNBREAKABLE: this script CANNOT
# be skipped — it's the canonical bridge between `task setup` and the
# operational state where dogfood-all --live-only expects to find
# functional fixtures.
#
# Flags:
#   --verify         pass --verify to each sub-step
#   --skip-vault     skip Step 1 (operator already ran vault-bootstrap)
#   --dry-run        print the chain without executing
#   --json           machine-readable verdict at end
#   --help|-h        print this header

set -euo pipefail

# B-024: PINS the cluster the seeds operate on, instead of letting them inherit the active context.
#
# Step 0a of this script runs `task k3d:secrets:create`, and `k3d cluster create` used to switch
# the `current-context` as a side effect — so every seed below it operated on the secrets
# cluster believing it was the core one, and reported "resource not found" about resources
# that existed. The taskfile stopped switching the context, but that alone would only change WHICH
# wrong cluster got hit: without pinning, the seeds would land wherever `task setup`
# happened to leave kubectl.
#
# The variable was already this script's convention (see the `kubectl --context "${CORE_CONTEXT:-...}"`
# further down); here it starts applying to the seeds too. `export` because `run_step` uses
# `eval`, and the binary reads it in `resolveContextName`.
export CORE_CONTEXT="${CORE_CONTEXT:-k3d-loopzero-core}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

VERIFY=0
SKIP_VAULT=0
DRY_RUN=0
JSON=0
for arg in "$@"; do
  case "$arg" in
    --verify) VERIFY=1 ;;
    --skip-vault) SKIP_VAULT=1 ;;
    --dry-run) DRY_RUN=1 ;;
    --json) JSON=1 ;;
    --help|-h)
      sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

log() { printf "%s [seed-post-cleanroom-all] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }

RESULTS=()
PASS=0
FAIL=0
SKIP=0

# Finding #5 of issue #91 (Cleanroom mandate canonical chain run
# 2026-06-18): run_step previously returned 1 on failure which, combined
# with the top-level `set -euo pipefail`, aborted the entire script at
# the FIRST failing step. The canonical-chain-summary log block (below
# the final step invocation) was UNREACHABLE on any failure — operators
# saw a "wrapper exit 0" with no summary of which steps actually ran,
# which passed, which failed. Worse, downstream steps that did NOT
# depend on the failing step were never attempted, so triage required
# re-running the script manually after each fix.
#
# Canonical fix: accumulate FAIL count + ALWAYS return 0. The final
# `[[ "$FAIL" == "0" ]] || exit 1` (last line of the script) preserves
# the fail-loud contract by exiting non-zero if any FAIL accumulated.
# Operators now get the full chain attempt + the summary + a non-zero
# exit when anything failed.
run_step() {
  local name="$1"; shift
  local cmd="$*"
  if [[ "$DRY_RUN" == "1" ]]; then
    log "DRY: $name → $cmd"
    return 0
  fi
  log "▶ $name"
  if eval "$cmd" >"/tmp/seed-${name}.log" 2>&1; then
    # A seed that skips exits 0 — skipping is not an error (seed.go) — so the exit
    # code alone called it PASS. On the 2026-09-29 cleanroom that hid a skipped admin
    # seed behind two downstream failures (#453). The seed's own verdict line decides.
    local skipped
    skipped="$(grep -m1 -oE 'SKIP — .*' "/tmp/seed-${name}.log" || true)"
    if [[ -n "$skipped" ]]; then
      RESULTS+=("SKIP: $name — ${skipped#SKIP — }")
      SKIP=$((SKIP + 1))
      log "  SKIP: $name — ${skipped#SKIP — }"
    else
      RESULTS+=("PASS: $name")
      PASS=$((PASS + 1))
    fi
  else
    RESULTS+=("FAIL: $name (see /tmp/seed-${name}.log)")
    FAIL=$((FAIL + 1))
    log "  FAIL: $name — see /tmp/seed-${name}.log"
  fi
  # ALWAYS return 0 so `set -e` does not abort the chain at the first
  # failing step. The final FAIL counter check at the end of the
  # script drives the actual exit code (Finding #5).
  return 0
}

skip_step() {
  local name="$1"; shift
  local reason="$1"
  RESULTS+=("SKIP: $name — $reason")
  SKIP=$((SKIP + 1))
}

# The seeds read their endpoints from the environment. On the 2026-09-29 cleanroom
# nothing set them — they had been supplied by hand on earlier rounds — so the admin
# seed skipped and the Hydra client seed could not reach Postgres (#453). The chain
# opens what its seeds need, exports it, and closes it on exit. A value the operator
# already set is left alone, and no port-forward is opened for it.
PORT_FORWARD_PIDS=()
# shellcheck disable=SC2317,SC2329  # invoked by the EXIT trap below, not called directly
close_port_forwards() {
  local p
  for p in "${PORT_FORWARD_PIDS[@]}"; do kill "$p" 2>/dev/null || true; done
}
trap close_port_forwards EXIT

# port_forward <namespace> <service> <local:remote> — waits until the local port answers.
port_forward() {
  kubectl --context "$CORE_CONTEXT" -n "$1" port-forward "svc/$2" "$3" >/dev/null 2>&1 &
  PORT_FORWARD_PIDS+=("$!")
  local port="${3%%:*}" _
  for _ in $(seq 1 30); do
    (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null && return 0
    sleep 1
  done
  log "  port-forward svc/$2 did not open on :$port"
  return 1
}

# secret_value <namespace> <secret> <key> — empty when the Secret or key is absent.
secret_value() {
  kubectl --context "$CORE_CONTEXT" -n "$1" get secret "$2" -o "jsonpath={.data.$3}" 2>/dev/null \
    | base64 -d 2>/dev/null || true
}

ensure_seed_endpoints() {
  if [[ -z "${KRATOS_ADMIN_URL:-}" ]] && port_forward loopzero-auth ory-kratos-admin 14435:80; then
    export KRATOS_ADMIN_URL="http://127.0.0.1:14435"
  fi
  if [[ -z "${LOOPZERO_ADMIN_HYDRA_PG_URL:-}" ]]; then
    local pw
    pw="$(secret_value loopzero-system loopzero-db-credentials password)"
    if [[ -n "$pw" ]] && port_forward loopzero-system postgresql 15432:5432; then
      export LOOPZERO_ADMIN_HYDRA_PG_URL="postgres://loopzero:${pw}@127.0.0.1:15432/loopzero?sslmode=disable"
    fi
  fi
  export LOOPZERO_API_URL="${LOOPZERO_API_URL:-http://localhost:13001}"
  if [[ -z "${LOOPZERO_BOOTSTRAP_SECRET:-}" ]]; then
    local bs
    bs="$(secret_value loopzero-system loopzero-api-internal LOOPZERO_BOOTSTRAP_SECRET)"
    if [[ -n "$bs" ]]; then export LOOPZERO_BOOTSTRAP_SECRET="$bs"; fi
  fi
}

# Each seed runs in its own process, so what one seed produces never reaches the next
# unless the chain hands it on. The admin seed reports the tenant it provisioned in its
# result ("(tenant tn-…)"); the fixture seed needs it as LOOPZERO_TENANT_ID (#453).
export_admin_tenant_id() {
  [[ -n "${LOOPZERO_TENANT_ID:-}" ]] && return 0
  local id
  id="$(grep -m1 -oE '\(tenant tn-[a-z0-9-]+\)' "$1" 2>/dev/null | grep -oE 'tn-[a-z0-9-]+' || true)"
  if [[ -n "$id" ]]; then export LOOPZERO_TENANT_ID="$id"; fi
  return 0
}

# The fixture step creates its project through the CLI. Prefer LOOPZERO_BIN, then a `loopzero` on
# PATH; otherwise install the PUBLISHED CLI — what a user gets — into a per-version cache.
# A cold droplet has none, and earlier rounds used one installed by hand (#453).
LOOPZERO_CLI_VERSION="${LOOPZERO_CLI_VERSION:-0.4.0}"
ensure_loopzero_cli() {
  [[ -n "${LOOPZERO_BIN:-}" ]] && return 0
  if command -v loopzero >/dev/null 2>&1; then
    LOOPZERO_BIN="$(command -v loopzero)"; export LOOPZERO_BIN; return 0
  fi
  local prefix="$HOME/.cache/loopzero-cli/${LOOPZERO_CLI_VERSION:-0.4.0}"
  if [[ ! -x "$prefix/node_modules/.bin/loopzero" ]]; then
    npm install --silent --no-audit --no-fund --prefix "$prefix" "@loopzero/cli@${LOOPZERO_CLI_VERSION:-0.4.0}" >/dev/null 2>&1 \
      || { log "  could not install @loopzero/cli@${LOOPZERO_CLI_VERSION:-0.4.0}"; return 0; }
  fi
  export LOOPZERO_BIN="$prefix/node_modules/.bin/loopzero"
}

VERIFY_FLAG=""
[[ "$VERIFY" == "1" ]] && VERIFY_FLAG="--verify"

# Step 0 (NEW 2026-06-20) — auto-provision the separate loopzero-secrets k3d
# cluster + OpenBao chart BEFORE Step 1 (vault-bootstrap-dev.sh) attempts
# init/unseal. Gated on LOOPZERO_HELMFILE_ENV=dev-public per ADR
# 2026-06-20-dev-public-vault-automated-init (sponsor-accepted risk: the
# dev-public TEST tier uses an AUTOMATED Shamir init; the witness +
# distributed-shard ceremony is scoped to prod/persistent vaults only).
#
# Approach A UNBREAKABLE: this uses `task k3d:secrets:create` +
# `task k3d:secrets:bootstrap` against the SEPARATE k3d-loopzero-secrets cluster.
# `task k3d:secrets:bootstrap-in-core` remains PROHIBITED (independent of
# witness scope — separate-cluster topology violation).
#
# Both tasks are idempotent: k3d:secrets:create skips if cluster exists;
# k3d:secrets:bootstrap re-syncs helm releases (cert-manager + cluster-issuer
# + openbao-cert + openbao chart) without destroying existing state. So
# this Step 0 is a no-op on a warm-vault re-run, and the only cost on a
# Cleanroom-fresh run is the cluster create + ~3min helm sync.
#
# In other envs (standalone-multi / unset / prod) this Step 0 is SKIPPED —
# standalone doesn't run loopzero-secrets, and prod requires the witnessed
# ceremony per the mandate § Sub-step 2.5 prod/persistent path.
if [[ "${LOOPZERO_HELMFILE_ENV:-}" == "dev-public" && "$SKIP_VAULT" != "1" ]]; then
  run_step "0a-secrets-cluster-create" "cd $REPO_ROOT && task k3d:secrets:create"
  run_step "0b-secrets-cluster-bootstrap" "cd $REPO_ROOT && task k3d:secrets:bootstrap"
else
  skip_step "0a-secrets-cluster-create" "skipped — only applicable in LOOPZERO_HELMFILE_ENV=dev-public (current: ${LOOPZERO_HELMFILE_ENV:-unset})"
  skip_step "0b-secrets-cluster-bootstrap" "skipped — only applicable in LOOPZERO_HELMFILE_ENV=dev-public (current: ${LOOPZERO_HELMFILE_ENV:-unset})"
fi

# Step 1
if [[ "$SKIP_VAULT" == "1" ]]; then
  skip_step "1-vault-bootstrap-dev" "operator already ran vault chain"
else
  run_step "1-vault-bootstrap-dev" "bash $SCRIPT_DIR/vault-bootstrap-dev.sh" || log "  (continuing — vault may already be bootstrapped)"
fi

# Step 1b (R3 of plan dev-public-cross-cluster-secrets, issue #111, sponsor
# approved 2026-06-19): apply ESO ClusterSecretStore + SA + CRB + CA
# ConfigMap on mgmt, then wait for CSS Ready=True. This was operator-time
# per the canonical runbook (VK.5) — codified here closes the seed chain
# FAIL=11 for the cross-cluster ESO read path. Reuses the existing
# canonical vault-bootstrap-eso.sh (NOT a new .sh — Rule 9 + un-homed-logic).
# Only meaningful under Approach A (separate loopzero-secrets cluster); single-
# cluster runs no-op via the script's own preflight.
if [[ "$SKIP_VAULT" == "1" ]]; then
  skip_step "1b-vault-bootstrap-eso" "skipped together with step 1 (operator already ran vault chain)"
else
  ESO_BOOTSTRAP="$SCRIPT_DIR/../openbao/vault-bootstrap-eso.sh"
  if [[ -f "$ESO_BOOTSTRAP" ]]; then
    run_step "1b-vault-bootstrap-eso" "bash $ESO_BOOTSTRAP --mgmt-context k3d-loopzero-core --vault-context k3d-loopzero-secrets" || \
      log "  (continuing — CSS loopzero-secrets-secrets may need operator triage)"
  else
    skip_step "1b-vault-bootstrap-eso" "vault-bootstrap-eso.sh not found at $ESO_BOOTSTRAP (skipped)"
  fi
fi

# Step 2
if [[ "$DRY_RUN" != "1" ]]; then
  ensure_seed_endpoints
  ensure_loopzero_cli
fi

run_step "2-seed-minio" "(cd $REPO_ROOT/cmd/loopzero-ops && go run . seed minio ${VERIFY_FLAG:+--verify})"

# Step 3 — the admin login submit needs the dev-public Kratos URLs, which
# `helmfile sync` renders (it used to wait on the patch-kratos and hydra-urls
# seeds, Finding #4 of issue #91; those patches are environment values now).
run_step "3-seed-admin-loopzero-dev" "(cd $REPO_ROOT/cmd/loopzero-ops && go run . seed admin-loopzero-dev ${VERIFY_FLAG:+--verify})"
export_admin_tenant_id "/tmp/seed-3-seed-admin-loopzero-dev.log"

# Step 3b (NEW 2026-06-29 per Red Team #117 closure) — QA Red Team
# canonical identity. The seed-admin script is generic over EMAIL +
# DISPLAY_NAME + USERNAME + TENANT_SLUG + SESSION_FILE env overrides,
# so we re-invoke it to provision paulohenrique.java@gmail.com (the
# QA operator's working inbox, accessible at gmail /u/2). Without
# this step, every Cleanroom obliterates the QA's identity and
# recovery returns the anti-enumeration mute for their address.
# Session is persisted to a separate file (~/.loopzero/redteam-qa-*) so
# the admin session at the canonical path is not overwritten.
run_step "3b-seed-redteam-qa-identity" "(cd $REPO_ROOT/cmd/loopzero-ops && EMAIL=paulohenrique.java@gmail.com SESSION_FILE=\$HOME/.loopzero/redteam-qa-session.txt SESSION_JWT_FILE=\$HOME/.loopzero/redteam-qa-session-jwt.txt go run . seed admin-loopzero-dev)"

# Step 5 (M15 W1: migrated to loopzero-ops seed; bash script deleted)
run_step "5-seed-github-app-dev" "(cd $REPO_ROOT/cmd/loopzero-ops && go run . seed github-app-dev ${VERIFY_FLAG:+--verify})"

# Step 5b (NEW 2026-07-04) — canonical GitHub webhook on the fixture
# repo. Without this, `loopzero push` on getloopscale/loopzerokit-example silently
# no-ops: GitHub never POSTs to /api/v1/webhooks/github, autodeploy
# never runs, dogfood-webhook-chain + chaos c11 chain link 9 FAIL. This
# is the seam that step 5 (the App PEM + webhook secret) does NOT
# cover — the REPO webhook is per-repo config on the GitHub side.
# Env-gated to dev-public (the only env whose Caddy edge handles
# /api/v1/webhooks/github → loopzero-api directly per Caddyfile app-dev
# block). SKIP-graceful when gh unauthenticated or dev-credentials.json
# absent (standalone-multi tier).
if [[ "${LOOPZERO_HELMFILE_ENV:-}" == "dev-public" ]]; then
  run_step "5b-seed-github-webhook-fixture" "(cd $REPO_ROOT/cmd/loopzero-ops && go run . seed github-webhook-fixture ${VERIFY_FLAG:+--verify})"
else
  skip_step "5b-seed-github-webhook-fixture-canonical" "skipped — only applicable in dev-public (current: ${LOOPZERO_HELMFILE_ENV:-unset})"
fi

# Step 6
run_step "6-seed-gitea-token" "(cd $REPO_ROOT/cmd/loopzero-ops && go run . seed gitea-token ${VERIFY_FLAG:+--verify})"

# Step 6b (NEW 2026-06-16) — ArgoCD ↔ Gitea repo-creds (MUST run AFTER step 6 so token is canonical)
run_step "6b-seed-argocd-gitea-creds" "(cd $REPO_ROOT/cmd/loopzero-ops && go run . seed argocd-gitea-creds ${VERIFY_FLAG:+--verify})"

# Step 7
run_step "7-seed-broker" "(cd $REPO_ROOT/cmd/loopzero-ops && go run . seed broker ${VERIFY_FLAG:+--verify})"

# Step 7b (NEW 2026-06-16) — Vault role_id into Secret loopzero-openbao-approle (MUST run AFTER vault-bootstrap-dev)
run_step "7b-seed-vault-role-id-sync" "(cd $REPO_ROOT/cmd/loopzero-ops && go run . seed vault-role-id-sync ${VERIFY_FLAG:+--verify})"

# Step 9
# #123 (qa-dogfood-cleanup-2026-06-21 T2.2 / ADR-Q7): pass `--team`
# explicitly so any inherited `LOOPZERO_PLAN_TIER` env (operator shell
# default, stale dev-credentials.json residue) cannot override the
# canonical dev-public tier. QA dogfooding 2026-06-21 caught the
# Scale page showing `starter` plan ceiling 2 even after the canonical
# chain ran — root cause was the env-shadowing path.
run_step "9-seed-tenant-fixture" "(cd $REPO_ROOT/cmd/loopzero-ops && LOOPZERO_PLAN_TIER=team go run . seed tenant-fixture ${VERIFY_FLAG:+--verify})"

# Step 10 (NEW 2026-06-16) — admin-cli Hydra OAuth2 client.
# Required for dogfood-login Suite 0 T5.4 (≥ 3 Hydra clients).
# Complements bootstrap-oauth-clients.sh (which provisions loopzero-cli +
# loopzero-bootstrap) with the admin-side client_credentials path.
# Per reference_admin_cli_hydra_client_canonical memory.
# Step 10 (M15 W2: migrated to loopzero-ops seed; bash script deleted)
run_step "10-seed-admin-hydra-client" "(cd $REPO_ROOT/cmd/loopzero-ops && go run . seed admin-hydra-client ${VERIFY_FLAG:+--verify})"

# Step 11 (NEW 2026-06-16) — connect canonical fixture (project "sample" +
# repo link). Required by LIVE wrappers that probe /projects/sample/* paths.
# Depends on step 10 (admin Hydra client) only indirectly via session JWT.
if [[ -x "$SCRIPT_DIR/connect-canonical-fixture.sh" ]]; then
  run_step "11-connect-canonical-fixture" "bash $SCRIPT_DIR/connect-canonical-fixture.sh"
else
  skip_step "11-connect-canonical-fixture" "script not yet present"
fi

# Step 12 (NEW M2 2026-06-23) — chaos-mesh canonical (mgmt-only, universal)
run_step "12-seed-chaos-mesh-canonical" "bash $SCRIPT_DIR/seed-chaos-mesh-canonical.sh $VERIFY_FLAG"

# Step 13 (NEW M2 2026-06-23) — ArgoCD admin token
# EC-3 absorption (v1.1): env-gate dev-public OR standalone-multi — ArgoCD does not exist in other envs
if [[ "${LOOPZERO_HELMFILE_ENV:-}" == "dev-public" || "${LOOPZERO_HELMFILE_ENV:-}" == "standalone-multi" ]]; then
  run_step "13-seed-argocd-token" "(cd $REPO_ROOT/cmd/loopzero-ops && go run . seed argocd-token ${VERIFY_FLAG:+--verify})"
else
  skip_step "13-seed-argocd-token" "skipped — only applicable in dev-public/standalone-multi (current: ${LOOPZERO_HELMFILE_ENV:-unset})"
fi

# Step 15 (M15 W1: migrated to loopzero-ops seed; bash script deleted) (NEW M2 2026-06-23) — loopzero-stripe-webhook Secret materialization (universal; SKIP-graceful on placeholder)
run_step "15-seed-stripe-webhook-secret" "(cd $REPO_ROOT/cmd/loopzero-ops && go run . seed stripe-webhook-secret ${VERIFY_FLAG:+--verify})"

# Step 15b (NEW 2026-06-24 — plan stripe-l2-wiring-canonical-fix T3.2) —
# loopzero-stripe-api-keys Secret materialization (sibling of 15 per ADR D1;
# separate rotation cadence: webhook secret vs API key per Stripe runbook).
run_step "15b-seed-stripe-api-keys" "(cd $REPO_ROOT/cmd/loopzero-ops && go run . seed stripe-api-keys ${VERIFY_FLAG:+--verify})"

# Step 15c (NEW 2026-07-01 — Red Team loopzero-cloud#156) — Stripe test-mode
# webhook endpoint provisioning + real whsec capture. Without this the
# canonical fulfillment flow (payment → checkout.session.completed →
# plan promotion) is dead because Stripe has no endpoint to POST to, and
# the placeholder `whsec_dev_*` in dev-credentials.json would fail
# signature validation even if events arrived. Idempotent: skips when
# endpoint already exists AND whsec canonical. SKIP graceful when
# stripe.api_key placeholder/missing (standalone-multi tier).
run_step "15c-seed-stripe-webhook-endpoint" "(cd $REPO_ROOT/cmd/loopzero-ops && go run . seed stripe-webhook-endpoint ${VERIFY_FLAG:+--verify})"

# Step 16 (NEW 2026-07-01 — plan dashboard-badge-data-plane-poller T6.1) —
# data-plane poller first-tick warm-up. EC-2 absorption: without this,
# the first /projects render post `task setup` is a wall of `Unknown`
# badges (up to POLLER_INTERVAL seconds until the first natural poller
# tick populates env_targets.last_replicas_ready_*). Rather than add a
# bespoke --first-tick-sync CLI flag (parsimony rung 5), we simply wait
# one canonical interval + a small buffer for the poller's ticker to
# fire naturally.
#
# Gate reads the CHART-INJECTED ConfigMap value (not shell env) so
# whether the poller is enabled is decided by helmfile overlay per env
# (dev-public: enabled=true; standalone-multi: not declared → treated
# as disabled). Legacy shell-env override kept for local dev workflows.
POLLER_ENABLED_CFG="$(kubectl --context "${CORE_CONTEXT:-k3d-loopzero-core}" \
  -n loopzero-system get configmap loopzero-api \
  -o jsonpath='{.data.LOOPZERO_DATA_PLANE_POLLER_ENABLED}' 2>/dev/null || true)"
if [[ "${LOOPZERO_DATA_PLANE_POLLER_ENABLED:-$POLLER_ENABLED_CFG}" == "true" ]]; then
  POLLER_WARMUP_S="${LOOPZERO_DATA_PLANE_POLLER_WARMUP_S:-45}"
  run_step "16-data-plane-poller-first-tick" "sleep $POLLER_WARMUP_S && log '   data-plane poller warmed up (${POLLER_WARMUP_S}s wait)' && true"
else
  skip_step "16-data-plane-poller-first-tick" "skipped — loopzero-api ConfigMap has LOOPZERO_DATA_PLANE_POLLER_ENABLED='${POLLER_ENABLED_CFG:-<absent>}' (poller not wired for this env)"
fi

# Step 17 (#470) — loopzero-cloud's platform-admin key for every data-plane service on the
# core cluster. loopzero-cloud mints per-tenant keys with it behind the edge's forward-auth;
# without it every authorised /v1/{memory,rag,lens}/* call answers 503. Keeps a stored
# key that still authenticates, mints one otherwise, and rolls loopzero-cloud onto the keys
# (waiting for it). SKIPs where no data-plane service is deployed.
run_step "17-seed-data-plane-admin-keys" "(cd $REPO_ROOT/cmd/loopzero-ops && go run . seed data-plane-admin-keys ${VERIFY_FLAG:+--verify})"

# Step 17a-g — data-plane services up, and the edge authorising them. Env-gated to
# dev-public, the only environment whose Caddy edge serves the data plane.
#
# The host probes reach each service's NodePort on the core cluster (infra/k3d/
# core-cluster.yaml): memory /v1/health 200, rag /v1/health 200 (HTTP surface contract
# B-115), lens /v1/healthz 200.
#
# The edge probe is `loopzero-ops seed data-plane-edge`: an anonymous call must be refused
# by loopzero-cloud's forward-auth itself. It replaced three `curl … = 401` checks, which
# the compose Traefik this edge used before #470 passed just as well — a 401 from the
# service, or from a different authoriser, looked identical to them.
if [[ "${LOOPZERO_HELMFILE_ENV:-}" == "dev-public" ]]; then
  DP_EDGE="${LOOPZERO_DATA_PLANE_EDGE:-https://app-dev.loopzero.example}"
  run_step "17a-data-plane-memory-host" "curl -fsS --max-time 5 http://localhost:18081/v1/health -o /dev/null"
  run_step "17c-data-plane-rag-host" "curl -fsS --max-time 5 http://localhost:18082/v1/health -o /dev/null"
  run_step "17e-data-plane-lens-host" "curl -fsS --max-time 5 http://localhost:18084/v1/healthz -o /dev/null"
  run_step "17g-seed-data-plane-edge" "(cd $REPO_ROOT/cmd/loopzero-ops && LOOPZERO_EDGE_URL=$DP_EDGE go run . seed data-plane-edge)"
else
  skip_step "17a-data-plane-memory-host" "skipped — only applicable in dev-public (current: ${LOOPZERO_HELMFILE_ENV:-unset})"
  skip_step "17c-data-plane-rag-host" "skipped — only applicable in dev-public (current: ${LOOPZERO_HELMFILE_ENV:-unset})"
  skip_step "17e-data-plane-lens-host" "skipped — only applicable in dev-public (current: ${LOOPZERO_HELMFILE_ENV:-unset})"
  skip_step "17g-seed-data-plane-edge" "skipped — only applicable in dev-public (current: ${LOOPZERO_HELMFILE_ENV:-unset})"
fi

# Summary
log ""
log "=== canonical chain summary ==="
for r in "${RESULTS[@]}"; do
  log "  $r"
done

VERDICT="PASS"
[[ "$FAIL" -gt 0 ]] && VERDICT="FAIL"

if [[ "$JSON" == "1" ]]; then
  RESULTS_JSON=$(printf '"%s",' "${RESULTS[@]}" | sed 's/,$//')
  printf '{"pass":%d,"fail":%d,"skip":%d,"total":%d,"verdict":"%s","results":[%s]}\n' \
    "$PASS" "$FAIL" "$SKIP" "$((PASS + FAIL + SKIP))" "$VERDICT" "$RESULTS_JSON"
else
  log "VERDICT: $VERDICT (PASS=$PASS FAIL=$FAIL SKIP=$SKIP)"
fi

[[ "$FAIL" == "0" ]] || exit 1
exit 0
