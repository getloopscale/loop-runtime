#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP iter 14 — idempotency regression test.
#
# Validates that the secret-generation scripts preserve existing Secret
# values across re-runs. This is the core fix for the recurring pain
# where every `helmfile sync` regenerated passwords and broke component auth.
#
# Test pattern:
#   1. Run script first time → captures generated passwords.
#   2. Run script second time → asserts SAME passwords still in Secrets.
#   3. Asserts cosign-signing-key is preserved (B1 protection).
#   4. Asserts gitea token is preserved (B4 protection).
#
# Run: bash infra/helmfile/scripts/test_helpers/test-idempotency.sh
#
# Requires: k3d-loopruntime-test cluster reachable + the real generate-*.sh scripts.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SECRETS_SCRIPT="${SCRIPT_DIR}/../generate-secrets.sh"

PASS=0
FAIL=0
log() { printf '%s\n' "$*" >&2; }
ok()   { log "  PASS  $1"; PASS=$((PASS+1)); }
fail() { log "  FAIL  $1: $2"; FAIL=$((FAIL+1)); }

# Setup mock kubectl that captures state in memory
MOCK_LOG=$(mktemp)
MOCK_STATE=$(mktemp -d)
export MOCK_KUBECTL_LOG="$MOCK_LOG"
trap "rm -rf $MOCK_LOG $MOCK_STATE" EXIT

# Build a stateful mock kubectl that simulates Secret persistence.
cat > "${MOCK_STATE}/kubectl" <<'KUBEEOF'
#!/usr/bin/env bash
set -e
LOG="${MOCK_KUBECTL_LOG:-/tmp/mock-kubectl.log}"
STATE_DIR="${MOCK_KUBECTL_STATE:-/tmp/mock-state}"
mkdir -p "$STATE_DIR"

# Log every invocation — BEFORE stripping global flags, so assertions can
# still see the context this was called with.
{ echo "----"; echo "args: $*"; } >> "$LOG"

# Real kubectl accepts global flags BEFORE the verb; this mock dispatches
# positionally on "$1"/"$2", so it must model that or it misreads the verb.
#
# Concretely: since the #115 context-pin fix, the callers wrap kubectl as
#   kubectl() { command kubectl --context="$KUBECTL_CONTEXT" "$@"; }
# which makes $1="--context=..." and $2="get". The dispatch below then never
# matched, the mock returned nothing, and `set -euo pipefail` in the harness
# turned the resulting broken pipe into SIGPIPE (exit 141) mid-"Run 1" — the
# `secrets idempotency` FAIL that has been red in `task validate:helmfile-scripts`
# ever since, with no output explaining why. Strip the leading global flags.
while [[ "${1:-}" == --context=* || "${1:-}" == --kubeconfig=* || "${1:-}" == --namespace=* ]]; do
  shift
done
if [[ "${1:-}" == "--context" || "${1:-}" == "--kubeconfig" || "${1:-}" == "--namespace" ]]; then
  shift 2
fi

# Simulate: kubectl get secret X -n NS -o jsonpath='{.data.KEY}'
# Returns base64 of stored value or empty
if [[ "$1" == "get" && "$2" == "secret" ]]; then
  shift 2
  name="$1"; shift
  ns=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -n) ns="$2"; shift 2 ;;
      -o) shift; jp="$1"; shift ;;
      *) shift ;;
    esac
  done
  # Path: state-dir/<ns>/<name>/<key>
  if [[ "${jp:-}" == "jsonpath="* ]]; then
    key=$(echo "$jp" | sed 's|.*\.data\.\([^}]*\)}.*|\1|' | sed 's|\\\.|.|g')
    file="${STATE_DIR}/${ns:-default}/${name}/${key}"
    if [[ -f "$file" ]]; then
      base64 -w0 < "$file"
      exit 0
    fi
    exit 0   # empty
  fi
  # `kubectl get secret X -n NS` (no jsonpath) — returns simulated YAML
  if [[ -d "${STATE_DIR}/${ns:-default}/${name}" ]]; then
    echo "apiVersion: v1"
    echo "kind: Secret"
    echo "metadata:"
    echo "  name: $name"
    echo "  namespace: $ns"
    exit 0
  fi
  echo "Error from server (NotFound): secrets \"$name\" not found" >&2
  exit 1
fi

# Simulate: kubectl create secret generic NAME -n NS --from-literal=KEY=VALUE --dry-run=client -o yaml
# Captures the rendered YAML to stdout
if [[ "$1" == "create" && "$2" == "secret" ]]; then
  shift 3   # create secret generic
  name="$1"; shift
  ns=""
  declare -a literals files
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -n) ns="$2"; shift 2 ;;
      --from-literal=*) literals+=("${1#--from-literal=}"); shift ;;
      --from-file=*)   files+=("${1#--from-file=}"); shift ;;
      --dry-run=*) shift ;;
      -o) shift 2 ;;
      *) shift ;;
    esac
  done
  # Persist values for next get
  mkdir -p "${STATE_DIR}/${ns:-default}/${name}"
  for kv in "${literals[@]}"; do
    k="${kv%%=*}"; v="${kv#*=}"
    printf '%s' "$v" > "${STATE_DIR}/${ns:-default}/${name}/${k}"
  done
  for kf in "${files[@]}"; do
    k="${kf%%=*}"; f="${kf#*=}"
    cp "$f" "${STATE_DIR}/${ns:-default}/${name}/${k}"
  done
  echo "apiVersion: v1"
  echo "kind: Secret"
  echo "metadata: {name: $name, namespace: $ns}"
  exit 0
