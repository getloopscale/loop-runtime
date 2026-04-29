# Pulumi State Cleanup — Layered Bootstrap

> CHG-2026-04-28-LAYERED-BOOTSTRAP — T1.2

The current Pulumi state on `s3://loopzero-dev-pulumi-state` (and equivalent
prod backend) contains ~200 resources from the pre-refactor codebase
(L1 cloud + L2 K8s + L3 expose). After the T1.1 refactor, the source code
manages only the cloud resources (L1) but the state still has K8s zombies.

## Why this is needed

`pulumi destroy` walks every resource in the state. K8s resources whose
underlying cluster has been replaced cause:

1. Errors trying to delete (404 / connection refused)
2. Infinite retries by `@pulumi/kubernetes` provider
3. State surgery as the only recovery (manual `pulumi state delete`)

These are exactly the TEARDOWN problems #34, #35, #36 in the regression
matrix.

## Procedure

```bash
cd /home/paulo/loopzero/infra/pulumi

# 1. Export current state (safety backup)
mkdir -p ~/.loopzero/backups
pulumi stack export --stack dev > ~/.loopzero/backups/state-$(date +%Y%m%d-%H%M).json

# 2. Count K8s resources currently in the state
jq '[.deployment.resources[] | select(.type | startswith("kubernetes:")) | .urn] | length' \
  ~/.loopzero/backups/state-*.json

# 3. Count Helm resources
jq '[.deployment.resources[] | select(.type | startswith("helm:")) | .urn] | length' \
  ~/.loopzero/backups/state-*.json

# 4. Build a list of URNs to delete
jq -r '.deployment.resources[] | select(.type | startswith("kubernetes:") or startswith("helm:")) | .urn' \
  ~/.loopzero/backups/state-*.json > /tmp/k8s-urns.txt
wc -l /tmp/k8s-urns.txt

# 5. Iteratively delete (small batch — verify after each batch)
while read -r urn; do
  pulumi state delete --yes --stack dev "$urn" 2>&1 | tail -1
done < /tmp/k8s-urns.txt

# 6. Validate: preview must show ZERO pending K8s deletes
pulumi preview --stack dev 2>&1 | grep -c 'kubernetes:'
# expected: 0
```

## Alternative — Fresh stack import

When state is too polluted (>500 K8s resources) it is faster to:

```bash
pulumi stack init dev-v2
pulumi import --stack dev-v2 \
  digitalocean:KubernetesCluster mgmt <existing-cluster-id>
# ... import each surviving DO/CF resource
pulumi up --stack dev-v2
pulumi stack rm dev --yes  # after dev-v2 is healthy
```

## Status

This procedure is documented but the actual execution requires:
- Real Pulumi backend credentials (S3 access)
- Operator authorization (destructive change to live state)
- Out of scope for current session.
