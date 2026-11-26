#!/usr/bin/env bash
# bootstrap-patch-argocd-gitea-creds.sh
#
# B30 fix (real-paas-cleanroom-dogfood-loop iter 25, 2026-05-05).
# Extracted from Taskfile.yml bootstrap:standalone (T5.3 of plan
# 2026-05-21-deep-review-remediation) so the patch can be re-run
# independently when only this slice needs fixing.
#
# Problem:
#   generate-secrets.sh creates `gitea-credentials-template` with a
#   random GITEA_TOKEN and username `loop-runtime` (org name, not user). When
#   ArgoCD picks this template (URL prefix matches), HTTP basic auth
#   to Gitea fails with 401. provision-api-tokens.sh creates
#   `argocd-creds-gitea` with the real registered token and username
#   `gitea_admin`, but ArgoCD's first-match-wins order is undefined.
#
# Fix:
#   Sync `gitea-credentials-template` to use the same registered
#   username + password so either Secret produces a valid auth.
#
# Pre-requisites:
#   - generate-secrets.sh has run (creates gitea-credentials-template)
#   - provision-api-tokens.sh has run (creates argocd-creds-gitea
#     with the canonical credentials)
#
# Idempotent — patches same value on re-run.

set -euo pipefail

CONTEXT="${KUBECONTEXT:-k3d-loop-runtime-core}"

REAL_PWD=$(kubectl --context "$CONTEXT" -n argocd get secret argocd-creds-gitea -o jsonpath='{.data.password}' 2>/dev/null | base64 -d)
REAL_USER=$(kubectl --context "$CONTEXT" -n argocd get secret argocd-creds-gitea -o jsonpath='{.data.username}' 2>/dev/null | base64 -d)

if [[ -z "$REAL_PWD" ]] || [[ -z "$REAL_USER" ]]; then
  echo "ERROR: argocd-creds-gitea Secret missing or empty in $CONTEXT/argocd" >&2
  echo "       Run bash infra/scripts/production/provision-api-tokens.sh first." >&2
  exit 1
fi

kubectl --context "$CONTEXT" -n argocd patch secret gitea-credentials-template \
  --type=merge \
  -p "{\"stringData\":{\"username\":\"${REAL_USER}\",\"password\":\"${REAL_PWD}\"}}"

echo "[bootstrap-patch-argocd-gitea-creds] OK — gitea-credentials-template synced with argocd-creds-gitea"
