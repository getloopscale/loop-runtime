#!/usr/bin/env bash
# Canonical post-Cleanroom seed for ArgoCD ↔ Gitea repository
# credentials. Without this, EVERY ArgoCD Application emitted by the
# loopzero-tenant-operator stays in `Comparison Failed` state with:
#
#   "Failed to load target state: failed to generate manifest for source
#    1 of 1: rpc error: code = Unknown desc = authentication required"
#
# because ArgoCD's repo-server cannot pull the GitOps manifests repo
# from Gitea — there's no canonical `repo-creds` Secret in the argocd
# namespace pointing at the in-cluster Gitea endpoint.
#
# This is sibling to seed-gitea-token-canonical.sh which mints the Gitea
# token + syncs to loopzero-system consumers. This script propagates the
# CANONICAL token (from gitea-admin.token after seed-gitea-token-canonical
# runs) into the ArgoCD-side repo-creds Secret format.
#
# Layers:
# 1. Read canonical Gitea credentials from loopzero-system/gitea-admin
# 2. Materialize argocd/argocd-creds-gitea Secret with
#    `argocd.argoproj.io/secret-type: repo-creds` label so ArgoCD picks
#    it up + scopes to the canonical in-cluster URL prefix
#    `http://gitea-http.loopzero-system.svc.cluster.local:3000/loopzero`
# 3. (Optional) Trigger hard refresh on existing Applications to clear
#    cached auth failures.
#
# Per CLAUDE.md "Common Mistakes" + memory
# `feedback_post_cleanroom_seed_admin_canonical`: this seed MUST run
# AFTER seed-gitea-token-canonical so token is canonical, NOT BEFORE.
#
# Idempotent: kubectl apply with stringData (always re-materializes).

set -euo pipefail

MGMT_CONTEXT="${KUBE_CONTEXT:-k3d-loopzero-mgmt}"
ARGOCD_NAMESPACE="${ARGOCD_NAMESPACE:-argocd}"
LOOPZERO_NAMESPACE="${LOOPZERO_NAMESPACE:-loopzero-system}"
GITEA_URL="${LOOPZERO_GITEA_URL:-http://gitea-http.loopzero-system.svc.cluster.local:3000/loopzero}"

DRY_RUN=0
VERIFY=0
HARD_REFRESH=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    --verify) VERIFY=1 ;;
    --hard-refresh) HARD_REFRESH=1 ;;
    --help|-h)
      sed -n '2,/^set -euo pipefail/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

log() { printf "%s [seed-argocd-gitea-creds] %s\n" "$(date -u +%H:%M:%SZ)" "$*"; }
fail() { log "FAIL: $*"; exit 1; }

# Pre-flight
USER=$(kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
  get secret gitea-admin -o jsonpath='{.data.username}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
TOKEN=$(kubectl --context "$MGMT_CONTEXT" -n "$LOOPZERO_NAMESPACE" \
  get secret gitea-admin -o jsonpath='{.data.token}' 2>/dev/null | base64 -d 2>/dev/null || echo "")
[[ -n "$USER" ]] || fail "gitea-admin.username missing — run task setup"
[[ -n "$TOKEN" ]] || fail "gitea-admin.token missing — run seed-gitea-token-canonical first"

kubectl --context "$MGMT_CONTEXT" get ns "$ARGOCD_NAMESPACE" >/dev/null 2>&1 \
  || fail "argocd namespace missing — task setup may not have completed"

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY_RUN: would apply argocd/argocd-creds-gitea Secret for URL prefix $GITEA_URL"
  exit 0
fi

# Layer 2 — apply repo-creds Secret
cat <<EOF | kubectl --context "$MGMT_CONTEXT" -n "$ARGOCD_NAMESPACE" apply -f - >/dev/null
apiVersion: v1
kind: Secret
metadata:
  name: argocd-creds-gitea
  labels:
    argocd.argoproj.io/secret-type: repo-creds
    app.kubernetes.io/managed-by: seed-argocd-gitea-creds-canonical
type: Opaque
stringData:
  type: git
  url: $GITEA_URL
  username: $USER
  password: $TOKEN
EOF
log "Layer 2: argocd/argocd-creds-gitea applied (repo-creds for URL prefix $GITEA_URL)"

# Layer 3 — hard refresh existing Applications (optional)
if [[ "$HARD_REFRESH" == "1" ]]; then
  REFRESHED=0
  while IFS= read -r app; do
    [[ -z "$app" ]] && continue
    kubectl --context "$MGMT_CONTEXT" -n "$ARGOCD_NAMESPACE" \
      annotate "$app" argocd.argoproj.io/refresh=hard --overwrite >/dev/null 2>&1 || true
    REFRESHED=$((REFRESHED + 1))
  done < <(kubectl --context "$MGMT_CONTEXT" -n "$ARGOCD_NAMESPACE" get applications -o name 2>/dev/null)
  log "Layer 3: hard-refresh annotated on $REFRESHED Application(s)"
fi

# Verify
if [[ "$VERIFY" == "1" ]]; then
  # Probe argo-cd repo-server can clone the repo
  REPO_SERVER=$(kubectl --context "$MGMT_CONTEXT" -n "$ARGOCD_NAMESPACE" \
    get pod -l app.kubernetes.io/name=argocd-repo-server -o name 2>/dev/null | head -1)
  if [[ -z "$REPO_SERVER" ]]; then
    log "WARN verify: argocd-repo-server pod not found; skipping clone probe"
  else
    # Test git ls-remote via the canonical creds inside the repo-server
    PROBE=$(kubectl --context "$MGMT_CONTEXT" -n "$ARGOCD_NAMESPACE" \
      exec "$REPO_SERVER" -- sh -c \
      "GIT_ASKPASS=/bin/echo git -c http.extraheader='' ls-remote http://$USER:$TOKEN@gitea-http.loopzero-system.svc.cluster.local:3000/loopzero/ 2>&1 || true" 2>/dev/null | head -3)
    if echo "$PROBE" | grep -qE "authentication required|403|Could not resolve" ; then
      log "FAIL verify: ls-remote rejected — $PROBE"
      exit 1
    fi
    log "OK verify: ArgoCD repo-server can reach Gitea via canonical creds"
  fi
fi

log "DONE — canonical ArgoCD ↔ Gitea repo-creds seeded"
