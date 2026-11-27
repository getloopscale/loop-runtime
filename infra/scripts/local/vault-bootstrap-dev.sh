#!/usr/bin/env bash
# T2.1 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
# T1.1 of plan dev-public-cross-cluster-secrets v1.1 (issue #111).
#
# Aggregates 10 canonical OpenBao Transit bootstrap manual steps into one
# idempotent task. Closes catch #5 from T6.2 session (memory
# j22-aprendizado-openbao-multi-secret-chain): Transit chain requires
# 6 synchronized artifacts (helm + init + unseal + approle + role_id +
# secret_id + K8s Secret + SPKI pin) — Cleanroom seeds ZERO of them.
# Without this task, operator hand-edits during live debug for 30+ min.
#
# SPLIT-CONTEXT contract (T1.1 / Approach A canonical sponsor mandate
# UNBREAKABLE 2026-06-19, issue #111): OpenBao runs in a SEPARATE
# k3d-loop-runtime-secrets cluster (NOT mgmt). This script splits the kubectl
# fan-out into:
#   - OPENBAO_CONTEXT (default k3d-loop-runtime-secrets) → bao operator init /
#     auth/approle / SPKI read — the secrets-cluster side
#   - TARGET_CONTEXT  (default k3d-loop-runtime-core)  → loop-runtime-openbao-approle +
#     openbao-spki-pin Secret apply (where loop-runtime-api Deployment envFrom
#     reads from) — the core-cluster side
# Legacy single-context invocations (everything against mgmt) were
# canonical only under the superseded `task k3d:secrets:bootstrap-in-core`
# shortcut. Approach A UNBREAKABLE forbids that shortcut.
#
# Per ADR D2: idempotent aggregator (re-run no-ops on already-initialized
# state). Stores Shamir shards + root token at ~/.loop-runtime/cleanroom-unseal.json
# (mode 0600) for re-unseal after Cleanroom OR pod restart.
#
# 10 canonical steps:
#   1. Pre-flight: assert OpenBao pod running + sealed
#   2. bao operator init -key-shares=5 -key-threshold=3 → ~/.loop-runtime/cleanroom-unseal.json
#   3. bao operator unseal × 3 (shards from cleanroom-unseal.json)
#   4. kubectl create secret openbao-init-token (root token)
#   5. helmfile sync --selector name=openbao-init (T5.1-T5.9 provisioning)
#   6. bao auth enable approle
#   7. bao write auth/approle/role/loop-runtime-api (policy + token_ttl=24h)
#   8. bao read auth/approle/role/loop-runtime-api/role-id → capture
#   9. bao write auth/approle/role/loop-runtime-api/secret-id → capture
#   10. kubectl create secret loop-runtime-openbao-approle (external-secrets ns) +
#       kubectl create secret openbao-spki-pin from openbao-tls cert SPKI
#
# Wired in: taskfiles/k3d.yml as `task k3d:secrets:bootstrap-dev`.
# Allowlist entry: KEEP-OPERATOR (live-cluster orchestration, irreducible).

set -uo pipefail

