#!/usr/bin/env bash
# Verify PR body has Quality Gates checklist completed.
# Plan 2026-05-21-comprehensive-quality-gates v1.1 T5.1 + EC-5.
#
# Env vars:
#   PR_BODY        — full PR body (markdown)
#   PR_AUTHOR      — GitHub PR author login
#   PR_LABELS      — comma-separated label list
#
# Bot allowlist (EC-5): SKIP gate if author is bot OR PR labeled automation
# Checklist: every `- [ ]` becomes `- [x]` OR explanation in "N/A justifications"

set -euo pipefail

PR_BODY="${PR_BODY:-}"
PR_AUTHOR="${PR_AUTHOR:-}"
PR_LABELS="${PR_LABELS:-}"

# EC-5 bot allowlist
BOT_ALLOWLIST=("dependabot[bot]" "renovate[bot]" "github-actions[bot]")
LABEL_ALLOWLIST=("dependencies" "automated")

for bot in "${BOT_ALLOWLIST[@]}"; do
  if [ "$PR_AUTHOR" = "$bot" ]; then
    echo "[template-compliance] EC-5: bot author $PR_AUTHOR — SKIP gate"
    exit 0
  fi
done

IFS=',' read -ra LABELS_ARR <<< "$PR_LABELS"
for lbl in "${LABELS_ARR[@]}"; do
  lbl="$(echo "$lbl" | tr -d ' ')"
  for allowed in "${LABEL_ALLOWLIST[@]}"; do
    if [ "$lbl" = "$allowed" ]; then
      echo "[template-compliance] EC-5: label '$lbl' present — SKIP gate"
      exit 0
    fi
  done
done

if [ -z "$PR_BODY" ]; then
  echo "[template-compliance] FAIL: empty PR body" >&2
  exit 1
fi

# Required template anchor (Quality Gates Checklist heading)
if ! echo "$PR_BODY" | grep -q "## Quality Gates Checklist"; then
  echo "[template-compliance] FAIL: PR body missing 'Quality Gates Checklist' section" >&2
  echo "[template-compliance] use .github/PULL_REQUEST_TEMPLATE.md as starting point" >&2
  exit 1
fi

# Count unchecked boxes in the checklist section
UNCHECKED=$(echo "$PR_BODY" | awk '
  /^## Quality Gates Checklist/ { in_section = 1; next }
  /^## / { in_section = 0 }
  in_section && /^- \[ \]/ { count++ }
  END { print count + 0 }
')

if [ "$UNCHECKED" -gt 0 ]; then
  # Check if N/A justifications section is non-empty
  has_na_section=$(echo "$PR_BODY" | awk '
    /^### N\/A justifications/ { in_section = 1; next }
    /^## / { in_section = 0 }
    in_section && /[A-Za-z0-9]/ && !/^<!--/ && !/^### / { found = 1 }
    END { print found + 0 }
  ')

  if [ "$has_na_section" -eq 1 ]; then
    echo "[template-compliance] WARN: $UNCHECKED unchecked box(es) with N/A justifications — manual review required"
    # Allow merge if N/A explained; reviewer is human gate
    exit 0
  fi

  echo "[template-compliance] FAIL: $UNCHECKED unchecked box(es) in Quality Gates Checklist" >&2
  echo "[template-compliance] check the boxes OR add explanations under 'N/A justifications'" >&2
  exit 1
fi

echo "[template-compliance] PASS — all checkboxes ticked"
