#!/usr/bin/env bash
# Canonical POST-CLEANROOM master seed — orchestrates the 7-step chain
# required between `task setup` completion and dogfood-all --live-only.
#
# Per `.claude/rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL +
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
# 2. seed-minio-canonical.sh       — MinIO password reset (mgmt + build)
# 3. seed-admin-loopzero-dev.sh     — Kratos identity + tenant + session JWT
# 4. patch-kratos-dev-public.sh    — Kratos ConfigMap URLs + cookies
# 5. seed-github-app-dev.sh        — GitHub App PEM + webhook secret
# 6. seed-gitea-token-canonical.sh — Gitea API token mint + 3-way sync
# 7. seed-broker-canonical.sh      — loopzero-broker token + CSS + envFrom
# 8. seed-loki-cross-cluster.sh    — Loki NodePort + Promtail URL +
#                                    loopzero-api LOKI_URL
# 9. seed-tenant-fixture-canonical.sh — subscription plan + GH installation
#
# Per cleanroom-up-live-test-mandate.md INQUEBRÁVEL: this script CANNOT
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

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

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
    RESULTS+=("PASS: $name")
    PASS=$((PASS + 1))
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

VERIFY_FLAG=""
[[ "$VERIFY" == "1" ]] && VERIFY_FLAG="--verify"

# Step 0 (NEW 2026-06-20) — auto-provision the separate loopzero-vault k3d
# cluster + OpenBao chart BEFORE Step 1 (vault-bootstrap-dev.sh) attempts
# init/unseal. Gated on LOOPZERO_HELMFILE_ENV=dev-public per ADR
# 2026-06-20-dev-public-vault-automated-init (sponsor-accepted risk: the
# dev-public TEST tier uses an AUTOMATED Shamir init; the witness +
# distributed-shard ceremony is scoped to prod/persistent vaults only).
#
# Jeito A INQUEBRÁVEL: this uses `task k3d:vault:create` +
# `task k3d:vault:bootstrap` against the SEPARATE k3d-loopzero-vault cluster.
# `task k3d:vault:bootstrap-in-mgmt` remains PROHIBITED (independent of
# witness scope — separate-cluster topology violation).
#
# Both tasks are idempotent: k3d:vault:create skips if cluster exists;
# k3d:vault:bootstrap re-syncs helm releases (cert-manager + cluster-issuer
# + openbao-cert + openbao chart) without destroying existing state. So
# this Step 0 is a no-op on a warm-vault re-run, and the only cost on a
# Cleanroom-fresh run is the cluster create + ~3min helm sync.
#
# In other envs (standalone-multi / unset / prod) this Step 0 is SKIPPED —
# standalone doesn't run loopzero-vault, and prod requires the witnessed
# ceremony per the mandate § Sub-step 2.5 prod/persistent path.
if [[ "${LOOPZERO_HELMFILE_ENV:-}" == "dev-public" && "$SKIP_VAULT" != "1" ]]; then
  REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
  run_step "0a-vault-cluster-create" "cd $REPO_ROOT && task k3d:vault:create"
  run_step "0b-vault-cluster-bootstrap" "cd $REPO_ROOT && task k3d:vault:bootstrap"
else
  skip_step "0a-vault-cluster-create" "skipped — only applicable in LOOPZERO_HELMFILE_ENV=dev-public (current: ${LOOPZERO_HELMFILE_ENV:-unset})"
  skip_step "0b-vault-cluster-bootstrap" "skipped — only applicable in LOOPZERO_HELMFILE_ENV=dev-public (current: ${LOOPZERO_HELMFILE_ENV:-unset})"
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
# Only meaningful under Jeito A (separate loopzero-vault cluster); single-
# cluster runs no-op via the script's own preflight.
if [[ "$SKIP_VAULT" == "1" ]]; then
  skip_step "1b-vault-bootstrap-eso" "skipped together with step 1 (operator already ran vault chain)"
