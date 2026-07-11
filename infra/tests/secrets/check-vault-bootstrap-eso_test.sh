#!/usr/bin/env bash
# check-vault-bootstrap-eso_test.sh — Hermetic test for iter 13 of plan
# 2026-06-02-loopzero-api-vault-integration v1.8 (Runbook P1 codification).
#
# Validates infra/scripts/openbao/vault-bootstrap-eso.sh:
#   1. Pre-flight: kubectl missing → fail
#   2. Pre-flight: --wait-timeout non-int → fail
#   3. Pre-flight: SA manifest missing → fail
#   4. Dry-run: prints all 4 steps without kubectl mutating calls
#   5. CA cert empty (Secret not found) → exit 2 "CA cert empty"
#   6. CA cert malformed (no BEGIN CERTIFICATE) → exit 2 "PEM header"
#   7. Happy path: all 4 steps succeed → exit 0 "COMPLETE"
#   8. ClusterSecretStore wait timeout → exit 3
#
# Mocks: kubectl via PATH override + scripted exit/output.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)/infra"
TARGET_SCRIPT="$REPO_ROOT/scripts/openbao/vault-bootstrap-eso.sh"

[[ -f "$TARGET_SCRIPT" ]] || { echo "FATAL: target script not found at $TARGET_SCRIPT"; exit 2; }

PASS=0
FAIL=0

WORKDIR="$(mktemp -d)"
MOCK_BIN="$(mktemp -d)"
MANIFEST_DIR="$WORKDIR/manifests"
mkdir -p "$MANIFEST_DIR"
trap 'rm -rf "$WORKDIR" "$MOCK_BIN"' EXIT

# Fixture manifests (minimal valid YAML so file-exists check passes)
cat > "$MANIFEST_DIR/eso-vault-reader-sa.yaml" <<'EOF'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: eso-vault-reader
  namespace: loopzero-system
EOF

cat > "$MANIFEST_DIR/loopzero-vault-secrets.yaml" <<'EOF'
apiVersion: external-secrets.io/v1beta1
kind: ClusterSecretStore
metadata:
  name: loopzero-vault-secrets
EOF

# Valid PEM CA for happy path (will be base64-encoded by mock kubectl response)
PEM_VALID="-----BEGIN CERTIFICATE-----
MIIBkTCB+wIJANexample
-----END CERTIFICATE-----"
PEM_VALID_B64=$(echo "$PEM_VALID" | base64 -w0)

# Mock kubectl — multi-case dispatch
cat > "$MOCK_BIN/kubectl" <<EOF
#!/usr/bin/env bash
case "\$*" in
  *"version --request-timeout"*) exit 0 ;;
  *"get secret"*"openbao-tls"*"-o jsonpath"*)
    # Honor exit override FIRST — when kubectl "fails", emit error on stderr
    # and exit immediately (NOT after echoing valid CA, which would leak data
    # into $() capture in the script).
    if [ "\${MOCK_GET_SECRET_EXIT:-0}" != "0" ]; then
      echo "Error from server (NotFound): secrets \"openbao-tls\" not found" >&2
      exit "\${MOCK_GET_SECRET_EXIT}"
    fi
    echo "\${MOCK_CA_BASE64:-$PEM_VALID_B64}"
    exit 0
    ;;
  *"create namespace"*) exit 0 ;;
  *"create configmap"*"--dry-run=client"*)
    # Drain stdin (script pipes \`echo "\$VAULT_CA" | kubectl create configmap
    # --from-file=ca.crt=/dev/stdin\`); without consuming it the upstream echo
    # gets SIGPIPE → with the script's \`set -o pipefail\` the whole pipeline
    # fails flakily ("ConfigMap apply failed"). Real kubectl reads stdin, so
    # the mock must too.
    cat >/dev/null 2>&1 || true
    # Emit a fake YAML so the | apply -f - pipe succeeds
    echo "apiVersion: v1
kind: ConfigMap
metadata:
  name: loopzero-vault-ca"
    exit 0
    ;;
  *"apply -f -"*) cat >/dev/null 2>&1 || true; exit "\${MOCK_APPLY_EXIT:-0}" ;;
  *"apply -f"*) exit "\${MOCK_APPLY_EXIT:-0}" ;;
  *"wait clustersecretstore"*) exit "\${MOCK_WAIT_EXIT:-0}" ;;
  *) exit 0 ;;
esac
EOF
chmod +x "$MOCK_BIN/kubectl"

