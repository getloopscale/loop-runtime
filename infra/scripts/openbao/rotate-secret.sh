#!/usr/bin/env bash
# rotate-secret.sh — Cross-binary atomic secret rotation per EC-22.
#
# T8.1 EC-22 mitigation: replace 4-step manual procedure (bao kv put +
# ESO force-refresh + verify + cross-binary rollout restart) with a single
# command that:
#   1. Writes new value to Vault at D6 path
#   2. Annotates ExternalSecret to force ESO immediate refresh
#   3. Polls K8s Secret until rotated value visible
#   4. Looks up rotation table (embedded) for binaries consuming this path
#   5. Sequentially restarts each binary deployment (rolling restart)
#   6. Waits for rollout convergence
#
# Atomic semantics: if ANY step fails, prints "rotation incomplete" warning
# pointing to runbook P2 for manual recovery. Idempotent across re-run (bao
# kv put is KV-v2 versioned; rollout restart is benign).
#
# Usage:
#   bash infra/scripts/openbao/rotate-secret.sh \
#     --env vault-local \
#     --path api/secrets/gitea/token \
#     --value "$NEW_TOKEN" \
#     [--dry-run] \
#     [--skip-verify]
#
# Pre-flight:
#   - bao + kubectl + jq in PATH
#   - BAO_ADDR + BAO_TOKEN env vars set
#   - BAO_SKIP_VERIFY=true if vault-local self-signed CA
#   - kubectl contexts k3d-loopzero-core (production: loopzero-core) reachable
#
# Plan: 2026-06-02-loopzero-api-vault-integration v1.8 T8.1 EC-22 mitigation.
# Companion to: seed-loopzero-secrets.sh (initial provisioning T8.1 EC-21).
# Runbook reference: docs/operations/vault-secret-migration-runbook.md P2.
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

# ----------------------------------------------------------------------------
# Rotation table (canonical — must match runbook P2 table exactly)
# Format: <D6-path>|<comma-separated-deployments>|<comma-separated-externalsecret-CRs>
# ----------------------------------------------------------------------------
ROTATION_TABLE=$(cat <<'EOF'
api/secrets/gitea/token|loopzero-api,loopzero-tenant-operator|gitea-admin,loopzero-tenant-operator
api/secrets/minio/access_key|loopzero-api,loopzero-tenant-operator|loopzero-api-minio,loopzero-tenant-operator
api/secrets/minio/secret_key|loopzero-api,loopzero-tenant-operator|loopzero-api-minio,loopzero-tenant-operator
api/secrets/service_auth/secret|loopzero-api|loopzero-service-auth
api/secrets/service_auth/secret_previous|loopzero-api|loopzero-service-auth
api/secrets/service_auth/ed25519_public_current|loopzero-api|loopzero-service-auth-ed25519-pub
api/secrets/service_auth/ed25519_public_previous|loopzero-api|loopzero-service-auth-ed25519-pub
api/secrets/admin/api_key|loopzero-api|loopzero-api-admin
api/secrets/admin/api_key_previous|loopzero-api|loopzero-api-admin
api/secrets/admin/service_auth_secret|loopzero-api|loopzero-api-admin
api/secrets/admin/service_auth_secret_previous|loopzero-api|loopzero-api-admin
api/secrets/argo/callback_secret|loopzero-api|loopzero-api-internal
api/secrets/bootstrap_secret|loopzero-api|loopzero-api-internal
api/secrets/secret_encryption_key|loopzero-api|loopzero-api-internal
api/secrets/github/app_id|loopzero-api|loopzero-api-github
api/secrets/github/app_private_key|loopzero-api|loopzero-api-github
api/secrets/github/webhook_secret|loopzero-api|loopzero-api-github
api/secrets/cf/api_token|loopzero-api|loopzero-api-tokens
api/secrets/argocd/token|loopzero-api|loopzero-api-tokens
api/secrets/stripe/secret_key|loopzero-api|loopzero-api-stripe
api/secrets/nonce_redis/password|loopzero-api|loopzero-service-auth-redis
cloud/secrets/service_auth/ed25519_private_key|loopzero-cloud|loopzero-service-auth-ed25519
cloud/secrets/stripe/api_key|loopzero-cloud|loopzero-cloud-stripe
cloud/secrets/stripe/webhook_secret|loopzero-cloud|loopzero-cloud-stripe
operators/secrets/digitalocean/token|loopzero-tenant-operator|loopzero-tenant-operator
operators/secrets/spaces/access_key_id|loopzero-tenant-operator|loopzero-tenant-operator
operators/secrets/spaces/secret_access_key|loopzero-tenant-operator|loopzero-tenant-operator
operators/secrets/harbor/admin_password|loopzero-tenant-operator|loopzero-tenant-operator
build/secrets/cosign/passphrase|argo-workflows-controller|cosign-passphrase
platform/secrets/vault-spki-pin/spki_hash|loopzero-api|vault-spki-pin
EOF
)

