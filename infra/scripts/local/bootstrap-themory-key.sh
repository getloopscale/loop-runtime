#!/usr/bin/env bash
# bootstrap-themory-key.sh — mint the loopruntime-cloud-dev-bridge API key via TheMemory CLI,
# materialize as K8s Secret `themory-dev-key` in `loopruntime-system` ns for cloud Pod consumption.
#
# Per plan v1.3 § Phase 2 / T2.1 of loopruntime-cloud-themory-e2e-finalize-plan.md.
# v1.2 absorption: real CLI signature is `keys create --name X --scope memory:read|write|delete`
# (NOT --scope admin which doesn't exist; admin access via THEMEMORY_ADMIN_BOOTSTRAP_TOKEN env).
# CLI output is structured stdout text (NOT --json which doesn't exist).
#
# Idempotency model:
#   - default: skip if Secret already exists
#   - --rotate: mint NEW key, replace Secret value
#
# Sequence:
#   1. EC-5: loopruntime-system ns existence guard
#   2. TheMemory /v1/health probe (refuse if not 200)
#   3. Read bootstrap token from .themory-bootstrap-token (refuse if missing/empty)
#   4. Read existing Secret; skip if present + no --rotate
#   5. Invoke `themory keys create --name loopruntime-cloud-dev-bridge --scope memory:{read,write,delete}`
#      with Bearer = bootstrap token; parse `loopruntime_live_*` from stdout via grep
#   6. kubectl apply Secret via stdin (dry-run + apply pattern; idempotent)
#   7. Optional local backup to .themory-key.txt (chmod 600) when THEMORY_KEY_LOCAL_BACKUP=true

set -euo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly ENV_FILE="${SCRIPT_DIR}/.themory.env"
readonly BOOTSTRAP_TOKEN_FILE="${SCRIPT_DIR}/.themory-bootstrap-token"
readonly MASTER_KEY_BACKUP="${SCRIPT_DIR}/.themory-master-key"

if [ -f "$ENV_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
fi

readonly THEMORY_CLI="${THEMORY_CLI:-$(realpath -m "${SCRIPT_DIR}/../../../../../loopruntime-memory/dist/agent-tools/cli.js")}"
readonly THEMORY_URL="${THEMORY_URL:-http://localhost:8080}"
readonly K8S_NAMESPACE="${K8S_NAMESPACE:-loopruntime-system}"
readonly K8S_SECRET_NAME="${K8S_SECRET_NAME:-themory-dev-key}"
readonly K8S_SECRET_KEY="${K8S_SECRET_KEY:-api-key}"
readonly KEY_NAME="${KEY_NAME:-loopruntime-cloud-dev-bridge}"

ROTATE=false
for arg in "$@"; do
  case "$arg" in
    --rotate) ROTATE=true ;;
    -h|--help)
      echo "Usage: $0 [--rotate]"
      echo "  --rotate   Mint NEW key, REPLACE existing Secret value"
      exit 0
      ;;
    *) echo "Unknown arg: $arg (use --help)"; exit 2 ;;
  esac
done

readonly C_RED='\033[0;31m'
readonly C_GREEN='\033[0;32m'
readonly C_YELLOW='\033[0;33m'
readonly C_RESET='\033[0m'

# Logs to stderr; functions returning data via stdout (read_bootstrap_token,
# mint_key) must not be contaminated by log lines via command substitution.
log_info()  { printf "%b[INFO]%b  %s\n" "$C_GREEN" "$C_RESET" "$1" >&2; }
log_warn()  { printf "%b[WARN]%b  %s\n" "$C_YELLOW" "$C_RESET" "$1" >&2; }
log_fatal() { printf "%b[FATAL]%b %s\n" "$C_RED" "$C_RESET" "$1" >&2; exit 1; }

ensure_namespace() {
  kubectl get ns "$K8S_NAMESPACE" >/dev/null 2>&1 \
    || log_fatal "Namespace $K8S_NAMESPACE missing — run 'task setup' first"
  log_info "Namespace $K8S_NAMESPACE present"
}

ensure_themory_live() {
  curl -fsS "${THEMORY_URL}/v1/health" >/dev/null 2>&1 \
    || log_fatal "TheMemory not reachable at $THEMORY_URL — run 'bash start-themory.sh' first"
  log_info "TheMemory health: 200"
}

read_bootstrap_token() {
  [ -f "$BOOTSTRAP_TOKEN_FILE" ] \
    || log_fatal "Bootstrap token file missing ($BOOTSTRAP_TOKEN_FILE) — re-run start-themory.sh to regenerate"
  local token
  token="$(cat "$BOOTSTRAP_TOKEN_FILE")"
  [ -n "$token" ] || log_fatal "Bootstrap token file is empty — re-run start-themory.sh"
  echo "$token"
}

secret_exists() {
  kubectl -n "$K8S_NAMESPACE" get secret "$K8S_SECRET_NAME" >/dev/null 2>&1
}

mint_key() {
  local bootstrap_token="$1"
  log_info "Minting API key '$KEY_NAME' via THEMORY_CLI keys create against $THEMORY_URL"
  local stdout
  stdout="$(THEMEMORY_API_KEY="$bootstrap_token" \
    THEMORY_BASE_URL="$THEMORY_URL" \
    node "$THEMORY_CLI" keys create \
      --name "$KEY_NAME" \
      --scope memory:read \
      --scope memory:write \
      --scope memory:delete 2>&1)" \
    || log_fatal "themory keys create FAILED — output: $stdout"
  local token
  token="$(echo "$stdout" | grep -oE 'loopruntime_live_[a-zA-Z0-9_-]+' | head -1)"
  [ -n "$token" ] \
    || log_fatal "Could not extract loopruntime_live_* token from CLI output. Raw: $stdout"
  echo "$token"
}

apply_secret() {
  local token="$1"
  log_info "Applying Secret $K8S_SECRET_NAME in ns $K8S_NAMESPACE"
  kubectl -n "$K8S_NAMESPACE" create secret generic "$K8S_SECRET_NAME" \
    --from-literal="${K8S_SECRET_KEY}=${token}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null \
    || log_fatal "kubectl apply Secret FAILED"
  log_info "Secret $K8S_SECRET_NAME ready"
}

optional_local_backup() {
  local token="$1"
  if [ "${THEMORY_KEY_LOCAL_BACKUP:-false}" = "true" ]; then
    echo "$token" > "$MASTER_KEY_BACKUP"
    chmod 600 "$MASTER_KEY_BACKUP"
    log_info "Master key backup written to $MASTER_KEY_BACKUP (chmod 600)"
  fi
}

main() {
  printf "bootstrap-themory-key.sh — mint + materialize Secret\n\n"
  ensure_namespace
  ensure_themory_live

  if secret_exists && [ "$ROTATE" = false ]; then
    log_info "Secret $K8S_SECRET_NAME already exists — skipping (use --rotate to mint new key)"
    exit 0
  fi

  local bootstrap_token
  bootstrap_token="$(read_bootstrap_token)"

  local new_token
  new_token="$(mint_key "$bootstrap_token")"
  log_info "Minted loopruntime_live_* key (length ${#new_token})"

  apply_secret "$new_token"
  optional_local_backup "$new_token"

  printf "\n%b✓ Secret %s ready in ns %s%b\n" "$C_GREEN" "$K8S_SECRET_NAME" "$K8S_NAMESPACE" "$C_RESET"
  printf "Next: T3.1 — wire valueFrom.secretKeyRef in infra/tilt/cloud-deployment.yaml\n"
}

main "$@"
