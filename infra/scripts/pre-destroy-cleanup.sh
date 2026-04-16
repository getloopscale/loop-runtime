#!/usr/bin/env bash
# pre-destroy-cleanup.sh — Removes finalizers and cloud resources before pulumi destroy.
#
# REVIEW-2026-03-30: Extracted from inline shell in infra-deploy.yml (INFRA-005).
# CRD/ArgoCD/Helm finalizers cause deadlocks during destroy — controllers being deleted
# can't process their own finalizers. This script removes them first.
#
# Required env vars:
#   DIGITALOCEAN_TOKEN       — DO API token
#   SPACES_ACCESS_KEY_ID     — DO Spaces access key
#   SPACES_SECRET_ACCESS_KEY — DO Spaces secret key
#   CLOUDFLARE_API_TOKEN     — CF API token (optional, for Sprint 10 resources)
#   STACK                    — Pulumi stack name (dev/staging/prod)
#   DOCTL_VERSION            — doctl version (default: 1.104.0)
#   PULUMI_WORKING_DIR       — Path to infra/pulumi/ directory
#
# Usage: STACK=dev bash infra/scripts/pre-destroy-cleanup.sh
set -o pipefail
# Intentionally NOT set -e — cleanup steps should continue even if individual ops fail.

: "${DIGITALOCEAN_TOKEN:?DIGITALOCEAN_TOKEN is required}"
: "${STACK:?STACK is required}"

echo "=== Pre-destroy cleanup for stack: $STACK ==="

# Derive stack-scoped prefix for safe filtering.
# Naming convention: loopzero-infra-{stack}-{region}[-suffix]
# e.g., loopzero-infra-dev-sfo3-mgmt, loopzero-infra-prod-nyc3-db
REGION=$(cd "${PULUMI_WORKING_DIR:-infra/pulumi}" && pulumi config get region --stack "organization/loopzero-infra/$STACK" 2>/dev/null || echo "sfo3")
PREFIX="loopzero-infra-${STACK}-${REGION}"
echo "Stack-scoped prefix: $PREFIX"

# --- Phase 1: Remove K8s finalizers ---
KUBECONFIG_DATA=$(pulumi stack output mgmtKubeconfig --show-secrets --stack "$STACK" 2>/dev/null)
if [ -z "$KUBECONFIG_DATA" ]; then
  echo "No mgmt kubeconfig — skipping K8s cleanup"
else
  echo "$KUBECONFIG_DATA" > /tmp/kubeconfig
  export KUBECONFIG=/tmp/kubeconfig

  echo "Removing finalizers from CRDs, ClusterIssuers, cert-manager resources..."
  for RESOURCE in applications appprojects tenants clustercredentials appsecrets previewenvironments reconciliationreports; do
    kubectl -n platform-system get "$RESOURCE" -o name 2>/dev/null | \
      xargs -I{} kubectl -n platform-system patch {} --type=json \
        -p '[{"op":"remove","path":"/metadata/finalizers"}]' 2>/dev/null || true
  done

  kubectl get clusterissuers -o name 2>/dev/null | \
    xargs -I{} kubectl patch {} --type=json \
      -p '[{"op":"remove","path":"/metadata/finalizers"}]' 2>/dev/null || true

  kubectl get crd -o name 2>/dev/null | \
    xargs -I{} kubectl patch {} --type=json \
      -p '[{"op":"remove","path":"/metadata/finalizers"}]' 2>/dev/null || true

  for TYPE in certificates certificaterequests orders challenges; do
    kubectl -n platform-system get "$TYPE" -o name 2>/dev/null | \
      xargs -I{} kubectl -n platform-system patch {} --type=json \
        -p '[{"op":"remove","path":"/metadata/finalizers"}]' 2>/dev/null || true
  done

  echo "Deleting Helm release secrets..."
  kubectl -n platform-system delete secrets -l owner=helm 2>/dev/null || true

  echo "Force-deleting namespaces on mgmt cluster..."
  for NS in platform-system argo; do
    kubectl delete ns "$NS" --force --grace-period=0 --timeout=30s 2>/dev/null || true
  done

  # Remote clusters
  for CLUSTER_KEY in buildKubeconfig runtimeKubeconfig; do
    KC=$(pulumi stack output "$CLUSTER_KEY" --show-secrets --stack "$STACK" 2>/dev/null)
    if [ -n "$KC" ]; then
      echo "$KC" > /tmp/kc-remote
      kubectl --kubeconfig=/tmp/kc-remote get ns -o name 2>/dev/null | \
        grep -v "kube-\|default" | \
        xargs -I{} kubectl --kubeconfig=/tmp/kc-remote delete {} \
          --force --grace-period=0 --timeout=30s 2>/dev/null || true
    fi
  done
