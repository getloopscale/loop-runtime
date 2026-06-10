#!/usr/bin/env bash
# Hermetic test: every K8s Secret created by scripts MUST be enumerated
# in docs/operations/credentials-required.md.
#
# Plan 2026-05-21-deep-review-remediation T1.1 + ADR D1 + ADR D5.
#
# Discovery strategy: grep `kubectl create secret generic <name>` patterns
# across all bootstrap scripts, then assert each name appears in the SoT doc.
#
# Why this matters: per memory feedback_skill_md_outdated_vs_code, docs
# silently drift away from code. The user complaint in the deep-review
# triggering this plan was specifically "credentials should be easy to
# manage" — without this gate, the SoT becomes stale within a sprint.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SOT_DOC="$REPO_ROOT/docs/operations/credentials-required.md"

FAIL=0
fail() { FAIL=$((FAIL + 1)); echo "FAIL: $*" >&2; }
info() { echo "[credentials-sot] $*"; }

# --- Pre-condition: SoT doc exists ---
if [ ! -f "$SOT_DOC" ]; then
  fail "SoT doc missing: $SOT_DOC"
  exit 1
fi

# --- Discover all kubectl create secret generic <name> patterns ---
# Match both bare names and "$variable" expansions (the latter we skip with
# heuristic: secret name in $VAR is opaque without runtime context).
DISCOVERED_FILE=$(mktemp)
trap 'rm -f "$DISCOVERED_FILE"' EXIT

# Extract literal secret names from canonical generator scripts. Skip:
#   - $VAR / "$..." expansions (resolved at runtime, opaque to static analysis)
#   - Heredoc names like SECRET_NAME (loop variables)
#   - Test helper scripts (mock/fixture content)
{
  for script in \
    infra/helmfile/scripts/generate-secrets.sh \
    infra/helmfile/scripts/generate-build-secrets.sh \
    infra/helmfile/scripts/generate-runtime-secrets.sh \
    infra/scripts/production/provision-api-tokens.sh; do
    if [ -f "$REPO_ROOT/$script" ]; then
      # `kubectl create secret generic NAME -n NS` where NAME is a literal token.
      # `|| true` neutralizes pipefail when a script has zero matches (under
      # set -euo pipefail, grep returning 1 aborts the loop subshell).
      { grep -hE 'kubectl[^#]*create secret (generic|docker-registry)\s+[a-z][a-z0-9_-]+\s' \
        "$REPO_ROOT/$script" 2>/dev/null \
        | grep -oE 'create secret (generic|docker-registry)\s+[a-z][a-z0-9_-]+' \
        | awk '{print $NF}'; } || true
    fi
  done
} | sort -u > "$DISCOVERED_FILE"

DISCOVERED_COUNT=$(wc -l < "$DISCOVERED_FILE")
info "Discovered $DISCOVERED_COUNT literal Secret names from canonical generator scripts"

if [ "$DISCOVERED_COUNT" -eq 0 ]; then
  fail "no literal Secret names discovered — grep pattern broken?"
  exit 1
fi

# --- Assert each discovered Secret appears in the SoT doc ---
while IFS= read -r secret_name; do
  [ -z "$secret_name" ] && continue
  # The doc uses backticks around Secret names in the "Secret name" column.
  # Match `<name>` (backticked) OR plain `name` in a table cell.
  if grep -qE "\`${secret_name}\`|^\| ${secret_name} " "$SOT_DOC"; then
    :  # found
  else
    fail "Secret '${secret_name}' created in code but NOT documented in $SOT_DOC"
  fi
done < "$DISCOVERED_FILE"

# --- Additionally: assert the 4 mandatory drill env vars are listed ---
for var in DIGITALOCEAN_TOKEN PULUMI_ACCESS_TOKEN PULUMI_CONFIG_PASSPHRASE CLOUDFLARE_API_TOKEN; do
  if ! grep -qE "\`${var}\`|\| ${var} " "$SOT_DOC"; then
    fail "Drill env var '${var}' missing from SoT doc"
  fi
done

# --- Additionally: assert pre-T1.3 hardcoded credentials are listed in
#     the "being eliminated" section (so operators see the migration plan) ---
if ! grep -qE "Chart-hardcoded credentials" "$SOT_DOC"; then
  fail "SoT doc missing 'Chart-hardcoded credentials' section (T1.3 traceability)"
fi

# --- Additionally: assert rotation playbook section exists ---
if ! grep -qE "## Rotation playbook" "$SOT_DOC"; then
  fail "SoT doc missing 'Rotation playbook' section"
fi

if [ "$FAIL" -gt 0 ]; then
  echo "[credentials-sot] FAIL: $FAIL issue(s)" >&2
  echo "" >&2
  echo "Discovered Secret names:" >&2
  sed 's/^/  - /' "$DISCOVERED_FILE" >&2
  exit 1
fi

info "Checked $DISCOVERED_COUNT bootstrap-created Secrets + 4 mandatory env vars + structural sections"
echo "[credentials-sot] PASS — all bootstrap Secrets enumerated in docs/operations/credentials-required.md"
