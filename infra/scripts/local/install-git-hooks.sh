#!/usr/bin/env bash
# Idempotent installer for loopzero git hooks (M8 1.5). Git hooks live in .git/
# (not versioned), so this copies the in-repo sources into .git/hooks/.
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
SRC="$ROOT/infra/scripts/local/git-hooks"
DST="$ROOT/.git/hooks"
for hook in pre-push; do
  install -m 0755 "$SRC/$hook" "$DST/$hook"
  echo "installed: .git/hooks/$hook"
done
