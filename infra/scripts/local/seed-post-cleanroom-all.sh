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

# Step 1
if [[ "$SKIP_VAULT" == "1" ]]; then
  skip_step "1-vault-bootstrap-dev" "operator already ran vault chain"
else
  run_step "1-vault-bootstrap-dev" "bash $SCRIPT_DIR/vault-bootstrap-dev.sh" || log "  (continuing — vault may already be bootstrapped)"
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

# Step 4
if [[ -x "$SCRIPT_DIR/patch-kratos-dev-public.sh" ]]; then
  run_step "4-patch-kratos-dev-public" "bash $SCRIPT_DIR/patch-kratos-dev-public.sh --verify"
else
  skip_step "4-patch-kratos-dev-public" "script not yet present in canonical chain"
fi

# Step 4b (NEW 2026-06-16) — Hydra URLs canonical (sibling of patch-kratos)
run_step "4b-seed-hydra-urls-canonical" "bash $SCRIPT_DIR/seed-hydra-urls-canonical.sh $VERIFY_FLAG"

# Step 3 (re-ordered 2026-06-18 per Finding #4 — admin login submit
# requires Kratos ConfigMap canonical URLs from Steps 4 + 4b first)
run_step "3-seed-admin-loopzero-dev" "bash $SCRIPT_DIR/seed-admin-loopzero-dev.sh"

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
run_step "9-seed-tenant-fixture-canonical" "bash $SCRIPT_DIR/seed-tenant-fixture-canonical.sh"

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
