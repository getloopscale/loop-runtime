#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# bootstrap-aws-clusters.sh <pre-core|post-core|broker-tokens>
#
# The in-cluster wiring of an environment whose infrastructure is AWS (profile
# awsSecretsManager): everything the platform needs that is NOT an AWS credential, which
# Secrets Manager delivers through External Secrets instead.
#
#   pre-core    before the core helmfile sync: the namespaces the core ExternalSecrets
#               write into, and the Gitea admin credentials the gitea chart reads.
#   post-core   after it: the scoped ServiceAccount kubeconfigs the core cluster reaches
#               build and the cells with, the ClusterCredentials loopruntime-api discovers
#               the cells from, the ArgoCD cluster registrations, and the Gitea and
#               ArgoCD tokens (`loopruntime-ops seed`).
#   broker-tokens once loopruntime-api is Ready: one secret-fetch broker token per cell
#               (`loopruntime-ops seed broker`), in core's loopruntime-system/loopruntime-broker-token
#               for the API and in each cell's external-secrets/loopruntime-broker-token for
#               its broker store. Tokens already issued are kept. The seed restarts
#               loopruntime-api and waits for it to settle, which is why it runs apart from
#               post-core, before which the API cannot be Ready.
#
# Contexts (one kubeconfig, one context per cluster):
#   CORE_CONTEXT    the core cluster (default: core)
#   BUILD_CONTEXT   the build cluster (default: build)
#   CELL_CONTEXTS   the cells, space-separated (default: "cell-0 cell-1"); each context
#                   name is the cell id.
#
# Idempotent: generated values are kept across runs, and every object is applied.
set -euo pipefail

PHASE="${1:?usage: $0 <pre-core|post-core|broker-tokens>}"
CORE_CONTEXT="${CORE_CONTEXT:-core}"
BUILD_CONTEXT="${BUILD_CONTEXT:-build}"
CELL_CONTEXTS="${CELL_CONTEXTS:-cell-0 cell-1}"
NS=loopruntime-system

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
chmod 700 "$WORK"

log() { printf '[bootstrap-aws-clusters] %s\n' "$*" >&2; }
core() { kubectl --context "$CORE_CONTEXT" "$@"; }

# The value of one key of a Secret, or "" when the Secret or the key is absent.
secret_value() {
  core -n "$1" get secret "$2" -o "jsonpath={.data.$3}" 2>/dev/null | base64 -d 2>/dev/null || true
}

pre_core() {
  for ns in loopruntime-system loopruntime-auth; do
    core create namespace "$ns" --dry-run=client -o yaml | core apply -f - >/dev/null
  done
  log "namespaces loopruntime-system and loopruntime-auth present"

  # Gitea's admin is an account of the in-cluster Gitea; its token is minted later by
  # `loopruntime-ops seed gitea-token`, which adds it to this Secret.
  local password token
  password="$(secret_value "$NS" gitea-admin password)"
  [[ -n "$password" ]] || password="$(openssl rand -hex 24)"
  token="$(secret_value "$NS" gitea-admin token)"
  core -n "$NS" create secret generic gitea-admin \
    --from-literal=username=gitea_admin \
    --from-literal=password="$password" \
    ${token:+--from-literal=token="$token"} \
    --dry-run=client -o yaml | core apply -f - >/dev/null
  log "secret $NS/gitea-admin present"
}

# A kubeconfig for one context of the current KUBECONFIG, credentials inlined.
context_kubeconfig() {
  local ctx="$1" out="$WORK/$1.admin.yaml"
  kubectl config view --minify --flatten --context "$ctx" >"$out"
  printf '%s' "$out"
}

# A kubeconfig the core cluster's pods use for <cluster>: a ServiceAccount token on the
# target (lib/scoped-kubeconfig.sh), its context named after the cluster.
scoped_kubeconfig() {
  local cluster="$1" ctx="$2" admin
  admin="$(context_kubeconfig "$ctx")"
  "$SCRIPT_DIR/lib/scoped-kubeconfig.sh" "$cluster" "$admin" "$WORK/$cluster.scoped.yaml" >/dev/null
  printf '%s' "$WORK/$cluster.scoped.yaml"
}

