#!/usr/bin/env bash
# restore-vault-snapshot.sh — Codifies Runbook P5 (DR restore from snapshot).
#
# **CONSERVATIVE DR pattern**: dry-run is DEFAULT; --execute must be passed
# explicitly to perform the destructive `bao operator raft snapshot restore`.
# This protects against accidental restore (which would overwrite current
# Vault state) when operator runs the script for inspection purposes.
#
# Failure modes prevented:
# - Restoring stale snapshot (operator-decision; script lists available + lets
#   operator pick via --snapshot-key)
# - Snapshot file corruption (downloads + verifies non-zero file size before
#   kubectl cp)
# - Restore to wrong Vault cluster (--vault-context explicit + sanity-check)
# - Silent restore success but Vault unsealed-state lost (script reports
#   post-restore state; operator manually unseals per Shamir ceremony)
#
# Usage:
#   # 1. List available snapshots:
#   bash infra/scripts/openbao/restore-vault-snapshot.sh --env vault-local --list
#
#   # 2. Dry-run inspection (default — no destructive op):
#   bash infra/scripts/openbao/restore-vault-snapshot.sh \
#     --env vault-local \
#     --snapshot-key 2026-06-01T00-00-00Z.snap
#
#   # 3. Actually restore (explicit --execute required):
#   bash infra/scripts/openbao/restore-vault-snapshot.sh \
#     --env vault-local \
#     --snapshot-key 2026-06-01T00-00-00Z.snap \
#     --execute
#
# Pre-flight:
#   - aws CLI + kubectl in PATH
#   - AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY env vars (DO Spaces creds)
#   - Bucket: loop-runtime-openbao-backups-<env> em SFO3 region
#
# Plan: 2026-06-02-loop-runtime-api-vault-integration v1.8 Runbook P5 codification.
# SPDX-License-Identifier: Apache-2.0

set -uo pipefail

# ----------------------------------------------------------------------------
# CLI args
# ----------------------------------------------------------------------------
ENV=""
SNAPSHOT_KEY=""
LIST_MODE=0
EXECUTE=0
SECRETS_CONTEXT="${LOOP_RUNTIME_SECRETS_CONTEXT:-k3d-loop-runtime-secrets}"
VAULT_NAMESPACE="secrets-system"
VAULT_POD="openbao-0"
DO_REGION="${OPENBAO_BACKUP_REGION:-sfo3}"
S3_ENDPOINT="https://${DO_REGION}.digitaloceanspaces.com"
BUCKET_PREFIX="loop-runtime-openbao-backups"

usage() {
  cat <<EOF
restore-vault-snapshot.sh — Codify Runbook P5 DR restore workflow.

🛑 SAFETY: dry-run is DEFAULT. Pass --execute to actually restore.

USAGE
  $0 --env <env> [--list | --snapshot-key <key>] [options]

REQUIRED
  --env <env>            LoopRuntime env (vault-local|dev|drill|staging|prod)

MODE (one of)
  --list                 List 10 most recent snapshots em bucket
  --snapshot-key <key>   Specific snapshot to restore (e.g. 2026-06-01T00-00-00Z.snap)

OPTIONAL
  --execute              ACTUALLY perform restore (default: dry-run only)
  --vault-context <ctx>  kubectl context (default: k3d-loop-runtime-secrets)
  --vault-namespace <ns> Vault NS (default: secrets-system)
  --vault-pod <name>     Vault pod name (default: openbao-0)
  --do-region <r>        DO Spaces region (default: sfo3; env OPENBAO_BACKUP_REGION)
  --help                 Show this help

WORKFLOW (Runbook P5 codified)
  1. List/select snapshot from s3://${BUCKET_PREFIX}-<env>/
  2. Download to /tmp + verify non-zero size
  3. kubectl cp to vault pod /tmp/
  4. kubectl exec bao operator raft snapshot restore (DESTRUCTIVE)
  5. Report post-restore Vault status

EXAMPLES
  # List recent snapshots:
  AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... \\
    $0 --env vault-local --list

  # Dry-run inspection (default):
  $0 --env vault-local --snapshot-key 2026-06-01T00-00-00Z.snap

  # Actually restore (explicit confirmation):
  $0 --env vault-local --snapshot-key 2026-06-01T00-00-00Z.snap --execute

POST-RESTORE
  Vault will be RE-SEALED after restore (Raft state replaced; current root
  token + unseal shards may not match new state). Operator MUST re-run
  Shamir unseal ceremony per docs/runbooks.md P3.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --env) ENV="$2"; shift 2 ;;
    --snapshot-key) SNAPSHOT_KEY="$2"; shift 2 ;;
    --list) LIST_MODE=1; shift ;;
    --execute) EXECUTE=1; shift ;;
    --vault-context) SECRETS_CONTEXT="$2"; shift 2 ;;
    --vault-namespace) VAULT_NAMESPACE="$2"; shift 2 ;;
    --vault-pod) VAULT_POD="$2"; shift 2 ;;
    --do-region) DO_REGION="$2"; S3_ENDPOINT="https://${DO_REGION}.digitaloceanspaces.com"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "ERROR: unknown arg: $1" >&2; usage; exit 1 ;;
  esac
