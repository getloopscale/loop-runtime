#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-COMPLETO Phase 6 (T6.1 + T6.2 + T6.3)
#
# Harness que invoca CADA comando CLI do LoopZero end-to-end, capturando:
#   - Exit code (deve estar em {0,1,2,3,4,5} — Rule 4)
#   - JSON shape válido quando --json é suportado (Rule 5)
#   - Stdout/stderr scan por internal terms (Rule 3)
#
# Resultado: report estruturado com pass/fail por comando + 3 contratos.
#
# Pré-requisitos:
#   LOOPZERO_TOKEN / LOOPZERO_URL (capturados de ~/.loopzero/local-dogfood-token.txt + localhost:13001)
#   Cluster k3d-standalone-multi up (preflight.sh deve passar antes)
#
# Uso:
#   bash infra/tests/dogfood/cli-coverage.sh         # human-readable report
#   bash infra/tests/dogfood/cli-coverage.sh --json  # JSON to stdout
#
# SPDX-License-Identifier: Apache-2.0

set -u  # NOT -e — comandos não-zero são DADOS, não erros.

JSON_ONLY=0
[[ "${1:-}" == "--json" ]] && JSON_ONLY=1

export LOOPZERO_TOKEN="${LOOPZERO_TOKEN:-$(cat "${HOME}/.loopzero/local-dogfood-token.txt" 2>/dev/null)}"
export LOOPZERO_URL="${LOOPZERO_URL:-http://localhost:13001}"
LOOPZERO_BIN="${LOOPZERO_BIN:-loopzero}"

# Forbidden internal terms (Rule 3 — CLAUDE.md).
INTERNAL_TERMS='ArgoCD|argocd|GitOps|gitops-push|tenant_id|advisory lock|pg_advisory|Gitea|MinIO|kubebuilder|envtest|Kaniko'

RESULTS_FILE=$(mktemp)
trap 'rm -f "$RESULTS_FILE" /tmp/cli-err' EXIT

log()  { [[ "$JSON_ONLY" -eq 1 ]] || echo "[cli-coverage] $*" >&2; }

# record appends a JSON line to RESULTS_FILE (subshell-safe; shared via fs).
record() {
  local cmd=$1 status=$2 exit_code=$3 json_valid=$4 has_internal=$5 evidence=$6
  local esc cmd_esc
  esc=$(printf '%s' "$evidence" | python3 -c 'import sys,json; print(json.dumps(sys.stdin.read()))')
  cmd_esc=$(printf '%s' "$cmd" | python3 -c 'import sys,json; print(json.dumps(sys.stdin.read()))')
  printf '{"cmd":%s,"status":"%s","exit_code":%s,"json_valid":%s,"has_internal_terms":%s,"evidence":%s}\n' \
    "$cmd_esc" "$status" "$exit_code" "$json_valid" "$has_internal" "$esc" >> "$RESULTS_FILE"
  log "[$status] $cmd (exit=$exit_code, json=$json_valid, internal=$has_internal)"
}

# Run a CLI invocation. Captures exit, stdout, stderr.
# Args: cmd_label expects_json [...args]
# expects_json: 1 = --json supported (validate JSON), 0 = no --json
run_cmd() {
  local label=$1
  local expects_json=$2
  shift 2
  local stdout stderr exit_code
  stdout=$("$LOOPZERO_BIN" "$@" 2>/tmp/cli-err); exit_code=$?
  stderr=$(cat /tmp/cli-err 2>/dev/null)

  # Validate exit code in {0..5} (Rule 4).
  local exit_ok=0
  case "$exit_code" in
    0|1|2|3|4|5) exit_ok=1 ;;
  esac

  # Validate JSON if expected.
  local json_valid="null"
  if [[ "$expects_json" -eq 1 ]]; then
    if echo "$stdout" | python3 -c 'import sys,json; json.loads(sys.stdin.read())' >/dev/null 2>&1; then
      json_valid="true"
    else
      json_valid="false"
    fi
  fi

  # Internal-terms scan (stdout + stderr) — Rule 3.
  local combined="${stdout}${stderr}"
  local has_internal="false"
  if echo "$combined" | grep -qE "$INTERNAL_TERMS"; then
    has_internal="true"
  fi

  # Decide PASS/FAIL: Rule 4 mandatory; Rule 3 strict; Rule 5 only when expected.
  local status="PASS"
  local notes="exit=$exit_code"
  if [[ "$exit_ok" -eq 0 ]]; then
    status="FAIL"
    notes="$notes (out of 0..5)"
  fi
  if [[ "$has_internal" == "true" ]]; then
    status="FAIL"
    local hits
    hits=$(echo "$combined" | grep -oE "$INTERNAL_TERMS" | sort -u | head -3 | tr '\n' ',' | sed 's/,$//')
    notes="$notes; Rule3 violation: $hits"
  fi
  if [[ "$expects_json" -eq 1 && "$json_valid" == "false" ]]; then
    status="FAIL"
    notes="$notes; Rule5 invalid JSON: ${stdout:0:80}"
  fi

  record "$label" "$status" "$exit_code" "$json_valid" "$has_internal" "$notes"
}

