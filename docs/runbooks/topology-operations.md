# Topology Operations Runbook

> How to add, remove, and scale clusters using declarative topology config.

---

## Add a Runtime Cluster (New Cell)

1. Edit `infra/pulumi-infra/Pulumi.dev.yaml`:

```yaml
config:
  loopzero-infra:topology:
    # ... existing config ...
    runtime:
      - id: runtime-default
        cellId: cell-default
        region: sfo3
        nodeClass: small
        minNodes: 2
        maxNodes: 3
      # ADD THIS:
      - id: runtime-nyc3-01
        cellId: cell-nyc3-01
        region: nyc3
        nodeClass: medium
        minNodes: 2
        maxNodes: 4
```

2. Preview changes:
```bash
cd infra/pulumi-infra && pulumi preview --stack dev
```

Expected: 1 new K8s cluster + updated DB firewall (trusted sources).

3. Apply:
```bash
pulumi up --stack dev --yes
```

4. Verify:
```bash
pulumi stack output clusterRegistry --json | jq '.["runtime-nyc3-01"]'
```

5. Bootstrap the new cell's addons:
```bash
cd infra/pulumi-platform && pulumi up --stack dev --yes
```

---

## Add a Build Cluster

Same pattern as runtime, but in `build[]`:

```yaml
build:
  - id: build-default
    region: sfo3
    nodeClass: medium
    minNodes: 2
  # ADD THIS:
  - id: build-nyc3
    region: nyc3
    nodeClass: large
    minNodes: 3
    maxNodes: 10
```

---

## Remove a Cluster

1. Remove the entry from `topology` in `Pulumi.<stack>.yaml`.
2. `pulumi preview` — should show the cluster being destroyed.
3. **Before applying**: migrate workloads off the cluster.
4. `pulumi up --yes`

**WARNING**: Removing a runtime cluster destroys all workloads on it.
Drain the cell first (migrate tenants, cancel builds, reassign cell).

---

## Scale a Cluster

Change `minNodes`, `maxNodes`, or `nodeClass` in the topology:

```yaml
runtime:
  - id: runtime-default
    cellId: cell-default
    region: sfo3
    nodeClass: medium  # was: small
    minNodes: 3        # was: 2
    maxNodes: 5        # was: 3
```

This triggers a node pool update (not a cluster replacement).

---

## Rename a Cluster ID

**⚠️ DESTRUCTIVE OPERATION**

Cluster ID is the Pulumi resource name. Renaming it:
- Destroys the old cluster
- Creates a new cluster
- Loses all workloads and PVCs

To safely rename:
1. Add the new cluster with the new ID
2. Migrate workloads from old to new
3. Remove the old cluster entry

---

## Verify Topology Health

```bash
# All clusters in registry
pulumi stack output clusterRegistry --json | jq 'keys'

# Cell registry
pulumi stack output cellRegistry --json | jq 'keys'

# Execution state
pulumi stack output execution --json | jq '.phases[0].components'
```

---

## Legacy Mode (No Topology Config)

If `loopzero-infra:topology` is not set, the loader falls back to singleton config fields:
- `mgmtNodeClass`, `mgmtMinNodes`, `mgmtMaxNodes`
- `runtimeNodeClass`, `runtimeMinNodes`, `runtimeMaxNodes`
- `buildNodeClass`, `buildMinNodes`, `buildMaxNodes`

This creates exactly 1 cluster per role with default IDs:
- `mgmt-default`, `build-default`, `runtime-default` (cellId: `cell-default`)

To migrate to topology mode, add the `topology` key to your stack config.
The legacy fields will be ignored once `topology` is present.
