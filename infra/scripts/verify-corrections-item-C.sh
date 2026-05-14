#!/usr/bin/env bash
# verify-corrections-item-C.sh
#
# CORRECTIONS.txt item C (plan T1.3) — operator-led verification helper.
# Wraps the 3-step doctl protocol from `finding-2026-05-13-item-C-resolution.md`
# into a single idempotent invocation with inline finding doc update.
#
# Prerequisites:
#   - doctl installed (`doctl version` >= 1.100)
#   - doctl authenticated (one of):
#       (a) `doctl auth init` previously run
#       (b) DIGITALOCEAN_ACCESS_TOKEN env var set
#       (c) DO_API_TOKEN env var set (alias normalized below)
#
# Usage:
#   bash infra/scripts/verify-corrections-item-C.sh
#   TARGET_IP=209.38.205.78 bash infra/scripts/verify-corrections-item-C.sh
#   DB_ID=<uuid> bash infra/scripts/verify-corrections-item-C.sh  # skip interactive picker
#
# Exit codes (aligned with sibling helper file-corrections-item-I-upstream.sh
# per iter-18 sprint audit — operator cancellation is NOT a usage error):
#   0  — verification complete (one of outcomes A/B/C chosen + finding doc updated)
#   1  — usage error / doctl not auth / no databases / invalid input
#   2  — outcome B chosen but `doctl databases firewalls remove` failed
#   3  — operator cancelled (explicit non-confirmation of DECOMMISSIONED)

set -uo pipefail

TARGET_IP="${TARGET_IP:-209.38.205.78}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && cd .. && pwd)"
FINDING_DOC="$REPO_ROOT/.claude/knowledge-base/concepts/operations/finding-2026-05-13-item-C-resolution.md"
AUDIT_DOC="$REPO_ROOT/.claude/knowledge-base/concepts/operations/finding-2026-05-13-corrections-txt-audit.md"

# Parse flags
DRY_RUN_FLAG=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN_FLAG=1; shift ;;
    -h|--help) sed -n '1,30p' "$0"; exit 0 ;;
    *) echo "usage: $0 [--dry-run]" >&2; exit 1 ;;
  esac
done

