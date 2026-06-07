#!/usr/bin/env bash
# preflight-themory.sh — verify all prerequisites for the TheMemory native-process dev loop.
#
# Per plan v1.1 § Phase 0 / T0.1 of loopzero-cloud-themory-e2e-finalize-plan.md.
# Refuses to proceed if any pre-condition fails (per workflow.md 95% Confidence Rule).
#
# Checks (5 total):
#   1. task doctor exits 0 with ≥ 38 PASS (k3d clusters + pods + JWT seed)
#   2. PostgreSQL reachable on localhost:5432 via libpq URL (EC-2 portable form)
#   3. pgvector extension creatable (CREATE EXTENSION IF NOT EXISTS vector)
#   4. TheMemory CLI built + reachable via THEMORY_CLI env var (EC-1 portable)
#   5. loopzero-system namespace exists (EC-5 fail-fast)
#
# Run BEFORE: bash infra/scripts/local/start-themory.sh
#
# Env overrides (operator-customizable):
#   PGURL          — PostgreSQL connection string (default postgresql://localhost:5432/postgres)
#   THEMORY_CLI    — path to TheMemory CLI (default $(realpath ../loopzero-memory)/dist/agent-tools/cli.js)

set -euo pipefail

readonly PGURL="${PGURL:-postgresql://localhost:5432/postgres}"
readonly THEMORY_CLI="${THEMORY_CLI:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../" && realpath ../loopzero-memory)/dist/agent-tools/cli.js}"

# Color codes for human-readable output
readonly C_RED='\033[0;31m'
readonly C_GREEN='\033[0;32m'
readonly C_YELLOW='\033[0;33m'
readonly C_RESET='\033[0m'

pass() { printf "%b[PASS]%b %s\n" "$C_GREEN" "$C_RESET" "$1"; }
fail() { printf "%b[FAIL]%b %s\n  Remediation: %s\n" "$C_RED" "$C_RESET" "$1" "$2"; exit 1; }
warn() { printf "%b[WARN]%b %s\n" "$C_YELLOW" "$C_RESET" "$1"; }

main() {
  printf "preflight-themory.sh — 5 checks\n\n"

  # Check 1: task doctor health
  if ! command -v task >/dev/null 2>&1; then
    fail "task binary not found" "Install go-task: https://taskfile.dev/installation/"
  fi
  if ! task doctor 2>/dev/null | grep -q "PASS="; then
    fail "task doctor did not produce PASS output" "Run 'task setup' to bootstrap clusters + helm + CRDs"
  fi
  local pass_count
  pass_count=$(task doctor 2>/dev/null | grep -oE "PASS=[0-9]+" | head -1 | grep -oE "[0-9]+" || echo "0")
  if [ "$pass_count" -lt 38 ]; then
    fail "task doctor reported PASS=$pass_count (expected ≥ 38)" "Run 'task setup' OR inspect 'task doctor' output for failed checks"
  fi
  pass "task doctor: PASS=$pass_count"

  # Check 2: PostgreSQL reachable via libpq URL (EC-2 portable — no -U postgres assumption)
  if ! command -v psql >/dev/null 2>&1; then
    fail "psql binary not found" "Install PostgreSQL client tools (apt install postgresql-client / brew install libpq)"
  fi
  if ! psql "$PGURL" -tAc 'SELECT version();' >/dev/null 2>&1; then
    fail "cannot connect to PostgreSQL at $PGURL" "Start local PG (brew services start postgresql / systemctl start postgresql) OR set PGURL env var to your connection string"
  fi
  pass "PostgreSQL reachable: $PGURL"

  # Check 3: pgvector extension creatable
  local probe_db="themory_preflight_probe_$$"
  if ! createdb -h "$(echo "$PGURL" | sed -E 's|.*://([^:/]+).*|\1|')" "$probe_db" 2>/dev/null; then
    warn "could not create throwaway DB $probe_db (may already exist OR insufficient permissions)"
  fi
  local probe_url="${PGURL%/*}/$probe_db"
  if ! psql "$probe_url" -tAc 'CREATE EXTENSION IF NOT EXISTS vector;' >/dev/null 2>&1; then
    dropdb -h "$(echo "$PGURL" | sed -E 's|.*://([^:/]+).*|\1|')" "$probe_db" 2>/dev/null || true
    fail "pgvector extension not creatable" "Install pgvector: see https://github.com/pgvector/pgvector#installation"
  fi
  dropdb -h "$(echo "$PGURL" | sed -E 's|.*://([^:/]+).*|\1|')" "$probe_db" 2>/dev/null || true
  pass "pgvector extension creatable"

  # Check 4: TheMemory CLI built + reachable (EC-1 portable)
  if [ ! -f "$THEMORY_CLI" ]; then
    fail "TheMemory CLI not found at $THEMORY_CLI" "Run 'cd ../loopzero-memory && npm run build' OR set THEMORY_CLI env var to your build path"
  fi
  pass "TheMemory CLI built: $THEMORY_CLI"

  # Check 5: loopzero-system namespace exists (EC-5 fail-fast)
  if ! kubectl get ns loopzero-system >/dev/null 2>&1; then
    fail "loopzero-system namespace missing" "Run 'task setup' to bootstrap K8s namespaces"
  fi
  pass "loopzero-system namespace exists"

  printf "\n%b✓ All 5 preflight checks passed%b\n" "$C_GREEN" "$C_RESET"
  printf "Next: bash infra/scripts/local/start-themory.sh\n"
}

main "$@"