# SPLIT-CONTEXT (T1.1 — Approach A UNBREAKABLE — issue #111).
# OPENBAO_CONTEXT is the secrets-cluster side (bao init/unseal/approle/SPKI).
# TARGET_CONTEXT is the core-cluster side (Secret apply that loop-runtime-api
# Deployment envFrom reads from loop-runtime-system ns).
OPENBAO_CONTEXT="${OPENBAO_CONTEXT:-k3d-loop-runtime-secrets}"
TARGET_CONTEXT="${TARGET_CONTEXT:-k3d-loop-runtime-core}"
# Backwards-compat alias preserved for inline log lines + helmfile sync
# selectors that pre-date the T1.1 split. New code MUST use one of the
# two split variables above.
NS="${OPENBAO_NAMESPACE:-secrets-system}"
POD="${OPENBAO_POD:-openbao-0}"
UNSEAL_FILE="${UNSEAL_FILE:-$HOME/.loop-runtime/cleanroom-unseal.json}"
# T2.1 of plan dev-public-cross-cluster-secrets (issue #111).
# AUTO_UNSEAL_MODE selects between the legacy Shamir init/unseal/shard
# chain (default — preserves backwards compat) and the new Transit
# auto-unseal path (opt-in — eliminates the shards-on-disk custody risk
# per blueprint D2). Operator opts in by exporting AUTO_UNSEAL_MODE=transit
# AND deploying the Transit backend (separate dev-mode OpenBao OR — for
# AWS — KMS). The Transit backend is ephemeral by design (EC-7) — a
# restart loses the wrapping key and the sealed data is unrecoverable;
# acceptable in Cleanroom because the whole stack is re-created each run.
# No seal-migration code (EC-8) — Cleanroom always fresh-inits OpenBao,
# so the vault is born with the active seal type; migrating an existing
# Shamir vault to Transit never happens here.
# When transit auto-unseal is active, main OpenBao auto-unseals at pod
# startup and the EC-3 contract requires the Transit backend to be
# reachable (sys/health 200) BEFORE main init.
AUTO_UNSEAL_MODE="${AUTO_UNSEAL_MODE:-shamir}"
OPENBAO_TRANSIT_ADDR="${OPENBAO_TRANSIT_ADDR:-http://openbao-transit.secrets-system.svc.cluster.local:8200}"
# Cleanroom mandate run 2026-06-18 caught Finding #2 (issue #91): the
# legacy default external-secrets ns put the loop-runtime-openbao-approle
# Secret out of reach of loop-runtime-api Deployment (envFrom reads from
# loop-runtime-system ns), so a fresh Cleanroom seed left loop-runtime-api in
# CrashLoopBackOff with "OPENBAO_VAULT_SECRET_ID required" and the
# downstream master seed Step 2 (`loop-runtime-ops seed minio` (M15 W1) Layer 4 timed
# out waiting for loop-runtime-api rollout. Default now points at the
# canonical consumer ns (loop-runtime-system) so the chain converges on first
# run. Operators with workloads still bound to external-secrets ns
# (pre-canonical setup) override via ESO_TARGET_NS env var.
TARGET_NS="${ESO_TARGET_NS:-loop-runtime-system}"
DRY_RUN="${DRY_RUN:-0}"

log() { printf '[vault-bootstrap-dev] %s\n' "$*" >&2; }
run() {
  log "+ $*"
  [[ "$DRY_RUN" -eq 1 ]] && return 0
  "$@"
}

# === Pre-flight ===
log "Step 1: pre-flight — OpenBao pod state in $OPENBAO_CONTEXT"
if ! kubectl --context "$OPENBAO_CONTEXT" -n "$NS" get pod "$POD" >/dev/null 2>&1; then
  log "FAIL: OpenBao pod $POD not found in $OPENBAO_CONTEXT/$NS."
  log "Approach A canonical (sponsor mandate UNBREAKABLE 2026-06-19, issue #111):"
  log "  1. Provision separate vault cluster: task k3d:secrets:create"
  log "  2. Install OpenBao chart:            task k3d:secrets:bootstrap"
  log "  3. Joint session with the sponsor: Shamir init + unseal × 3 (see docs/runbooks.md)"
  log "Override OPENBAO_CONTEXT env if your vault cluster has a different name."
  exit 1
fi

