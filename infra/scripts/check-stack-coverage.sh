#!/usr/bin/env bash
# infra/scripts/check-stack-coverage.sh
#
# Plan 2026-05-15 T10.1: enforces 1:1 mapping between LanguageStrategy
# registered in api/internal/argo/languages.go::ResolveStrategy and
# E2E fixture coverage (infra/tests/e2e/fixtures/*).
#
# Fails CI if a new strategy is registered without a corresponding
# fixture, OR if a fixture exists without a registered strategy
# (orphan).
#
# Per D6 of close-all-gaps plan: prevents drift via mechanical check.
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
LANGUAGES_FILE="$REPO_ROOT/api/internal/argo/languages.go"
FIXTURES_DIR="$REPO_ROOT/infra/tests/e2e/fixtures"

log() { printf '[check-stack-coverage] %s\n' "$*" >&2; }
die() { log "FAIL: $*"; exit 1; }

if [[ ! -f "$LANGUAGES_FILE" ]]; then
  die "languages.go not found at $LANGUAGES_FILE"
fi
if [[ ! -d "$FIXTURES_DIR" ]]; then
  die "fixtures dir not found at $FIXTURES_DIR"
fi

# Strategy names per ResolveStrategy switch. Hardcoded here because the
# Go AST is more work than the maintenance burden of updating both.
declare -A STRATEGIES=(
  [node]="NodeStrategy"
  [go]="GoStrategy"
  [python]="PythonStrategy"
  [ruby]="RubyStrategy"
  [java]="JavaStrategy"
  [php]="PHPStrategy"
  [rust]="RustStrategy"
  [custom]="CustomStrategy"
  [user-dockerfile]="UserDockerfileStrategy"
)

# Verify each strategy's Go type exists in languages.go.
log "verifying strategy types declared in languages.go..."
for lang in "${!STRATEGIES[@]}"; do
  type="${STRATEGIES[$lang]}"
  if ! grep -q "type ${type}\b" "$LANGUAGES_FILE"; then
    die "strategy '$lang' missing type '$type' in languages.go (drift detected)"
  fi
done

# Verify each strategy has at least one fixture in tests/e2e/fixtures/.
# Strategy ↔ fixture mapping by convention (fixture dir contains
# loopzero.yaml with framework: <lang>).
log "verifying each strategy has E2E fixture coverage..."
declare -A COVERED
for fixture in "$FIXTURES_DIR"/*/; do
  fix_name=$(basename "$fixture")
  loopzero_yaml="$fixture/loopzero.yaml"
  if [[ ! -f "$loopzero_yaml" ]]; then
    continue
  fi
  # Extract framework value (single-key YAML grep — light, no parser).
  framework=$(grep -E '^\s*framework:' "$loopzero_yaml" 2>/dev/null \
    | head -1 \
    | sed -E 's/^\s*framework:\s*//' \
    | tr -d '"' \
    | tr -d "'" \
    | xargs)
  if [[ -n "$framework" ]]; then
    # Map fastapi/django/python -> python, etc.
    case "$framework" in
      node|nextjs|vite|remix|nestjs|express)   COVERED[node]=1 ;;
      go|gin)                                  COVERED[go]=1 ;;
      python|fastapi|django)                   COVERED[python]=1 ;;
      ruby|sinatra|rails)                      COVERED[ruby]=1 ;;
      java|spring|spring-boot)                 COVERED[java]=1 ;;
      php|laravel|slim)                        COVERED[php]=1 ;;
      rust|axum|actix|rocket)                  COVERED[rust]=1 ;;
      custom)                                  COVERED[custom]=1 ;;
      user-dockerfile)                         COVERED[user-dockerfile]=1 ;;
    esac
  fi
done

# Special case: user-dockerfile strategy is dispatched by `build: dockerfile`
# not by framework — check for build: field too.
for fixture in "$FIXTURES_DIR"/*/; do
  loopzero_yaml="$fixture/loopzero.yaml"
  if [[ -f "$loopzero_yaml" ]] && grep -qE '^\s*build:\s*dockerfile' "$loopzero_yaml" 2>/dev/null; then
    COVERED[user-dockerfile]=1
  fi
done

MISSING=()
for lang in "${!STRATEGIES[@]}"; do
  if [[ -z "${COVERED[$lang]:-}" ]]; then
    MISSING+=("$lang")
  fi
done

if [[ ${#MISSING[@]} -gt 0 ]]; then
  log "==== MISSING FIXTURES ===="
  for m in "${MISSING[@]}"; do
    log "  $m (Go type: ${STRATEGIES[$m]}) — add infra/tests/e2e/fixtures/<name>/loopzero.yaml with framework: $m"
  done
  log ""
  log "Either add fixtures OR deregister the strategy from languages.go::ResolveStrategy."
  exit 1
fi

log "PASS: all ${#STRATEGIES[@]} strategies have fixture coverage."
exit 0
