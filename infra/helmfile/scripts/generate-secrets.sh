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
#   env:     dev | prod | standalone
#   cluster: mgmt | runtime
#
# Optional env vars (override existing — used for explicit rotation):
#   LOOPZERO_DB_HOST, LOOPZERO_DB_PORT, LOOPZERO_DB_NAME, LOOPZERO_DB_USER, LOOPZERO_DB_PASSWORD
#   LOOPZERO_REGISTRY_ENDPOINT, LOOPZERO_BUCKET_NAME, LOOPZERO_BUCKET_ENDPOINT
#   LOOPZERO_SPACES_KEY, LOOPZERO_SPACES_SECRET
#   LOOPZERO_GITEA_ADMIN_PASSWORD, LOOPZERO_GITEA_TOKEN
#   LOOPZERO_HARBOR_ADMIN_PASSWORD, LOOPZERO_HARBOR_REDIS_PASSWORD
#
# Backup: every successful run dumps loopzero-platform-secrets to
#   ~/.loopzero/backups/secrets-mgmt-{env}-{timestamp}.yaml (chmod 600)
# Restore: kubectl apply -f <backup-file>
set -euo pipefail

ENV="${1:?usage: $0 <env> <cluster>}"
CLUSTER="${2:?usage: $0 <env> <cluster>}"

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

backup_secrets() {
  local backup_dir="${HOME}/.loopzero/backups"
  mkdir -p "$backup_dir"
  chmod 700 "$backup_dir"
  local ts
  ts=$(date +%Y%m%d-%H%M%S)
  local out="${backup_dir}/secrets-${CLUSTER}-${ENV}-${ts}.yaml"
  # Dump every LoopZero-managed Secret in loopzero-system into a single file.
  # CRITICAL (iter 17 fix): strip server-managed fields (resourceVersion, uid,
  # creationTimestamp, managedFields, ownerReferences) so `kubectl apply -f <file>`
  # works on a different cluster OR after the original Secret was modified.
  # Without stripping, restore fails with: "Conflict: object has been modified".
  kubectl get secret -n loopzero-system \
    loopzero-db-credentials gitea-admin minio-credentials \
    harbor-admin harbor-redis-credentials loopzero-registry-pull \
    -o yaml 2>/dev/null | python3 -c "
import sys, yaml
docs = list(yaml.safe_load_all(sys.stdin))
out = []
for d in docs:
    if not d:
        continue
    # If list of items (kubectl get -o yaml returns a List)
    items = d.get('items', [d]) if d.get('kind') == 'List' else [d]
    for item in items:
        if not item:
            continue
        meta = item.get('metadata', {})
        # Remove server-generated fields that block apply on a different state
        for f in ('resourceVersion', 'uid', 'creationTimestamp',
                  'managedFields', 'ownerReferences', 'selfLink', 'generation'):
            meta.pop(f, None)
        # Drop status (server-side only)
        item.pop('status', None)
        out.append(item)
if out:
    print(yaml.safe_dump_all(out, default_flow_style=False))
" > "$out" 2>/dev/null || true
  if [[ -s "$out" ]]; then
    chmod 600 "$out"
    log "backup written: $out (cleaned of server-managed fields — restorable)"
    # Keep only the 10 most recent backups per cluster/env to avoid disk bloat
    # while preserving recent rotation history.
    ls -t "${backup_dir}/secrets-${CLUSTER}-${ENV}-"*.yaml 2>/dev/null | tail -n +11 | xargs -r rm -f
  else
    rm -f "$out"
    log "backup skipped (no LoopZero Secrets present yet — first install)"
  fi
}

if [[ "$CLUSTER" != "mgmt" ]]; then
  log "noop: cluster=$CLUSTER (only mgmt handled by this script)"
  exit 0
fi

log "generating mgmt secrets for env=$ENV (idempotent mode)"