# T-A4 LIVE finding 2026-06-20 (follow-up to ADR 2026-06-20-dev-public-vault-automated-init):
# When `task k3d:secrets:bootstrap` returns (helm install completes), the openbao-0
# pod may still be Pending without a host assigned — the openbao chart declares
# `wait: false` (per memory feedback_helmfile_wait_false_for_bootstrap_deps). The
# very first `kubectl exec` then fails with "pod openbao-0 does not have a host
# assigned", `bao operator init` silently no-ops, and the unseal file lands
# empty (size 1, just a newline). Downstream Steps 2-10 cascade-fail with
# "Vault is sealed" (Step 6/7) and the AppRole secret never materializes.
#
# Canonical fix: wait for openbao-0 to be Scheduled + Initialized + Running
# BEFORE Step 2 attempts the first `kubectl exec`. `--for=condition=Ready` is
# the right gate because the openbao container's readiness probe goes Ready
# the moment the listener is up — even when sealed (the chart sets
# `livenessProbe.path: /v1/sys/health?standbyok=true&sealedcode=204` which
# tolerates sealed state). 300s budget is generous for cold k3d cluster boot
# (image pull + cert-manager certificate issuance + pod schedule).
log "Step 1.1: wait openbao-0 Ready (300s) — guards against the kubectl-exec-before-schedule race"
if ! kubectl --context "$OPENBAO_CONTEXT" -n "$NS" wait --for=condition=Ready "pod/$POD" --timeout=300s 2>&1; then
  log "FAIL: openbao-0 pod did NOT reach Ready in 300s — Step 2 (bao operator init) cannot proceed safely."
  log "  Probe state: kubectl --context $OPENBAO_CONTEXT -n $NS describe pod $POD"
  exit 1
fi
log "  OK: openbao-0 Ready — proceeding to Step 2"

mkdir -p "$(dirname "$UNSEAL_FILE")"

# LIVE bug fix (T6.2 Cleanroom TOTAL 2026-06-12): TLS x509 unknown authority
# inside the pod because the bao CLI doesn't have the openbao-tls CA bundle.
# Pass BAO_SKIP_VERIFY=true so all `bao` invocations skip cert verification
# (acceptable for dev — prod path uses real CA via cert-manager).
bao() { kubectl --context "$OPENBAO_CONTEXT" -n "$NS" exec "$POD" -- env BAO_SKIP_VERIFY=true bao "$@"; }

# === Step 1.5: EC-3 Transit-ready gate (only when AUTO_UNSEAL_MODE=transit) ===
# Per blueprint D2 + plan T2.1 EC-3: the main OpenBao auto-unseals
# against the Transit backend at pod startup; if the Transit backend is
# not yet ready, main pod CrashLoops. The bootstrap MUST wait for the
# Transit backend /v1/sys/health 200 BEFORE allowing main init.
# When AUTO_UNSEAL_MODE=shamir (default), this gate no-ops.
wait_transit_ready() {
  local attempts="${TRANSIT_READY_ATTEMPTS:-30}"  # 30 * 2s = 60s budget
  log "Step 1.5: EC-3 — wait Transit backend sys/health 200 at $OPENBAO_TRANSIT_ADDR"
  for i in $(seq 1 "$attempts"); do
    # curl from inside the pod (cluster DNS resolves transit endpoint).
    # /v1/sys/health returns 200 on a healthy initialized+unsealed transit
    # OR 472/473 when still booting — anything < 500 means listener is up.
    if kubectl --context "$OPENBAO_CONTEXT" -n "$NS" exec "$POD" -- \
        curl -sk -o /dev/null -w '%{http_code}' --max-time 3 \
        "$OPENBAO_TRANSIT_ADDR/v1/sys/health" 2>/dev/null | grep -qE '^(200|429|472|473|501)$'; then
      log "OK: Transit backend ready (attempt $i)"
      return 0
    fi
    sleep 2
  done
  log "FAIL: Transit backend $OPENBAO_TRANSIT_ADDR/v1/sys/health did not respond < 500 within $((attempts * 2))s — EC-3 gate FAIL; main OpenBao would CrashLoop on auto-unseal"
  return 1
}

if [[ "$AUTO_UNSEAL_MODE" == "transit" ]]; then
  wait_transit_ready || exit 1