else
  ESO_BOOTSTRAP="$SCRIPT_DIR/../openbao/vault-bootstrap-eso.sh"
  if [[ -f "$ESO_BOOTSTRAP" ]]; then
    run_step "1b-vault-bootstrap-eso" "bash $ESO_BOOTSTRAP --mgmt-context k3d-loopzero-mgmt --vault-context k3d-loopzero-vault" || \
      log "  (continuing — CSS loopzero-vault-secrets may need operator triage)"
  else
    skip_step "1b-vault-bootstrap-eso" "vault-bootstrap-eso.sh not found at $ESO_BOOTSTRAP (skipped)"
  fi
fi

# Step 2
run_step "2-seed-minio-canonical" "bash $SCRIPT_DIR/seed-minio-canonical.sh $VERIFY_FLAG"

# CANONICAL ORDER FIX 2026-06-18 (Finding #4 of issue #91 — Cleanroom
# mandate canonical chain run): Step 4 (patch-kratos-dev-public) MUST
# precede Step 3 (seed-admin-loopzero-dev) because Step 3 HTTP-POSTs a
# login submit against the canonical Kratos URL (auth-dev.loopzero.example)
# and Kratos rejects the request with HTTP 000 when the ConfigMap is
# still helm-default (localhost:4455 + missing cors / cookies.domain).
# Same family: Step 4b (hydra URLs canonical) must also precede admin
# so the OIDC callback chain is consistent. Memory
# feedback_post_cleanroom_kratos_patch_dev_public already documents
# the dependency — this re-ordering codifies it in the master script.

# ENV-GATE 2026-06-19 (issue #99 of cleanroom-mandate-run-2026-06-19):
# Steps 4 + 4b configure dev-public URLs (auth-dev.loopzero.example,
# app-dev.loopzero.example) which are INAPPLICABLE in standalone-multi /
# drill / prod envs. patch-kratos-dev-public.sh also requires
# LOOPZERO_GITHUB_CLIENT_ID + LOOPZERO_GOOGLE_CLIENT_ID OAuth credentials
# which operators typically only provision for dev-public. Running
# these steps unconditionally produced VERDICT=FAIL when
# `LOOPZERO_HELMFILE_ENV=standalone-multi task setup` operators ran
# the master seed afterwards (and CASCADE-failed Steps 3, 9, 11
# which depend on the dev-public Kratos config). Canonical fix:
# gate both steps on `LOOPZERO_HELMFILE_ENV == dev-public` with a clear
# SKIP message in other envs. Defaults to SKIP when env var unset.
if [[ "${LOOPZERO_HELMFILE_ENV:-}" == "dev-public" ]]; then
  # Step 4
  if [[ -x "$SCRIPT_DIR/patch-kratos-dev-public.sh" ]]; then
    run_step "4-patch-kratos-dev-public" "bash $SCRIPT_DIR/patch-kratos-dev-public.sh --verify"
  else
    skip_step "4-patch-kratos-dev-public" "script not yet present in canonical chain"
  fi

  # Step 4b (NEW 2026-06-16) — Hydra URLs canonical (sibling of patch-kratos)
  run_step "4b-seed-hydra-urls-canonical" "bash $SCRIPT_DIR/seed-hydra-urls-canonical.sh $VERIFY_FLAG"
else
  skip_step "4-patch-kratos-dev-public" "skipped — only applicable in LOOPZERO_HELMFILE_ENV=dev-public (current: ${LOOPZERO_HELMFILE_ENV:-unset})"
  skip_step "4b-seed-hydra-urls-canonical" "skipped — only applicable in LOOPZERO_HELMFILE_ENV=dev-public (current: ${LOOPZERO_HELMFILE_ENV:-unset})"
fi

# Step 3 (re-ordered 2026-06-18 per Finding #4 — admin login submit
# requires Kratos ConfigMap canonical URLs from Steps 4 + 4b first)
run_step "3-seed-admin-loopzero-dev" "bash $SCRIPT_DIR/seed-admin-loopzero-dev.sh"