# 1) Database credentials (consumed by loopzero-api, gitea, ory-kratos, ory-hydra).
# DB password comes from Pulumi L1 — env var is canonical source. If absent
# (e.g. standalone install without --provider), fall back to in-cluster
# Postgres release (B33 fix) so loopzero-api receives a valid DATABASE_URL.
#
# B33 (CHG-2026-04-28-LAYERED-BOOTSTRAP) — Without this fallback, the
# `loopzero-db-credentials` secret was never created in standalone profile,
# causing loopzero-api to start with `DATABASE_URL not set`, fail readiness
# probes, and crashloop. Detected on fresh standalone bootstrap 2026-04-28.
if [[ -z "${LOOPZERO_DB_HOST:-}" ]]; then
  LOOPZERO_DB_HOST="postgresql.loopzero-system.svc.cluster.local"
  LOOPZERO_DB_PORT="5432"
  LOOPZERO_DB_USER="${LOOPZERO_DB_USER:-loopzero}"
  LOOPZERO_DB_NAME="${LOOPZERO_DB_NAME:-loopzero}"
  # STANDALONE (local k3d) DB password is the DETERMINISTIC well-known dev
  # password `loopzero-dev-password`. This is NOT a hardcoded production credential
  # (T1.3 correctly removed those from the CHARTS, which default "" so prod
  # fails fast). It is the single local-dev contract that the ENTIRE local
  # toolchain depends on: api/internal/config/config.go, Tiltfile (db-migrate),
  # Taskfile, infra/tests/seed/seed.go, parity tests, etc. (58 refs). Making it
  # random broke all of them (e.g. Tiltfile db-migrate SASL auth failure).
  # Env override (LOOPZERO_DB_PASSWORD) still wins for cloud/drill. Root cause:
  # finding-2026-05-22-pg-password-divergence § local-dev-password.
  LOOPZERO_DB_PASSWORD="${LOOPZERO_DB_PASSWORD:-loopzero-dev-password}"
  DB_SSLMODE="disable"
  log "B33: standalone fallback — DB at $LOOPZERO_DB_HOST (sslmode=disable)"
else
  DB_SSLMODE="require"
fi
DB_PASSWORD="${LOOPZERO_DB_PASSWORD:-$(get_or_generate loopzero-db-credentials loopzero-system password)}"
DB_URI="${LOOPZERO_DB_URI:-postgresql://${LOOPZERO_DB_USER:-loopzero}:${DB_PASSWORD}@${LOOPZERO_DB_HOST}:${LOOPZERO_DB_PORT:-5432}/${LOOPZERO_DB_NAME:-loopzero}?sslmode=${DB_SSLMODE}}"
# Superuser (postgres) password — required so the in-cluster Bitnami PostgreSQL
# (standalone profile) can consume this secret via auth.existingSecret. Bitnami
# needs BOTH the admin key (postgres-password) and the custom-user key (password).
# Without it the chart auto-generates a random password for the `loopzero` user that
# diverges from what kratos/hydra/loopzero-api read here — kratos then fails SASL auth
# at config-load and blocks `task setup` on the helmfile --wait. Root cause:
# finding-2026-05-22-pg-password-divergence. Cloud (--provider) uses managed PG and
# does NOT install the in-cluster chart, so this key is inert there.
DB_ADMIN_PASSWORD="${LOOPZERO_DB_ADMIN_PASSWORD:-$(get_or_generate loopzero-db-credentials loopzero-system postgres-password)}"
apply_secret loopzero-db-credentials loopzero-system \
  --from-literal=host="${LOOPZERO_DB_HOST}" \
  --from-literal=port="${LOOPZERO_DB_PORT:-5432}" \
  --from-literal=database="${LOOPZERO_DB_NAME:-loopzero}" \
  --from-literal=user="${LOOPZERO_DB_USER:-loopzero}" \
  --from-literal=password="${DB_PASSWORD}" \
  --from-literal=postgres-password="${DB_ADMIN_PASSWORD}" \
  --from-literal=uri="${DB_URI}"