fi

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
# T2.1 (issue #111): under AUTO_UNSEAL_MODE=transit the main OpenBao
# auto-unseals at pod startup against the Transit backend — no Shamir
# init, no shard file written to disk (blueprint D2). The recovery
# key emitted by `operator init -recovery-shares` is still useful for
# break-glass but operator-time, not Cleanroom-time. EC-7: Transit
# ephemeral; EC-8: no seal-migration — Cleanroom always fresh-init.
# AUTO_UNSEAL_MODE=awskms (staging and production, seal "awskms" in secrets.helmfile):
# the vault unseals itself with the cloud KMS key, so init issues recovery keys instead
# of unseal shares, and the init file keeps the root token and those recovery keys.
INIT_SHARES="-key-shares=5 -key-threshold=3"
[[ "$AUTO_UNSEAL_MODE" == "awskms" ]] && INIT_SHARES="-recovery-shares=5 -recovery-threshold=3"
if [[ "$AUTO_UNSEAL_MODE" != "transit" ]]; then
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
      # shellcheck disable=SC2086 # INIT_SHARES is two flags
      INIT_OUT=$(bao operator init $INIT_SHARES -format=json)
      echo "$INIT_OUT" > "$UNSEAL_FILE"
      chmod 0600 "$UNSEAL_FILE"
      log "Init complete. Shards + root_token saved to $UNSEAL_FILE (mode 0600)"
    fi
  else
    log "Skip: vault already initialized"
  fi
else
  log "Skip Step 2 (Shamir init) — AUTO_UNSEAL_MODE=transit; main OpenBao auto-unseals at pod startup (no shard file written — blueprint D2)"
fi

# === Step 3: unseal (idempotent — skip if already unsealed) ===
# CANONICAL FIX 2026-06-15 (same family as Step 2 above): pipefail + bao rc=2
# (sealed-but-init normal exit code) was triggering `|| echo "true"` (lowercase)
# that NEVER matched `"True"` (Python-capitalized) compare → unseal silently
# SKIPPED on every fresh-init invocation. Vault stayed sealed forever; operator
# had to manually `bao operator unseal $SHARD x3`. The sponsor's mandate
# ("no WORKAROUNDs") is explicit: capture stdout first, parse rc-independent,
# normalize to "True". Verbatim, as the vault-bootstrap-unseal-loop gate requires:
# Sponsor "sem WORKAROUNDs" # english-only: verbatim sponsor mandate the unseal-loop gate asserts
log "Step 3: bao operator unseal × 3 (idempotent)"
# T2.1: under AUTO_UNSEAL_MODE=transit, the Transit backend auto-unseals
# main OpenBao at startup — no manual unseal required (blueprint D2).
if [[ "$AUTO_UNSEAL_MODE" == "transit" || "$AUTO_UNSEAL_MODE" == "awskms" ]]; then
  log "Skip Step 3 (Shamir unseal): AUTO_UNSEAL_MODE=$AUTO_UNSEAL_MODE unseals at pod startup"
else
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
fi  # close AUTO_UNSEAL_MODE != transit branch

# === Step 4: root token Secret ===
log "Step 4: kubectl create secret openbao-init-token (idempotent)"
ROOT_TOKEN=$(python3 -c "import json; print(json.load(open('$UNSEAL_FILE'))['root_token'])" 2>/dev/null || true)
if [[ -n "$ROOT_TOKEN" ]]; then
  # openbao-init-token lives in the vault cluster (consumed by the
  # openbao-init helmfile sync at Step 5 — policy bootstrap).
  run kubectl --context "$OPENBAO_CONTEXT" -n "$NS" create secret generic openbao-init-token \
    --from-literal=BAO_TOKEN="$ROOT_TOKEN" \
    --dry-run=client -o yaml | kubectl --context "$OPENBAO_CONTEXT" -n "$NS" apply -f -
fi

# LIVE finding 2026-06-20 dev-public Cleanroom (T-A4 follow-up): the
# `bao_auth` function was historically defined later (after Step 5.5)
# alongside other auth-required steps. Step 5.5 therefore called an
# undefined function — `bao_auth auth list` produced no stdout, the
# Python check evaluated `'k8s-core/' in {}` → False every iteration,
# the 180s wait loop ALWAYS timed out, and `auth/k8s-core/config` was
# NEVER written. Downstream consequence: CSS loop-runtime-secrets-secrets stayed
# `Ready=False reason=InvalidProviderConfig` (1b-vault-bootstrap-eso
# FAIL), and ESO cross-cluster workload-identity path silently
# broke. Define bao_auth NOW (after ROOT_TOKEN is set, before Step 5.5
# needs it) so the R3 TokenReview canonical writes succeed on first run.
bao_auth() { kubectl --context "$OPENBAO_CONTEXT" -n "$NS" exec "$POD" -- env BAO_TOKEN="$ROOT_TOKEN" BAO_SKIP_VERIFY=true bao "$@"; }

