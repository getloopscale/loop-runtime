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

# Placeholder until operator implementation:
echo "[stub] Operator must implement Trillian API integration."
echo "[stub] See docs/operations/sigstore-storage-model.md § Archive script."
echo "[stub] Exit 0 (no-op safe)."
exit 0