loopruntime_ops() {
  local bin="$WORK/loopruntime-ops"
  if [[ ! -x "$bin" ]]; then
    (cd "$REPO_ROOT/cmd/loopruntime-ops" && GOWORK=off go build -buildvcs=false -o "$bin" .)
  fi
  CORE_CONTEXT="$CORE_CONTEXT" "$bin" "$@"
}

post_core() {
  local build_kc first_cell=""
  build_kc="$(scoped_kubeconfig build "$BUILD_CONTEXT")"
  log "scoped kubeconfig for build"

  core -n "$NS" create configmap loopruntime-build-kubeconfig --from-file=config="$build_kc" \
    --dry-run=client -o yaml | core apply -f - >/dev/null
  KUBECTL_CONTEXT="$CORE_CONTEXT" "$SCRIPT_DIR/lib/argocd-register.sh" build "$build_kc"

  local cell cell_kc
  for cell in $CELL_CONTEXTS; do
    cell_kc="$(scoped_kubeconfig "$cell" "$cell")"
    log "scoped kubeconfig for $cell"
    # The CR loopruntime-api's DynamicRouter discovers the cell from; the platform operator
    # probes it and sets status.connected. The scoped kubeconfig carries the cluster CA.
    KUBECTL_CONTEXT="$CORE_CONTEXT" LOOPRUNTIME_NS="$NS" INGRESS_DOMAIN="${LOOPRUNTIME_INGRESS_DOMAIN:-}" \
      MANAGED_BY=bootstrap-aws-clusters TLS_INSECURE=false \
      bash "$SCRIPT_DIR/lib/clustercredential-create.sh" "$cell" "$cell" "$cell_kc"
    # The clustercredential controller registers the cell in Argo CD from that CR (Secret
    # cluster-<cell>) and keeps it current on rotation. argocd-register.sh would add a second
    # cluster with the same name, and Argo CD then resolves no Application destination.
    if [[ -z "$first_cell" ]]; then
      first_cell="$cell"
      core -n "$NS" create configmap loopruntime-cell-0-kubeconfig --from-file=config="$cell_kc" \
        --dry-run=client -o yaml | core apply -f - >/dev/null
    fi
  done

  # loopruntime-api's three cluster kubeconfigs: core is the pod's own ServiceAccount.
  cat >"$WORK/core.kubeconfig" <<'EOF'
apiVersion: v1
kind: Config
current-context: in-cluster
contexts:
  - name: in-cluster
    context:
      cluster: in-cluster
      user: in-cluster
      namespace: loopruntime-system
clusters:
  - name: in-cluster
    cluster:
      server: https://kubernetes.default.svc
      certificate-authority: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
users:
  - name: in-cluster
    user:
      tokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
EOF
  core -n "$NS" create secret generic loopruntime-cluster-kubeconfigs \
    --from-file=core.kubeconfig="$WORK/core.kubeconfig" \
    --from-file=build.kubeconfig="$build_kc" \
    --from-file=cell-0.kubeconfig="$WORK/$first_cell.scoped.yaml" \
    --dry-run=client -o yaml | core apply -f - >/dev/null
  log "secret $NS/loopruntime-cluster-kubeconfigs applied"

  loopruntime_ops seed gitea-token
  loopruntime_ops seed argocd-token
  loopruntime_ops seed argocd-gitea-creds
}

# The seed finds the cells in core's ClusterCredentials and reaches each one through the
# context named after its id, as CELL_CONTEXTS names them here; an empty prefix replaces
# the seed's k3d default (k3d-loopruntime-<cell>).
broker_tokens() {
  LOOPRUNTIME_CELL_CONTEXT_PREFIX="" loopruntime_ops seed broker
}

case "$PHASE" in
  pre-core) pre_core ;;
  post-core) post_core ;;
  broker-tokens) broker_tokens ;;
  *) log "unknown phase: $PHASE (pre-core|post-core|broker-tokens)"; exit 1 ;;
esac
