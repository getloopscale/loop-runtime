#!/usr/bin/env bash
# T2.1 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
#
# Aggregates 10 canonical OpenBao Transit bootstrap manual steps into one
# idempotent task. Closes catch #5 from T6.2 session (memory
# j22-aprendizado-openbao-multi-secret-chain): Transit chain requires
# 6 synchronized artifacts (helm + init + unseal + approle + role_id +
# secret_id + K8s Secret + SPKI pin) — Cleanroom seeds ZERO of them.
# Without this task, operator hand-edits during live debug for 30+ min.
#
# Per ADR D2: idempotent aggregator (re-run no-ops on already-initialized
# state). Stores Shamir shards + root token at ~/.loopzero/cleanroom-unseal.json
# (mode 0600) for re-unseal after Cleanroom OR pod restart.
#
# 10 canonical steps:
#   1. Pre-flight: assert OpenBao pod running + sealed
#   2. bao operator init -key-shares=5 -key-threshold=3 → ~/.loopzero/cleanroom-unseal.json
#   3. bao operator unseal × 3 (shards from cleanroom-unseal.json)
#   4. kubectl create secret openbao-init-token (root token)
#   5. helmfile sync --selector name=openbao-init (T5.1-T5.9 provisioning)
#   6. bao auth enable approle
#   7. bao write auth/approle/role/loopzero-api (policy + token_ttl=24h)
#   8. bao read auth/approle/role/loopzero-api/role-id → capture
#   9. bao write auth/approle/role/loopzero-api/secret-id → capture
#   10. kubectl create secret loopzero-openbao-approle (external-secrets ns) +
#       kubectl create secret openbao-spki-pin from openbao-tls cert SPKI
#
# Wired in: taskfiles/k3d.yml as `task k3d:vault:bootstrap-dev`.
# Allowlist entry: KEEP-OPERATOR (live-cluster orchestration, irreducible).

set -uo pipefail

CONTEXT="${OPENBAO_CONTEXT:-k3d-loopzero-mgmt}"
NS="${OPENBAO_NAMESPACE:-vault-system}"
POD="${OPENBAO_POD:-openbao-0}"
UNSEAL_FILE="${UNSEAL_FILE:-$HOME/.loopzero/cleanroom-unseal.json}"
TARGET_NS="${ESO_TARGET_NS:-external-secrets}"
DRY_RUN="${DRY_RUN:-0}"

log() { printf '[vault-bootstrap-dev] %s\n' "$*" >&2; }
run() {
  log "+ $*"
  [[ "$DRY_RUN" -eq 1 ]] && return 0
  "$@"
}

# === Pre-flight ===
log "Step 1: pre-flight — OpenBao pod state"
if ! kubectl --context "$CONTEXT" -n "$NS" get pod "$POD" >/dev/null 2>&1; then
  log "FAIL: OpenBao pod $POD not found in $NS. Run \`task k3d:vault:bootstrap-in-mgmt\` first."
  exit 1
fi

mkdir -p "$(dirname "$UNSEAL_FILE")"

# LIVE bug fix (T6.2 Cleanroom TOTAL 2026-06-12): TLS x509 unknown authority
# inside the pod because the bao CLI doesn't have the openbao-tls CA bundle.
# Pass BAO_SKIP_VERIFY=true so all `bao` invocations skip cert verification
# (acceptable for dev — prod path uses real CA via cert-manager).
bao() { kubectl --context "$CONTEXT" -n "$NS" exec "$POD" -- env BAO_SKIP_VERIFY=true bao "$@"; }

