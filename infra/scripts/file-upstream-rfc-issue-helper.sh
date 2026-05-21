#!/usr/bin/env bash
# file-upstream-rfc-issue-helper.sh
#
# CORRECTIONS.txt item I (plan T3.1) — sponsor-led upstream issue filing helper.
# Renders the RFC at `concepts/operations/rfc-2026-05-13-loopzero-forge-node-affinity.md`
# into a ready-to-file GitHub issue body and either:
#   - opens `gh issue create --web` for interactive submission (default), OR
#   - submits directly via `gh issue create` with --title + --body-file (if --auto)
#
# Prerequisites:
#   - gh CLI installed (`gh --version`)
#   - gh authenticated to a token with `repo` scope AND write access to
#     `getloopscale/loopzero-forge` (sponsor org access — NOT a personal token)
#
# Usage:
#   bash infra/scripts/file-upstream-rfc-issue-helper.sh             # interactive --web
#   bash infra/scripts/file-upstream-rfc-issue-helper.sh --auto      # non-interactive
#   bash infra/scripts/file-upstream-rfc-issue-helper.sh --dry-run   # print body, don't file
#
# Env overrides:
#   UPSTREAM_REPO  — default `getloopscale/loopzero-forge`
#   ISSUE_TITLE    — default derived from RFC
#
# CONCURRENCY ASSUMPTION (per iter-19 sprint audit):
#   Sponsor-led one-shot filing. NO concurrency guard. If 2 sponsors ran
#   simultaneously, loopzero-forge would receive 2 duplicate issues (GitHub
#   does not deduplicate by title). Operator can close the duplicate
#   manually after the fact. Not guarded because sponsor-coordination is
#   the documented workflow for T3.1. Use --dry-run first if uncertain.
#
# Exit codes:
#   0 — issue filed OR dry-run printed
#   1 — usage / prerequisite missing / RFC not found
#   2 — gh issue create failed
#   3 — operator cancelled

set -euo pipefail

set -uo pipefail

UPSTREAM_REPO="${UPSTREAM_REPO:-getloopscale/loopzero-forge}"
ISSUE_TITLE="${ISSUE_TITLE:-RFC: Default podAffinity for build workflows (eliminates PVC RWO Multi-Attach in per-consumer wraps)}"

REPO_ROOT="$(cd "$(dirname "$0")/.." && cd .. && pwd)"
RFC_DOC="$REPO_ROOT/.claude/knowledge-base/concepts/operations/rfc-2026-05-13-loopzero-forge-node-affinity.md"

MODE="interactive"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --auto)    MODE="auto"; shift ;;
    --dry-run) MODE="dryrun"; shift ;;
    -h|--help) sed -n '1,30p' "$0"; exit 0 ;;
    *) echo "usage: $0 [--auto|--dry-run]" >&2; exit 1 ;;
  esac
done