log() { printf '[verify-C] %s\n' "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

# ---------------------------------------------------------------------------
# 0. Pre-flight: doctl present + authenticated
# ---------------------------------------------------------------------------
command -v doctl >/dev/null 2>&1 || die "doctl not installed (https://docs.digitalocean.com/reference/doctl/how-to/install/)"

# Normalize token env var (doctl accepts DIGITALOCEAN_ACCESS_TOKEN)
if [[ -z "${DIGITALOCEAN_ACCESS_TOKEN:-}" && -n "${DO_API_TOKEN:-}" ]]; then
  export DIGITALOCEAN_ACCESS_TOKEN="$DO_API_TOKEN"
fi

if ! doctl account get >/dev/null 2>&1; then
  die "doctl not authenticated. Run: doctl auth init  OR set DIGITALOCEAN_ACCESS_TOKEN/DO_API_TOKEN"
fi

log "doctl auth OK ($(doctl account get --format Email --no-header 2>/dev/null || echo 'account verified'))"

# ---------------------------------------------------------------------------
# 1. Resolve DB ID (interactive or explicit)
# ---------------------------------------------------------------------------
if [[ -z "${DB_ID:-}" ]]; then
  log "Listing databases..."
  DB_LIST=$(doctl databases list --format ID,Name,Engine,Status 2>&1)
  echo "$DB_LIST" >&2
  echo "" >&2

  # Count data rows (subtract 1 for header line).
  DB_ROWS=$(echo "$DB_LIST" | wc -l)
  if [[ "$DB_ROWS" -le 1 ]]; then
    log "WARN: auth context shows 0 visible databases."
    log "Possible reasons:"
    log "  (a) different DO account/team holds the production DB"
    log "      → run \`doctl auth init\` to switch context, then re-run this script"
    log "  (b) production DB was decommissioned"
    log "      → if confirmed, classify as Outcome A (RESOLVED — firewall no longer exists)"
    log "      → re-run with DB_ID=<placeholder> or update finding doc manually"
    log "  (c) account scope limitation (team-shared DB requires explicit context switch)"
    log "      → \`doctl account ratelimit\` may indicate scope; ask team admin"
    log ""
    log "If you want to proceed without a DB (e.g., to record Outcome A for decommissioned DB):"
    log "  DB_ID=DECOMMISSIONED bash $0"
    log "(Script will skip firewall check and record Outcome A with note.)"
    die "no databases visible in current auth context; cannot verify firewall"
  fi

  read -rp "DB ID to check (paste UUID from list above): " DB_ID
  [[ -n "$DB_ID" ]] || die "no DB_ID provided"
fi

# Special-case: operator confirms DB was decommissioned (no firewall to check).
DB_DECOMMISSIONED=0
if [[ "$DB_ID" == "DECOMMISSIONED" ]]; then
  # Require explicit operator confirmation before recording Outcome A — prevents
  # accidental write during `bash -x` test invocations.
  if [[ "$DRY_RUN_FLAG" != "1" ]]; then
    read -rp "Confirm DB is decommissioned (no firewall exists)? [y/N]: " confirm
    if [[ "${confirm,,}" != "y" ]]; then
      log "DECOMMISSIONED not confirmed; aborting"
      exit 3  # operator cancelled — not a usage error (per iter-18 alignment)
    fi
  fi
  log "Operator confirmed DB decommissioned — recording Outcome A (RESOLVED) without firewall check."
  CHOSEN_OUTCOME="A"
  CLASSIFICATION="RESOLVED"
  ACTION_TAKEN="none (DB decommissioned; firewall no longer exists)"
  DB_DECOMMISSIONED=1
fi

log "Target DB: $DB_ID"
log "Target IP: $TARGET_IP"

# ---------------------------------------------------------------------------
# 2. List firewall rules + grep for target IP (skip if decommissioned)
# ---------------------------------------------------------------------------
if [[ "$DB_DECOMMISSIONED" == "0" ]]; then
  log "Fetching firewall rules..."
  FIREWALL_OUT=$(doctl databases firewalls list "$DB_ID" --format Type,Value 2>&1) || die "doctl firewalls list failed: $FIREWALL_OUT"

  echo "$FIREWALL_OUT" >&2
  echo "" >&2

  if echo "$FIREWALL_OUT" | grep -qF "$TARGET_IP"; then
    IP_PRESENT=1
    log "Target IP $TARGET_IP IS present in firewall."
  else
    IP_PRESENT=0
    log "Target IP $TARGET_IP NOT present (firewall is already clean)."
  fi
fi

# ---------------------------------------------------------------------------
# 3. Outcome selection (skip if decommissioned — already set above)
# ---------------------------------------------------------------------------
if [[ "$DB_DECOMMISSIONED" == "1" ]]; then
  : # outcome already set
elif [[ "$IP_PRESENT" == "0" ]]; then
  CHOSEN_OUTCOME="A"
  CLASSIFICATION="RESOLVED"
  ACTION_TAKEN="none (verified empty already)"
else
  echo "" >&2
  echo "Outcome choices:" >&2
  echo "  B — remove $TARGET_IP from firewall now (RESOLVED)" >&2
  echo "  C — keep $TARGET_IP intentionally (ACTIVE_OPERATIONAL_TODO; requires rationale)" >&2
  echo "" >&2
  read -rp "Choose [B/C]: " choice
  case "${choice^^}" in
    B)
      CHOSEN_OUTCOME="B"
      CLASSIFICATION="RESOLVED"
      ACTION_TAKEN="doctl databases firewalls remove $DB_ID --rule ip_addr:$TARGET_IP"
      log "Removing $TARGET_IP from firewall..."
      if ! doctl databases firewalls remove "$DB_ID" --rule "ip_addr:$TARGET_IP" 2>&1; then
        log "ERROR: doctl databases firewalls remove failed"
        exit 2
      fi
      log "Firewall rule removed."
      ;;
    C)
      CHOSEN_OUTCOME="C"
      CLASSIFICATION="ACTIVE_OPERATIONAL_TODO"
      read -rp "Reason to keep the IP (will be recorded in finding doc): " keep_reason
      read -rp "Revisit trigger (when should this be re-evaluated?): " keep_trigger
      ACTION_TAKEN="none (intentional)"
      ;;
    *)
      die "invalid choice; aborting"
      ;;
  esac