# === Step 2: init (idempotent — skip if already initialized) ===
# CANONICAL FIX 2026-06-15 Phase 4 dev-public Cleanroom: `bao status` uses
# exit-code SEMANTICS (rc=0 unsealed, rc=2 sealed-but-init, rc=1 error).
# Mixing rc with `set -o pipefail` propagated rc=2 as PIPELINE FAILURE,
# activating `|| echo "false"` fallback regardless of stdout JSON content.
# Result: even when stdout JSON declared initialized=true, the fallback
# lowercased the string, mismatching the "True" (Python-cap) compare below.
# Fix: capture stdout first (always works because bao -format=json emits
# JSON on both rc=0 AND rc=2), then parse independent of rc.
log "Step 2: bao operator init (idempotent)"
BAO_STATUS_JSON=$(bao status -format=json 2>/dev/null || true)
INIT_STATUS=$(echo "$BAO_STATUS_JSON" | python3 -c "import sys,json; d=json.loads(sys.stdin.read() or '{}'); print(d.get('initialized', False))" 2>/dev/null || echo "False")
if [[ "$INIT_STATUS" != "True" ]]; then
  # LIVE bug fix (T6.2 Cleanroom TOTAL 2026-06-12): when OpenBao is fresh
  # (Initialized=false) post-Cleanroom destroy, any existing unseal file is
  # STALE from the previous cycle — auto-archive rather than abort.
  if [[ -f "$UNSEAL_FILE" ]]; then
    ARCHIVE="${UNSEAL_FILE}.pre-cleanroom-$(date +%Y%m%d-%H%M%S)"
    log "INFO: vault fresh + $UNSEAL_FILE exists from previous Cleanroom → archiving to $ARCHIVE"
    mv "$UNSEAL_FILE" "$ARCHIVE"
  fi
  if [[ "$DRY_RUN" -eq 0 ]]; then
    INIT_OUT=$(bao operator init -key-shares=5 -key-threshold=3 -format=json)
    echo "$INIT_OUT" > "$UNSEAL_FILE"
    chmod 0600 "$UNSEAL_FILE"
    log "Init complete. Shards + root_token saved to $UNSEAL_FILE (mode 0600)"
  fi
else
  log "Skip: vault already initialized"
fi

# === Step 3: unseal (idempotent — skip if already unsealed) ===
# CANONICAL FIX 2026-06-15 (same family as Step 2 above): pipefail + bao rc=2
# (sealed-but-init normal exit code) was triggering `|| echo "true"` (lowercase)
# that NEVER matched `"True"` (Python-capitalized) compare → unseal silently
# SKIPPED on every fresh-init invocation. Vault stayed sealed forever; operator
# had to manually `bao operator unseal $SHARD x3`. Sponsor "sem WORKAROUNDs"
# explicit: capture stdout first, parse rc-independent, normalize to "True".
log "Step 3: bao operator unseal × 3 (idempotent)"
BAO_STATUS_JSON=$(bao status -format=json 2>/dev/null || true)
SEALED=$(echo "$BAO_STATUS_JSON" | python3 -c "import sys,json; d=json.loads(sys.stdin.read() or '{}'); print(d.get('sealed', True))" 2>/dev/null || echo "True")
if [[ "$SEALED" == "True" ]]; then
  if [[ ! -f "$UNSEAL_FILE" ]]; then
    log "FAIL: $UNSEAL_FILE missing — cannot unseal without shards. Run init first OR restore from backup."
    exit 1
  fi
  for i in 0 1 2; do
    SHARD=$(python3 -c "import json; print(json.load(open('$UNSEAL_FILE'))['unseal_keys_b64'][$i])")
    run bao operator unseal "$SHARD"
  done
else
  log "Skip: vault already unsealed"
fi

# === Step 4: root token Secret ===
log "Step 4: kubectl create secret openbao-init-token (idempotent)"
ROOT_TOKEN=$(python3 -c "import json; print(json.load(open('$UNSEAL_FILE'))['root_token'])" 2>/dev/null || true)
if [[ -n "$ROOT_TOKEN" ]]; then
  run kubectl --context "$CONTEXT" -n "$NS" create secret generic openbao-init-token \
    --from-literal=BAO_TOKEN="$ROOT_TOKEN" \
    --dry-run=client -o yaml | kubectl --context "$CONTEXT" -n "$NS" apply -f -
fi

# === Step 5: openbao-init helmfile sync ===
log "Step 5: helmfile sync openbao-init (T5.1-T5.9 provisioning)"
if [[ -f "infra/helmfile/vault.helmfile.yaml.gotmpl" ]]; then
  ENV_NAME="${HELMFILE_ENV:-vault-local}"
  run helmfile -f infra/helmfile/vault.helmfile.yaml.gotmpl -e "$ENV_NAME" \
    --kube-context "$CONTEXT" --selector name=openbao-init sync || \
    log "WARN: openbao-init helmfile sync had issues — manual inspect recommended"
fi

# === Step 6+7: approle + policy ===
log "Step 6+7: approle + loopzero-api policy"
export BAO_TOKEN="$ROOT_TOKEN"
bao_auth() { kubectl --context "$CONTEXT" -n "$NS" exec "$POD" -- env BAO_TOKEN="$ROOT_TOKEN" BAO_SKIP_VERIFY=true bao "$@"; }

bao_auth auth enable approle 2>/dev/null || log "Skip: approle already enabled"