log() { printf '[file-I] %s\n' "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

# ---------------------------------------------------------------------------
# 0. Pre-flight
# ---------------------------------------------------------------------------
command -v gh >/dev/null 2>&1 || die "gh CLI not installed (https://cli.github.com/)"
[[ -f "$RFC_DOC" ]] || die "RFC doc not found: $RFC_DOC"

if [[ "$MODE" != "dryrun" ]]; then
  if ! gh auth status >/dev/null 2>&1; then
    die "gh not authenticated. Run: gh auth login  (token needs 'repo' scope + write access to $UPSTREAM_REPO)"
  fi
fi

# ---------------------------------------------------------------------------
# 1. Render RFC into issue body
# ---------------------------------------------------------------------------
# Strip the YAML frontmatter (everything between first two `---` lines), drop
# the H1 title (since GitHub issue title is separate), and prepend a brief
# context header that identifies the source RFC.

ISSUE_BODY_FILE=$(mktemp -t loopzero-forge-issue-body.XXXXXX.md)
# shellcheck disable=SC2064  # cleanup path captured at trap-set time (script-scoped var)
trap "rm -f '$ISSUE_BODY_FILE'" EXIT

cat > "$ISSUE_BODY_FILE" <<EOF
> **Source RFC:** \`github.com/loopzero/loopzero\` →
> \`.claude/knowledge-base/concepts/operations/rfc-2026-05-13-loopzero-forge-node-affinity.md\`
>
> **Filing context:** CORRECTIONS.txt item I (2026-04-09 stress test war
> journal). LoopZero main repo currently mitigates the PVC RWO Multi-Attach
> symptom via per-consumer renderer wrap; this issue proposes adopting the
> fix upstream as a loopzero-forge default so consumers stop drifting.

---

EOF

# Strip frontmatter + top H1, append the RFC body.
awk '
  BEGIN { fm_state=0; seen_content=0; skipped_h1=0 }
  /^---$/ {
    if (fm_state == 0) { fm_state = 1; next }
    if (fm_state == 1) { fm_state = 2; next }
  }
  fm_state == 2 {
    # Skip leading blank lines until the first content line.
    if (!seen_content) {
      if (NF == 0) next
      seen_content = 1
      # The first content line is the H1 title — issue has its own title,
      # so skip it (and one optional blank line that follows).
      if (/^# /) { skipped_h1 = 1; next }
    } else if (skipped_h1 && NF == 0) {
      # Consume the single blank line after the skipped H1.
      skipped_h1 = 0
      next
    }
    print
  }
' "$RFC_DOC" >> "$ISSUE_BODY_FILE"

# Append a closure block.
cat >> "$ISSUE_BODY_FILE" <<EOF

---

### Acceptance criteria (from filing side)

- [ ] loopzero-forge maintainer acknowledges the proposal
- [ ] Default \`podAffinity\` (or \`nodeAffinity\`) lands in \`BuildContainer\`
      template
- [ ] loopzero-forge release notes mention the default + migration impact
- [ ] LoopZero main repo cleanup PR removes \`api/internal/argo/workflow_builder.go:262-288\`

### Cross-refs

- LoopZero main repo finding: \`.claude/knowledge-base/concepts/operations/rfc-2026-05-13-loopzero-forge-node-affinity.md\`
- LoopZero's current per-consumer wrap: [\`workflow_builder.go:262-288\`](https://github.com/loopzero/loopzero/blob/develop/api/internal/argo/workflow_builder.go#L262-L288)
- Original war journal: \`infra/tests/load/CORRECTIONS.txt\` § item I
EOF

# ---------------------------------------------------------------------------
# 2. Display + confirm
# ---------------------------------------------------------------------------
log "Upstream repo:  $UPSTREAM_REPO"
log "Issue title:    $ISSUE_TITLE"
log "Body length:    $(wc -l < "$ISSUE_BODY_FILE") lines, $(wc -c < "$ISSUE_BODY_FILE") bytes"

if [[ "$MODE" == "dryrun" ]]; then
  echo "" >&2
  log "=== DRY RUN: rendered issue body below ==="
  echo "" >&2
  cat "$ISSUE_BODY_FILE"
  echo "" >&2
  log "=== END DRY RUN. To file: re-run without --dry-run ==="
  exit 0
fi

# ---------------------------------------------------------------------------
# 3. File the issue
# ---------------------------------------------------------------------------
case "$MODE" in
  interactive)
    log "Opening gh issue create in --web mode (preview + edit in browser before submit)..."
    if gh issue create --repo "$UPSTREAM_REPO" --title "$ISSUE_TITLE" --body-file "$ISSUE_BODY_FILE" --web; then
      log "Browser opened. Submit the issue from there."
      exit 0
    else
      log "ERROR: gh issue create --web failed (possible: not auth'd or no access to $UPSTREAM_REPO)"
      exit 2
    fi
    ;;
  auto)
    echo "" >&2
    log "About to file issue on $UPSTREAM_REPO (--auto mode, non-interactive)"
    log "Title: $ISSUE_TITLE"
    read -rp "Proceed? [y/N]: " confirm
    if [[ "${confirm,,}" != "y" ]]; then
      log "Operator cancelled."
      exit 3
    fi
    if ! gh issue create --repo "$UPSTREAM_REPO" --title "$ISSUE_TITLE" --body-file "$ISSUE_BODY_FILE"; then
      log "ERROR: gh issue create failed (possible: not auth'd or no access to $UPSTREAM_REPO)"
      exit 2
    fi
    log "Issue filed successfully."
    exit 0
    ;;
esac
