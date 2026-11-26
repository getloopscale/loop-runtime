#!/usr/bin/env bash
# Writes origin:<cell> into the routes key value store for every cell, valued with the DNSName of
# the ALB the traefik-edge Ingress made on that cell. The app router's function reads it to send
# a tenant app's request to its cell (infra-modules edge/app-router). The ALBs change with every
# rebuild, so `task staging:edge` runs this after the cells are up.
#
#   EDGE_KVS_ARN         the routes store (infra-live edge/app-routes)
#   EDGE_CLUSTER_PREFIX  the EKS cluster names' prefix; loopruntime-staging-cell-0 is cell-0
#   EDGE_ORIGINS_FILE    optional: also write one "<cell> <DNSName>" line per cell there, for
#                        Caddy, which sends a custom domain to its cell without the app router
# The aws CLI reaches the emulator through the AWS_* variables the caller exports.
set -euo pipefail

: "${EDGE_KVS_ARN:?}" "${EDGE_CLUSTER_PREFIX:?}"

read -ra arns <<<"$(aws elbv2 describe-load-balancers --query 'LoadBalancers[].LoadBalancerArn' --output text)"
cells=$(aws elbv2 describe-tags --resource-arns "${arns[@]}" --output json | jq -r --arg prefix "$EDGE_CLUSTER_PREFIX" '
  .TagDescriptions[]
  | select(any(.Tags[]; .Key == "ingress.k8s.aws/stack" and .Value == "traefik/traefik-edge"))
  | (.Tags[] | select(.Key == "elbv2.k8s.aws/cluster") | .Value) as $cluster
  | select($cluster | startswith($prefix + "cell-"))
  | "\($cluster | ltrimstr($prefix)) \(.ResourceArn)"')
if [ -z "$cells" ]; then
  echo "no traefik-edge ALB on a ${EDGE_CLUSTER_PREFIX}cell-* cluster" >&2
  exit 1
fi

origins=""
etag=$(aws cloudfront-keyvaluestore describe-key-value-store --kvs-arn "$EDGE_KVS_ARN" --query ETag --output text)
while read -r cell arn; do
  dns=$(aws elbv2 describe-load-balancers --load-balancer-arns "$arn" --query 'LoadBalancers[0].DNSName' --output text)
  etag=$(aws cloudfront-keyvaluestore put-key --kvs-arn "$EDGE_KVS_ARN" --key "origin:$cell" --value "$dns" \
    --if-match "$etag" --output json | jq -r .ETag)
  origins+="$cell $dns"$'\n'
  echo "origin:$cell -> $dns"
done <<<"$cells"
if [ -n "${EDGE_ORIGINS_FILE:-}" ]; then
  printf '%s' "$origins" | sort >"$EDGE_ORIGINS_FILE.tmp"
  mv "$EDGE_ORIGINS_FILE.tmp" "$EDGE_ORIGINS_FILE"
fi