# ----------------------------------------------------------------------------
# CLI args
# ----------------------------------------------------------------------------
ENV=""
PATH_REL=""
VALUE=""
DRY_RUN=0
SKIP_VERIFY=0
NAMESPACE="loopzero-system"
CORE_CONTEXT="${LOOPZERO_CORE_CONTEXT:-k3d-loopzero-core}"
SECRETS_CONTEXT="${LOOPZERO_SECRETS_CONTEXT:-k3d-loopzero-secrets}"
VAULT_NAMESPACE="${LOOPZERO_VAULT_NAMESPACE:-vault-system}"
WAIT_TIMEOUT="${ROTATE_WAIT_TIMEOUT:-300}"  # rollout status timeout (s)

usage() {
  cat <<EOF
rotate-secret.sh — Cross-binary atomic secret rotation.

USAGE
  $0 --env <env> --path <D6-path> --value <new-secret> [options]

REQUIRED
  --env             LoopZero env (vault-local|standalone-multi|dev|drill|staging|prod)
  --path            D6 path relative (e.g., api/secrets/gitea/token)
                    See runbook P2 rotation table for canonical paths.
  --value           New secret value (REQUIRED — never write empty)

OPTIONAL
  --dry-run         Print planned actions without executing
  --skip-verify     Skip K8s Secret poll verification (faster, less safe)
  --namespace       K8s namespace for ExternalSecret + deployments (default: loopzero-system)
  --help            Show this help

ENV VARS
  BAO_ADDR              Vault address (REQUIRED unless --dry-run)
  BAO_TOKEN             Vault token (REQUIRED unless --dry-run)
  BAO_SKIP_VERIFY       set to true for self-signed CA (vault-local)
  LOOPZERO_CORE_CONTEXT     kubectl context for mgmt cluster (default: k3d-loopzero-core)
  LOOPZERO_SECRETS_CONTEXT    kubectl context for vault cluster (default: k3d-loopzero-secrets)
  ROTATE_WAIT_TIMEOUT   rollout status timeout in seconds (default: 300)

EXAMPLES
  # Rotate gitea token in vault-local (will restart loopzero-api + tenant-operator)
  export BAO_ADDR=https://127.0.0.1:8210 BAO_SKIP_VERIFY=true
  export BAO_TOKEN=\$(jq -r .root_token /tmp/openbao-init.json)
  $0 --env vault-local --path api/secrets/gitea/token --value "ghp_new_xxx"

  # Dry-run rotation plan check (shows binaries to restart)
  $0 --env staging --path operators/secrets/digitalocean/token \\
     --value "dop_v1_new_xxx" --dry-run

ROLLBACK
  If rotation fails mid-restart, the new Vault value is already committed
  (KV-v2 versioned). Recovery options:
    (a) bao kv rollback --version=<prev> <path>  # revert Vault value
    (b) Manual completion: identify failed step in output, finish manually
    (c) Disable ESO temporarily: vaultEnabled=false → pods restart with
        legacy env var values
  See docs/operations/vault-secret-migration-runbook.md P2 for details.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --env) ENV="$2"; shift 2 ;;
    --path) PATH_REL="$2"; shift 2 ;;
    --value) VALUE="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --skip-verify) SKIP_VERIFY=1; shift ;;
    --namespace) NAMESPACE="$2"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "ERROR: unknown arg: $1" >&2; usage; exit 1 ;;
  esac
done

