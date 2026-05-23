#!/usr/bin/env bash
# Hermetic test: every infra/scripts/ path referenced in any
# .claude/skills/*/SKILL.md MUST exist on disk.
#
# Plan 2026-05-21-deep-review-remediation T0.1 + EC D5.
#
# Background: drill-test/SKILL.md had 6 broken paths (bootstrap-cell-*-cc-drill,
# drill-cancel-refresh, drill-teardown-validator, drill/provision, drill/teardown).
# Skill was not executable. This gate prevents regression.
#
# Also catches the dogfood/SKILL.md GH_TOKEN false reference + to-plan/SKILL.md
# `task teardown` non-existent ref by checking `task X` invocations against
# Taskfile + sub-taskfiles.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SKILLS_DIR="$REPO_ROOT/.claude/skills"

FAIL=0
CHECKED_SKILLS=0
CHECKED_PATHS=0
CHECKED_TASKS=0

fail() { FAIL=$((FAIL + 1)); echo "FAIL: $*" >&2; }
info() { echo "[skill-paths] $*"; }

# 1. Collect canonical task names from Taskfile.yml + taskfiles/*.yml.
#
# Taskfile.yml uses `includes:` with two patterns:
#   - flatten: true  → sub-file tasks appear at root namespace (e.g., `task setup`)
#   - flatten: omitted → sub-file tasks are namespaced (e.g., `task drill:up`)
#
# Only `drill:` is non-flattened today (Taskfile.yml:48). All other 8 includes
# are flattened. So:
#   - Root Taskfile.yml tasks → bare names
#   - 8 flattened sub-files (testfile, helmfile, infrafile, devfile, dbfile,
#     k3dfile, securityfile, observabilityfile) → bare names
#   - taskfiles/drill.yml → prepend `drill:`
ALL_TASKS_FILE=$(mktemp)
trap 'rm -f "$ALL_TASKS_FILE"' EXIT

extract_tasks() {
  # Args: file_path
  # Prints task names (the 2-space-indented `name:` lines under top-level tasks: block).
  # Bug fix: task names contain colons (e.g., `k3d:heal:`, `clean:all:`). Strip
  # only the TERMINATING colon (last `:` before EOL or comment), not internal ones.
  awk '
    /^tasks:/ { in_tasks=1; next }
    in_tasks && /^[a-z]/ { in_tasks=0 }
    in_tasks && match($0, /^  [a-z][a-z0-9:_-]*:[[:space:]]*(#.*)?$/) {
      # Extract task name: everything between leading 2-space and the terminating colon
      line = $0
      sub(/^  /, "", line)         # drop leading indent
      sub(/:[[:space:]]*(#.*)?$/, "", line)   # drop trailing colon + optional comment
      print line
    }
  ' "$1"
}

{
  extract_tasks "$REPO_ROOT/Taskfile.yml"
  # Flattened sub-files: bare task names
  for sub in test helm infra dev db k3d security observability; do
    extract_tasks "$REPO_ROOT/taskfiles/${sub}.yml"
  done
  # Non-flattened (drill): prepend namespace
  extract_tasks "$REPO_ROOT/taskfiles/drill.yml" | sed 's/^/drill:/'
} | sort -u > "$ALL_TASKS_FILE"

info "Discovered $(wc -l < "$ALL_TASKS_FILE") task names across Taskfile.yml + taskfiles/"

# 2. Walk each SKILL.md
for skill_md in "$SKILLS_DIR"/*/SKILL.md; do
  [ -f "$skill_md" ] || continue
  skill_name=$(basename "$(dirname "$skill_md")")
  CHECKED_SKILLS=$((CHECKED_SKILLS + 1))

  # 2a. Extract infra/scripts/ paths
  # Match `infra/scripts/<anything>.sh` (excludes wildcards `infra/scripts/foo-*-bar.sh`
  # which are doc patterns, not real paths).
  while IFS= read -r path; do
    [ -z "$path" ] && continue
    case "$path" in
      *"*"*) continue ;;  # skip wildcard patterns in docs
    esac
    CHECKED_PATHS=$((CHECKED_PATHS + 1))
    if [ ! -e "$REPO_ROOT/$path" ]; then
      fail "$skill_name: missing path $path"
    fi
  done < <(grep -oE 'infra/scripts/[a-zA-Z0-9_./-]+\.sh' "$skill_md" 2>/dev/null | sort -u)

  # 2a-bis. EC-1: SKILLs reference dr/ scripts in an ABBREVIATED form too
  # (e.g. `dr/teardown-validator.sh` without the infra/scripts/ prefix). The
  # 2a grep misses these, so a SKILL could point at a deleted dr/ script and
  # pass falsely — the exact failure class that abandoned the develop-bkp
  # migration. Resolve the bare form against infra/scripts/dr/ and assert
  # existence. (Lines carrying the full path are caught here too, harmlessly
  # resolving to the same file.)
  while IFS= read -r drref; do
    [ -z "$drref" ] && continue
    case "$drref" in
      *"*"*) continue ;;  # skip wildcard doc patterns
    esac
    CHECKED_PATHS=$((CHECKED_PATHS + 1))
    if [ ! -e "$REPO_ROOT/infra/scripts/$drref" ]; then
      fail "$skill_name: missing dr/ script (abbreviated form) infra/scripts/$drref"
    fi
  done < <(grep -oE 'dr/[a-zA-Z0-9_-]+\.sh' "$skill_md" 2>/dev/null | sort -u)

  # 2b. Extract `task <name>` invocations — ONLY from backticked refs or code blocks.
  # Natural language ("this task accomplishes Y") shares the `task <word>` shape
  # but is NOT an invocation; the only reliable signal is markdown backtick code.
  # Skip the `task-automation` skill which is generic (not LoopZero-customized).
  if [ "$skill_name" = "task-automation" ]; then
    : # skip — generic third-party skill, not LoopZero task refs
  else
    while IFS= read -r task_name; do
      [ -z "$task_name" ] && continue
      CHECKED_TASKS=$((CHECKED_TASKS + 1))
      if ! grep -qxF "$task_name" "$ALL_TASKS_FILE"; then
        fail "$skill_name: references non-existent task '$task_name'"
      fi
    done < <(
      # Match `task X` or `task X:Y` INSIDE backticks only.
      grep -oE '`task [a-z][a-z0-9_:-]+`' "$skill_md" 2>/dev/null \
        | sed -E 's/^`task ([a-z][a-z0-9_:-]+)`$/\1/' \
        | sort -u
    )
  fi

  # 2c. Specific anti-pattern guards (per T0.2 + T0.3)
  if [ "$skill_name" = "dogfood" ]; then
    if grep -qE 'GH_TOKEN.*\.env|\.env.*GH_TOKEN' "$skill_md"; then
      fail "$skill_name: SKILL.md still references GH_TOKEN flow via .env (T0.2 — replace with ~/.loopzero/local-dogfood-token.txt)"
    fi
  fi
  if [ "$skill_name" = "to-plan" ]; then
    if grep -qE '\btask teardown\b' "$skill_md"; then
      fail "$skill_name: SKILL.md still references non-existent 'task teardown' (T0.3 — replace with task clean:all)"
    fi
  fi
done

info "Checked $CHECKED_SKILLS skills · $CHECKED_PATHS infra/scripts paths · $CHECKED_TASKS task invocations"

if [ "$FAIL" -gt 0 ]; then
  echo "[skill-paths] FAIL: $FAIL broken references across SKILL.md files" >&2
  exit 1
fi

echo "[skill-paths] PASS — all skill doc references resolve"