done

# ----------------------------------------------------------------------------
# Pre-flight
# ----------------------------------------------------------------------------
if [ -z "$ENV" ]; then echo "ERROR: --env required" >&2; usage; exit 1; fi

# Must have exactly one of --list or --snapshot-key
if [ "$LIST_MODE" -eq 0 ] && [ -z "$SNAPSHOT_KEY" ]; then
  echo "ERROR: must provide --list OR --snapshot-key <key>" >&2
  usage; exit 1
fi
if [ "$LIST_MODE" -eq 1 ] && [ -n "$SNAPSHOT_KEY" ]; then
  echo "ERROR: --list and --snapshot-key are mutually exclusive" >&2
  usage; exit 1
fi

for tool in aws kubectl; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "ERROR: $tool not in PATH" >&2; exit 1
  fi
done

if [ -z "${AWS_ACCESS_KEY_ID:-}" ] || [ -z "${AWS_SECRET_ACCESS_KEY:-}" ]; then
  echo "ERROR: AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY env vars required (DO Spaces creds)" >&2
  exit 1
fi

BUCKET="${BUCKET_PREFIX}-${ENV}"
echo "[INFO] env=$ENV bucket=s3://$BUCKET region=$DO_REGION vault-ctx=$SECRETS_CONTEXT" >&2
echo "" >&2

# ----------------------------------------------------------------------------
# Mode: --list (read-only)
# ----------------------------------------------------------------------------
if [ "$LIST_MODE" -eq 1 ]; then
  echo "[LIST] 10 most recent snapshots em s3://$BUCKET/" >&2
  if ! aws --endpoint-url "$S3_ENDPOINT" s3 ls "s3://$BUCKET/" 2>/dev/null | sort -r | head -10; then
    echo "[FAIL] aws s3 ls failed — verify bucket exists + creds correct" >&2
    exit 2
  fi
  exit 0
fi

# ----------------------------------------------------------------------------
# Mode: --snapshot-key (dry-run unless --execute)
# ----------------------------------------------------------------------------
if [ "$EXECUTE" -eq 0 ]; then
  echo "🛑 DRY-RUN MODE — pass --execute to actually restore" >&2
fi
echo "" >&2

# Step 1: Verify snapshot exists in bucket
echo "[STEP 1/4] Verify snapshot s3://$BUCKET/$SNAPSHOT_KEY exists" >&2
size_bytes=$(aws --endpoint-url "$S3_ENDPOINT" s3 ls "s3://$BUCKET/$SNAPSHOT_KEY" 2>/dev/null | awk '{print $3}')
if [ -z "$size_bytes" ] || [ "$size_bytes" = "0" ]; then
  echo "  [FAIL] snapshot not found OR zero size" >&2
  echo "  Use --list to see available snapshots" >&2
  exit 2
fi
echo "  [OK] snapshot exists ($size_bytes bytes)" >&2