# loopzero-api policy with transit/keys/* wildcard (catch #8 from session)
POLICY='path "transit/keys/*" { capabilities = ["create","read","update","delete","list"] }
path "transit/encrypt/*" { capabilities = ["update"] }
path "transit/decrypt/*" { capabilities = ["update"] }
path "kv/data/loopzero/*" { capabilities = ["read"] }
path "kv/metadata/loopzero/*" { capabilities = ["list"] }'

if [[ "$DRY_RUN" -eq 0 ]]; then
  # LIVE bug fix 2026-06-15 Phase 4 Cleanroom: `policy write` invocation
  # via stdin pipe was missing BAO_SKIP_VERIFY=true; cert chain x509
  # verification failed inside the pod (no CA bundle for openbao-tls).
  # Mirrors the bao() / bao_auth() helper fixes.
  echo "$POLICY" | kubectl --context "$CONTEXT" -n "$NS" exec -i "$POD" -- \
    env BAO_TOKEN="$ROOT_TOKEN" BAO_SKIP_VERIFY=true bao policy write loopzero-api - || true
  bao_auth write auth/approle/role/loopzero-api \
    token_policies=loopzero-api \
    token_ttl=24h \
    token_max_ttl=72h || true
fi

# === Step 8+9: extract role_id + secret_id ===
log "Step 8+9: capture role_id + secret_id"
if [[ "$DRY_RUN" -eq 0 ]]; then
  ROLE_ID=$(bao_auth read -format=json auth/approle/role/loopzero-api/role-id 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['role_id'])" 2>/dev/null || true)
  SECRET_ID=$(bao_auth write -f -format=json auth/approle/role/loopzero-api/secret-id 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['secret_id'])" 2>/dev/null || true)
fi

# === Step 10: loopzero-openbao-approle + openbao-spki-pin Secrets ===
log "Step 10: loopzero-openbao-approle Secret in $TARGET_NS + openbao-spki-pin"
if [[ "$DRY_RUN" -eq 0 && -n "${SECRET_ID:-}" ]]; then
  kubectl --context "$CONTEXT" -n "$TARGET_NS" create secret generic loopzero-openbao-approle \
    --from-literal=OPENBAO_VAULT_SECRET_ID="$SECRET_ID" \
    --dry-run=client -o yaml | kubectl --context "$CONTEXT" -n "$TARGET_NS" apply -f - || \
    log "WARN: loopzero-openbao-approle creation failed — namespace $TARGET_NS may not exist yet"
fi

# Extract SPKI pin from openbao-tls cert (catch #7 — dynamic extraction)
log "Step 10b: extract SPKI pin from openbao-tls cert"
if [[ "$DRY_RUN" -eq 0 ]]; then
  TLS_CRT=$(kubectl --context "$CONTEXT" -n "$NS" get secret openbao-tls -o jsonpath='{.data.tls\.crt}' 2>/dev/null | base64 -d 2>/dev/null || true)
  if [[ -n "$TLS_CRT" ]]; then
    # CANONICAL FIX 2026-06-15: pkg/vault/transit_client.go:199 expects
    # HEX-encoded SHA-256 (via hex.DecodeString). Previous base64url
    # encoding (`base64 | tr ...`) caused loopzero-api to crash on startup
    # with "openbao transit client: vault: SPKIPinSHA256 hex decode:
    # encoding/hex: invalid byte: U+0073 's'". `openssl dgst -sha256`
    # with no `-binary` flag emits "SHA256(stdin)= <hex>" so we
    # post-process to strip the prefix; equivalently use `-binary | xxd`.
    SPKI=$(echo "$TLS_CRT" | openssl x509 -pubkey -noout 2>/dev/null | openssl pkey -pubin -outform DER 2>/dev/null | openssl dgst -sha256 -binary 2>/dev/null | xxd -p -c 999 | tr -d '\n' || true)
    if [[ -n "$SPKI" ]]; then
      kubectl --context "$CONTEXT" -n loopzero-system create secret generic openbao-spki-pin \
        --from-literal=OPENBAO_SPKI_PIN_SHA256="$SPKI" \
        --dry-run=client -o yaml | kubectl --context "$CONTEXT" -n loopzero-system apply -f -
      log "OK: openbao-spki-pin Secret created with extracted SPKI"
    fi
  fi
fi

log ""
log "════════════════════════════════════════════════════════"
log "  vault-bootstrap-dev COMPLETE"
log "  Next: trigger ESO refresh or loopzero-api restart to pick up Secret"
log "════════════════════════════════════════════════════════"