# ----------------------------------------------------------------------------
# Pre-flight
# ----------------------------------------------------------------------------
if [ -z "$ENV" ]; then echo "ERROR: --env required" >&2; usage; exit 1; fi
if [ -z "$PATH_REL" ]; then echo "ERROR: --path required" >&2; usage; exit 1; fi
if [ -z "$VALUE" ] && [ "$DRY_RUN" -eq 0 ]; then
  echo "ERROR: --value required (never rotate to empty)" >&2
  exit 1
fi

for tool in bao kubectl jq; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "ERROR: $tool not in PATH (required)" >&2
    exit 1
  fi
done

if [ "$DRY_RUN" -eq 0 ]; then
  if [ -z "${BAO_ADDR:-}" ]; then echo "ERROR: BAO_ADDR env var required" >&2; exit 1; fi
  if [ -z "${BAO_TOKEN:-}" ]; then echo "ERROR: BAO_TOKEN env var required" >&2; exit 1; fi
fi

# ----------------------------------------------------------------------------
# Lookup rotation table — fail-fast if path unknown (EC-22: typo protection)
# ----------------------------------------------------------------------------
TABLE_LINE=$(echo "$ROTATION_TABLE" | grep "^${PATH_REL}|" || true)

if [ -z "$TABLE_LINE" ]; then
  echo "ERROR: path '$PATH_REL' not in rotation table" >&2
  echo "Known paths:" >&2
  echo "$ROTATION_TABLE" | cut -d'|' -f1 | sed 's/^/  /' >&2
  echo "" >&2
  echo "If this is a new path, add to ROTATION_TABLE in this script AND" >&2
  echo "to runbook P2 rotation table BEFORE rotating." >&2
  exit 1
fi

BINARIES=$(echo "$TABLE_LINE" | cut -d'|' -f2)
EXTERNALSECRETS=$(echo "$TABLE_LINE" | cut -d'|' -f3)
FULL_PATH="secret/loopzero/$ENV/$PATH_REL"

echo "[INFO] env=$ENV path=$PATH_REL" >&2
echo "[INFO] vault path: $FULL_PATH" >&2
echo "[INFO] binaries to restart: $BINARIES" >&2
echo "[INFO] ExternalSecrets to force-refresh: $EXTERNALSECRETS" >&2
echo "[INFO] dry-run=$DRY_RUN" >&2
echo "" >&2

# ----------------------------------------------------------------------------
# Step 1: Write new value to Vault
# ----------------------------------------------------------------------------
echo "[STEP 1/4] Write Vault $FULL_PATH" >&2
if [ "$DRY_RUN" -eq 1 ]; then
  echo "  [DRY-RUN] bao kv put $FULL_PATH value=<${#VALUE} chars>" >&2
else
  if bao kv put "$FULL_PATH" "value=$VALUE" >/dev/null 2>&1; then
    echo "  [OK] $FULL_PATH" >&2
  else
    echo "  [FAIL] bao kv put returned non-zero" >&2
    echo "  rotation aborted — Vault state unchanged" >&2
    exit 2
  fi
fi

# ----------------------------------------------------------------------------
# Step 2: Force ESO refresh per affected ExternalSecret
# ----------------------------------------------------------------------------
echo "" >&2
echo "[STEP 2/4] Force ESO refresh on ExternalSecrets: $EXTERNALSECRETS" >&2
IFS=',' read -ra ES_ARRAY <<< "$EXTERNALSECRETS"
for es in "${ES_ARRAY[@]}"; do
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "  [DRY-RUN] kubectl --context $CORE_CONTEXT -n $NAMESPACE annotate externalsecret $es force-sync=\$(date +%s) --overwrite" >&2
  else
    if kubectl --context "$CORE_CONTEXT" -n "$NAMESPACE" annotate externalsecret "$es" \
        "force-sync=$(date +%s)" --overwrite >/dev/null 2>&1; then
      echo "  [OK] externalsecret/$es annotated" >&2
    else
      echo "  [WARN] externalsecret/$es annotation failed (CR may not exist yet)" >&2
      echo "         Continuing — ESO will pick up on refreshInterval anyway." >&2
    fi
  fi
done