# Skip — for commands not safe to run blackbox (mutating + interactive).
skip_cmd() {
  local label=$1 reason=$2
  record "$label" "SKIP" "-1" "null" "false" "$reason"
}

log "starting CLI coverage harness"

# =============================================================================
# Read-only / safe commands (run for real)
# =============================================================================

# Global help/version — must be exit 0 and not contain internal terms.
run_cmd "loopzero --help"            0 --help
run_cmd "loopzero --version"         0 --version
run_cmd "loopzero --json --help"     0 --json --help

# §1 Auth (whoami safe; login/logout require browser/state mutation)
run_cmd "loopzero whoami"                 0 whoami
run_cmd "loopzero whoami --json"          1 whoami --json
skip_cmd "loopzero login"                 "interactive device flow"
skip_cmd "loopzero logout"                "mutates ~/.loopzero/config.json"

# §2 Project init (mutates filesystem; tested in Phase 1)
skip_cmd "loopzero init"                  "filesystem mutation; covered in Phase 1"

# §2 validate (read-only on loopzero.yaml)
( cd /tmp/dogfood-fb && run_cmd "loopzero validate" 0 validate )
( cd /tmp/dogfood-fb && run_cmd "loopzero validate --json" 1 validate --json )

# §3 Deploy pipeline read-only — CLI infers project from loopzero.yaml in cwd
( cd /tmp/dogfood-fb && run_cmd "loopzero status --env production"  0 status --env production )
( cd /tmp/dogfood-fb && run_cmd "loopzero status --json"            1 status --env production --json )
( cd /tmp/dogfood-fb && run_cmd "loopzero logs --tail 20"           0 logs --tail 20 )
( cd /tmp/dogfood-fb && run_cmd "loopzero logs --json --tail 20"    1 logs --tail 20 --json )
skip_cmd "loopzero deploy"                        "covered Phase 1"
skip_cmd "loopzero rollback"                      "mutating; Phase 3.6"
skip_cmd "loopzero promote"                       "mutating; Phase 3.5"
skip_cmd "loopzero cancel <build-id>"             "covered Phase 1 (build-8adf cancel)"
skip_cmd "loopzero scale"                         "mutating; Phase 3.4"

# §4 Projetos
run_cmd "loopzero project list"           0 project list
run_cmd "loopzero project list --json"    1 project list --json
run_cmd "loopzero project inspect dogfood-fb"           0 project inspect dogfood-fb
run_cmd "loopzero project inspect dogfood-fb --json"    1 project inspect dogfood-fb --json
skip_cmd "loopzero project create"                      "mutating; Phase 3"
skip_cmd "loopzero project delete"                      "Prova de Morte; Phase 3.8"

# §5 Equipe (read)
run_cmd "loopzero team list"              0 team list
run_cmd "loopzero team list --json"       1 team list --json
skip_cmd "loopzero team add"              "mutating"
skip_cmd "loopzero team remove"           "mutating"
skip_cmd "loopzero team role"             "mutating"

# §6 Ambientes
run_cmd "loopzero env list"               0 env list
run_cmd "loopzero env list --json"        1 env list --json
run_cmd "loopzero env inspect production" 0 env inspect production
skip_cmd "loopzero env create"            "mutating"
skip_cmd "loopzero env delete"            "mutating"

