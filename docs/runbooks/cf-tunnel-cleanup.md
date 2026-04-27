# Runbook: Cloudflare Tunnel Cleanup

**Owners:** infra-engineer, platform-security-engineer
**Source:** Sprint 8 v3.2 / 2026-04-27
**Status:** STABLE

---

## Why this runbook exists

`pulumi destroy` of a stack carrying a CF Tunnel can fail with:

```
error: This tunnel has active connections. Please stop all cloudflared replicas,
or wait a few minutes for connections to close, then try again.
```

This is by-design. Cloudflare prevents accidental destruction of tunnels that still have running replicas serving live traffic. The fix is **operational** (drain cloudflared first), not a pulumi bug.

Sprint 8 v3.2 bootstrap surfaced this against `loopzero-dev` tunnel (8 active connections from runtime cluster pods).

## Pre-conditions for tunnel destroy

1. `cloudflared` Deployment(s) referencing this tunnel must be scaled to 0
2. All active connections must close (CF API reports `connections: []`)
3. No DNS records can still CNAME to `<tunnel-id>.cfargotunnel.com` (otherwise traffic resumes)

## Procedure

### Step 1: Identify tunnels and their connections

```bash
eval "$(grep -E '^[A-Z][A-Z_0-9]*=' /home/paulo/loopzero/.env | sed 's/^/export /')"

curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  "https://api.cloudflare.com/client/v4/accounts/$CLOUDFLARE_ACCOUNT_ID/cfd_tunnel?is_deleted=false" \
  | python3 -c "
import json, sys
d = json.load(sys.stdin)
for t in d.get('result', []):
    conn = len(t.get('connections', []))
    age = t.get('created_at', 'unknown')
    print(f\"{t['name']:30} id={t['id']:40} connections={conn:3} created={age}\")"
```

### Step 2: Identify cloudflared deployments

CF Tunnels are served by `cloudflared` pods. They may be deployed to:
- Runtime cluster (per Pulumi `bootstrap.ts:49-54`)
- Mgmt cluster (rare — only if MEET-2026-04-12-XYZ-style decision)
- Standalone droplet (legacy — pre-DOKS architecture)

```bash
# Search all DOKS clusters
for ID in $(doctl kubernetes cluster list --format ID --no-header); do
  NAME=$(doctl kubernetes cluster get $ID --format Name --no-header)
  KCFG=${HOME}/.kube/loopzero-${NAME}.yaml
  test -f "$KCFG" || doctl kubernetes cluster kubeconfig show $ID > "$KCFG"
  chmod 600 "$KCFG"
  echo "=== $NAME ==="
  KUBECONFIG=$KCFG kubectl get deploy -A -l app.kubernetes.io/name=cloudflared 2>/dev/null
done
```

### Step 3: Drain the cloudflared replicas

For each Deployment found:

```bash
KUBECONFIG=$KCFG kubectl scale deployment <name> -n <namespace> --replicas=0
KUBECONFIG=$KCFG kubectl rollout status deployment <name> -n <namespace> --timeout=120s
```

Wait ~60s for CF to register connections as closed. Verify via API:

```bash
curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  "https://api.cloudflare.com/client/v4/accounts/$CLOUDFLARE_ACCOUNT_ID/cfd_tunnel/<tunnel-id>" \
  | python3 -c "import json,sys; d=json.load(sys.stdin); print(f\"connections: {len(d['result'].get('connections',[]))}\")"
```

When count reaches 0, proceed.

### Step 4: Delete DNS records pointing to the tunnel

CF tunnels are typically referenced by CNAME records `*.<env>.<domain> -> <tunnel-id>.cfargotunnel.com`. Find and delete them:

```bash
ZONE_ID=9f9a41bef0bcb7adf15969c81c303bef  # loopzero.example zone

curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  "https://api.cloudflare.com/client/v4/zones/$ZONE_ID/dns_records?per_page=100" \
  | python3 -c "
import json, sys
d = json.load(sys.stdin)
for r in d.get('result', []):
    if 'cfargotunnel.com' in r.get('content', ''):
        print(f\"DELETE {r['type']:5} {r['name']:35} -> {r['content']} (id={r['id']})\")"

# Then delete each by id (or via pulumi destroy if state-tracked)
curl -s -X DELETE -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  "https://api.cloudflare.com/client/v4/zones/$ZONE_ID/dns_records/<record-id>"
```

### Step 5: Delete the tunnel

```bash
curl -s -X DELETE -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  "https://api.cloudflare.com/client/v4/accounts/$CLOUDFLARE_ACCOUNT_ID/cfd_tunnel/<tunnel-id>" \
  | python3 -c "import json,sys; d=json.load(sys.stdin); print('DELETED' if d.get('success') else f'ERR: {d}')"
```

### Step 6: Reconcile pulumi state

If the tunnel was tracked in pulumi state:

```bash
pulumi refresh --stack <STACK> --yes --skip-preview
```

`refresh` detects the externally-deleted tunnel and removes it from state without trying to destroy again.

## Orphan detection (preventive)

Run periodically:

```bash
eval "$(grep -E '^[A-Z][A-Z_0-9]*=' /home/paulo/loopzero/.env | sed 's/^/export /')"

curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  "https://api.cloudflare.com/client/v4/accounts/$CLOUDFLARE_ACCOUNT_ID/cfd_tunnel?is_deleted=false" \
  | python3 -c "
import json, sys
d = json.load(sys.stdin)
orphans = [t for t in d.get('result', []) if len(t.get('connections', [])) == 0]
if orphans:
    print('=== ORPHAN TUNNELS (zero connections, candidates for deletion) ===')
    for t in orphans:
        print(f\"{t['name']:30} created={t['created_at']}\")
else:
    print('No orphan tunnels detected.')"
```

Orphans incur **zero cost** but pollute the CF account namespace. Delete via Step 5 once confirmed dead.

## Sprint 8 v3.2 incident reference

On 2026-04-27, two CF tunnels coexisted in the account:

- `loopzero-dev` (8 active connections — current dev environment, in use)
- `loopzero-homolog` (0 connections — orphan from previous environment)

`loopzero-homolog` was deleted via Step 5 directly (no Steps 1-4 needed since 0 connections). `loopzero-dev` was preserved (active). The runbook applies to both scenarios.

## References

- Cloudflare API: https://developers.cloudflare.com/api/operations/cloudflare-tunnel-delete-a-cloudflare-tunnel
- ADR-0015 — Pre-bootstrap Adopt-or-Create Policy (covers tunnel adopt logic)
- `infra/pulumi/src/components/bootstrap.ts:49` — pulumi tunnel resource
- `infra/pulumi/src/core/adopt-or-create.ts:lookupExistingTunnel` — adoption logic