# === Step 5: openbao-init helmfile sync ===
log "Step 5: helmfile sync openbao-init (T5.1-T5.9 provisioning)"
if [[ -f "infra/helmfile/secrets.helmfile.yaml.gotmpl" ]]; then
  ENV_NAME="${HELMFILE_ENV:-vault-local}"
  run helmfile -f infra/helmfile/secrets.helmfile.yaml.gotmpl -e "$ENV_NAME" \
    --kube-context "$OPENBAO_CONTEXT" --selector name=openbao-init sync || \
    log "WARN: openbao-init helmfile sync had issues — manual inspect recommended"
fi

# === Step 5.5: R3 — canonical TokenReview config for auth/k8s-core ===
# R3 of plan dev-public-cross-cluster-secrets (issue #111, sponsor approved
# 2026-06-19). The openbao-init Job (Step 5) provisions the `k8s-core`
# auth mount + the `eso` role bound to eso-vault-reader@loop-runtime-system.
# But the mount needs `auth/k8s-core/config` (kubernetes_host + CA +
# token_reviewer_jwt) for OpenBao to validate mgmt-issued SA tokens
# via the TokenReview API. The canonical operator runbook documented
# this as "operator-time" — codifying it here closes seed chain FAIL=11
# for the cross-cluster ESO read path (per
# docs/runbooks.md).
#
# Mount-precedence guard: bao MUST report `k8s-core/` enabled before
# `bao write auth/k8s-core/config` — else Vault returns 404
# `no handler for route auth/k8s-core/config`. Sponsor reinforced.
#
# Long-lived token contract: K8s ≥1.24 `kubectl create token` produces
# bounded TokenRequest JWTs that expire — useless for OpenBao's
# continuous TokenReview path. Use a Secret-based
# `kubernetes.io/service-account-token` instead — K8s populates
# `.data.token` with a JWT that never expires (legacy mechanism still
# supported). Sponsor reinforced.
#
# This step only runs when the TARGET_CONTEXT (mgmt) is different from
# OPENBAO_CONTEXT (vault) — single-cluster setups don't need
# cross-cluster TokenReview wiring.
log "Step 5.5: R3 — canonical TokenReview config for auth/k8s-core"
if [[ "$OPENBAO_CONTEXT" != "$TARGET_CONTEXT" && "$DRY_RUN" -eq 0 ]]; then
  # Wait for openbao-init Job to provision the k8s-core mount.
  log "  Waiting for auth/k8s-core/ mount (openbao-init Job creates it)"
  # LIVE-measured 2026-06-19 Cleanroom: the openbao-init Job takes
  # ~78s to complete (helmfile sync + Job init + bao auth enable across
  # 6 mounts + transit keys). The 60s budget skipped R3 mid-LIVE; bumped
  # to 180s + added 5s grace to absorb Cleanroom load variability.
  MOUNT_READY=0
  for i in $(seq 1 90); do  # 90 * 2s = 180s budget
    if bao_auth auth list -format=json 2>/dev/null | python3 -c \
        "import sys,json; d=json.loads(sys.stdin.read() or '{}'); sys.exit(0 if 'k8s-core/' in d else 1)" 2>/dev/null; then
      MOUNT_READY=1
      log "  OK: auth/k8s-core/ mount present (attempt $i)"
      break
    fi
    sleep 2
  done
  if [[ "$MOUNT_READY" -eq 0 ]]; then
    log "  WARN: auth/k8s-core/ mount not present after 180s — skipping R3 config write (seed chain FAIL=11 will surface)"
  else
    # MGMT side: ensure vault-tokenreviewer SA + CRB exist.
    log "  Applying vault-tokenreviewer SA + CRB on $TARGET_CONTEXT"
    kubectl --context "$TARGET_CONTEXT" apply -f - <<'TR_EOF' >/dev/null
