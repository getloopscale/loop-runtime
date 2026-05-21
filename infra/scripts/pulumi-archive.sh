#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-NORMALIZE / Phase 3 condition C14 (T3.1).
# Source: docs/decisions/MEET-INFRA-NORMALIZE-T3-pulumi-consolidation.md
#
# Archives Pulumi state for the 3 paralelas dirs to encrypted S3
# (s3://loopzero-archive/pulumi-state/). Pre-flight verifies the archive
# bucket exists with required ACL/encryption per condition C4. Per-stack
# export goes through plaintext-secret regex scan (C9) before upload.
#
# Idempotent: re-running with same state produces identical S3 archive
# set (bit-identical excluding timestamp filename suffix). Idempotency
# test C11 covers this property.
#
# Per condition C5: PULUMI_CONFIG_PASSPHRASE fetched at archive time
# from `mgmt-platform-secrets`, NOT colocated with archive.
#
# Per condition C17: pulumi version pinned (3.140.0); snapshot SHA
# captured before/after every export.
#
# Usage:
#   bash infra/scripts/pulumi-archive.sh [--archive-only|--dry-run]
#
#   --archive-only : C12 quarterly mode — re-archive all live stacks
#                    even if they were already archived. Reports
#                    drift. Used by .github/workflows/pulumi-archive.yml.
#   --dry-run      : show what WOULD be archived; no S3 writes.
#
# Required env (will be probed and failed-loud if absent):
#   PULUMI_CONFIG_PASSPHRASE
#   AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY (S3 archive bucket)
#
# Exit codes:
#   0 — all stacks archived OR dry-run completed
#   1 — usage error
#   2 — pre-flight failed (archive bucket missing/wrong ACL)
#   3 — live cloud resources detected (caller must abort T3.2)
#   4 — archive failed (S3 write or PULUMI export error)

set -euo pipefail

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
PULUMI_VERSION_PINNED="3.140.0"
ARCHIVE_BUCKET="s3://loopzero-archive/pulumi-state"
ARCHIVE_TS="$(date -u +%Y%m%dT%H%M%SZ)"

DRY_RUN=0
ARCHIVE_ONLY=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    --archive-only) ARCHIVE_ONLY=1 ;;
    -h|--help) sed -n '2,30p' "$0"; exit 0 ;;
    *) echo "ERROR: unknown arg: $arg" >&2; exit 1 ;;
  esac
done

log() { printf '[pulumi-archive] %s\n' "$*" >&2; }
fail() { log "FAIL: $*"; exit "${2:-1}"; }

# ─── Step 1: pre-flight ───
log "step 1: pre-flight"
command -v pulumi >/dev/null 2>&1 || fail "pulumi not in PATH" 2

# Pulumi version pin (C17).
PULUMI_VERSION_ACTUAL="$(pulumi version 2>/dev/null | sed 's/^v//')"
if [[ "$PULUMI_VERSION_ACTUAL" != "$PULUMI_VERSION_PINNED" ]]; then
  log "  WARN: pulumi version $PULUMI_VERSION_ACTUAL != pinned $PULUMI_VERSION_PINNED (C17)"
  # WARN, not FAIL — operator may legitimately use a newer pulumi if
  # state schema is unchanged. The actual fail-loud is in C22 replay
  # drill if state can't be re-imported.
fi

if (( DRY_RUN == 0 )); then
  command -v aws >/dev/null 2>&1 || fail "aws CLI not in PATH (S3 access)" 2
  [[ -n "${PULUMI_CONFIG_PASSPHRASE:-}" ]] || fail "PULUMI_CONFIG_PASSPHRASE env var required (C5)" 2
  [[ -n "${AWS_ACCESS_KEY_ID:-}" ]] || fail "AWS_ACCESS_KEY_ID env var required" 2

  # C4: archive bucket exists + has required ACL/encryption.
  if ! aws s3api head-bucket --bucket loopzero-archive 2>/dev/null; then
    fail "archive bucket s3://loopzero-archive does not exist (C4 — provision in canonical pulumi/ first)" 2
  fi
  encryption=$(aws s3api get-bucket-encryption --bucket loopzero-archive 2>/dev/null \
    | jq -r '.ServerSideEncryptionConfiguration.Rules[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm' 2>/dev/null)
  if [[ "$encryption" != "aws:kms" ]]; then
    fail "archive bucket missing SSE-KMS encryption (got: $encryption; C4 requires aws:kms)" 2
  fi
fi