fi

TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
log "Chosen outcome: $CHOSEN_OUTCOME ($CLASSIFICATION)"

# ---------------------------------------------------------------------------
# 4. Inline-update finding doc (skip if --dry-run)
# ---------------------------------------------------------------------------
[[ -f "$FINDING_DOC" ]] || die "finding doc not found: $FINDING_DOC"

# Append an "Operator decision" section near the top (after the YAML
# frontmatter) so it's the first thing reviewers see.
SUMMARY_BLOCK=$(cat <<EOF

## Operator decision (recorded $TIMESTAMP)

- **Chosen outcome:** $CHOSEN_OUTCOME
- **Classification:** $CLASSIFICATION
- **Action taken:** $ACTION_TAKEN
- **DB ID:** \`$DB_ID\`
- **Target IP:** \`$TARGET_IP\`
EOF
)

if [[ "$CHOSEN_OUTCOME" == "C" ]]; then
  SUMMARY_BLOCK="$SUMMARY_BLOCK
- **Reason to keep:** ${keep_reason:-[operator did not provide]}
- **Revisit trigger:** ${keep_trigger:-[operator did not provide]}"
fi

if [[ "$DRY_RUN_FLAG" == "1" ]]; then
  log "=== DRY RUN: would insert this block into $FINDING_DOC ==="
  echo "$SUMMARY_BLOCK"
  log "=== END DRY RUN (no file modified) ==="
  exit 0
fi

# Insert after the closing --- of YAML frontmatter (find the 2nd --- line).
# We use awk to do an in-place-style insert via temp file (atomic replace).
# EXIT trap guarantees cleanup on interrupt/failure (mv on success consumes
# the temp; trap is a no-op in that case). Per iter-17 sprint audit: mktemp
# without EXIT-trap cleanup is a leak-on-failure pattern.
TMPDOC=$(mktemp -t finding-C.XXXXXX.md)
trap 'rm -f "$TMPDOC" 2>/dev/null || true' EXIT
awk -v block="$SUMMARY_BLOCK" '
  BEGIN { fm_close=0 }
  /^---$/ {
    if (fm_close == 0) { print; fm_close = 1; next }
    if (fm_close == 1) { print; fm_close = 2; print block; next }
  }
  { print }
' "$FINDING_DOC" > "$TMPDOC" || die "awk failed; finding doc unchanged"
mv "$TMPDOC" "$FINDING_DOC" || die "mv failed; finding doc may be in inconsistent state"
log "Updated finding doc: $FINDING_DOC"

# ---------------------------------------------------------------------------
# 5. Hint operator for follow-up doc updates
# ---------------------------------------------------------------------------
echo "" >&2
log "Next operator steps (manual):"
log "  1. Review the inserted 'Operator decision' block in:"
log "     $FINDING_DOC"
log "  2. Update audit doc row C status accordingly:"
log "     $AUDIT_DOC"
log "  3. Commit:"
log "     git add $FINDING_DOC $AUDIT_DOC"
log "     git commit -m \"fix(corrections-C): operator outcome $CHOSEN_OUTCOME ($CLASSIFICATION)\""
echo "" >&2

exit 0