# 2) Gitea admin password + token — IDEMPOTENT (iter 14 fix).
GITEA_PWD="${LOOPZERO_GITEA_ADMIN_PASSWORD:-$(get_or_generate gitea-admin loopzero-system password)}"
GITEA_TOKEN="${LOOPZERO_GITEA_TOKEN:-$(get_or_generate gitea-admin loopzero-system token)}"

# B4 — printf '%s' prevents trailing newline that broke L3 #1.
GITEA_PWD_FILE="$(mktemp)"
GITEA_TOKEN_FILE="$(mktemp)"
trap 'rm -f "$GITEA_PWD_FILE" "$GITEA_TOKEN_FILE"' EXIT
emit_token "$GITEA_PWD" > "$GITEA_PWD_FILE"
emit_token "$GITEA_TOKEN" > "$GITEA_TOKEN_FILE"

apply_secret gitea-admin loopzero-system \
  --from-literal=username="${LOOPZERO_GITEA_ADMIN_USER:-gitea_admin}" \
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
    loopzero.io/managed-by: loopzero-platform
type: Opaque
stringData:
  type: git
  url: http://gitea-http.loopzero-system.svc.cluster.local:3000/
  username: loopzero
  password: ${GITEA_TOKEN}
EOF

# F-12 (CHG-2026-04-29-DOGFOOD T5.4) — loopzero-api `/api/v1/health/deep` probe
# de ArgoCD requer um JWT válido. Pré-fix: ARGOCD_TOKEN nunca era setado em
# standalone-multi → API responde `{"argocd":"auth_failed","status":"degraded"}`
# HTTP 503 mesmo com ArgoCD saudável. Fix idempotente: login admin via REST,
# capturar JWT, persistir em Secret `loopzero-api-tokens` (loopzero-system) com chave
# `argocd-token` — exatamente o key esperado pelo Tilt manifest e pelo
# helmfile loopzero-api Deployment template.
#
# Login pattern: argocd-server expõe HTTP plain em port 80 (server.insecure:
# true via Helm overlay). Token JWT default validade ~24h — refresh é
# responsabilidade de iter 2 (fora do escopo deste fix). Falha silenciosa
# se ArgoCD não estiver Ready (ex: bootstrap mid-flight): loopzero-api volta a
# reportar degraded até o próximo sync, comportamento aceitável (não bloqueia
# install).
if kubectl get namespace argocd >/dev/null 2>&1 && \
   kubectl get secret -n argocd argocd-initial-admin-secret >/dev/null 2>&1; then
  ADMIN_PW=$(kubectl get secret -n argocd argocd-initial-admin-secret -o jsonpath='{.data.password}' 2>/dev/null | base64 -d)
  ARGOCD_HOST="argocd-server.argocd.svc.cluster.local"
  if [[ -n "$ADMIN_PW" ]]; then
    # Run login via short-lived debug Pod (kubectl run + curl). Idempotente:
    # mesmo password produz tokens novos a cada call — preserva existing
    # Secret se login falhar (mantém valor anterior em vez de zerar).
    ARGOCD_TOKEN=$(kubectl run argocd-token-bootstrap-$$ \
      --rm -i --restart=Never --image=curlimages/curl:8.5.0 \
      --command -- curl -s --max-time 5 \
        -X POST -H "Content-Type: application/json" \
        -d "{\"username\":\"admin\",\"password\":\"$ADMIN_PW\"}" \
        "http://${ARGOCD_HOST}/api/v1/session" 2>/dev/null \
      | python3 -c 'import sys,json; print(json.loads(sys.stdin.read() or "{}").get("token",""))' 2>/dev/null || true)
    if [[ -z "$ARGOCD_TOKEN" ]]; then
      ARGOCD_TOKEN=$(get_or_generate loopzero-api-tokens loopzero-system argocd-token)
      [[ -n "$ARGOCD_TOKEN" ]] && log "preserving existing argocd-token (login failed; cluster likely mid-bootstrap)"
    fi
    if [[ -n "$ARGOCD_TOKEN" ]]; then
      apply_secret loopzero-api-tokens loopzero-system \
        --from-literal=argocd-token="$ARGOCD_TOKEN" \
        --from-literal=gitea-token="$GITEA_TOKEN"
      log "F-12: loopzero-api-tokens applied (argocd-token JWT + gitea-token)"
    fi

    # CHG-2026-04-30-DOGFOOD-LOCAL-HEAL NEW-15 — argocd-refresh-credentials
    # Secret consumed by api/internal/argocd/KubeMintingTokenProvider. Two
    # supported credential modes:
    #   (a) bearer: long-lived loopzero-api apiKey token persisted as
    #       data.argocd-account-token. Preferred; survives admin password
    #       rotation and never expires when sourced from a configured
    #       account: loopzero-api: { apiKey: true } in argocd-cm.
    #   (b) password: data.argocd-username + data.argocd-password fallback
    #       to admin/initial-admin-password. Local k3d only.
    # The provider reads (a) first, falls back to (b). Storing both is OK.
    apply_secret argocd-refresh-credentials loopzero-system \
      --from-literal=argocd-username=admin \
      --from-literal=argocd-password="$ADMIN_PW"
    log "F-12: argocd-refresh-credentials applied (admin password fallback for in-process token mint)"
  fi
