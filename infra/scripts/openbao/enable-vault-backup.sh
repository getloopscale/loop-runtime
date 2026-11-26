#!/usr/bin/env bash
# enable-vault-backup.sh — Codifies Runbook P4 (backup enable via D12).
#
# Manual P4 procedure (3 steps) requer: edit env values + create K8s Secret
# from operator-provisioned `~/.openbao-<env>-resources.env` + helmfile sync.
#
# This script codifies step 2 (Secret creation) + step 3 (helmfile sync) +
# step 4 (verify CronJob registered). Step 1 (edit values.yaml) remains manual
# because changing yaml-as-code is not a one-shot operation — operator
# decides if it's per-env override or global.
#
# Failure modes prevented:
# - Sourcing wrong env file → wrong creds Secret → CronJob fails silently
# - Secret key name typo (must match openbao/openbao-snapshot-agent expected
#   env var names: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
# - Helmfile sync without snapshotAgentEnabled=true → release not installed
# - Re-running on existing Secret → kubectl create errors (use apply pattern)
#
# Usage:
#   bash infra/scripts/openbao/enable-vault-backup.sh \
#     --env vault-local \
#     [--creds-file ~/.openbao-vault-local-resources.env] \
#     [--vault-context k3d-loopruntime-secrets] \
#     [--vault-namespace secrets-system] \
#     [--helmfile-path infra/helmfile/secrets.helmfile.yaml.gotmpl] \
#     [--dry-run] \
#     [--skip-helmfile]  # only sync Secret, skip helmfile (manual flip)
#
# Pre-flight:
#   - kubectl + helmfile in PATH
#   - Creds file has OPENBAO_BACKUP_ACCESS_KEY_ID + OPENBAO_BACKUP_SECRET_ACCESS_KEY
#   - env vars snapshotAgentEnabled=true em env values OR sponsor override
#
# Plan: 2026-06-02-loopruntime-api-vault-integration v1.8 Runbook P4 codification.
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

# ----------------------------------------------------------------------------
# CLI args
# ----------------------------------------------------------------------------
ENV=""
CREDS_FILE=""
SECRETS_CONTEXT="${LOOPRUNTIME_SECRETS_CONTEXT:-k3d-loopruntime-secrets}"
VAULT_NAMESPACE="secrets-system"
HELMFILE_PATH="${REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || echo .)}/infra/helmfile/secrets.helmfile.yaml.gotmpl"
DRY_RUN=0
SKIP_HELMFILE=0
SECRET_NAME="openbao-snapshot-s3-creds"

usage() {
  cat <<EOF
enable-vault-backup.sh — Codify Runbook P4 backup enable workflow.

USAGE
  $0 --env <env> [options]

REQUIRED
  --env <env>          LoopRuntime env (vault-local|dev|drill|staging|prod)

OPTIONAL
  --creds-file <path>  Operator creds file (default: ~/.openbao-<env>-resources.env)
  --vault-context <ctx>   kubectl context (default: k3d-loopruntime-secrets; env LOOPRUNTIME_SECRETS_CONTEXT)
  --vault-namespace <ns>  Vault NS (default: secrets-system)
  --helmfile-path <p>  Path to secrets.helmfile (default: infra/helmfile/secrets.helmfile.yaml.gotmpl)
  --skip-helmfile      Sync Secret only; skip helmfile sync (operator does it manually)
  --dry-run            Print planned actions without executing
  --help               Show this help

WORKFLOW (Runbook P4 steps 2-4 codified)
  1. (manual operator step — flip snapshotAgentEnabled=true em env values.yaml)
  2. Source creds file + create/update openbao-snapshot-s3-creds K8s Secret
  3. Helmfile sync openbao-snapshot-agent release (skipped if --skip-helmfile)
  4. Verify CronJob registered + report next-run schedule

EXAMPLES
  # Vault-local (k3d):
  $0 --env vault-local

  # Production with explicit creds file:
  $0 --env prod --creds-file ~/.openbao-prod-resources.env

  # Skip helmfile sync (operator flipped values + will sync separately):
  $0 --env staging --skip-helmfile

PRE-REQUISITES
  - Creds file ($HOME/.openbao-<env>-resources.env) provisioned via
    infra/scripts/openbao/provision-aws-resources.sh; chmod 600.
  - Must contain: OPENBAO_BACKUP_ACCESS_KEY_ID + OPENBAO_BACKUP_SECRET_ACCESS_KEY
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --env) ENV="$2"; shift 2 ;;
    --creds-file) CREDS_FILE="$2"; shift 2 ;;
    --vault-context) SECRETS_CONTEXT="$2"; shift 2 ;;
    --vault-namespace) VAULT_NAMESPACE="$2"; shift 2 ;;
    --helmfile-path) HELMFILE_PATH="$2"; shift 2 ;;
    --skip-helmfile) SKIP_HELMFILE=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *) echo "ERROR: unknown arg: $1" >&2; usage; exit 1 ;;
  esac