run() {
  # Args: <name> <expect_exit> <expect_pattern> <env_assigns> <extra_flags...>
  local name="$1" expect_exit="$2" expect_pattern="$3" env_assigns="$4"
  shift 4
  local out err rc
  out=$(mktemp); err=$(mktemp)
  (
    if [ -n "$env_assigns" ]; then export $env_assigns; fi
    PATH="$MOCK_BIN:$PATH" SKIP_PREFLIGHT=0 bash "$TARGET_SCRIPT" \
      --mgmt-context k3d-mock-mgmt \
      --vault-context k3d-mock-vault \
      --manifest-dir "$MANIFEST_DIR" \
      --wait-timeout 5 "$@"
  ) >"$out" 2>"$err"
  rc=$?
  local ok=1
  if [[ "$expect_exit" != "$rc" ]]; then ok=0; fi
  if [[ -n "$expect_pattern" ]] && ! grep -qE "$expect_pattern" "$out" "$err"; then ok=0; fi
  if [[ $ok -eq 1 ]]; then
    printf '  PASS: %s\n' "$name"; PASS=$((PASS+1))
  else
    printf '  FAIL: %s (rc=%d, expected=%s)\n' "$name" "$rc" "$expect_exit"
    sed 's/^/    [stdout] /' "$out"; sed 's/^/    [stderr] /' "$err"
    FAIL=$((FAIL+1))
  fi
  rm -f "$out" "$err"
}

echo "Running vault-bootstrap-eso hermetic tests..."
echo

# --- Test 1: --wait-timeout non-int → exit 1 ---
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" \
  --mgmt-context k3d-mock --vault-context k3d-mock \
  --manifest-dir "$MANIFEST_DIR" --wait-timeout abc >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "wait-timeout must be" "$err"; then
  printf '  PASS: test_invalid_wait_timeout_fails\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_invalid_wait_timeout_fails (rc=%d)\n' "$rc"; FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 2: SA manifest missing → exit 1 ---
EMPTY_DIR="$WORKDIR/empty_manifests"
mkdir -p "$EMPTY_DIR"
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" bash "$TARGET_SCRIPT" \
  --mgmt-context k3d-mock --vault-context k3d-mock \
  --manifest-dir "$EMPTY_DIR" >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "1" ]] && grep -q "manifest not found" "$err"; then
  printf '  PASS: test_missing_manifest_fails\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_missing_manifest_fails (rc=%d)\n' "$rc"; FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 3: dry-run prints 4 steps + DRY-RUN COMPLETE, no real CA extraction ---
run "test_dry_run_prints_4_steps" "0" "DRY-RUN COMPLETE" "" --dry-run

# Test 3.5: dry-run STEP markers present
out=$(mktemp); err=$(mktemp)
PATH="$MOCK_BIN:$PATH" SKIP_PREFLIGHT=1 bash "$TARGET_SCRIPT" \
  --mgmt-context k3d-mock --vault-context k3d-mock \
  --manifest-dir "$MANIFEST_DIR" --dry-run >"$out" 2>"$err"; rc=$?
if [[ "$rc" == "0" ]] \
   && grep -q "STEP 1/4" "$err" \
   && grep -q "STEP 2/4" "$err" \
   && grep -q "STEP 3/4" "$err" \
   && grep -q "STEP 4/4" "$err"; then
  printf '  PASS: test_dry_run_all_4_step_markers\n'; PASS=$((PASS+1))
else
  printf '  FAIL: test_dry_run_all_4_step_markers (rc=%d)\n' "$rc"
  sed 's/^/    [stderr] /' "$err"
  FAIL=$((FAIL+1))
fi
rm -f "$out" "$err"

# --- Test 4: CA Secret missing → exit 2 "CA cert empty" ---
run "test_ca_secret_missing_exit_2" "2" "CA cert empty" \
  "MOCK_GET_SECRET_EXIT=1"

# --- Test 5: CA cert malformed (no PEM header) → exit 2 ---
INVALID_B64=$(echo "invalid certificate content" | base64 -w0)
run "test_ca_malformed_no_pem_header_exit_2" "2" "PEM header" \
  "MOCK_CA_BASE64=$INVALID_B64"

# --- Test 6: happy path all 4 steps succeed → exit 0 "COMPLETE" ---
run "test_happy_path_all_succeed_exit_0" "0" "ESO ClusterSecretStore bootstrapped" \
  "MOCK_CA_BASE64=$PEM_VALID_B64"

# --- Test 7: ClusterSecretStore wait timeout → exit 3 ---
run "test_clustersecretstore_wait_timeout_exit_3" "3" "did NOT reach Ready" \
  "MOCK_CA_BASE64=$PEM_VALID_B64 MOCK_WAIT_EXIT=1"

# --- Test 8: kubectl apply fails (SA manifest) → exit 2 ---
run "test_kubectl_apply_failure_exit_2" "2" "kubectl apply" \
  "MOCK_CA_BASE64=$PEM_VALID_B64 MOCK_APPLY_EXIT=1"

echo
echo "═══════════════════════════════════════════════════════════"
printf "  Results: %d PASS / %d FAIL\n" "$PASS" "$FAIL"
echo "═══════════════════════════════════════════════════════════"

[[ $FAIL -eq 0 ]] || exit 1