fi

# --- Phase 2: Delete cloud resources via DO API ---
echo "Deleting cloud resources via DO API..."

# Install doctl if not available
if ! command -v doctl &>/dev/null; then
  DOCTL_VERSION="${DOCTL_VERSION:-1.104.0}"
  curl -sL "https://github.com/digitalocean/doctl/releases/download/v${DOCTL_VERSION}/doctl-${DOCTL_VERSION}-linux-amd64.tar.gz" | tar xz -C /usr/local/bin 2>/dev/null
fi
doctl auth init --access-token "$DIGITALOCEAN_TOKEN" 2>/dev/null

# Find VPC IDs for this stack (needed to scope LB deletion)
STACK_VPC_IDS=$(curl -s -H "Authorization: Bearer $DIGITALOCEAN_TOKEN" \
  "https://api.digitalocean.com/v2/vpcs" | \
  python3 -c "import sys,json; prefix='$PREFIX'; [print(v['id']) for v in json.load(sys.stdin)['vpcs'] if v['name'].startswith(prefix)]" 2>/dev/null || echo "")

# Load balancers (block VPC deletion) — only delete LBs in stack-scoped VPCs
for ID in $(curl -s -H "Authorization: Bearer $DIGITALOCEAN_TOKEN" \
  "https://api.digitalocean.com/v2/load_balancers" | \
  python3 -c "
import sys,json
vpc_ids = set('$STACK_VPC_IDS'.split())
lbs = json.load(sys.stdin).get('load_balancers',[])
[print(lb['id']) for lb in lbs if lb.get('vpc_uuid','') in vpc_ids]
" 2>/dev/null); do
  curl -s -X DELETE -H "Authorization: Bearer $DIGITALOCEAN_TOKEN" \
    "https://api.digitalocean.com/v2/load_balancers/$ID" 2>/dev/null
  echo "  LB $ID deleting"
done

# Kubernetes clusters
for ID in $(curl -s -H "Authorization: Bearer $DIGITALOCEAN_TOKEN" \
  "https://api.digitalocean.com/v2/kubernetes/clusters" | \
  python3 -c "import sys,json; prefix='$PREFIX'; [print(c['id']) for c in json.load(sys.stdin)['kubernetes_clusters'] if c['name'].startswith(prefix)]" 2>/dev/null); do
  curl -s -X DELETE -H "Authorization: Bearer $DIGITALOCEAN_TOKEN" \
    "https://api.digitalocean.com/v2/kubernetes/clusters/$ID?dangerous=true" 2>/dev/null
  echo "  Cluster $ID deleting"
done

# Database
for ID in $(curl -s -H "Authorization: Bearer $DIGITALOCEAN_TOKEN" \
  "https://api.digitalocean.com/v2/databases" | \
  python3 -c "import sys,json; prefix='$PREFIX'; [print(d['id']) for d in json.load(sys.stdin)['databases'] if d['name'].startswith(prefix)]" 2>/dev/null); do
  curl -s -X DELETE -H "Authorization: Bearer $DIGITALOCEAN_TOKEN" \
    "https://api.digitalocean.com/v2/databases/$ID" 2>/dev/null
  echo "  DB $ID deleting"
done

# Container registry — NEVER delete automatically.
# DO allows only 1 registry per account (shared across all stacks).
# retainOnDelete: true in Pulumi already prevents destroy from deleting it.
# Manual deletion only: doctl registry delete (requires confirmation).
echo "  Registry: skipped (shared resource, retainOnDelete)"

# Spaces buckets (stack-scoped prefix, skip pulumi-state)
# Bucket naming: loopzero-infra-{stack}-{region}[-suffix] (from naming.ts:bucketName)
for BUCKET_REGION in nyc3 sfo3 ams3 fra1 sgp1 lon1 sfo2 syd1; do
  for BUCKET in $(AWS_ACCESS_KEY_ID="${SPACES_ACCESS_KEY_ID}" \
    AWS_SECRET_ACCESS_KEY="${SPACES_SECRET_ACCESS_KEY}" \
    aws s3 ls --endpoint-url "https://${BUCKET_REGION}.digitaloceanspaces.com" \
    --region us-east-1 2>/dev/null | awk '{print $3}' | grep -E "^${PREFIX}" | grep -v pulumi-state); do
    EP="https://${BUCKET_REGION}.digitaloceanspaces.com"
    # Step 1: delete current objects
    AWS_ACCESS_KEY_ID="${SPACES_ACCESS_KEY_ID}" \
      AWS_SECRET_ACCESS_KEY="${SPACES_SECRET_ACCESS_KEY}" \
      aws s3 rm "s3://${BUCKET}" --recursive \
      --endpoint-url "$EP" --region us-east-1 2>/dev/null || true
    # Step 2: delete versioned objects + delete markers (versioned buckets
    # cannot be deleted until ALL versions are removed — aws s3 rm only
    # removes current versions, leaving old versions behind).
    AWS_ACCESS_KEY_ID="${SPACES_ACCESS_KEY_ID}" \
      AWS_SECRET_ACCESS_KEY="${SPACES_SECRET_ACCESS_KEY}" \
      aws s3api list-object-versions --bucket "${BUCKET}" \
      --endpoint-url "$EP" --region us-east-1 --output json 2>/dev/null | \
      python3 -c "
import json, sys, subprocess, os
data = json.load(sys.stdin)
env = {**os.environ, 'AWS_ACCESS_KEY_ID': os.environ.get('SPACES_ACCESS_KEY_ID',''), 'AWS_SECRET_ACCESS_KEY': os.environ.get('SPACES_SECRET_ACCESS_KEY','')}
for v in data.get('Versions', []) + data.get('DeleteMarkers', []):
    subprocess.run(['aws', 's3api', 'delete-object', '--bucket', '${BUCKET}', '--key', v['Key'], '--version-id', v['VersionId'], '--endpoint-url', '$EP', '--region', 'us-east-1'], env=env, capture_output=True)
" 2>/dev/null || true
    # Step 3: delete empty bucket
    AWS_ACCESS_KEY_ID="${SPACES_ACCESS_KEY_ID}" \
      AWS_SECRET_ACCESS_KEY="${SPACES_SECRET_ACCESS_KEY}" \
      aws s3 rb "s3://${BUCKET}" \
      --endpoint-url "$EP" --region us-east-1 2>/dev/null || true
    echo "  Bucket ${BUCKET} deleted from ${BUCKET_REGION}"
  done
done

echo "Cloud resources deletion initiated. Waiting 2 min for clusters/LBs..."
sleep 120

# VPC (after clusters + LBs are gone)
for ID in $(curl -s -H "Authorization: Bearer $DIGITALOCEAN_TOKEN" \
  "https://api.digitalocean.com/v2/vpcs" | \
  python3 -c "import sys,json; prefix='$PREFIX'; [print(v['id']) for v in json.load(sys.stdin)['vpcs'] if v['name'].startswith(prefix) and not v.get('default',False)]" 2>/dev/null); do
  RESP=$(curl -s -w "\n%{http_code}" -X DELETE -H "Authorization: Bearer $DIGITALOCEAN_TOKEN" \
    "https://api.digitalocean.com/v2/vpcs/$ID" 2>/dev/null)
  HTTP_CODE=$(echo "$RESP" | tail -1)
  if [ "$HTTP_CODE" = "409" ]; then
    echo "  VPC $ID has members, waiting 60s and retrying..."
    sleep 60
    for LBID in $(curl -s -H "Authorization: Bearer $DIGITALOCEAN_TOKEN" \
      "https://api.digitalocean.com/v2/load_balancers" | \
      python3 -c "
import sys,json
vpc_ids = set('$STACK_VPC_IDS'.split())
lbs = json.load(sys.stdin).get('load_balancers',[])
[print(lb['id']) for lb in lbs if lb.get('vpc_uuid','') in vpc_ids]
" 2>/dev/null); do
      curl -s -X DELETE -H "Authorization: Bearer $DIGITALOCEAN_TOKEN" \
        "https://api.digitalocean.com/v2/load_balancers/$LBID" 2>/dev/null
    done
    sleep 30
    curl -s -X DELETE -H "Authorization: Bearer $DIGITALOCEAN_TOKEN" \
      "https://api.digitalocean.com/v2/vpcs/$ID" 2>/dev/null
  fi
  echo "  VPC $ID deleting"
done

# DNS records — stack-scoped: only delete records matching {x}.{stack}.loopzero.example
DNS_SUFFIX=".${STACK}."
for ID in $(curl -s -H "Authorization: Bearer $DIGITALOCEAN_TOKEN" \
  "https://api.digitalocean.com/v2/domains/loopzero.example/records" | \
  python3 -c "
import sys,json
suffix='$DNS_SUFFIX'
stack='$STACK'
records = json.load(sys.stdin)['domain_records']
for r in records:
    name = r.get('name','')
    # Match: name ends with .{stack} (DO strips the domain suffix) or name == stack
    if (name.endswith('.'+stack) or name == stack) and r['type'] in ['A','CNAME']:
        print(r['id'])
" 2>/dev/null); do
  curl -s -X DELETE -H "Authorization: Bearer $DIGITALOCEAN_TOKEN" \
    "https://api.digitalocean.com/v2/domains/loopzero.example/records/$ID" 2>/dev/null
done
echo "  DNS records deleted (stack: $STACK)"

# --- Phase 3: Cloudflare resources (Sprint 10) ---
if [ -n "${CLOUDFLARE_API_TOKEN:-}" ]; then
  CF_ACCOUNT_ID=$(pulumi config get cfAccountId --stack "$STACK" 2>/dev/null || echo "")
  CF_ZONE_ID=$(pulumi config get cfZoneId --stack "$STACK" 2>/dev/null || echo "")
  if [ -n "$CF_ACCOUNT_ID" ] && [ -n "$CF_ZONE_ID" ]; then
    echo "Cleaning Cloudflare resources..."
    # Worker routes
    for ROUTE_ID in $(curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
      "https://api.cloudflare.com/client/v4/zones/$CF_ZONE_ID/workers/routes" | \
      python3 -c "import sys,json; [print(r['id']) for r in json.load(sys.stdin).get('result',[])]" 2>/dev/null); do
      curl -s -X DELETE -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
        "https://api.cloudflare.com/client/v4/zones/$CF_ZONE_ID/workers/routes/$ROUTE_ID" 2>/dev/null
    done
    # Worker script
    curl -s -X DELETE -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
      "https://api.cloudflare.com/client/v4/accounts/$CF_ACCOUNT_ID/workers/scripts/loopzero-static-delivery" 2>/dev/null
    # KV namespaces
    for NS_ID in $(curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
      "https://api.cloudflare.com/client/v4/accounts/$CF_ACCOUNT_ID/storage/kv/namespaces" | \
      python3 -c "import sys,json; stack='$STACK'; [print(ns['id']) for ns in json.load(sys.stdin).get('result',[]) if ns['title'].endswith('-'+stack)]" 2>/dev/null); do
      curl -s -X DELETE -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
        "https://api.cloudflare.com/client/v4/accounts/$CF_ACCOUNT_ID/storage/kv/namespaces/$NS_ID" 2>/dev/null
    done
    # DNS CNAME records
    for REC_ID in $(curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
      "https://api.cloudflare.com/client/v4/zones/$CF_ZONE_ID/dns_records?type=CNAME" | \
      python3 -c "import sys,json; stack='$STACK'; [print(r['id']) for r in json.load(sys.stdin).get('result',[]) if '.'+stack+'.' in r.get('name','') or r.get('name','').endswith('.'+stack)]" 2>/dev/null); do
      curl -s -X DELETE -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
        "https://api.cloudflare.com/client/v4/zones/$CF_ZONE_ID/dns_records/$REC_ID" 2>/dev/null
    done
    # Tunnels — scale down cloudflared first
    RUNTIME_KUBECONFIG=$(pulumi stack output runtimeKubeconfig --show-secrets --stack "$STACK" 2>/dev/null)
    if [ -n "$RUNTIME_KUBECONFIG" ]; then
      echo "$RUNTIME_KUBECONFIG" > /tmp/runtime-kc.yaml
      kubectl --kubeconfig=/tmp/runtime-kc.yaml -n platform-system scale deployment cloudflared --replicas=0 2>/dev/null || true
      echo "  Scaled cloudflared to 0 — waiting 30s..."
      sleep 30
      rm -f /tmp/runtime-kc.yaml
    fi
    for TUN_ID in $(curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
      "https://api.cloudflare.com/client/v4/accounts/$CF_ACCOUNT_ID/cfd_tunnel?name=loopzero-${STACK}&is_deleted=false" | \
      python3 -c "import sys,json; [print(t['id']) for t in json.load(sys.stdin).get('result',[])]" 2>/dev/null); do
      curl -s -X DELETE -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
        "https://api.cloudflare.com/client/v4/accounts/$CF_ACCOUNT_ID/cfd_tunnel/$TUN_ID/connections" 2>/dev/null
      sleep 5
      curl -s -X DELETE -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
        "https://api.cloudflare.com/client/v4/accounts/$CF_ACCOUNT_ID/cfd_tunnel/$TUN_ID" 2>/dev/null
    done
    echo "  Cloudflare resources deleted"
  fi
fi

echo "=== Pre-destroy cleanup complete ==="