done

# ----------------------------------------------------------------------------
# Pre-flight
# ----------------------------------------------------------------------------
if [ -z "$ENV" ]; then echo "ERROR: --env required" >&2; usage; exit 1; fi

# Resolve default creds file path AFTER --env known
if [ -z "$CREDS_FILE" ]; then
  CREDS_FILE="$HOME/.openbao-${ENV}-resources.env"
fi

for tool in kubectl; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "ERROR: $tool not in PATH" >&2; exit 1
  fi
done

if [ "$SKIP_HELMFILE" -eq 0 ] && [ "$DRY_RUN" -eq 0 ]; then
  if ! command -v helmfile >/dev/null 2>&1; then
    echo "ERROR: helmfile not in PATH (use --skip-helmfile to sync Secret only)" >&2; exit 1
  fi
fi

if [ ! -f "$CREDS_FILE" ]; then
  echo "ERROR: creds file not found: $CREDS_FILE" >&2
  echo "       Provision via infra/scripts/openbao/provision-aws-resources.sh --env $ENV" >&2
  exit 1
fi

# Sanity-check creds file permissions (warn if world-readable)
if [ -r "$CREDS_FILE" ]; then
  perms=$(stat -c '%a' "$CREDS_FILE" 2>/dev/null || stat -f '%A' "$CREDS_FILE" 2>/dev/null || echo "?")
  if [ "$perms" != "600" ] && [ "$perms" != "?" ]; then
    echo "[WARN] creds file permissions $perms (should be 600); continuing" >&2
  fi
fi

if [ "${SKIP_PREFLIGHT:-0}" != "1" ] && [ "$DRY_RUN" -eq 0 ]; then
  if ! kubectl --context "$SECRETS_CONTEXT" version --request-timeout=5s >/dev/null 2>&1; then
    echo "ERROR: kubectl context '$SECRETS_CONTEXT' not reachable" >&2
    exit 1
  fi
fi

# ----------------------------------------------------------------------------
# Source creds + extract required keys
# ----------------------------------------------------------------------------
# shellcheck disable=SC1090
set -a
source "$CREDS_FILE"
set +a

if [ -z "${OPENBAO_BACKUP_ACCESS_KEY_ID:-}" ]; then
  echo "ERROR: OPENBAO_BACKUP_ACCESS_KEY_ID not set in $CREDS_FILE" >&2
  exit 1
fi
if [ -z "${OPENBAO_BACKUP_SECRET_ACCESS_KEY:-}" ]; then
  echo "ERROR: OPENBAO_BACKUP_SECRET_ACCESS_KEY not set in $CREDS_FILE" >&2
  exit 1
fi

echo "[INFO] env=$ENV creds=$CREDS_FILE vault-ctx=$SECRETS_CONTEXT dry-run=$DRY_RUN" >&2
echo "[INFO] Secret target: $SECRETS_CONTEXT:$VAULT_NAMESPACE/$SECRET_NAME" >&2
echo "" >&2

