#!/usr/bin/env bash
# CHG-2026-04-29-DOGFOOD-REAL-E2E — Phase 2 (T2.1)
#
# Orquestrador das jornadas dogfood real ponta-a-ponta. Honra ADR D7
# (state persistence) e D1 (fix-systemic-on-error: bloqueia em FAIL sem
# fix_commit). Executa journeys em ordem semântica (D2), com Prova de Vida
# + Prova de Morte (D3) entre cada uma.
#
# Uso:
#   bash infra/tests/dogfood/full-loop.sh                    # run all from current state
#   bash infra/tests/dogfood/full-loop.sh --reset            # zera state e roda do zero
#   bash infra/tests/dogfood/full-loop.sh --dry-run          # mostra próxima journey sem rodar
#   bash infra/tests/dogfood/full-loop.sh --only journey-01  # só essa journey
#   bash infra/tests/dogfood/full-loop.sh --status           # imprime estado atual
#
# State em ~/.loopzero/dogfood_state.json:
#   {
#     "version": "1.0",
#     "started_at": "...",
#     "current_journey": "journey-01-solo-deploy",
#     "journeys": [
#       {"name": "preflight", "status": "PASS", "duration_sec": 4},
#       {"name": "journey-01-solo-deploy", "status": "FAIL", "blocker_issue": "...", "fix_commit": null}
#     ]
#   }
#
# Exit codes:
#   0  — todos os journeys (até o ponto pedido) PASS
#   1  — journey FAIL sem fix_commit (loop bloqueado, requer fix em código)
#   2  — pré-requisito ausente (preflight FAIL)
#   3  — argumento inválido
#
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
DOGFOOD_DIR="${REPO_ROOT}/infra/tests/dogfood"
JOURNEYS_DIR="${DOGFOOD_DIR}/journeys"
STATE_FILE="${HOME}/.loopzero/dogfood_state.json"

MODE="run"
ONLY_JOURNEY=""

# Argument parsing.
while [[ $# -gt 0 ]]; do
  case "$1" in
    --reset)   MODE="reset" ;;
    --dry-run) MODE="dry-run" ;;
    --status)  MODE="status" ;;
    --only)    shift; ONLY_JOURNEY="$1" ;;
    -h|--help)
      sed -n '4,28p' "$0"; exit 0 ;;
    *)
      echo "ERROR: unknown arg: $1" >&2
      exit 3 ;;
  esac
  shift
done

# Ordered list of journeys (Phase 0 + Phase 3-13). Phase 1 (F7 fix) lives in
# code, not as a journey.
JOURNEYS=(
  "preflight"
  "journey-01-solo-deploy"
  "journey-02-multi-server"
  "journey-03-secrets"
  "journey-04-scale"
  "journey-05-promote"
  "journey-06-rollback"
  "journey-07-custom-domain"
  "journey-08-github-autodeploy"
  "journey-09-preview"
  "journey-10-failures"
  "journey-11-admin"
)

# Ensure state dir exists + initial doc if absent or --reset.
init_state() {
  mkdir -p "$(dirname "$STATE_FILE")"
  if [[ "$MODE" == "reset" ]] || [[ ! -f "$STATE_FILE" ]]; then
    cat > "$STATE_FILE" <<EOF
{
  "version": "1.0",
  "started_at": "$(date -Iseconds)",
  "current_journey": "${JOURNEYS[0]}",
  "journeys": []
}
EOF
    echo "[full-loop] state initialized at $STATE_FILE" >&2
  fi
}

# Read state field via python (jq-free).
state_field() {
  local field=$1
  python3 - "$STATE_FILE" "$field" <<'PY'
import json, sys
path, field = sys.argv[1], sys.argv[2]
try:
    with open(path) as f:
        data = json.load(f)
except Exception:
    print("")
    sys.exit(0)
keys = field.split(".")
cur = data
for k in keys:
    if isinstance(cur, dict):
        cur = cur.get(k, "")
    else:
        print("")
        sys.exit(0)
print(cur if isinstance(cur, str) else json.dumps(cur))
PY
}