# Step 3b (NEW 2026-06-29 per Red Team #117 closure) — QA Red Team
# canonical identity. The seed-admin script is generic over EMAIL +
# DISPLAY_NAME + USERNAME + TENANT_SLUG + SESSION_FILE env overrides,
# so we re-invoke it to provision paulohenrique.java@gmail.com (the
# QA operator's working inbox, accessible at gmail /u/2). Without
# this step, every Cleanroom obliterates the QA's identity and
# recovery returns the anti-enumeration mute for their address.
# Session is persisted to a separate file (~/.loopzero/redteam-qa-*) so
# the admin session at the canonical path is not overwritten.
run_step "3b-seed-redteam-qa-identity" "EMAIL=paulohenrique.java@gmail.com DISPLAY_NAME='Red Team QA' USERNAME=redteam-qa TENANT_SLUG=redteam-qa SESSION_FILE=\$HOME/.loopzero/redteam-qa-session.txt SESSION_JWT_FILE=\$HOME/.loopzero/redteam-qa-session-jwt.txt bash $SCRIPT_DIR/seed-admin-loopzero-dev.sh"

# Step 5
run_step "5-seed-github-app-dev" "bash $SCRIPT_DIR/seed-github-app-dev.sh $VERIFY_FLAG"

# Step 6
run_step "6-seed-gitea-token-canonical" "bash $SCRIPT_DIR/seed-gitea-token-canonical.sh $VERIFY_FLAG"

# Step 6b (NEW 2026-06-16) — ArgoCD ↔ Gitea repo-creds (MUST run AFTER step 6 so token is canonical)
run_step "6b-seed-argocd-gitea-creds-canonical" "bash $SCRIPT_DIR/seed-argocd-gitea-creds-canonical.sh --hard-refresh"

# Step 7
run_step "7-seed-broker-canonical" "bash $SCRIPT_DIR/seed-broker-canonical.sh $VERIFY_FLAG"

# Step 7b (NEW 2026-06-16) — Vault role_id sync into loopzero-api (MUST run AFTER vault-bootstrap-dev)
run_step "7b-seed-vault-role-id-sync" "bash $SCRIPT_DIR/seed-vault-role-id-sync.sh $VERIFY_FLAG"

# Step 8
run_step "8-seed-loki-cross-cluster" "bash $SCRIPT_DIR/seed-loki-cross-cluster.sh $VERIFY_FLAG"

# Step 9
# #123 (qa-dogfood-cleanup-2026-06-21 T2.2 / ADR-Q7): pass `--team`
# explicitly so any inherited `LOOPZERO_PLAN_TIER` env (operator shell
# default, stale dev-credentials.json residue) cannot override the
# canonical dev-public tier. QA dogfooding 2026-06-21 caught the
# Scale page showing `starter` plan ceiling 2 even after the canonical
# chain ran — root cause was the env-shadowing path.
run_step "9-seed-tenant-fixture-canonical" "bash $SCRIPT_DIR/seed-tenant-fixture-canonical.sh --team"

# Step 10 (NEW 2026-06-16) — admin-cli Hydra OAuth2 client.
# Required for dogfood-login Suite 0 T5.4 (≥ 3 Hydra clients).
# Complements bootstrap-oauth-clients.sh (which provisions loopzero-cli +
# loopzero-bootstrap) with the admin-side client_credentials path.
# Per reference_admin_cli_hydra_client_canonical memory.
if [[ -x "$SCRIPT_DIR/seed-admin-hydra-client-canonical.sh" ]]; then
  run_step "10-seed-admin-hydra-client-canonical" "bash $SCRIPT_DIR/seed-admin-hydra-client-canonical.sh $VERIFY_FLAG"
else
  skip_step "10-seed-admin-hydra-client-canonical" "script not yet present"
fi

# Step 11 (NEW 2026-06-16) — connect canonical fixture (project "teste" +
# repo link). Required by LIVE wrappers that probe /projects/teste/* paths.
# Depends on step 10 (admin Hydra client) only indirectly via session JWT.
if [[ -x "$SCRIPT_DIR/connect-canonical-fixture.sh" ]]; then
  run_step "11-connect-canonical-fixture" "bash $SCRIPT_DIR/connect-canonical-fixture.sh"