fi

# 3) MinIO / S3 root creds — IDEMPOTENT.
# Cloud (DO Spaces): uses LOOPZERO_SPACES_KEY / LOOPZERO_SPACES_SECRET from Pulumi.
# Standalone: generates with MinIO chart defaults (minioadmin/minioadmin).
if [[ -n "${LOOPZERO_SPACES_KEY:-}" ]]; then
  apply_secret minio-credentials loopzero-system \
    --from-literal=accesskey="${LOOPZERO_SPACES_KEY}" \
    --from-literal=secretkey="${LOOPZERO_SPACES_SECRET:-}"
else
  MINIO_ACCESS="${MINIO_ROOT_USER:-minioadmin}"
  MINIO_SECRET="${MINIO_ROOT_PASSWORD:-$(get_or_generate minio-credentials loopzero-system secretkey)}"
  apply_secret minio-credentials loopzero-system \
    --from-literal=accesskey="$MINIO_ACCESS" \
    --from-literal=secretkey="$MINIO_SECRET"
fi

# 3b) Velero S3 credentials — IDEMPOTENT.
# velero-backup-dr-plan v1.0 / T1.3 + EC-3 fix.
#
# Velero expects an AWS credentials INI file under key "cloud" in the
# Secret. Same LOOPZERO_SPACES_KEY/SECRET (or MinIO root creds in standalone)
# that drive minio-credentials — DO Spaces uses global API keys so we
# can't isolate IAM per bucket (documented as EC-6 risk in the plan).
#
# ensure_namespace velero-system runs BEFORE the Helmfile installs the
# velero release (which would create the namespace via createNamespace).
# Without this, the first `kubectl apply` on a fresh cluster fails with
# "namespaces not found" because generate-secrets runs before helmfile
# sync. Idempotent re-runs work because the namespace will already exist.
ensure_namespace velero-system
if [[ -n "${LOOPZERO_SPACES_KEY:-}" ]]; then
  VELERO_AK="${LOOPZERO_SPACES_KEY}"
  VELERO_SK="${LOOPZERO_SPACES_SECRET:-}"
else
  VELERO_AK="${MINIO_ROOT_USER:-minioadmin}"
  VELERO_SK="${MINIO_ROOT_PASSWORD:-$(get_or_generate minio-credentials loopzero-system secretkey)}"
fi
VELERO_CLOUD_INI="$(printf '[default]\naws_access_key_id=%s\naws_secret_access_key=%s\n' "$VELERO_AK" "$VELERO_SK")"
apply_secret velero-s3-credentials velero-system \
  --from-literal=cloud="$VELERO_CLOUD_INI"

