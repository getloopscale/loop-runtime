#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# T1.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure.
# Archives Rekor entries older than RETENTION_DAYS (default 365) to S3,
# then deletes locally to free PV space.
#
# Per ADR-2026-05-27-T2.2-OFFLINE-TUF — append-only constraint preserved
# via S3 archive (immutable bucket) + Trillian Merkle root retention.
#
# Exit codes:
#   0 = success (any number of entries archived, including zero)
#   1 = configuration error (missing env vars, rekor-cli unavailable)
#   2 = runtime error (S3 upload failed, rekor delete failed)
#
# Operator activation: schedule via CronWorkflow (Phase 5 of plan
# 2026-05-27-t0.2-cenario-b-argo-workflows-ci-poc).

set -euo pipefail

# ----- Configuration -----
REKOR_API="${REKOR_API_URL:-}"
S3_BUCKET="${S3_ARCHIVE_BUCKET:-}"
RETENTION_DAYS="${RETENTION_DAYS:-365}"
DRY_RUN="${DRY_RUN:-0}"

# T4.6 of plan 2026-05-28-meet-001-engineering-closure-plan.md.
# CR-11 (MEET-2026-05-28-001 Production Design Review): the Trillian
# admin API integration is intentionally deferred to a separate operator
# workstream. Until it lands, this script short-circuits with WARN
# when invoked with --manual-procedure-only, pointing at
# docs/operations/rekor-retention.md. Without the flag, the script
# proceeds to its existing pre-flight + stub flow — the flag is opt-in
# so a future scheduled invocation cannot silently become a no-op.
MANUAL_PROCEDURE_ONLY=0
for arg in "$@"; do
  case "$arg" in
    --manual-procedure-only) MANUAL_PROCEDURE_ONLY=1 ;;
    --help|-h)
      cat <<USAGE
Usage: $0 [--manual-procedure-only]

  --manual-procedure-only   short-circuit with WARN pointing at the
                            manual procedure runbook
                            (docs/operations/rekor-retention.md).
                            The Trillian admin API integration is
                            intentionally deferred to a separate
                            operator workstream. T4.6 of plan
                            2026-05-28-meet-001-engineering-closure.

Environment:
  REKOR_API_URL       (required unless DRY_RUN=1 or --manual-procedure-only)
  S3_ARCHIVE_BUCKET   (required unless DRY_RUN=1 or --manual-procedure-only)
  RETENTION_DAYS      default 365
  DRY_RUN             1 = print what would happen, exit 0
USAGE
      exit 0
      ;;
  esac
done

if [[ "$MANUAL_PROCEDURE_ONLY" == "1" ]]; then
  echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] WARN: rekor-archive-old-entries in MANUAL_PROCEDURE_ONLY mode."
  echo "  Trillian admin API integration is deferred to a separate operator workstream."
  echo "  Follow docs/operations/rekor-retention.md to archive old Rekor entries manually."
  echo "  (T4.6 of plan 2026-05-28-meet-001-engineering-closure, CR-11)"
  exit 0
fi

# ----- Pre-flight checks (skipped in DRY_RUN — operator validates env early) -----
if [[ "$DRY_RUN" != "1" ]]; then
  if [[ -z "$REKOR_API" ]]; then
    echo "ERROR: REKOR_API_URL not set" >&2
    exit 1
  fi
  if [[ -z "$S3_BUCKET" ]]; then
    echo "ERROR: S3_ARCHIVE_BUCKET not set" >&2
    exit 1
  fi
  if ! command -v rekor-cli &>/dev/null; then
    echo "ERROR: rekor-cli not installed (https://github.com/sigstore/rekor/releases)" >&2
    exit 1
  fi
  if ! command -v aws &>/dev/null && ! command -v mc &>/dev/null; then
    echo "ERROR: neither aws nor mc (minio-client) installed" >&2
    exit 1
  fi
  if ! command -v jq &>/dev/null; then
    echo "ERROR: jq not installed" >&2
    exit 1
  fi
fi

# ----- Stub implementation -----
# NOTE: Full implementation deferred to operator post-Sigstore activation.
# This script provides safe pre-flight + structure; operator fills in
# Trillian API integration details once Rekor is provisioned.
#
# The placeholder logic below is a SAFE-NO-OP — it queries Rekor but
# does NOT delete entries. Operator validates query semantics + adds
# delete step once Rekor admin API access is configured.

CUTOFF_TS=$(date -u -d "${RETENTION_DAYS} days ago" +%s 2>/dev/null \
  || date -u -v-${RETENTION_DAYS}d +%s)  # GNU + BSD compat

echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] rekor-archive-old-entries.sh starting"
echo "  REKOR_API:       $REKOR_API"
echo "  S3_BUCKET:       $S3_BUCKET"
echo "  RETENTION_DAYS:  $RETENTION_DAYS"
echo "  CUTOFF_TS:       $CUTOFF_TS"
echo "  DRY_RUN:         $DRY_RUN"

if [[ "$DRY_RUN" == "1" ]]; then
  echo "[dry-run] Would archive Rekor entries with integratedTime < $CUTOFF_TS"
  echo "[dry-run] Would upload to s3://$S3_BUCKET/<uuid>.json"
  echo "[dry-run] Would delete via rekor-cli delete --uuid <uuid>"
  exit 0
fi

# T4.6 (CR-11): Trillian admin API integration is not implemented yet
# AND the operator did NOT pass --manual-procedure-only. Exit 1 so a
# scheduled invocation surfaces loud instead of silently no-op'ing.
# Operator path: re-invoke with --manual-procedure-only and follow
# docs/operations/rekor-retention.md.
echo "ERROR: Trillian admin API not implemented (CR-11)." >&2
echo "  Re-invoke with --manual-procedure-only and follow" >&2
echo "  docs/operations/rekor-retention.md to archive entries manually." >&2
exit 1