# Update a journey entry (idempotent — replaces if name matches).
record_journey() {
  local name=$1 status=$2 duration=$3 evidence=$4
  python3 - "$STATE_FILE" "$name" "$status" "$duration" "$evidence" <<'PY'
import json, sys
path, name, status, duration, evidence = sys.argv[1:6]
with open(path) as f:
    data = json.load(f)
journeys = data.get("journeys", [])
journeys = [j for j in journeys if j.get("name") != name]
journeys.append({
    "name": name,
    "status": status,
    "duration_sec": int(duration),
    "evidence": evidence,
    "fix_commit": None if status == "FAIL" else "n/a",
})
data["journeys"] = journeys
with open(path, "w") as f:
    json.dump(data, f, indent=2)
PY
}

# Find the next journey to run (skip PASS, stop at FAIL without fix_commit).
next_journey() {
  python3 - "$STATE_FILE" "${JOURNEYS[@]}" <<'PY'
import json, sys
path = sys.argv[1]
order = sys.argv[2:]
with open(path) as f:
    data = json.load(f)
status_by_name = {j["name"]: j for j in data.get("journeys", [])}
for name in order:
    j = status_by_name.get(name)
    if j is None:
        print(name); break
    if j["status"] == "PASS":
        continue
    if j["status"] == "FAIL" and not j.get("fix_commit") or j.get("fix_commit") == "n/a":
        # Wait — already failed with no fix.
        # If fix_commit was populated by humans (set to a sha), allow retry.
        if j.get("fix_commit") in (None, "", "n/a"):
            print(f"BLOCKED:{name}"); break
    print(name); break
else:
    print("ALL_DONE")
PY
}

run_one() {
  local name=$1
  local script
  if [[ "$name" == "preflight" ]]; then
    script="${DOGFOOD_DIR}/preflight.sh"
  else
    script="${JOURNEYS_DIR}/${name}.sh"
  fi
  if [[ ! -f "$script" ]]; then
    echo "[full-loop] $name: script $script not found — recording as PENDING" >&2
    record_journey "$name" "PENDING" 0 "script not yet implemented"
    return 0
  fi
  if [[ ! -x "$script" ]]; then chmod +x "$script"; fi
  echo "[full-loop] === $name ===" >&2
  local start end status evidence
  start=$(date +%s)
  if "$script"; then
    status="PASS"
    evidence="exit 0"
  else
    status="FAIL"
    evidence="exit $? (see logs above)"
  fi
  end=$(date +%s)
  record_journey "$name" "$status" "$(( end - start ))" "$evidence"
  echo "[full-loop] $name: $status (duration: $(( end - start ))s)" >&2
  if [[ "$status" == "FAIL" ]]; then
    return 1
  fi
}

# --- Driver ---

init_state

case "$MODE" in
  status)
    cat "$STATE_FILE"
    exit 0
    ;;
  dry-run)
    next=$(next_journey)
    echo "[full-loop] next: $next"
    exit 0
    ;;
  reset)
    echo "[full-loop] state reset; re-run without --reset to start the loop" >&2
    exit 0
    ;;
esac

if [[ -n "$ONLY_JOURNEY" ]]; then
  run_one "$ONLY_JOURNEY"
  exit $?
fi

# Run loop.
exit_code=0
while :; do
  next=$(next_journey)
  case "$next" in
    "")          echo "[full-loop] no next journey; halting" >&2; break ;;
    "ALL_DONE")  echo "[full-loop] ALL DONE — every journey PASS" >&2; break ;;
    BLOCKED:*)
      blocked="${next#BLOCKED:}"
      echo "[full-loop] BLOCKED on '$blocked' — fix in code, then re-run (state at $STATE_FILE)" >&2
      exit_code=1; break
      ;;
  esac
  if ! run_one "$next"; then
    exit_code=1
    break
  fi
done

exit $exit_code
