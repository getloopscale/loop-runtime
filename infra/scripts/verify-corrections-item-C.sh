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
# Exit codes:
#   0  — verification complete (one of outcomes A/B/C chosen + finding doc updated)
#   1  — usage error / doctl not auth / no databases / operator cancelled
#   2  — outcome B chosen but `doctl databases firewalls remove` failed

set -uo pipefail

TARGET_IP="${TARGET_IP:-209.38.205.78}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && cd .. && pwd)"
FINDING_DOC="$REPO_ROOT/.claude/knowledge-base/concepts/operations/finding-2026-05-13-item-C-resolution.md"
AUDIT_DOC="$REPO_ROOT/.claude/knowledge-base/concepts/operations/finding-2026-05-13-corrections-txt-audit.md"

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
  doctl databases list --format ID,Name,Engine,Status >&2
  echo "" >&2
  read -rp "DB ID to check (paste UUID from list above): " DB_ID
  [[ -n "$DB_ID" ]] || die "no DB_ID provided"
fi

log "Target DB: $DB_ID"
log "Target IP: $TARGET_IP"

# ---------------------------------------------------------------------------
# 2. List firewall rules + grep for target IP
# ---------------------------------------------------------------------------
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

# ---------------------------------------------------------------------------
# 3. Outcome selection
# ---------------------------------------------------------------------------
if [[ "$IP_PRESENT" == "0" ]]; then
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
# 4. Inline-update finding doc
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

# Insert after the closing --- of YAML frontmatter (find the 2nd --- line).
# We use awk to do an in-place-style insert via temp file (atomic replace).
TMPDOC=$(mktemp -t finding-C.XXXXXX.md)
awk -v block="$SUMMARY_BLOCK" '
  BEGIN { fm_close=0 }
  /^---$/ {
    if (fm_close == 0) { print; fm_close = 1; next }
    if (fm_close == 1) { print; fm_close = 2; print block; next }
  }
  { print }
' "$FINDING_DOC" > "$TMPDOC"
mv "$TMPDOC" "$FINDING_DOC"
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