apiVersion: v1
kind: ServiceAccount
metadata:
  name: vault-tokenreviewer
  namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: vault-tokenreviewer-auth-delegator
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:auth-delegator
subjects:
  - kind: ServiceAccount
    name: vault-tokenreviewer
    namespace: kube-system
---
apiVersion: v1
kind: Secret
metadata:
  name: vault-tokenreviewer-token
  namespace: kube-system
  annotations:
    kubernetes.io/service-account.name: vault-tokenreviewer
type: kubernetes.io/service-account-token
TR_EOF

    # Wait for K8s to populate the Secret-based token (legacy
    # non-expiring SA token mechanism; sponsor explicit — TokenRequest
    # is bounded). Up to 30s wait.
    TR_JWT=""
    for i in $(seq 1 15); do
      TR_JWT=$(kubectl --context "$TARGET_CONTEXT" -n kube-system get secret vault-tokenreviewer-token -o jsonpath='{.data.token}' 2>/dev/null | base64 -d 2>/dev/null || true)
      if [[ -n "$TR_JWT" ]]; then
        log "  OK: vault-tokenreviewer token populated (attempt $i)"
        break
      fi
      sleep 2
    done
    if [[ -z "$TR_JWT" ]]; then
      log "  WARN: vault-tokenreviewer token not populated after 30s — skipping R3"
    else
      # Extract MGMT API server URL + CA bundle.
      MGMT_HOST=$(kubectl --context "$TARGET_CONTEXT" config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null || true)
      MGMT_CA=$(kubectl --context "$TARGET_CONTEXT" -n default get cm kube-root-ca.crt -o jsonpath='{.data.ca\.crt}' 2>/dev/null || true)
      # CANONICAL FIX 2026-06-20 (revised, loop-runtime commit 7c255524) — rewrite the
      # host-scoped kubeconfig URL to a SAN-aligned name reachable from the OpenBao
      # pod's network namespace. Regression-locked by
      # infra/tests/charts/vault_bootstrap_san_aligned_host_test.go (F1 guard).
      # Two requirements MUST hold simultaneously:
      #   (a) reachability — from the vault pod (in loop-runtime-secrets cluster), the URL
      #       must resolve to + connect to the mgmt API server on the host.
      #   (b) TLS hostname/SAN — OpenBao's Go HTTP client verifies the server cert
      #       strictly. The k3d-mgmt API cert SAN list is:
      #         DNS: k3d-loop-runtime-core-server-0, k3d-loop-runtime-core-serverlb,
      #              kubernetes(.default(.svc(.cluster.local))), localhost
      #         IP : 0.0.0.0, 10.43.0.1, 127.0.0.1, <mgmt-server-0-IP>
      #       `host.k3d.internal` is NOT in the SAN list — using it triggers
      #       SAN-mismatch ("x509: certificate is valid for ... not host.k3d.internal")
      #       which OpenBao surfaces to callers as a generic "permission denied"
      #       from auth/k8s-core/login for every role (blanket 403).
      #
      # The canonical SAN-aligned + cross-cluster-reachable name is the k3d
      # serverlb docker container name `k3d-${cluster_name}-serverlb`, derived
      # from TARGET_CONTEXT (e.g., `k3d-loop-runtime-core` → `k3d-loop-runtime-core-serverlb`).
      # k3d injects this name into every cluster's CoreDNS NodeHosts via the
      # shared docker network, so it resolves canonically from loop-runtime-secrets.
      MGMT_HOST_ORIG="$MGMT_HOST"
      CORE_CLUSTER="${TARGET_CONTEXT#k3d-}"  # k3d-loop-runtime-core → loop-runtime-core
      SERVERLB_HOST="k3d-${CORE_CLUSTER}-serverlb"
      MGMT_HOST=$(echo "$MGMT_HOST" | sed -E "s|//(0\\.0\\.0\\.0\|127\\.0\\.0\\.1\|localhost\|host\\.k3d\\.internal)(:[0-9]+)?|//${SERVERLB_HOST}\\2|")
      if [[ "$MGMT_HOST" != "$MGMT_HOST_ORIG" ]]; then
        log "  Rewrote MGMT_HOST: $MGMT_HOST_ORIG → $MGMT_HOST (SAN-aligned, reachable from loop-runtime-secrets)"
      fi
      if [[ -z "$MGMT_HOST" || -z "$MGMT_CA" ]]; then
        log "  WARN: could not extract MGMT_HOST or MGMT_CA — skipping R3"
      else
        # Write auth/k8s-core/config inside the OpenBao pod (avoids
        # CA distribution overhead — pod has bao CLI + connectivity).
        log "  Writing auth/k8s-core/config with MGMT_HOST=$MGMT_HOST"
        kubectl --context "$OPENBAO_CONTEXT" -n "$NS" exec -i "$POD" -- \
          env BAO_TOKEN="$ROOT_TOKEN" BAO_SKIP_VERIFY=true \
          bao write auth/k8s-core/config \
            kubernetes_host="$MGMT_HOST" \
            kubernetes_ca_cert="$MGMT_CA" \
            token_reviewer_jwt="$TR_JWT" \
            disable_iss_validation=true 2>&1 | tail -5 || \
          log "  WARN: bao write auth/k8s-core/config returned non-zero"
        log "  R3 codification complete"
      fi
    fi
  fi
