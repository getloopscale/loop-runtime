# Runbook: Pulumi State Recovery

**When to use:** Pulumi commands fail with state-related errors (pending operations, snapshot integrity, lock conflicts, phantom resources).

## Decision Tree

```
Error detected
  │
  ├─ "locked by N lock(s)"
  │   └─ pulumi cancel --stack <stack> --yes
  │      Then retry your command.
  │
  ├─ "pending operations from previous deployment"
  │   ├─ pulumi refresh --stack <stack> --yes --clear-pending-creates
  │   └─ If still broken → Manual export/edit (see below)
  │
  ├─ "snapshot integrity" / corrupt state
  │   ├─ pulumi state repair --stack <stack>
  │   └─ If repair fails → Restore from S3 versioned backup
  │
  ├─ Resource exists in cloud but not in state ("phantom")
  │   └─ pulumi import <type> <name> <cloud-id> --stack <stack>
  │      (Resource is auto-protected after import)
  │
  ├─ Resource in state but deleted from cloud
  │   └─ pulumi refresh --stack <stack> --yes
  │
  └─ Resource with wrong properties
      └─ pulumi refresh --stack <stack> --yes
```

## Manual Export/Edit/Import (Last Resort)

```bash
# 1. Backup current state
pulumi stack export --stack <stack> --file backup-$(date +%Y%m%d-%H%M%S).json

# 2. Export working copy
pulumi stack export --stack <stack> --file state.json

# 3. Edit state.json:
#    - Clear: "pending_operations": []
#    - Remove phantom resources from "resources": [...]
#    - NEVER touch "version" or "manifest" fields

# 4. Import edited state
pulumi stack import --stack <stack> --file state.json

# 5. Verify
pulumi refresh --stack <stack> --yes
pulumi preview --stack <stack>
```

## Prevention Rules

| Rule | Why |
|------|-----|
| NEVER Ctrl+C a `pulumi up/destroy` | Creates pendingOperations. Use `pulumi cancel`. |
| NEVER run 2 Pulumi commands in parallel | Lock contention → state corruption. |
| NEVER `pulumi state delete --force` without deleting real resource first | State says "gone", cloud says "exists" → conflict on next up. |
| ALWAYS `pulumi refresh` before `pulumi up` | Syncs state with cloud reality. Catches manual changes. |
| ALWAYS use CI for `pulumi up` | Local runs risk stale credentials and parallel conflicts. |

## DO Spaces Performance

DO Spaces rate-limits API calls, making Pulumi slow. Mitigation:

```bash
export PULUMI_SKIP_CHECKPOINTS=true  # 10x faster, skips intermediate state saves
```

Safe because: if process dies mid-operation, `pulumi refresh` recovers.

## Backup & Restore

The state bucket `loopzero-infra-pulumi-state` should have **versioning enabled**:

```bash
# Enable versioning (one-time)
aws s3api put-bucket-versioning \
  --bucket loopzero-infra-pulumi-state \
  --versioning-configuration Status=Enabled \
  --endpoint-url https://nyc3.digitaloceanspaces.com

# List versions (to find a good backup)
aws s3api list-object-versions \
  --bucket loopzero-infra-pulumi-state \
  --prefix .pulumi/stacks/organization/loopzero-infra/homolog.json \
  --endpoint-url https://nyc3.digitaloceanspaces.com

# Restore specific version
aws s3api get-object \
  --bucket loopzero-infra-pulumi-state \
  --key .pulumi/stacks/organization/loopzero-infra/homolog.json \
  --version-id <version-id> \
  /tmp/restored-state.json \
  --endpoint-url https://nyc3.digitaloceanspaces.com
pulumi stack import --stack homolog --file /tmp/restored-state.json
```

## Common Scenarios

### After manual kubectl/doctl changes
```bash
pulumi refresh --stack homolog --yes  # Syncs state with reality
pulumi preview --stack homolog        # Shows what `up` would change
# If preview is clean → done
# If preview shows unwanted changes → investigate
```

### After failed destroy (orphan resources)
```bash
# 1. Check what exists in DO
doctl kubernetes cluster list
doctl databases list
doctl compute droplet list

# 2. Delete orphans via doctl (NOT pulumi state delete)
doctl kubernetes cluster delete <name> --force --dangerous

# 3. Refresh state
pulumi refresh --stack homolog --yes

# 4. Check orphan droplets (DOKS leaves nodes behind)
doctl compute droplet list | grep loopzero-infra
# Delete each orphan (NEVER delete loopzero-code-vm!)
```

### Total reset (nuclear option)
```bash
# Use task wrapper (has audit trail):
task infra:state-delete URN=<urn> REASON="total reset after corruption"

# Or the E2E pattern (wipes all resources from state):
pulumi stack export --stack homolog --file state.json
python3 -c "
import json
with open('state.json') as f: d = json.load(f)
d['deployment']['resources'] = [r for r in d['deployment']['resources'] if r.get('type') == 'pulumi:pulumi:Stack']
d['deployment']['pending_operations'] = []
with open('state.json', 'w') as f: json.dump(d, f)
"
pulumi stack import --stack homolog --file state.json
pulumi up --stack homolog --yes  # Recreates everything
```
