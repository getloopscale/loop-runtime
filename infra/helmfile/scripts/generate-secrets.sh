#!/usr/bin/env bash
# CHG-2026-04-28-LAYERED-BOOTSTRAP — T2.1 / T2.4
# Generates Secrets required by mgmt cluster releases.
#
# IDEMPOTENT (iter 14): on re-run, READS existing Secret values and reuses
# them. Only generates new randoms on FIRST install. Resolves the recurring
# pain where every `helmfile sync` regenerated passwords and broke auth
# (B1 cosign placeholder, B4 token drift, generic password rotation churn).
#
# Usage: generate-secrets.sh <env> <cluster>
#        generate-secrets.sh --backup-only <file-prefix> <namespace/name>...
#          Only writes a restorable backup of the named Secrets (the context comes
#          from KUBECTL_CONTEXT). The build and runtime generators use this.
#   env:     dev | prod | standalone
#   cluster: core | runtime
#
# Optional env vars (override existing — used for explicit rotation):
#   LOOP_RUNTIME_DB_HOST, LOOP_RUNTIME_DB_PORT, LOOP_RUNTIME_DB_NAME, LOOP_RUNTIME_DB_USER, LOOP_RUNTIME_DB_PASSWORD
#   LOOP_RUNTIME_REGISTRY_ENDPOINT, LOOP_RUNTIME_BUCKET_NAME, LOOP_RUNTIME_BUCKET_ENDPOINT
#   LOOP_RUNTIME_SPACES_KEY, LOOP_RUNTIME_SPACES_SECRET
#   LOOP_RUNTIME_GITEA_ADMIN_PASSWORD, LOOP_RUNTIME_GITEA_TOKEN
#
# Backup: every successful run dumps loop-runtime-platform-secrets to
#   ~/.loop-runtime/backups/secrets-mgmt-{env}-{timestamp}.yaml (chmod 600)
# Restore: kubectl apply -f <backup-file>
set -euo pipefail