else
  log "  Skip R3: OPENBAO_CONTEXT == TARGET_CONTEXT (single-cluster topology) OR DRY_RUN"
fi

# === Step 6+7: approle (the policy belongs to the chart) ===
log "Step 6+7: approle loop-runtime-api (policy comes from the openbao-init chart)"
export BAO_TOKEN="$ROOT_TOKEN"
# `bao_auth` is now defined right after ROOT_TOKEN is set (Step 4) so
# Step 5.5's auth/k8s-core wait-loop can actually call it. The legacy
# duplicate definition here was the source of the silent R3 skip
# (issue #111 cross-cluster ESO path).

bao_auth auth enable approle 2>/dev/null || log "Skip: approle already enabled"

# The policy is NOT written here. It is declared in the infra/helm/openbao-init chart
# and applied by helmfile in step 5, just above.
#
# Until 2026-08-13 this block ran `bao policy write loop-runtime-api` with
# transit/keys/* including delete and list — minutes AFTER the chart applied a
# policy of the same name. Last writer wins, so the versioned version, which is the
# one that goes through review, never reached any runtime: the B-072 fix
# became invisible. And the set here granted `delete` on transit keys, which is the
# crypto-shredding verb, to an identity that (B-075) has no shredding path
# at all.
#
# Measured before removing it: the TransitClient builds exactly three paths, all
# under loop-runtime-tenants-<id> (encrypt, keys, decrypt), plus the AppRole login.
# No LIST, no DELETE, no KV. The chart's set covers all three.
#
# What stays here is the CREDENTIAL lifecycle — create the AppRole, generate the
# secret_id, materialize the Secret — because that only runs against a live vault and
# cannot be declared in a template.
#
# Decision: knowledge-base/decisions/MEET-ASYNC-AMENDMENT-2026-08-13-vault-policy-ownership.md
if [[ "$DRY_RUN" -eq 0 ]]; then
  bao_auth write auth/approle/role/loop-runtime-api \
    token_policies=loop-runtime-api \
    token_ttl=24h \
    token_max_ttl=72h || true
fi

# === Step 8+9: extract role_id + secret_id ===
log "Step 8+9: capture role_id + secret_id"
if [[ "$DRY_RUN" -eq 0 ]]; then
  # The role_id is NOT captured here: what syncs it is step 7b of
  # seed-post-cleanroom-all.sh (`loop-runtime-ops seed vault-role-id-sync`), which runs
  # AFTER this script and writes the value where loop-runtime-api reads it. The capture that
  # used to exist here was assigned and discarded — it gave the impression that this
  # script owned the artifact when it does not.
  SECRET_ID=$(bao_auth write -f -format=json auth/approle/role/loop-runtime-api/secret-id 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['secret_id'])" 2>/dev/null || true)