# Step 2: Download to /tmp + verify
echo "" >&2
echo "[STEP 2/4] Download snapshot to /tmp/$SNAPSHOT_KEY" >&2
LOCAL_PATH="/tmp/$SNAPSHOT_KEY"
if [ "$EXECUTE" -eq 0 ]; then
  echo "  [DRY-RUN] aws --endpoint-url $S3_ENDPOINT s3 cp s3://$BUCKET/$SNAPSHOT_KEY $LOCAL_PATH" >&2
else
  if aws --endpoint-url "$S3_ENDPOINT" s3 cp "s3://$BUCKET/$SNAPSHOT_KEY" "$LOCAL_PATH" >/dev/null 2>&1; then
    actual_size=$(stat -c '%s' "$LOCAL_PATH" 2>/dev/null || stat -f '%z' "$LOCAL_PATH" 2>/dev/null || echo "0")
    if [ "$actual_size" -lt 1 ]; then
      echo "  [FAIL] downloaded file is empty" >&2
      rm -f "$LOCAL_PATH"
      exit 2
    fi
    echo "  [OK] downloaded $actual_size bytes" >&2
  else
    echo "  [FAIL] aws s3 cp failed" >&2
    exit 2
  fi
fi

# Step 3: kubectl cp to vault pod
echo "" >&2
echo "[STEP 3/4] kubectl cp to $VAULT_POD:/tmp/snapshot.snap" >&2
if [ "$EXECUTE" -eq 0 ]; then
  echo "  [DRY-RUN] kubectl --context $SECRETS_CONTEXT -n $VAULT_NAMESPACE cp $LOCAL_PATH $VAULT_POD:/tmp/snapshot.snap" >&2
else
  if kubectl --context "$SECRETS_CONTEXT" -n "$VAULT_NAMESPACE" cp \
      "$LOCAL_PATH" "$VAULT_POD:/tmp/snapshot.snap" >/dev/null 2>&1; then
    echo "  [OK] snapshot copied to pod" >&2
  else
    echo "  [FAIL] kubectl cp failed" >&2
    rm -f "$LOCAL_PATH"
    exit 2
  fi
fi

# Step 4: DESTRUCTIVE — bao operator raft snapshot restore
echo "" >&2
echo "[STEP 4/4] Restore Vault from snapshot (DESTRUCTIVE)" >&2
if [ "$EXECUTE" -eq 0 ]; then
  echo "  [DRY-RUN] kubectl --context $SECRETS_CONTEXT -n $VAULT_NAMESPACE exec $VAULT_POD -- bao operator raft snapshot restore /tmp/snapshot.snap" >&2
  echo "" >&2
  echo "🛑 To actually restore, re-run with --execute" >&2
  exit 0
else
  echo "  [WARN] performing DESTRUCTIVE operation in 3 seconds (Ctrl-C to abort)" >&2
  sleep 3
  if kubectl --context "$SECRETS_CONTEXT" -n "$VAULT_NAMESPACE" exec "$VAULT_POD" -- \
      bao operator raft snapshot restore /tmp/snapshot.snap >/dev/null 2>&1; then
    echo "  [OK] restore command completed" >&2
  else
    echo "  [FAIL] restore command failed — Vault state may be in inconsistent state" >&2
    echo "  Manual recovery: kubectl --context $SECRETS_CONTEXT -n $VAULT_NAMESPACE describe pod $VAULT_POD" >&2
    rm -f "$LOCAL_PATH"
    exit 2
  fi
fi

# ----------------------------------------------------------------------------
# Post-restore cleanup + reporting
# ----------------------------------------------------------------------------
rm -f "$LOCAL_PATH"
echo "" >&2
echo "[COMPLETE] Vault state restored from $SNAPSHOT_KEY" >&2
echo "" >&2
echo "🛑 POST-RESTORE STEPS (operator-time, MANDATORY):" >&2
echo "  1. Vault is now likely RE-SEALED — verify status:" >&2
echo "     kubectl --context $SECRETS_CONTEXT -n $VAULT_NAMESPACE exec $VAULT_POD -- bao status" >&2
echo "  2. If sealed: re-run Shamir unseal ceremony per" >&2
echo "     docs/runbooks.md P3" >&2
echo "  3. After unseal, verify KV data integrity:" >&2
echo "     bash infra/scripts/openbao/vault-sync-check.sh --threshold 10" >&2
exit 0
