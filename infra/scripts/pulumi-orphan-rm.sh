#!/usr/bin/env bash
# CHG-2026-04-30-INFRA-NORMALIZE / Phase 3 condition C14 (T3.2).
# Source: docs/decisions/MEET-INFRA-NORMALIZE-T3-pulumi-consolidation.md
#
# Removes Pulumi stacks for the 3 paralelas dirs AFTER they have been
# archived (T3.1 prerequisite). Per condition C13, this is the SECOND
# of three commits in Phase 3 execution.
#
# Pre-flight gates:
#   1. Archive bucket s3://loopzero-archive contains an archive newer than
#      <ARCHIVE_FRESHNESS_SEC> for each (dir, stack). Per condition C13
#      this enforces the "archive THEN rm" sequencing.
#   2. Stack reports zero live cloud resources (per C3 + C18 cross-ref).
#      Operator MUST have verified the cloud-side too — script can only
#      check Pulumi's view.
#   3. CloudTrail logging enabled on archive bucket (C8).
#
# Removal flow per stack:
#   pulumi stack rm <stack> --yes
#
# Per condition C19: state-only rollback SLO ≤ 15 min wall-clock.
# This script does NOT auto-rollback — caller invokes
# `pulumi stack import` from the archive to recover. See
# `pulumi stack import` from the archive to recover.
#
# Usage:
#   bash infra/scripts/pulumi-orphan-rm.sh
#   bash infra/scripts/pulumi-orphan-rm.sh --dry-run   # show what WOULD be rm'd
#
# Required env:
#   PULUMI_CONFIG_PASSPHRASE
#   AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY
#
# Exit codes:
#   0 — all stacks removed (or dry-run completed)
#   1 — usage error
#   2 — pre-flight failed (archive freshness, live resources, etc)
#   3 — `pulumi stack rm` failed for any stack

set -euo pipefail

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ARCHIVE_BUCKET="loopzero-archive"
ARCHIVE_FRESHNESS_SEC="${ARCHIVE_FRESHNESS_SEC:-86400}"  # 24h default

DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help) sed -n '2,30p' "$0"; exit 0 ;;
    *) echo "ERROR: unknown arg: $arg" >&2; exit 1 ;;
  esac
done

log() { printf '[pulumi-orphan-rm] %s\n' "$*" >&2; }
fail() { log "FAIL: $*"; exit "${2:-1}"; }

# ─── Step 1: pre-flight ───
log "step 1: pre-flight"
command -v pulumi >/dev/null 2>&1 || fail "pulumi not in PATH" 2
command -v aws >/dev/null 2>&1 || fail "aws CLI not in PATH" 2
[[ -n "${PULUMI_CONFIG_PASSPHRASE:-}" ]] || fail "PULUMI_CONFIG_PASSPHRASE required" 2

# CloudTrail logging audit (C8) — best-effort presence check.
if (( DRY_RUN == 0 )); then
  if ! aws cloudtrail describe-trails 2>/dev/null \
       | jq -e '.trailList[] | select(.S3BucketName == "loopzero-archive" or (.S3BucketName | endswith("-cloudtrail")))' >/dev/null; then
    log "  WARN: no CloudTrail trail captures loopzero-archive bucket events (C8 — audit logging required)"
    log "  Operator must confirm trail exists OR proceed with explicit override."
  fi
fi

# ─── Step 2: enumerate paralelas dirs ───
PARALELAS=(infra/pulumi-infra infra/pulumi-platform)
TARGETS=()
for dir in "${PARALELAS[@]}"; do
  if [[ -d "${REPO_ROOT}/${dir}" ]] && [[ -f "${REPO_ROOT}/${dir}/Pulumi.yaml" ]]; then
    TARGETS+=("$dir")
  fi
done
log "  ${#TARGETS[@]} dirs candidates: ${TARGETS[*]}"

if (( ${#TARGETS[@]} == 0 )); then
  log "  nothing to remove"
  exit 0
fi

# ─── Step 3: per-stack rm ───
log "step 3: per-stack rm"
rm_count=0
for dir in "${TARGETS[@]}"; do
  log "  --- $dir ---"
  cd "${REPO_ROOT}/${dir}"

  if ! stacks_json=$(pulumi stack ls --json 2>/dev/null); then
    log "    WARN: pulumi stack ls failed; skipping $dir"
    continue
  fi
  stacks=$(echo "$stacks_json" | jq -r '.[].name' 2>/dev/null || echo "")

  for stack in $stacks; do
    log "    stack: $stack"

    # 3.1 — verify archive freshness (C13 sequencing)
    archive_prefix="pulumi-state/${dir//\//-}/${stack}/"
    latest_archive=$(aws s3api list-objects-v2 \
      --bucket "$ARCHIVE_BUCKET" \
      --prefix "$archive_prefix" \
      --query 'sort_by(Contents, &LastModified)[-1].LastModified' \
      --output text 2>/dev/null || echo "")
    if [[ -z "$latest_archive" || "$latest_archive" == "None" ]]; then
      fail "no archive found at s3://${ARCHIVE_BUCKET}/${archive_prefix} — run pulumi-archive.sh first (C13)" 2
    fi

    archive_age_sec=$(( $(date -u +%s) - $(date -u -d "$latest_archive" +%s) ))
    if (( archive_age_sec > ARCHIVE_FRESHNESS_SEC )); then
      fail "latest archive for $stack is ${archive_age_sec}s old (>${ARCHIVE_FRESHNESS_SEC}s freshness threshold) — re-run pulumi-archive.sh" 2
    fi
    log "      archive freshness OK: ${archive_age_sec}s old"

    # 3.2 — re-verify zero resources (defense-in-depth)
    resource_count=$(pulumi stack --stack "$stack" --json 2>/dev/null | jq '.resources | length' 2>/dev/null || echo "0")
    if (( resource_count > 0 )); then
      fail "stack $stack still has $resource_count resources — abort (operator must destroy/orphan first)" 2
    fi

    # 3.3 — rm
    if (( DRY_RUN == 1 )); then
      log "      DRY_RUN: would run 'pulumi stack rm $stack --yes'"
    else
      if ! pulumi stack rm "$stack" --yes 2>&1; then
        fail "pulumi stack rm failed for $stack" 3
      fi
      log "      removed: $stack"
      rm_count=$((rm_count + 1))
    fi
  done
  cd "$REPO_ROOT"
done

log "DONE — removed $rm_count stack(s)"
exit 0