# §7 Custom Domains (read)
run_cmd "loopzero domain list"            0 domain list
run_cmd "loopzero domain list --json"     1 domain list --json
skip_cmd "loopzero domain add"            "mutating; Phase 2.4"
skip_cmd "loopzero domain remove"         "mutating; Phase 2.4"
skip_cmd "loopzero domain verify"         "mutating; Phase 2.4"
skip_cmd "loopzero domain status"         "needs existing domain"
skip_cmd "loopzero domain dns"            "needs existing domain"

# §8 Builds (project inferred from loopzero.yaml)
( cd /tmp/dogfood-fb && run_cmd "loopzero build list --limit 5"           0 build list --limit 5 )
( cd /tmp/dogfood-fb && run_cmd "loopzero build list --limit 5 --json"    1 build list --limit 5 --json )
skip_cmd "loopzero build inspect <build-id>"                      "needs build-id from above"
skip_cmd "loopzero build cancel <build-id>"                       "duplicate of loopzero cancel"

# §9 Secrets (project inferred from loopzero.yaml)
( cd /tmp/dogfood-fb && run_cmd "loopzero secret list --env production"        0 secret list --env production )
( cd /tmp/dogfood-fb && run_cmd "loopzero secret list --env production --json" 1 secret list --env production --json )
skip_cmd "loopzero secret set"                                              "mutating; Phase 2.1"
skip_cmd "loopzero secret delete"                                           "mutating; Phase 2.1"

# §10 GitHub App
skip_cmd "loopzero connect"               "mutating; Phase 4"
skip_cmd "loopzero disconnect"            "mutating; Phase 4"

# §11 Audit (no project flag; tenant-wide)
run_cmd "loopzero audit"                  0 audit
run_cmd "loopzero audit --json"           1 audit --json

# §12 Preview environments (project inferred from loopzero.yaml)
( cd /tmp/dogfood-fb && run_cmd "loopzero preview list"           0 preview list )
( cd /tmp/dogfood-fb && run_cmd "loopzero preview list --json"    1 preview list --json )
skip_cmd "loopzero preview inspect"       "needs existing preview"
skip_cmd "loopzero preview delete"        "mutating"

# §13 Billing
skip_cmd "loopzero billing portal"        "opens browser"

# §14 Admin (operator-only — likely 403 with normal token)
# Note: `admin tenants` only exposes `evict` subcommand — no `list`.
run_cmd "loopzero admin tenants evict --dry-run"        0 admin tenants evict --dry-run
run_cmd "loopzero admin tenants evict --dry-run --json" 1 admin tenants evict --dry-run --json

# Platform install/upgrade/uninstall — out of dogfood scope (cluster-affecting)
skip_cmd "loopzero platform install"      "creates cluster — out of scope"
skip_cmd "loopzero platform upgrade"      "mutates cluster"
skip_cmd "loopzero platform uninstall"    "destructive"
run_cmd  "loopzero platform status"       0 platform status
skip_cmd "loopzero platform secrets"      "interactive"

# =============================================================================
# Output
# =============================================================================

# Aggregate results from RESULTS_FILE (one JSON object per line).
# Note: `grep -c` exits 1 with no matches (still prints "0" to stdout).
# `|| echo 0` concatenates a second "0" → the variable becomes "0\n0" and
# arithmetic fails with "syntax error in expression". Use `grep | wc -l`
# which always exits 0 and produces a clean single-line count.
PASS=$(grep '"status":"PASS"' "$RESULTS_FILE" 2>/dev/null | wc -l | tr -d '[:space:]')
FAIL=$(grep '"status":"FAIL"' "$RESULTS_FILE" 2>/dev/null | wc -l | tr -d '[:space:]')
SKIP=$(grep '"status":"SKIP"' "$RESULTS_FILE" 2>/dev/null | wc -l | tr -d '[:space:]')
TOTAL=$((PASS+FAIL+SKIP))

joined=$(paste -sd, "$RESULTS_FILE")
json="{\"phase\":6,\"results\":[${joined}],\"summary\":{\"pass\":$PASS,\"fail\":$FAIL,\"skip\":$SKIP,\"total\":$TOTAL}}"

if [[ "$JSON_ONLY" -eq 1 ]]; then
  echo "$json"
else
  echo "[cli-coverage] DONE — pass=$PASS fail=$FAIL skip=$SKIP total=$TOTAL" >&2
  echo "$json" | python3 -m json.tool
fi

[[ "$FAIL" -gt 0 ]] && exit 1
exit 0