else
  skip_step "11-connect-canonical-fixture" "script not yet present"
fi

# Step 12 (NEW M2 2026-06-23) — chaos-mesh canonical (mgmt-only, universal)
run_step "12-seed-chaos-mesh-canonical" "bash $SCRIPT_DIR/seed-chaos-mesh-canonical.sh $VERIFY_FLAG"

# Step 13 (NEW M2 2026-06-23) — ArgoCD admin token
# EC-3 absorption (v1.1): env-gate dev-public OR standalone-multi — ArgoCD não existe em outros envs
if [[ "${LOOPZERO_HELMFILE_ENV:-}" == "dev-public" || "${LOOPZERO_HELMFILE_ENV:-}" == "standalone-multi" ]]; then
  run_step "13-seed-argocd-token-canonical" "bash $SCRIPT_DIR/seed-argocd-token-canonical.sh $VERIFY_FLAG"
else
  skip_step "13-seed-argocd-token-canonical" "skipped — only applicable in dev-public/standalone-multi (current: ${LOOPZERO_HELMFILE_ENV:-unset})"
fi

# Step 14 (NEW M2 2026-06-23) — OPENBAO_VAULT_ADDR cross-cluster (dev-public only — Jeito A)
if [[ "${LOOPZERO_HELMFILE_ENV:-}" == "dev-public" ]]; then
  run_step "14-seed-vault-cross-cluster-addr" "bash $SCRIPT_DIR/seed-vault-cross-cluster-addr-canonical.sh $VERIFY_FLAG"
else
  skip_step "14-seed-vault-cross-cluster-addr" "skipped — only applicable in dev-public (current: ${LOOPZERO_HELMFILE_ENV:-unset})"
fi

# Step 15 (NEW M2 2026-06-23) — loopzero-stripe-webhook Secret materialization (universal; SKIP-graceful on placeholder)
run_step "15-seed-stripe-secret-canonical" "bash $SCRIPT_DIR/seed-stripe-secret-canonical.sh $VERIFY_FLAG"

# Step 15b (NEW 2026-06-24 — plan stripe-l2-wiring-canonical-fix T3.2) —
# loopzero-stripe-api-keys Secret materialization (sibling of 15 per ADR D1;
# separate rotation cadence: webhook secret vs API key per Stripe runbook).
run_step "15b-seed-stripe-api-keys-canonical" "bash $SCRIPT_DIR/seed-stripe-api-keys-canonical.sh $VERIFY_FLAG"

# Step 16 (NEW 2026-07-01 — plan dashboard-badge-data-plane-poller T6.1) —
# data-plane poller first-tick warm-up. EC-2 absorption: without this,
# the first /projects render post `task setup` is a wall of `Unknown`
# badges (up to POLLER_INTERVAL seconds until the first natural poller
# tick populates env_targets.last_replicas_ready_*). Rather than add a
# bespoke --first-tick-sync CLI flag (parsimony rung 5), we simply wait
# one canonical interval + a small buffer for the poller's ticker to
# fire naturally. Env-gated: only fires when the operator is expected
# to be running the poller (LOOPZERO_DATA_PLANE_POLLER_ENABLED=true — which
# app-operator's env-gate honors). The wait is idempotent + cheap.
if [[ "${LOOPZERO_DATA_PLANE_POLLER_ENABLED:-}" == "true" ]]; then
  POLLER_WARMUP_S="${LOOPZERO_DATA_PLANE_POLLER_WARMUP_S:-45}"
  run_step "16-data-plane-poller-first-tick" "sleep $POLLER_WARMUP_S && log '   data-plane poller warmed up (${POLLER_WARMUP_S}s wait)' && true"
else
  skip_step "16-data-plane-poller-first-tick" "skipped — LOOPZERO_DATA_PLANE_POLLER_ENABLED != true (poller not wired for this env)"
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
