#!/usr/bin/env bash
# Idempotent installer for loopzero git hooks (M8 1.5). Git hooks live in .git/
# (not versioned), so this copies the in-repo sources into .git/hooks/.
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
SRC="$ROOT/infra/scripts/local/git-hooks"
DST="$ROOT/.git/hooks"
# A one-item list today, and the loop exists so it keeps being a list
# when the second hook arrives. shellcheck warns that the iteration runs only once
# — that is true and intentional.
# shellcheck disable=SC2043
for hook in pre-push; do
  install -m 0755 "$SRC/$hook" "$DST/$hook"
  echo "installed: .git/hooks/$hook"
done

# loopzero#256 — keep the SSH connection alive while pre-push runs.
#
# git opens the connection to the remote BEFORE running pre-push, and only
# transfers objects after the hook returns. `task quality:gates` takes minutes
# (trivy-scan alone downloads the CVE database), so the connection sits idle
# that whole time and the remote closes it. git then writes to a dead socket
# and the push dies with SIGPIPE:
#
#     Connection to github.com closed by remote host.
#     exit 141
#
# 141 is 128+13. Nothing failed a gate — the log shows every one passing — but
# the exit code is indistinguishable from a gate rejection, which sends the
# operator hunting through a green log.
#
# Shortening the hook was considered and rejected: with this repo's CI blocked
# (#253), `task quality:gates` is the ONLY live consumer of these gates. Moving
# the slow ones to a scheduled target would leave them registered-but-unrun —
# the exact defect closed in #255.
#
# --local so this stays inside the clone and never edits the operator's
# ~/.ssh/config or global git config.
git config --local core.sshCommand "ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=20"
echo "configured: core.sshCommand keepalive (loopzero#256 — pre-push outlives the idle timeout)"
