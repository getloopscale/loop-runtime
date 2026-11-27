#!/usr/bin/env bash
# bootstrap-patch-argocd-repo-server-restart.sh
#
# B28 fix (real-paas-cleanroom-dogfood-loop iter 23, 2026-05-05).
# Extracted from Taskfile.yml bootstrap:standalone per T5.3 of plan
# 2026-05-21-deep-review-remediation.
#
# Problem:
#   Without restart, ArgoCD caches the previous (or empty) repo-creds
#   and returns `authentication required` when resolving tenant gitops
#   repo revisions, which fails post-build sync and marks the build FAILED.
#
# Fix:
#   Restart argocd-repo-server so it picks up the freshly written
#   repo-creds Secrets. Wait until rollout is complete.
#
# Pre-requisites:
#   - bootstrap-patch-argocd-gitea-creds.sh ran successfully
#
# Idempotent.

set -euo pipefail

CONTEXT="${KUBECONTEXT:-k3d-loop-runtime-core}"

kubectl --context "$CONTEXT" -n argocd rollout restart deploy/argocd-repo-server
kubectl --context "$CONTEXT" -n argocd rollout status deploy/argocd-repo-server --timeout=60s

echo "[bootstrap-patch-argocd-repo-server-restart] OK — argocd-repo-server restarted + Ready"