# 4) Harbor admin password — IDEMPOTENT.
HARBOR_PWD="${LOOPZERO_HARBOR_ADMIN_PASSWORD:-$(get_or_generate harbor-admin loopzero-system password)}"
apply_secret harbor-admin loopzero-system \
  --from-literal=password="$HARBOR_PWD"

# 5) Harbor Redis credentials — IDEMPOTENT.
# Host comes from Pulumi (L1 DO Managed Valkey), password is the Valkey password.
HARBOR_REDIS_PWD="${LOOPZERO_HARBOR_REDIS_PASSWORD:-$(get_or_generate harbor-redis-credentials loopzero-system password)}"
apply_secret harbor-redis-credentials loopzero-system \
  --from-literal=password="$HARBOR_REDIS_PWD" \
  --from-literal=host="${LOOPZERO_HARBOR_REDIS_HOST:-harbor-redis-master.loopzero-system.svc.cluster.local}" \
  --from-literal=port="${LOOPZERO_HARBOR_REDIS_PORT:-6379}"

# 6) Registry pull secret (LoopZero's DOCR/Harbor) — passthrough from Pulumi outputs.
if [[ -n "${LOOPZERO_REGISTRY_ENDPOINT:-}" ]]; then
  REG_AUTH=$(printf '%s' "${LOOPZERO_SPACES_KEY:-}:${LOOPZERO_SPACES_SECRET:-}" | base64 -w0)
  cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: v1
kind: Secret
type: kubernetes.io/dockerconfigjson
metadata:
  name: loopzero-registry-pull
  namespace: loopzero-system
data:
  .dockerconfigjson: $(printf '%s' "{\"auths\":{\"${LOOPZERO_REGISTRY_ENDPOINT}\":{\"auth\":\"${REG_AUTH}\"}}}" | base64 -w0)
EOF
  log "secret loopzero-system/loopzero-registry-pull applied"
fi

# 7) Cluster kubeconfigs for loopzero-api / controllers (B36 fix).
#
# B36 (CHG-2026-04-28-LAYERED-BOOTSTRAP) — loopzero-api requires kubeconfigs
# for mgmt/build/runtime clusters to submit Argo Workflows and reconcile
# CRDs cross-cluster. In LoopZero Cloud, Pulumi generates 3 distinct
# kubeconfigs (one per DOKS cluster). In standalone single-cluster, all
# 3 collapse to the SAME cluster — a synthetic in-cluster kubeconfig
# pointing to the local Kubernetes API.
#
# Without this, loopzero-api / controllers crash with `argo: build cluster
# config: stat //.kube/config: no such file or directory`.
if [[ "${LOOPZERO_PROFILE:-standalone}" == "standalone" ]] || [[ -z "${LOOPZERO_BUILD_KUBECONFIG:-}" ]]; then
  log "B36: standalone — generating synthetic in-cluster kubeconfig"
  KUBECONFIG_TMP="$(mktemp)"
  cat > "$KUBECONFIG_TMP" <<'KUBECONFIG_EOF'
apiVersion: v1
kind: Config
current-context: in-cluster
contexts:
  - name: in-cluster
    context:
      cluster: in-cluster
      user: in-cluster
      namespace: loopzero-system
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
  apply_secret loopzero-cluster-kubeconfigs loopzero-system \
    --from-file=mgmt.kubeconfig="$KUBECONFIG_TMP" \
    --from-file=build.kubeconfig="$KUBECONFIG_TMP" \
    --from-file=runtime.kubeconfig="$KUBECONFIG_TMP"
  rm -f "$KUBECONFIG_TMP"
fi

# 8) Backup all LoopZero-managed Secrets for disaster recovery.
backup_secrets

log "mgmt secrets generated for env=$ENV (idempotent — re-runs preserve values)"
