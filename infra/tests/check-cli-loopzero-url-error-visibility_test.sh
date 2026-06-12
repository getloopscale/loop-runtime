#!/usr/bin/env bash
# T3.8 of plan 2026-06-11-cleanroom-canonical-5-defense-layers.
#
# Hermetic source-level grep gate asserting `cli/src/` does NOT contain
# silent error-swallowing antipatterns that hide CLI failures from users.
# Closes catch #13 from T6.2 session (memory `reference_t62_cleanroom_
# session_2026_06_11` + `j05-aprendizado-cli-loopzero-url-subshell-env-
# propagation`): `export LOOPZERO_URL=...` followed by separate `loopzero login`
# in subshell wrapped with try/catch that returned null silently — user
# saw no error, no login, no clue.
#
# Per memory `feedback_swallowed_silent_errors_pattern`, the canonical
# anti-pattern is:
#   try { ... } catch (e) { return null; }
# OR equivalents that lose error context. Errors must propagate OR be
# logged with useful context (URL attempted, env var name, suggestion).
#
# Wired in: task quality:gates.
# Allowlist entry: KEEP-CI-WIRED (T3.8 of cleanroom-canonical plan).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CLI_SRC="$REPO_ROOT/cli/src"

pass=0
fail=0

log() { printf '[cli-loopzero-url-error-visibility] %s\n' "$*" >&2; }

if [[ ! -d "$CLI_SRC" ]]; then
  log "FAIL [cli/src/ directory not found at $CLI_SRC]"
  exit 1
fi

# === Test 1: silent catch antipattern ===
# Pattern: `catch (...) { return null }` OR `catch { return null }`
# OR `catch (e) { return undefined }`. Returning nullish from catch
# without logging hides the error from caller.
#
# Comment lines (starting with `//` or `*`) are excluded to avoid false
# positives where the antipattern is documented in prose (e.g., "Prior
# `catch { return null }` was misleading...").
TOTAL_SILENT=0
while IFS=: read -r file line content; do
  # Skip pure comment lines (// or * leading)
  if echo "$content" | grep -qE '^[[:space:]]*(\*|//)'; then continue; fi
  TOTAL_SILENT=$((TOTAL_SILENT + 1))
done < <(grep -rnE 'catch[[:space:]]*(\([^)]*\))?[[:space:]]*\{[[:space:]]*return[[:space:]]+(null|undefined)' "$CLI_SRC" --include='*.ts' --include='*.tsx' 2>/dev/null)

if [[ "$TOTAL_SILENT" -eq 0 ]]; then
  log "PASS [no silent catch+return null/undefined antipattern found]"
  pass=$((pass + 1))
else
  log "FAIL [silent catch antipattern found in $TOTAL_SILENT location(s) — errors swallowed]"
  log "  Inspect with: grep -rnE 'catch.*\\{[[:space:]]*return[[:space:]]+(null|undefined)' $CLI_SRC"
  fail=$((fail + 1))
fi

# === Test 2: empty catch blocks (catch { }) ===
EMPTY_CATCH_HITS=$(grep -rEc 'catch[[:space:]]*(\([^)]*\))?[[:space:]]*\{[[:space:]]*\}' "$CLI_SRC" --include='*.ts' --include='*.tsx' 2>/dev/null | awk -F: '$2>0 {sum+=$2} END {print sum+0}')

if [[ "$EMPTY_CATCH_HITS" -eq 0 ]]; then
  log "PASS [no empty catch{} blocks found]"
  pass=$((pass + 1))
else
  log "FAIL [empty catch{} blocks found in $EMPTY_CATCH_HITS location(s) — errors silently dropped]"
  fail=$((fail + 1))
fi

# === Test 3: positive — error messages mention LOOPZERO_URL when caught ===
# At least 1 file must contain useful diagnostic mentioning LOOPZERO_URL env var
# (proves operator gets actionable hint when login/api calls fail).
LOOPZERO_URL_ERR_HITS=$(grep -rEc "(LOOPZERO_URL|loopzero[_-]url).*\\b(error|fail|invalid|unreachable|undefined|missing)" "$CLI_SRC" --include='*.ts' --include='*.tsx' 2>/dev/null | awk -F: '$2>0 {sum+=$2} END {print sum+0}')

if [[ "$LOOPZERO_URL_ERR_HITS" -ge 1 ]]; then
  log "PASS [LOOPZERO_URL referenced in error diagnostic in $LOOPZERO_URL_ERR_HITS location(s)]"
  pass=$((pass + 1))
else
  log "FAIL [no LOOPZERO_URL diagnostic message found — operator gets no hint on env propagation issues]"
  fail=$((fail + 1))
fi

log ""
log "════════════════════════════════════════════════════════"
log "  cli-loopzero-url-error-visibility aggregate: $pass PASS / $fail FAIL"
log "════════════════════════════════════════════════════════"

[[ "$fail" -eq 0 ]]