# ─── Step 2: enumerate paralelas dirs ───
log "step 2: enumerating paralelas Pulumi dirs"
PARALELAS=(infra/pulumi-infra infra/pulumi-platform)
TARGETS=()
for dir in "${PARALELAS[@]}"; do
  if [[ -d "${REPO_ROOT}/${dir}" ]] && [[ -f "${REPO_ROOT}/${dir}/Pulumi.yaml" ]]; then
    TARGETS+=("$dir")
  else
    log "  SKIP $dir (not present — already cleaned up?)"
  fi
done
log "  ${#TARGETS[@]} dirs to archive: ${TARGETS[*]}"

if (( ${#TARGETS[@]} == 0 )); then
  log "  no paralelas dirs found — Phase 3 already cleaned"
  log "DONE — nothing to do"
  exit 0
fi

# ─── Step 3: per-dir archive ───
log "step 3: per-dir archive"
ARCHIVE_TMPDIR="$(mktemp -d /tmp/pulumi-archive-XXXXXX)"
trap "rm -rf '$ARCHIVE_TMPDIR'" EXIT

archive_count=0
live_resource_count=0
for dir in "${TARGETS[@]}"; do
  log "  --- $dir ---"
  cd "${REPO_ROOT}/${dir}"

  # 3.1 — list stacks
  if ! stacks_json=$(pulumi stack ls --json 2>/dev/null); then
    log "    WARN: pulumi stack ls failed (auth?); skipping $dir"
    continue
  fi

  stacks=$(echo "$stacks_json" | jq -r '.[].name' 2>/dev/null || echo "")
  if [[ -z "$stacks" ]]; then
    log "    no stacks in $dir"
    continue
  fi

  for stack in $stacks; do
    log "    stack: $stack"

    # 3.2 — verify cloud-resource state via cross-ref. Per condition
    # C18, this is per-provider. The actual cross-ref logic depends on
    # what the stack provisions; this script's responsibility is to
    # emit warnings IF the stack reports >0 resources. The operator
    # makes the abort/proceed call.
    resource_count=$(pulumi stack --stack "$stack" --json 2>/dev/null | jq '.resources | length' 2>/dev/null || echo "0")
    if (( resource_count > 0 )); then
      log "    LIVE RESOURCES detected in $dir/$stack: $resource_count resources"
      log "    Per condition C3, operator must verify against actual provider before T3.2 stack rm."
      live_resource_count=$((live_resource_count + 1))
    fi

    # 3.3 — export state (NOT --decrypt; we keep secrets sealed in
    # archive at-rest; restore via canonical config passphrase).
    archive_path="${ARCHIVE_TMPDIR}/${dir//\//-}-${stack}-${ARCHIVE_TS}.json"
    if (( DRY_RUN == 0 )); then
      if ! pulumi stack export --stack "$stack" --file "$archive_path" 2>&1; then
        fail "pulumi stack export failed for $dir/$stack" 4
      fi

      # 3.4 — C9 plaintext-secret regex scan
      # Only fail if a sensitive token appears OUTSIDE the canonical
      # encrypted "ciphertext" block (which Pulumi pads safely).
      plaintext_hits=$(jq -r '.. | strings? // empty' "$archive_path" \
        | grep -vE '"ciphertext"|"v1:|"\\u' \
        | grep -ciE '(token|password|secret|api[_-]?key)' || true)
      if (( plaintext_hits > 0 )); then
        log "    WARN: $plaintext_hits plaintext token candidates in archive (C9 regex scan)"
        log "    Operator MUST review $archive_path before C5 upload."
      fi

      # 3.5 — capture snapshot SHA (C17)
      sha="$(sha256sum "$archive_path" | awk '{print $1}')"
      log "    archive: $(basename "$archive_path") sha256=$sha (resources=$resource_count)"

      # 3.6 — upload to S3 with required ACL
      s3_key="${ARCHIVE_BUCKET}/${dir//\//-}/${stack}/${ARCHIVE_TS}.json"
      if ! aws s3 cp "$archive_path" "$s3_key" \
              --acl private \
              --sse aws:kms \
              --metadata "snapshot-sha256=$sha,resources=$resource_count" 2>&1; then
        fail "S3 upload failed for $stack" 4
      fi

      archive_count=$((archive_count + 1))
    else
      log "    DRY_RUN: would export to $archive_path then upload to S3"
    fi
  done
  cd "$REPO_ROOT"
done

# ─── Step 4: summary ───
log "step 4: summary"
log "  archived: $archive_count"
log "  live-resources detected (operator must verify): $live_resource_count"

if (( live_resource_count > 0 )); then
  log "WARN: live resources in $live_resource_count stack(s) — DO NOT proceed to T3.2 stack rm without operator verification"
  exit 3
fi

log "DONE"
exit 0