fi

# === Step 10: loop-runtime-openbao-approle + openbao-spki-pin Secrets ===
# T1.1 split-context: writes target the MGMT cluster (TARGET_CONTEXT)
# where loop-runtime-api Deployment envFrom reads from. EC-4 absorption:
# ensure target ns exists BEFORE the Secret apply — fail-fast if
# the mgmt-side ESO target namespace was never provisioned.
log "Step 10: loop-runtime-openbao-approle Secret in $TARGET_CONTEXT/$TARGET_NS + openbao-spki-pin"
if [[ "$DRY_RUN" -eq 0 && -n "${SECRET_ID:-}" ]]; then
  # EC-4: idempotent namespace ensure on TARGET_CONTEXT side BEFORE Secret apply.
  kubectl --context "$TARGET_CONTEXT" create namespace "$TARGET_NS" --dry-run=client -o yaml | \
    kubectl --context "$TARGET_CONTEXT" apply -f - >/dev/null
  kubectl --context "$TARGET_CONTEXT" -n "$TARGET_NS" create secret generic loop-runtime-openbao-approle \
    --from-literal=OPENBAO_VAULT_SECRET_ID="$SECRET_ID" \
    --dry-run=client -o yaml | kubectl --context "$TARGET_CONTEXT" -n "$TARGET_NS" apply -f - || \
    log "WARN: loop-runtime-openbao-approle creation failed in $TARGET_CONTEXT/$TARGET_NS"
fi

# Extract SPKI pin from openbao-tls cert (catch #7 — dynamic extraction)
log "Step 10b: extract SPKI pin from openbao-tls cert"
if [[ "$DRY_RUN" -eq 0 ]]; then
  # Read openbao-tls from the VAULT cluster (where cert-manager issued
  # the Secret), then write the SPKI pin to the MGMT cluster (where
  # loop-runtime-api chart envFrom reads from).
  TLS_CRT=$(kubectl --context "$OPENBAO_CONTEXT" -n "$NS" get secret openbao-tls -o jsonpath='{.data.tls\.crt}' 2>/dev/null | base64 -d 2>/dev/null || true)
  if [[ -n "$TLS_CRT" ]]; then
    # CANONICAL FIX 2026-06-15: pkg/vault/transit_client.go:199 expects
    # HEX-encoded SHA-256 (via hex.DecodeString). Previous base64url
    # encoding (`base64 | tr ...`) caused loop-runtime-api to crash on startup
    # with "openbao transit client: vault: SPKIPinSHA256 hex decode:
    # encoding/hex: invalid byte: U+0073 's'". `openssl dgst -sha256`
    # with no `-binary` flag emits "SHA256(stdin)= <hex>" so we
    # post-process to strip the prefix; equivalently use `-binary | xxd`.
    SPKI=$(echo "$TLS_CRT" | openssl x509 -pubkey -noout 2>/dev/null | openssl pkey -pubin -outform DER 2>/dev/null | openssl dgst -sha256 -binary 2>/dev/null | xxd -p -c 999 | tr -d '\n' || true)
    if [[ -n "$SPKI" ]]; then
      kubectl --context "$TARGET_CONTEXT" -n loop-runtime-system create secret generic openbao-spki-pin \
        --from-literal=OPENBAO_SPKI_PIN_SHA256="$SPKI" \
        --dry-run=client -o yaml | kubectl --context "$TARGET_CONTEXT" -n loop-runtime-system apply -f -
      log "OK: openbao-spki-pin Secret created with extracted SPKI"
    fi
  fi
fi

log ""
log "════════════════════════════════════════════════════════"
log "  vault-bootstrap-dev COMPLETE"
log "  Next: trigger ESO refresh or loop-runtime-api restart to pick up Secret"
log "════════════════════════════════════════════════════════"