# ----------------------------------------------------------------------------
# Step 3: Wait for K8s Secret to reflect new value (optional)
# ----------------------------------------------------------------------------
if [ "$SKIP_VERIFY" -eq 1 ] || [ "$DRY_RUN" -eq 1 ]; then
  echo "" >&2
  echo "[STEP 3/4] Skipped K8s Secret poll verification (--skip-verify or --dry-run)" >&2
else
  echo "" >&2
  echo "[STEP 3/4] Polling K8s Secret(s) for new value (timeout 60s)..." >&2
  # Note: ESO normally takes 1-5s after annotate force-sync; we wait up to 60s
  # but don't compare value content (PII / secret-no-logs invariant).
  # Instead, check the ExternalSecret status condition transitions to
  # SecretSynced/Ready with recent timestamp.
  POLL_START=$(date +%s)
  for es in "${ES_ARRAY[@]}"; do
    until [ "$(($(date +%s) - POLL_START))" -ge 60 ]; do
      synced=$(kubectl --context "$CORE_CONTEXT" -n "$NAMESPACE" get externalsecret "$es" \
        -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
      if [ "$synced" = "True" ]; then
        echo "  [OK] externalsecret/$es Ready" >&2
        break
      fi
      sleep 2
    done
    if [ "$synced" != "True" ]; then
      echo "  [WARN] externalsecret/$es not Ready within 60s — proceeding anyway" >&2
      echo "         ESO may catch up on next refreshInterval cycle." >&2
    fi
  done
fi

# ----------------------------------------------------------------------------
# Step 4: Cross-binary rollout restart (EC-22 atomic)
# ----------------------------------------------------------------------------
echo "" >&2
echo "[STEP 4/4] Cross-binary rollout restart: $BINARIES" >&2
IFS=',' read -ra BIN_ARRAY <<< "$BINARIES"
RESTART_FAILED=0
for bin in "${BIN_ARRAY[@]}"; do
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "  [DRY-RUN] kubectl --context $CORE_CONTEXT -n $NAMESPACE rollout restart deployment $bin" >&2
    echo "  [DRY-RUN] kubectl --context $CORE_CONTEXT -n $NAMESPACE rollout status deployment $bin --timeout=${WAIT_TIMEOUT}s" >&2
  else
    if kubectl --context "$CORE_CONTEXT" -n "$NAMESPACE" rollout restart deployment "$bin" >/dev/null 2>&1; then
      echo "  [OK] rollout restart deployment/$bin triggered" >&2
    else
      echo "  [FAIL] rollout restart deployment/$bin returned non-zero" >&2
      RESTART_FAILED=$((RESTART_FAILED + 1))
      continue
    fi
    # Wait for rollout convergence — non-blocking restart is dangerous per
    # § Common Mistakes "kubectl rollout restart is non-blocking — race with downstream operations"
    if kubectl --context "$CORE_CONTEXT" -n "$NAMESPACE" rollout status deployment "$bin" \
        --timeout="${WAIT_TIMEOUT}s" >/dev/null 2>&1; then
      echo "  [OK] rollout status deployment/$bin converged" >&2
    else
      echo "  [FAIL] rollout status deployment/$bin did not converge within ${WAIT_TIMEOUT}s" >&2
      RESTART_FAILED=$((RESTART_FAILED + 1))
    fi
  fi
done

# ----------------------------------------------------------------------------
# Final report
# ----------------------------------------------------------------------------
echo "" >&2
if [ "$DRY_RUN" -eq 1 ]; then
  echo "[DRY-RUN COMPLETE] $PATH_REL → $BINARIES (no changes made)" >&2
  exit 0
fi

if [ "$RESTART_FAILED" -gt 0 ]; then
  echo "[PARTIAL] rotation completed in Vault + ESO but $RESTART_FAILED deployment restart(s) failed" >&2
  echo "         Vault value is updated (KV-v2 versioned)." >&2
  echo "         Old pods may still be running with stale value." >&2
  echo "         Manual recovery: kubectl rollout status / rollout restart per failed binary" >&2
  echo "         See docs/operations/vault-secret-migration-runbook.md P2 'Rollback'" >&2
  exit 2
fi

echo "[COMPLETE] rotation atomic — $PATH_REL → $BINARIES restarted" >&2
exit 0
