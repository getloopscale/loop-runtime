#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# restart-uninjected-pods.sh <context>...
#
# Restarts each Deployment that runs a pod the emulator's EKS layer never wired to the
# emulator: the pod carries an IRSA role (AWS_ROLE_ARN) and no AWS_ENDPOINT_URL.
#
# The emulator's EKS provisioner adds the endpoint, its CA and AWS_ENDPOINT_URL to a pod
# when the pod is created, through a mutating webhook, and never to a pod that already
# runs. A pod created before the provisioner's webhook covered its namespace (kube-system
# is covered by a webhook of its own, which an earlier provisioner did not register)
# keeps calling the real AWS endpoints. Measured on 2026-11-23: the AWS Load Balancer
# Controller of build, cell-0 and cell-1 failed every reconcile with `STS:
# AssumeRoleWithWebIdentity ... InvalidIdentityToken`, so their Ingresses never got a
# load balancer. On EKS every pod with a role is wired by EKS itself, so a production
# environment has nothing for this to do.
set -euo pipefail

[[ $# -ge 1 ]] || { echo "usage: $0 <context>..." >&2; exit 2; }

for ctx in "$@"; do
  pods="$(kubectl --context "$ctx" get pods --all-namespaces -o json)"
  # The namespace and Deployment of each such pod: its ReplicaSet minus the pod-template-hash.
  mapfile -t stale < <(jq -r '
    .items[]
    | select([.spec.containers[].env[]?.name] | index("AWS_ROLE_ARN"))
    | select([.spec.containers[].env[]?.name] | index("AWS_ENDPOINT_URL") | not)
    | .metadata.namespace as $ns
    | (.metadata.labels["pod-template-hash"] // "") as $hash
    | select($hash != "")
    | .metadata.ownerReferences[]? | select(.kind == "ReplicaSet")
    | "\($ns) \(.name | sub("-" + $hash + "$"; ""))"
    ' <<<"$pods" | sort -u)
  for entry in "${stale[@]}"; do
    read -r namespace deployment <<<"$entry"
    echo "restart-uninjected-pods: $ctx $namespace/$deployment runs a pod with an IRSA role and no AWS_ENDPOINT_URL; restarting it" >&2
    kubectl --context "$ctx" -n "$namespace" rollout restart "deployment/$deployment"
    kubectl --context "$ctx" -n "$namespace" rollout status "deployment/$deployment" --timeout=5m
  done
done