BACKUP_ONLY=false
if [[ "${1:-}" == "--backup-only" ]]; then
  BACKUP_ONLY=true
  shift
  [[ $# -ge 2 ]] || { echo "usage: $0 --backup-only <file-prefix> <namespace/name>..." >&2; exit 1; }
else
  ENV="${1:?usage: $0 <env> <cluster>}"
  CLUSTER="${2:?usage: $0 <env> <cluster>}"
fi

# Canonical 2026-06-20 (issue #115): pin kubectl context to the mgmt cluster.
# This script applies Secrets to mgmt — without the pin, an active shell
# context residue (e.g. `k3d-loop-runtime-secrets` from a prior T0.1 / vault-bootstrap
# session) silently misprovisions Secrets to the wrong cluster, and
# `task setup` fails at `infra:install:core` with the secrets landed in
# loop-runtime-secrets. Override via `KUBECTL_CONTEXT` for non-k3d targets (DOKS prod,
# drill cluster). The `kubectl()` function override below injects
# `--context="$KUBECTL_CONTEXT"` on every call — all bare `kubectl` calls
# downstream inherit the canonical context by construction. Regression-locked
# by infra/tests/charts/generate_secrets_context_pin_test.go.
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-k3d-loop-runtime-core}"
kubectl() { command kubectl --context="$KUBECTL_CONTEXT" "$@"; }

log() { printf '[generate-secrets] %s\n' "$*" >&2; }
random_password() { openssl rand -base64 32 | tr -d '/+=' | head -c 32; }
emit_token() { printf '%s' "$1"; }   # B4 — no trailing newline

# get_or_generate: read existing Secret value; if absent or empty, generate
# a new random. This is the IDEMPOTENCY FIX (iter 14).
#
# Args: <secret-name> <namespace> <data-key>
# Stdout: the value (existing or freshly generated)
get_or_generate() {
  local name="$1" ns="$2" key="$3"
  # `kubectl get secret -o jsonpath` returns the data field base64-encoded.
  # Empty string if Secret or key doesn't exist.
  local raw existing
  raw=$(kubectl get secret "$name" -n "$ns" -o "jsonpath={.data.${key//./\\.}}" 2>/dev/null || true)
  if [[ -n "$raw" ]]; then
    existing=$(printf '%s' "$raw" | base64 -d 2>/dev/null || true)
    if [[ -n "$existing" && "$existing" != *PLACEHOLDER* ]]; then
      printf '%s' "$existing"
      return 0
    fi
  fi
  random_password
}

# existing_value: the current value of an externally-sourced key, or nothing. Unlike
# get_or_generate it never invents one: a random provider key would look authentic and
# never authenticate.
existing_value() {
  local name="$1" ns="$2" key="$3" raw
  raw=$(kubectl get secret "$name" -n "$ns" -o "jsonpath={.data.${key//./\\.}}" 2>/dev/null || true)
  if [[ -n "$raw" ]]; then
    printf '%s' "$raw" | base64 -d 2>/dev/null || true
  fi
}

ensure_namespace() {
  local ns="$1"
  kubectl create namespace "$ns" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
}

apply_secret() {
  local name="$1" ns="$2"
  shift 2
  local args=("$@")
  ensure_namespace "$ns"
  kubectl create secret generic "$name" -n "$ns" "${args[@]}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  log "secret $ns/$name applied (idempotent)"
}

# backup_secrets <file-prefix> <namespace/name>...
#
# The one restorable Secret backup of the bootstrap scripts. generate-build-secrets.sh
# and generate-runtime-secrets.sh call it through `--backup-only` instead of keeping
# their own copies — theirs dumped raw `kubectl get -o yaml`, which carries
# resourceVersion/uid/managedFields and fails to re-apply ("object has been modified").
#
# Writes ~/.loop-runtime/backups/<file-prefix>-<timestamp>.yaml (chmod 600) and keeps the ten
# most recent per prefix.
backup_secrets() {
  local prefix="$1"
  shift
  local backup_dir="${HOME}/.loop-runtime/backups"
  mkdir -p "$backup_dir"
  chmod 700 "$backup_dir"
  local ts
  ts=$(date +%Y%m%d-%H%M%S)
  local out="${backup_dir}/${prefix}-${ts}.yaml"
  local raw item
  raw=$(mktemp)
  for item in "$@"; do
    # A Secret not created yet (first install) is simply absent from the backup.
    kubectl get secret -n "${item%%/*}" "${item#*/}" -o yaml 2>/dev/null >>"$raw" && echo '---' >>"$raw" || true
  done
  # CRITICAL (iter 17 fix): strip server-managed fields (resourceVersion, uid,
  # creationTimestamp, managedFields, ownerReferences) so `kubectl apply -f <file>`
  # works on a different cluster OR after the original Secret was modified.
  # Without stripping, restore fails with: "Conflict: object has been modified".
  python3 -c "
import sys, yaml
out = []
for d in yaml.safe_load_all(sys.stdin):
    # Only a mapping can be a Secret; anything else kubectl printed is not one.
    if not isinstance(d, dict):
        continue
    items = d.get('items', [d]) if d.get('kind') == 'List' else [d]
    for item in items:
        if not isinstance(item, dict):
            continue
        meta = item.get('metadata', {})
        for f in ('resourceVersion', 'uid', 'creationTimestamp',
                  'managedFields', 'ownerReferences', 'selfLink', 'generation'):
            meta.pop(f, None)
        item.pop('status', None)
        out.append(item)
if out:
    print(yaml.safe_dump_all(out, default_flow_style=False))
" <"$raw" >"$out"
  rm -f "$raw"
  if [[ -s "$out" ]]; then
    chmod 600 "$out"
    log "backup written: $out (cleaned of server-managed fields — restorable)"
    ls -t "${backup_dir}/${prefix}-"*.yaml 2>/dev/null | tail -n +11 | xargs -r rm -f
  else
    rm -f "$out"
    log "backup skipped (no Secrets present yet — first install)"
  fi
}

if [[ "$BACKUP_ONLY" == "true" ]]; then
  backup_secrets "$@"
  exit 0
fi

# The central cluster has been called `core` since the 2026-08-09 rename (ADR
# docs/decisions.md). This used to log "noop" and exit 0 for any other name, and
# two Taskfile callers still passing `mgmt` then installed the platform with no
# database Secret — which only a cold install noticed (#397). A name this script
# does not handle is the caller's mistake, so it is refused.
if [[ "$CLUSTER" != "core" ]]; then
  log "ERROR: cluster=$CLUSTER is not handled — this script provisions the core cluster only; pass 'core' (the pre-rename name was 'mgmt')"
  exit 1
fi

log "generating mgmt secrets for env=$ENV (idempotent mode)"

# 1) Database credentials (consumed by loop-runtime-api, gitea, ory-kratos, ory-hydra).
# DB password comes from Pulumi L1 — env var is canonical source. If absent
# (e.g. standalone install without --provider), fall back to in-cluster
# Postgres release (B33 fix) so loop-runtime-api receives a valid DATABASE_URL.
#
# B33 (CHG-2026-04-28-LAYERED-BOOTSTRAP) — Without this fallback, the
# `loop-runtime-db-credentials` secret was never created in standalone profile,
# causing loop-runtime-api to start with `DATABASE_URL not set`, fail readiness
# probes, and crashloop. Detected on fresh standalone bootstrap 2026-04-28.
if [[ -z "${LOOP_RUNTIME_DB_HOST:-}" ]]; then
  LOOP_RUNTIME_DB_HOST="postgresql.loop-runtime-system.svc.cluster.local"
  LOOP_RUNTIME_DB_PORT="5432"
  LOOP_RUNTIME_DB_USER="${LOOP_RUNTIME_DB_USER:-loop_runtime}"
  LOOP_RUNTIME_DB_NAME="${LOOP_RUNTIME_DB_NAME:-loop_runtime}"
  # STANDALONE (local k3d) DB password is the DETERMINISTIC well-known dev
  # password `loop-runtime-dev-password`. This is NOT a hardcoded production credential
  # (T1.3 correctly removed those from the CHARTS, which default "" so prod
  # fails fast). It is the single local-dev contract that the ENTIRE local
  # toolchain depends on: api/internal/config/config.go, Tiltfile (db-migrate),
  # Taskfile, infra/tests/seed/seed.go, parity tests, etc. (58 refs). Making it
  # random broke all of them (e.g. Tiltfile db-migrate SASL auth failure).
  # Env override (LOOP_RUNTIME_DB_PASSWORD) still wins for cloud/drill. Root cause:
  # finding-2026-05-22-pg-password-divergence § local-dev-password.
  # T6.5 of plan 2026-06-02-openbao-adoption — standalone local k3d DB creds are
  # sourced from the Vault database engine (database/creds/loop-runtime-api) when
  # available, falling back to the deterministic dev password BELOW ONLY when
  # Vault is not reachable (cold-bootstrap before openbao-init runs). Phased
  # cutover: when BAO_ADDR + BAO_TOKEN are set and the database engine + role
  # `loop-runtime-api` exist, mint a fresh 1h-TTL credential; otherwise the cold-bootstrap
  # fallback keeps the chicken-and-egg working. Long-term (T10.3, post T+60d) the
  # fallback is removed and Vault becomes the only path. Restored 2026-06-17 after
  # #69 (a cloud-CI commit) accidentally dropped it — caught by the
  # cold-bootstrap fallback test (infra/tests/secrets/generate_secrets_vault_fallback_test.go).
  # The Vault branch runs
  # BEFORE the legacy fallback so `${LOOP_RUNTIME_DB_PASSWORD:-loop-runtime-dev-password}` only
  # applies when Vault did NOT set it.
  if [[ -n "${BAO_ADDR:-}" ]] && [[ -n "${BAO_TOKEN:-}" ]] && command -v bao >/dev/null 2>&1; then
    VAULT_DB_RESP="$(BAO_ADDR="${BAO_ADDR}" BAO_TOKEN="${BAO_TOKEN}" bao read -format=json database/creds/loop-runtime-api 2>/dev/null || true)"
    if [[ -n "${VAULT_DB_RESP}" ]]; then
      VAULT_DB_USER="$(echo "${VAULT_DB_RESP}" | jq -r '.data.username // empty')"
      VAULT_DB_PASS="$(echo "${VAULT_DB_RESP}" | jq -r '.data.password // empty')"
      if [[ -n "${VAULT_DB_USER}" ]] && [[ -n "${VAULT_DB_PASS}" ]]; then
        LOOP_RUNTIME_DB_USER="${VAULT_DB_USER}"
        LOOP_RUNTIME_DB_PASSWORD="${VAULT_DB_PASS}"
        log "T6.5: standalone DB creds from Vault engine — user=${VAULT_DB_USER} (TTL 1h)"
      fi
    fi
  fi
  LOOP_RUNTIME_DB_PASSWORD="${LOOP_RUNTIME_DB_PASSWORD:-loop-runtime-dev-password}"
  DB_SSLMODE="disable"
  log "B33: standalone fallback — DB at $LOOP_RUNTIME_DB_HOST (sslmode=disable)"
else
  DB_SSLMODE="require"
fi
DB_PASSWORD="${LOOP_RUNTIME_DB_PASSWORD:-$(get_or_generate loop-runtime-db-credentials loop-runtime-system password)}"
DB_URI="${LOOP_RUNTIME_DB_URI:-postgresql://${LOOP_RUNTIME_DB_USER:-loop_runtime}:${DB_PASSWORD}@${LOOP_RUNTIME_DB_HOST}:${LOOP_RUNTIME_DB_PORT:-5432}/${LOOP_RUNTIME_DB_NAME:-loop_runtime}?sslmode=${DB_SSLMODE}}"
# Superuser (postgres) password — required so the in-cluster Bitnami PostgreSQL
# (standalone profile) can consume this secret via auth.existingSecret. Bitnami
# needs BOTH the admin key (postgres-password) and the custom-user key (password).
# Without it the chart auto-generates a random password for the `loop-runtime` user that
# diverges from what kratos/hydra/loop-runtime-api read here — kratos then fails SASL auth
# at config-load and blocks `task setup` on the helmfile --wait. Root cause:
# finding-2026-05-22-pg-password-divergence. Cloud (--provider) uses managed PG and
# does NOT install the in-cluster chart, so this key is inert there.
DB_ADMIN_PASSWORD="${LOOP_RUNTIME_DB_ADMIN_PASSWORD:-$(get_or_generate loop-runtime-db-credentials loop-runtime-system postgres-password)}"
# #396 — loop-runtime-api refuses to serve requests over a pool that can bypass RLS
# (db.AssertRequestPoolIsolated), so it connects as the non-owner `loop_runtime_api` role
# and runs its cross-tenant loops as `loop_runtime_worker`. loop-console refuses to serve as a
# role holding an unnamed cross-tenant privilege, so it connects as `loop_console`, the
# non-owner role migration 000063 creates (#438). `uri` stays the OWNER's URL: only
# migrations run as the owner.
#   in-cluster Postgres: both roles are created by `task db:roles` with these
#     passwords — the same deterministic dev default as the owner's, for the same
#     reason (the local toolchain and the Tilt values read it), overridable per role.
#   managed Postgres: the provider creates loop_runtime_api and hands its credentials in
#     LOOP_RUNTIME_DB_USER, so the request URL is DB_URI itself; the worker URL comes from
#     LOOP_RUNTIME_WORKER_DB_URI when the environment provides one.
if [[ "$LOOP_RUNTIME_DB_HOST" == "postgresql.loop-runtime-system.svc.cluster.local" ]]; then
  DB_API_PASSWORD="${LOOP_RUNTIME_DB_API_PASSWORD:-loop-runtime-dev-password}"
  DB_WORKER_PASSWORD="${LOOP_RUNTIME_DB_WORKER_PASSWORD:-loop-runtime-dev-password}"
  DB_CLOUD_PASSWORD="${LOOP_RUNTIME_DB_CLOUD_PASSWORD:-loop-runtime-dev-password}"
  DB_API_URI="postgresql://loop_runtime_api:${DB_API_PASSWORD}@${LOOP_RUNTIME_DB_HOST}:${LOOP_RUNTIME_DB_PORT:-5432}/${LOOP_RUNTIME_DB_NAME:-loop_runtime}?sslmode=${DB_SSLMODE}"
  DB_WORKER_URI="postgresql://loop_runtime_worker:${DB_WORKER_PASSWORD}@${LOOP_RUNTIME_DB_HOST}:${LOOP_RUNTIME_DB_PORT:-5432}/${LOOP_RUNTIME_DB_NAME:-loop_runtime}?sslmode=${DB_SSLMODE}"
  DB_CLOUD_URI="postgresql://loop_console:${DB_CLOUD_PASSWORD}@${LOOP_RUNTIME_DB_HOST}:${LOOP_RUNTIME_DB_PORT:-5432}/${LOOP_RUNTIME_DB_NAME:-loop_runtime}?sslmode=${DB_SSLMODE}"
  DB_ROLE_LITERALS=(
    --from-literal=api-password="${DB_API_PASSWORD}"
    --from-literal=worker-password="${DB_WORKER_PASSWORD}"
    --from-literal=api-uri="${DB_API_URI}"
    --from-literal=worker-uri="${DB_WORKER_URI}"
    --from-literal=cloud-password="${DB_CLOUD_PASSWORD}"
    --from-literal=cloud-uri="${DB_CLOUD_URI}"
  )
else
  DB_ROLE_LITERALS=(--from-literal=api-uri="${DB_URI}")
  if [[ -n "${LOOP_RUNTIME_WORKER_DB_URI:-}" ]]; then
    DB_ROLE_LITERALS+=(--from-literal=worker-uri="${LOOP_RUNTIME_WORKER_DB_URI}")
  fi
  if [[ -n "${LOOP_CONSOLE_DB_URI:-}" ]]; then
    DB_ROLE_LITERALS+=(--from-literal=cloud-uri="${LOOP_CONSOLE_DB_URI}")
  fi
fi
apply_secret loop-runtime-db-credentials loop-runtime-system \
  --from-literal=host="${LOOP_RUNTIME_DB_HOST}" \
  --from-literal=port="${LOOP_RUNTIME_DB_PORT:-5432}" \
  --from-literal=database="${LOOP_RUNTIME_DB_NAME:-loop_runtime}" \
  --from-literal=user="${LOOP_RUNTIME_DB_USER:-loop_runtime}" \
  --from-literal=password="${DB_PASSWORD}" \
  --from-literal=postgres-password="${DB_ADMIN_PASSWORD}" \
  --from-literal=uri="${DB_URI}" \
  "${DB_ROLE_LITERALS[@]}"

# 1.b) Service-auth shared secret (loop-runtime-api ↔ loop-console X-Service-Auth header).
# T1.1 of plan 2026-05-29-dashboard-public-integration-canonicalization.
# Single Secret consumed via envFrom by BOTH deployments → no drift possible.
# v1.1 EC-04: presync uses kubectl create | apply pattern (idempotent).
# v1.1 EC-S1: random_password emits 32 base64-safe ASCII chars (no newline,
# no quote, no comma) — sanity-check enforces.
SERVICE_AUTH_SECRET="${LOOP_RUNTIME_SERVICE_AUTH_SECRET:-$(get_or_generate loop-runtime-service-auth loop-runtime-system LOOP_RUNTIME_SERVICE_AUTH_SECRET)}"
# v1.1 EC-S1 sanity check: emitted value MUST be a printable, single-line
# token; otherwise downstream envFrom semantics could leak literal newlines
# into env vars (which would break HTTP header construction in cloud/cmd/main.go).
if ! printf '%s' "$SERVICE_AUTH_SECRET" | grep -Eq '^[A-Za-z0-9_/+=-]{32,}$'; then
  log "ERROR: generated LOOP_RUNTIME_SERVICE_AUTH_SECRET fails base64-safe ASCII sanity check"
  log "  value=${SERVICE_AUTH_SECRET:0:8}... (length=${#SERVICE_AUTH_SECRET})"
  exit 1
fi
apply_secret loop-runtime-service-auth loop-runtime-system \
  --from-literal=LOOP_RUNTIME_SERVICE_AUTH_SECRET="${SERVICE_AUTH_SECRET}"

# 1.b') Bearer the public edge presents to /internal/edge/route (host -> cell
# lookup, docs/decisions.md 2026-09-29). loop-runtime-api reads it through the optional
# `loop-runtime-edge-route-token` envFrom source; the edge reads the same Secret when it
# is (re)configured. Preserved across runs so a reinstall does not lock the edge out.
EDGE_ROUTE_TOKEN="$(get_or_generate loop-runtime-edge-route-token loop-runtime-system LOOP_RUNTIME_EDGE_ROUTE_TOKEN)"
apply_secret loop-runtime-edge-route-token loop-runtime-system \
  --from-literal=LOOP_RUNTIME_EDGE_ROUTE_TOKEN="${EDGE_ROUTE_TOKEN}"

# 1.b'') The secret /internal/bootstrap requires. loop-runtime-api reads loop-runtime-api-internal as an
# optional envFrom source; without it the binary falls back to its development default and
# the endpoint is guarded only by the network (#453). Preserved across runs, like the rest.
BOOTSTRAP_SECRET="${LOOP_RUNTIME_BOOTSTRAP_SECRET:-$(get_or_generate loop-runtime-api-internal loop-runtime-system LOOP_RUNTIME_BOOTSTRAP_SECRET)}"
apply_secret loop-runtime-api-internal loop-runtime-system \
  --from-literal=LOOP_RUNTIME_BOOTSTRAP_SECRET="${BOOTSTRAP_SECRET}"

# 1.e) The data plane (#446): dev-public runs memory, RAG and lens from Helmfile in the
# loop-runtime-data-plane namespace, and each release reads one Secret named after it. The database
# password, and the passwords of the roles its app and worker connect as (postgres.roles in
# infra/helm/data-plane-service), are generated and preserved across runs, like the rest; the model provider key and
# the admin bootstrap token are the operator's (`loop-runtime-ops creds env`), with the bootstrap
# token generated when none is given. Other environments do not run the data plane here.
if [[ "$ENV" == "dev-public" ]]; then
  for svc in loop-runtime-memory loop-runtime-rag loop-runtime-lens; do
    OPENAI_KEY="${LOOP_RUNTIME_OPENAI_API_KEY:-$(existing_value "$svc" loop-runtime-data-plane openai-api-key)}"
    if [[ -z "$OPENAI_KEY" ]]; then
      OPENAI_KEY="sk-NOT_SET"
      log "WARN: LOOP_RUNTIME_OPENAI_API_KEY unset and no existing $svc Secret. $svc cannot embed or judge until it is set."
    fi
    apply_secret "$svc" loop-runtime-data-plane \
      --from-literal=postgres-password="$(get_or_generate "$svc" loop-runtime-data-plane postgres-password)" \
      --from-literal=app-role-password="$(get_or_generate "$svc" loop-runtime-data-plane app-role-password)" \
      --from-literal=worker-role-password="$(get_or_generate "$svc" loop-runtime-data-plane worker-role-password)" \
      --from-literal=admin-bootstrap-token="${LOOP_RUNTIME_DATA_PLANE_ADMIN_BOOTSTRAP_TOKEN:-$(get_or_generate "$svc" loop-runtime-data-plane admin-bootstrap-token)}" \
      --from-literal=openai-api-key="$OPENAI_KEY"
  done
fi

# 1.c) Resend HTTP-API key for Kratos courier (MEET-2026-04-06-036: DO blocks
# SMTP, courier delivery_strategy=http via api.resend.com/emails). Consumed
# by ory-kratos Deployment + ory-kratos-courier StatefulSet via
# deployment.environmentSecretsName + statefulSet.environmentSecretsName
# (infra/helm/ory-kratos/values.yaml.gotmpl).
#
# Provisioning is externally-sourced (NOT randomly generated — must be a
# real Resend key starting with "re_"). Resolution order:
#   1. LOOP_RUNTIME_RESEND_API_KEY env (operator-provided / Pulumi config)
#   2. Existing Secret value (idempotent re-run)
#   3. Loud "re_NOT_SET" sentinel — first email attempt 401s from Resend
#      and logs in courier; better than a silent random value that would
#      always look authentic but never authenticate.
RESEND_KEY="${LOOP_RUNTIME_RESEND_API_KEY:-}"
if [[ -z "${RESEND_KEY}" ]]; then
  RESEND_EXISTING=$(kubectl get secret ory-kratos-resend -n loop-runtime-auth \
    -o "jsonpath={.data.RESEND_API_KEY}" 2>/dev/null || true)
  if [[ -n "${RESEND_EXISTING}" ]]; then
    RESEND_KEY=$(printf '%s' "${RESEND_EXISTING}" | base64 -d 2>/dev/null || true)
  fi
fi
if [[ -z "${RESEND_KEY}" ]]; then
  RESEND_KEY="re_NOT_SET"
  log "WARN: LOOP_RUNTIME_RESEND_API_KEY unset and no existing ory-kratos-resend Secret. Outbound emails will 401."
fi
apply_secret ory-kratos-resend loop-runtime-auth \
  --from-literal=RESEND_API_KEY="${RESEND_KEY}"

# 2) Gitea admin password + token — IDEMPOTENT (iter 14 fix).
GITEA_PWD="${LOOP_RUNTIME_GITEA_ADMIN_PASSWORD:-$(get_or_generate gitea-admin loop-runtime-system password)}"
GITEA_TOKEN="${LOOP_RUNTIME_GITEA_TOKEN:-$(get_or_generate gitea-admin loop-runtime-system token)}"

# B4 — printf '%s' prevents trailing newline that broke L3 #1.
GITEA_PWD_FILE="$(mktemp)"
GITEA_TOKEN_FILE="$(mktemp)"
trap 'rm -f "$GITEA_PWD_FILE" "$GITEA_TOKEN_FILE"' EXIT
emit_token "$GITEA_PWD" > "$GITEA_PWD_FILE"
emit_token "$GITEA_TOKEN" > "$GITEA_TOKEN_FILE"

apply_secret gitea-admin loop-runtime-system \
  --from-literal=username="${LOOP_RUNTIME_GITEA_ADMIN_USER:-gitea_admin}" \
  --from-file=password="$GITEA_PWD_FILE" \
  --from-file=token="$GITEA_TOKEN_FILE"

# B80 (end-to-end-deploy-validation-plan iter 12 / 2026-04-29) — ArgoCD reads
# tenant gitops repos from Gitea over HTTP basic auth. The argocd-application
# -controller resolves repo credentials by scanning Secrets in the argocd
# namespace labelled `argocd.argoproj.io/secret-type=repo-creds` whose `url`
# field is a *prefix* of the App's repoURL. Without this credential template
# every Application stays sync=Unknown with "authentication required" in
# operationState.message.
ensure_namespace argocd
cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: v1
kind: Secret
metadata:
  name: gitea-credentials-template
  namespace: argocd
  labels:
    argocd.argoproj.io/secret-type: repo-creds
    runtime.getloopscale.com/managed-by: loop-runtime-platform
type: Opaque
stringData:
  type: git
  url: http://gitea-http.loop-runtime-system.svc.cluster.local:3000/
  username: loop_runtime
  password: ${GITEA_TOKEN}
EOF

# F-12 (CHG-2026-04-29-DOGFOOD T5.4) — loop-runtime-api `/api/v1/health/deep` probe
# of ArgoCD requires a valid JWT. Pre-fix: ARGOCD_TOKEN was never set in
# standalone-multi → the API answers `{"argocd":"auth_failed","status":"degraded"}`
# HTTP 503 even with a healthy ArgoCD. Idempotent fix: admin login via REST,
# capture the JWT, persist it in Secret `loop-runtime-api-tokens` (loop-runtime-system) under the key
# `argocd-token` — exactly the key expected by the Tilt manifest and by the
# helmfile loop-runtime-api Deployment template.
#
# Login pattern: argocd-server exposes plain HTTP on port 80 (server.insecure:
# true via Helm overlay). Default JWT token validity ~24h — refresh is
# the responsibility of iter 2 (out of scope for this fix). Fails silently
# if ArgoCD is not Ready (e.g. bootstrap mid-flight): loop-runtime-api goes back to
# reporting degraded until the next sync, acceptable behaviour (does not block
# the install).
if kubectl get namespace argocd >/dev/null 2>&1 && \
   kubectl get secret -n argocd argocd-initial-admin-secret >/dev/null 2>&1; then
  ADMIN_PW=$(kubectl get secret -n argocd argocd-initial-admin-secret -o jsonpath='{.data.password}' 2>/dev/null | base64 -d)
  ARGOCD_HOST="argocd-server.argocd.svc.cluster.local"
  if [[ -n "$ADMIN_PW" ]]; then
    # Run login via short-lived debug Pod (kubectl run + curl). Idempotent:
    # the same password produces new tokens on every call — preserves the existing
    # Secret if login fails (keeps the previous value instead of zeroing it).
    ARGOCD_TOKEN=$(kubectl run argocd-token-bootstrap-$$ \
      --rm -i --restart=Never --image=curlimages/curl:8.5.0 \
      --command -- curl -s --max-time 5 \
        -X POST -H "Content-Type: application/json" \
        -d "{\"username\":\"admin\",\"password\":\"$ADMIN_PW\"}" \
        "http://${ARGOCD_HOST}/api/v1/session" 2>/dev/null \
      | python3 -c 'import sys,json; print(json.loads(sys.stdin.read() or "{}").get("token",""))' 2>/dev/null || true)
    if [[ -z "$ARGOCD_TOKEN" ]]; then
      ARGOCD_TOKEN=$(get_or_generate loop-runtime-api-tokens loop-runtime-system argocd-token)
      [[ -n "$ARGOCD_TOKEN" ]] && log "preserving existing argocd-token (login failed; cluster likely mid-bootstrap)"
    fi
    if [[ -n "$ARGOCD_TOKEN" ]]; then
      apply_secret loop-runtime-api-tokens loop-runtime-system \
        --from-literal=argocd-token="$ARGOCD_TOKEN" \
        --from-literal=gitea-token="$GITEA_TOKEN"
      log "F-12: loop-runtime-api-tokens applied (argocd-token JWT + gitea-token)"
    fi

    # Plan 2026-06-04-dev-public-docr-fix-forward T3.1 — propagate
    # gitea-token to canonical `loop-runtime-tenant-operator` Secret (uppercase
    # GITEA_TOKEN key) consumed by charts/loop-runtime-tenant-operator deployment
    # via envFrom. Envs that override values with
    # `secrets.create: false, secrets.existingSecret: loop-runtime-tenant-operator`
    # rely on this block. Idempotent. Replaces the deleted parallel
    # script `infra/scripts/local/generate-dev-public-secrets.sh`.
    if [[ -n "$GITEA_TOKEN" ]]; then
      apply_secret loop-runtime-tenant-operator loop-runtime-system \
        --from-literal=GITEA_TOKEN="$GITEA_TOKEN"
      log "T3.1: loop-runtime-tenant-operator Secret applied (GITEA_TOKEN env-var key)"
    fi

    # CHG-2026-04-30-DOGFOOD-LOCAL-HEAL NEW-15 — argocd-refresh-credentials
    # Secret consumed by api/internal/argocd/KubeMintingTokenProvider. Two
    # supported credential modes:
    #   (a) bearer: long-lived loop-runtime-api apiKey token persisted as
    #       data.argocd-account-token. Preferred; survives admin password
    #       rotation and never expires when sourced from a configured
    #       account: loop-runtime-api: { apiKey: true } in argocd-cm.
    #   (b) password: data.argocd-username + data.argocd-password fallback
    #       to admin/initial-admin-password. Local k3d only.
    # The provider reads (a) first, falls back to (b). Storing both is OK.
    apply_secret argocd-refresh-credentials loop-runtime-system \
      --from-literal=argocd-username=admin \
      --from-literal=argocd-password="$ADMIN_PW"
    log "F-12: argocd-refresh-credentials applied (admin password fallback for in-process token mint)"
  fi
fi

# 3) MinIO / S3 root creds — IDEMPOTENT.
# Cloud (DO Spaces): uses LOOP_RUNTIME_SPACES_KEY / LOOP_RUNTIME_SPACES_SECRET from Pulumi.
# Standalone: MUST match the MinIO chart root (rootUser=loop-runtime-admin from
# infra/helm/minio/values.yaml + rootPassword=loop-runtime-dev-password pinned in the
# standalone helmfile set:) AND the loop-runtime-api env (infra/tilt/api-deployment.yaml).
# Using minioadmin/random here diverged from MinIO's actual root, causing
# SOURCE_UPLOAD_FAILED (access denied) on every deploy. Deterministic creds,
# same pattern as PG (T1.3).
if [[ -n "${LOOP_RUNTIME_SPACES_KEY:-}" ]]; then
  apply_secret minio-credentials loop-runtime-system \
    --from-literal=accesskey="${LOOP_RUNTIME_SPACES_KEY}" \
    --from-literal=secretkey="${LOOP_RUNTIME_SPACES_SECRET:-}"
else
  MINIO_ACCESS="${MINIO_ROOT_USER:-loop-runtime-admin}"
  MINIO_SECRET="${MINIO_ROOT_PASSWORD:-loop-runtime-dev-password}"
  apply_secret minio-credentials loop-runtime-system \
    --from-literal=accesskey="$MINIO_ACCESS" \
    --from-literal=secretkey="$MINIO_SECRET"
fi

# 3b) Velero S3 credentials — IDEMPOTENT.
# velero-backup-dr-plan v1.0 / T1.3 + EC-3 fix.
#
# Velero expects an AWS credentials INI file under key "cloud" in the
# Secret. Same LOOP_RUNTIME_SPACES_KEY/SECRET (or MinIO root creds in standalone)
# that drive minio-credentials — DO Spaces uses global API keys so we
# can't isolate IAM per bucket (documented as EC-6 risk in the plan).
#
# ensure_namespace velero-system runs BEFORE the Helmfile installs the
# velero release (which would create the namespace via createNamespace).
# Without this, the first `kubectl apply` on a fresh cluster fails with
# "namespaces not found" because generate-secrets runs before helmfile
# sync. Idempotent re-runs work because the namespace will already exist.
ensure_namespace velero-system
if [[ -n "${LOOP_RUNTIME_SPACES_KEY:-}" ]]; then
  VELERO_AK="${LOOP_RUNTIME_SPACES_KEY}"
  VELERO_SK="${LOOP_RUNTIME_SPACES_SECRET:-}"
else
  VELERO_AK="${MINIO_ROOT_USER:-loop-runtime-admin}"
  VELERO_SK="${MINIO_ROOT_PASSWORD:-loop-runtime-dev-password}"
fi
VELERO_CLOUD_INI="$(printf '[default]\naws_access_key_id=%s\naws_secret_access_key=%s\n' "$VELERO_AK" "$VELERO_SK")"
apply_secret velero-s3-credentials velero-system \
  --from-literal=cloud="$VELERO_CLOUD_INI"

# 4) Registry pull secret (LoopRuntime's DOCR) — passthrough from Pulumi outputs.
if [[ -n "${LOOP_RUNTIME_REGISTRY_ENDPOINT:-}" ]]; then
  REG_AUTH=$(printf '%s' "${LOOP_RUNTIME_SPACES_KEY:-}:${LOOP_RUNTIME_SPACES_SECRET:-}" | base64 -w0)
  cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: v1
kind: Secret
type: kubernetes.io/dockerconfigjson
metadata:
  name: loop-runtime-registry-pull
  namespace: loop-runtime-system
data:
  .dockerconfigjson: $(printf '%s' "{\"auths\":{\"${LOOP_RUNTIME_REGISTRY_ENDPOINT}\":{\"auth\":\"${REG_AUTH}\"}}}" | base64 -w0)
EOF
  log "secret loop-runtime-system/loop-runtime-registry-pull applied"
fi

# 5) Cluster kubeconfigs for loop-runtime-api / controllers (B36 fix).
#
# B36 (CHG-2026-04-28-LAYERED-BOOTSTRAP) — loop-runtime-api requires kubeconfigs
# for mgmt/build/runtime clusters to submit Argo Workflows and reconcile
# CRDs cross-cluster. In LoopRuntime Cloud, Pulumi generates 3 distinct
# kubeconfigs (one per DOKS cluster). In standalone single-cluster, all
# 3 collapse to the SAME cluster — a synthetic in-cluster kubeconfig
# pointing to the local Kubernetes API.
#
# Without this, loop-runtime-api / controllers crash with `argo: build cluster
# config: stat //.kube/config: no such file or directory`.
if [[ "${LOOP_RUNTIME_PROFILE:-standalone}" == "standalone" ]] || [[ -z "${LOOP_RUNTIME_BUILD_KUBECONFIG:-}" ]]; then
  # Plan 2026-06-04-dev-public-docr-fix-forward FAANG fix —
  # Per-slot idempotency for loop-runtime-cluster-kubeconfigs Secret.
  #
  # ROOT CAUSE the fix addresses (caught LIVE during dev-public dogfood,
  # 2026-06-04): this block USED to unconditionally overwrite ALL 3 keys
  # with `https://kubernetes.default.svc` placeholders. The `k3d:heal`
  # script (`infra/scripts/local/k3d-cross-cluster-heal.sh` H2) is meant
  # to patch the Secret with real container IPs + SA tokens AFTER
  # bootstrap. But ANY subsequent `task infra:install:core` re-ran
  # generate-secrets.sh and wiped those real values back to placeholders
  # → loop-runtime-api submitted Workflow CRs to mgmt cluster (no
  # workflows.argoproj.io CRD) → "the server could not find the
  # requested resource" → 12+ dogfood deploy specs FAIL.
  #
  # FIX (per-slot idempotency):
  #   1. For each of {mgmt,build,runtime}.kubeconfig:
  #      - If the existing value looks like a REAL kubeconfig (server URL
  #        is NOT `kubernetes.default.svc`), preserve it verbatim.
  #      - If the existing value is missing OR is the placeholder
  #        pointing at kubernetes.default.svc, install the synthetic
  #        in-cluster default.
  #   2. Same blast radius as before in fresh-bootstrap (all 3 default).
  #      But re-runs after k3d:heal preserve real IPs/tokens.
  #
  # SAFE-ON-CORRUPTION: if Secret is partially corrupted (e.g. mgmt is
  # real but runtime is placeholder), each slot is evaluated
  # independently — only broken slots are reset.
  log "B36: standalone — ensuring loop-runtime-cluster-kubeconfigs (per-slot idempotent)"

  IN_CLUSTER_KUBECONFIG=$(cat <<'KUBECONFIG_EOF'
apiVersion: v1
kind: Config
current-context: in-cluster
contexts:
  - name: in-cluster
    context:
      cluster: in-cluster
      user: in-cluster
      namespace: loop-runtime-system
clusters:
  - name: in-cluster
    cluster:
      server: https://kubernetes.default.svc
      certificate-authority: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
users:
  - name: in-cluster
    user:
      tokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
KUBECONFIG_EOF
)

  resolve_slot() {
    # $1 = slot key (core.kubeconfig | build.kubeconfig | cell-0.kubeconfig)
    local key="$1"
    local existing
    existing=$(kubectl get secret loop-runtime-cluster-kubeconfigs -n loop-runtime-system \
      -o "jsonpath={.data.${key//./\\.}}" 2>/dev/null | base64 -d 2>/dev/null || true)
    if [[ -n "$existing" ]] && ! printf '%s' "$existing" | grep -q "kubernetes.default.svc"; then
      log "  preserving existing $key (already patched with real server)"
      printf '%s' "$existing"
    else
      log "  installing in-cluster default for $key"
      printf '%s' "$IN_CLUSTER_KUBECONFIG"
    fi
  }

  MGMT_KC="$(resolve_slot core.kubeconfig)"
  BUILD_KC="$(resolve_slot build.kubeconfig)"
  RUNTIME_KC="$(resolve_slot cell-0.kubeconfig)"

  apply_secret loop-runtime-cluster-kubeconfigs loop-runtime-system \
    --from-literal=core.kubeconfig="$MGMT_KC" \
    --from-literal=build.kubeconfig="$BUILD_KC" \
    --from-literal=cell-0.kubeconfig="$RUNTIME_KC"
fi

# 6) Backup all LoopRuntime-managed Secrets for disaster recovery.
backup_secrets "secrets-${CLUSTER}-${ENV}" \
  loop-runtime-system/loop-runtime-db-credentials loop-runtime-system/gitea-admin loop-runtime-system/minio-credentials \
  loop-runtime-system/loop-runtime-registry-pull

log "mgmt secrets generated for env=$ENV (idempotent — re-runs preserve values)"
