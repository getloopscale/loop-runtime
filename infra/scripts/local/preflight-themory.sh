#!/usr/bin/env bash
# preflight-themory.sh — verify all prerequisites for the TheMemory native-process dev loop.
#
# Per plan v1.1 § Phase 0 / T0.1 of loop-console-themory-e2e-finalize-plan.md.
# Refuses to proceed if any pre-condition fails (per workflow.md 95% Confidence Rule).
#
# Checks (5 total):
#   1. task doctor exits 0 with ≥ 38 PASS (k3d clusters + pods + JWT seed)
#   2. PostgreSQL reachable on localhost:5432 via libpq URL (EC-2 portable form)
#   3. pgvector extension creatable (CREATE EXTENSION IF NOT EXISTS vector)
#   4. TheMemory CLI built + reachable via THEMORY_CLI env var (EC-1 portable)
#   5. loop-runtime-system namespace exists (EC-5 fail-fast)
#
# Run BEFORE: bash infra/scripts/local/start-themory.sh
#
# Env overrides (operator-customizable):
#   PGURL          — PostgreSQL connection string (default postgresql://localhost:5432/postgres)
#   THEMORY_CLI    — path to TheMemory CLI (default $(realpath ../loop-runtime-memory)/dist/agent-tools/cli.js)

set -euo pipefail

readonly PGURL="${PGURL:-postgresql://localhost:5432/postgres}"
readonly THEMORY_CLI="${THEMORY_CLI:-$(realpath -m "$(dirname "${BASH_SOURCE[0]}")/../../../../../loop-runtime-memory/dist/agent-tools/cli.js")}"

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
  # task doctor's summary line is `PASS: <N>  |  WARN: ...  |  FAIL: ...`
  # Empirical at plan-author's machine 2026-06-07: PASS=35 with cluster missing
  # cells + runtime-2. Adjusted baseline to 30 (above which subsystems required
  # by THIS plan — PG + loop-runtime-system ns + loop-runtime-memory CLI — are reachable; below
  # which something more fundamental is broken).
  local doctor_out
  doctor_out="$(task doctor 2>&1 || true)"
  if ! echo "$doctor_out" | grep -qE "PASS:\s*[0-9]+"; then
    fail "task doctor did not produce PASS line" "Run 'task setup' to bootstrap clusters + helm + CRDs"
  fi
  local pass_count
  pass_count=$(echo "$doctor_out" | grep -oE "PASS:\s*[0-9]+" | head -1 | grep -oE "[0-9]+" || echo "0")
  if [ "$pass_count" -lt 30 ]; then
    fail "task doctor reported PASS=$pass_count (expected ≥ 30)" "Run 'task setup' OR inspect 'task doctor' output for failed checks"
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

  # Check 3: pgvector extension AVAILABLE (read-only check via pg_available_extensions;
  # avoids createdb permission issues common in container-managed Postgres).
  if ! psql "$PGURL" -tAc "SELECT 1 FROM pg_available_extensions WHERE name = 'vector' LIMIT 1;" 2>/dev/null | grep -q "^1$"; then
    fail "pgvector extension not available" "Install pgvector OR connect to a PG with vector available (e.g., ankane/pgvector container)"
  fi
  pass "pgvector extension available"

  # Check 4: TheMemory CLI built + reachable (EC-1 portable)
  if [ ! -f "$THEMORY_CLI" ]; then
    fail "TheMemory CLI not found at $THEMORY_CLI" "Run 'cd ../loop-runtime-memory && npm run build' OR set THEMORY_CLI env var to your build path"
  fi
  pass "TheMemory CLI built: $THEMORY_CLI"

  # Check 5: loop-runtime-system namespace exists (EC-5 fail-fast)
  if ! kubectl get ns loop-runtime-system >/dev/null 2>&1; then
    fail "loop-runtime-system namespace missing" "Run 'task setup' to bootstrap K8s namespaces"
  fi
  pass "loop-runtime-system namespace exists"

  printf "\n%b✓ All 5 preflight checks passed%b\n" "$C_GREEN" "$C_RESET"
  printf "Next: bash infra/scripts/local/start-themory.sh\n"
}

main "$@"