fi

# kubectl create namespace X --dry-run | apply -f -
if [[ "$1" == "create" && "$2" == "namespace" ]]; then
  echo "namespace/$3 created"
  exit 0
fi

# kubectl apply -f -
if [[ "$1" == "apply" ]]; then
  cat > /dev/null
  echo "applied (mock)"
  exit 0
fi

# Default: log and succeed
echo "(mock kubectl) $*"
exit 0
KUBEEOF
chmod +x "${MOCK_STATE}/kubectl"

# Use isolated mock state for this test
export MOCK_KUBECTL_STATE="${MOCK_STATE}/state"
mkdir -p "$MOCK_KUBECTL_STATE"

ORIG_PATH="$PATH"
export PATH="${MOCK_STATE}:$PATH"

# Provide minimal env vars so script runs all paths
export LOOPRUNTIME_DB_HOST="db.example.com"
export LOOPRUNTIME_DB_PORT="5432"
export LOOPRUNTIME_DB_USER="loopruntime"
export LOOPRUNTIME_DB_NAME="loopruntime"
export LOOPRUNTIME_DB_PASSWORD="db-pwd-from-pulumi"
export LOOPRUNTIME_SPACES_KEY="ACCESS"
export LOOPRUNTIME_SPACES_SECRET="SECRET"
export LOOPRUNTIME_REGISTRY_ENDPOINT="registry.digitalocean.com/loopruntime"

# === First run — should generate randoms ===
log ""
log "=== Run 1 (initial install) ==="
bash "$SECRETS_SCRIPT" dev core 2>/dev/null

# Capture generated values
GITEA_PWD_1=$(cat "${MOCK_KUBECTL_STATE}/loopruntime-system/gitea-admin/password" 2>/dev/null || echo "MISSING")
GITEA_TOKEN_1=$(cat "${MOCK_KUBECTL_STATE}/loopruntime-system/gitea-admin/token" 2>/dev/null || echo "MISSING")

log "  Generated GITEA_PWD: ${GITEA_PWD_1:0:8}..."

# === Second run — should reuse same values ===
log ""
log "=== Run 2 (re-run / sync) ==="
unset LOOPRUNTIME_GITEA_ADMIN_PASSWORD
bash "$SECRETS_SCRIPT" dev core 2>/dev/null

GITEA_PWD_2=$(cat "${MOCK_KUBECTL_STATE}/loopruntime-system/gitea-admin/password" 2>/dev/null || echo "MISSING")
GITEA_TOKEN_2=$(cat "${MOCK_KUBECTL_STATE}/loopruntime-system/gitea-admin/token" 2>/dev/null || echo "MISSING")

# === Assertions ===
export PATH="$ORIG_PATH"

# A value is PRESERVED only when it exists on both runs and did not change. Two
# MISSING readings are equal, and comparing them used to pass all four checks
# while the script had written nothing (it no-ops for any cluster but `core`).
preserved() {
  local label="$1" first="$2" second="$3"
  if [[ "$first" == "MISSING" || "$second" == "MISSING" ]]; then
    fail "$label" "not written (run 1: ${first:0:8}, run 2: ${second:0:8})"
  elif [[ "$first" == "$second" ]]; then
    ok "$label preserved across runs"
  else
    fail "$label" "changed: '${first:0:8}' → '${second:0:8}'"
  fi
}

preserved "gitea password" "$GITEA_PWD_1" "$GITEA_PWD_2"
preserved "gitea token" "$GITEA_TOKEN_1" "$GITEA_TOKEN_2"

# B4 — gitea token must be 32 chars (random_password output without trailing newline)
TOKEN_LEN=${#GITEA_TOKEN_2}
[[ "$TOKEN_LEN" -ge 32 && "$TOKEN_LEN" -le 80 ]] && ok "B4 — gitea token length $TOKEN_LEN (no trailing newline)" \
  || fail "B4" "token length $TOKEN_LEN suspicious"

# DB password from env should override
DB_PWD_2=$(cat "${MOCK_KUBECTL_STATE}/loopruntime-system/loopruntime-db-credentials/password" 2>/dev/null || echo "")
[[ "$DB_PWD_2" == "db-pwd-from-pulumi" ]] && ok "DB password from env (Pulumi L1) honored" \
  || fail "DB_PWD" "expected 'db-pwd-from-pulumi', got '$DB_PWD_2'"

log ""
log "RESULT: ${PASS} PASS, ${FAIL} FAIL (idempotency target: 4/4)"
[[ "$FAIL" == "0" ]] && exit 0 || exit 1