# ----------------------------------------------------------------------------
# Step 1: Create/update K8s Secret (idempotent via dry-run | apply pattern)
# ----------------------------------------------------------------------------
echo "[STEP 1/3] Create/update Secret $SECRET_NAME em $VAULT_NAMESPACE" >&2
if [ "$DRY_RUN" -eq 1 ]; then
  echo "  [DRY-RUN] kubectl create secret generic $SECRET_NAME" >&2
  echo "  [DRY-RUN]   --from-literal=AWS_ACCESS_KEY_ID=<${#OPENBAO_BACKUP_ACCESS_KEY_ID} chars>" >&2
  echo "  [DRY-RUN]   --from-literal=AWS_SECRET_ACCESS_KEY=<${#OPENBAO_BACKUP_SECRET_ACCESS_KEY} chars>" >&2
  echo "  [DRY-RUN]   --dry-run=client -o yaml | kubectl apply -f -" >&2
else
  if kubectl --context "$SECRETS_CONTEXT" -n "$VAULT_NAMESPACE" create secret generic "$SECRET_NAME" \
      --from-literal=AWS_ACCESS_KEY_ID="$OPENBAO_BACKUP_ACCESS_KEY_ID" \
      --from-literal=AWS_SECRET_ACCESS_KEY="$OPENBAO_BACKUP_SECRET_ACCESS_KEY" \
      --dry-run=client -o yaml \
      | kubectl --context "$SECRETS_CONTEXT" apply -f - >/dev/null 2>&1; then
    echo "  [OK] Secret synced" >&2
  else
    echo "  [FAIL] Secret apply failed" >&2
    exit 2
  fi
fi

# ----------------------------------------------------------------------------
# Step 2: Helmfile sync openbao-snapshot-agent (optional skip)
# ----------------------------------------------------------------------------
echo "" >&2
if [ "$SKIP_HELMFILE" -eq 1 ]; then
  echo "[STEP 2/3] SKIPPED — operator will run helmfile sync separately" >&2
else
  echo "[STEP 2/3] Helmfile sync openbao-snapshot-agent (env=$ENV)" >&2
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "  [DRY-RUN] helmfile -f $HELMFILE_PATH -e $ENV --selector name=openbao-snapshot-agent sync" >&2
  else
    if helmfile -f "$HELMFILE_PATH" -e "$ENV" \
        --kube-context "$SECRETS_CONTEXT" \
        --selector name=openbao-snapshot-agent sync >/dev/null 2>&1; then
      echo "  [OK] helmfile sync complete" >&2
    else
      echo "  [FAIL] helmfile sync — check that snapshotAgentEnabled=true em $ENV env values" >&2
      exit 2
    fi
  fi
fi

# ----------------------------------------------------------------------------
# Step 3: Verify CronJob registered
# ----------------------------------------------------------------------------
echo "" >&2
echo "[STEP 3/3] Verify CronJob registered" >&2
if [ "$DRY_RUN" -eq 1 ]; then
  echo "  [DRY-RUN] kubectl get cronjob openbao-snapshot -n $VAULT_NAMESPACE" >&2
elif [ "$SKIP_HELMFILE" -eq 1 ]; then
  echo "  [SKIP] verification deferred (helmfile not synced)" >&2
else
  schedule=$(kubectl --context "$SECRETS_CONTEXT" -n "$VAULT_NAMESPACE" \
    get cronjob openbao-snapshot -o jsonpath='{.spec.schedule}' 2>/dev/null)
  if [ -n "$schedule" ]; then
    echo "  [OK] CronJob registered (schedule: $schedule)" >&2
  else
    echo "  [WARN] CronJob not yet registered — wait 10s for helmfile reconcile, then re-run verification:" >&2
    echo "         kubectl --context $SECRETS_CONTEXT -n $VAULT_NAMESPACE get cronjob openbao-snapshot" >&2
  fi
fi

# ----------------------------------------------------------------------------
# Final report
# ----------------------------------------------------------------------------
echo "" >&2
if [ "$DRY_RUN" -eq 1 ]; then
  echo "[DRY-RUN COMPLETE] no changes made" >&2
else
  echo "[COMPLETE] Vault backup enabled for env=$ENV" >&2
  echo "" >&2
  echo "Next steps:" >&2
  echo "  - Inspect CronJob logs after first run:" >&2
  echo "      kubectl --context $SECRETS_CONTEXT -n $VAULT_NAMESPACE logs -l app=openbao-snapshot-agent --tail=50" >&2
  echo "  - Verify snapshot uploaded to DO Spaces bucket per env" >&2
fi
exit 0
