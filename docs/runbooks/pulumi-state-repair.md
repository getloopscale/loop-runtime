# Runbook: Pulumi State Repair

**Owners:** infra-engineer, sre-engineer
**Source:** Sprint 8 v3.2 / CHG-001 / 2026-04-27 (MEET-2026-04-27-060 derivation)
**Status:** STABLE — supersedes ad-hoc state surgery

---

## When to use

Pulumi state diverges from cloud reality, and `pulumi up` cannot reconcile automatically. Symptoms:

- `pulumi up` aborts with "Resource X already exists" but state has no entry
- `pulumi up` warns "5 pending operations from previous deployment" that never clear
- `pulumi up` tries to `delete X` then `create X` but cloud already has X
- `pulumi refresh` reports recursos that were destroyed externally
- Stack lock files persist after killed `pulumi up`

This runbook is the **canonical** procedure. **Do NOT** improvise state edits via `pulumi state delete` ad-hoc — repeated mistakes during Sprint 8 bootstrap (3 separate state surgery attempts) demonstrated the import/export workflow is safer.

## Pre-flight

```bash
# 1. Backup state IMMEDIATELY (chmod 600, never /tmp)
TS=$(date +%Y%m%d-%H%M%S)
mkdir -p ${HOME}/.loopzero/backups
chmod 700 ${HOME}/.loopzero/backups
pulumi stack export --stack <STACK> > ${HOME}/.loopzero/backups/state-pre-repair-${TS}.json
chmod 600 ${HOME}/.loopzero/backups/state-pre-repair-${TS}.json

# 2. Verify backup is valid JSON (not error output)
python3 -c "import json; json.load(open('${HOME}/.loopzero/backups/state-pre-repair-${TS}.json'))" && echo OK
```

If backup fails, **STOP**. State repair without backup is irresponsible.

## Scenario A: pending CREATE operations stuck

Symptom: `pulumi up` warns "Attempting to deploy or update resources with N pending operations from previous deployment" listing URNs as "interrupted while creating".

**Cause:** A previous `pulumi up` was killed (SIGTERM/SIGKILL) while creating resources. State recorded "started creating X" but never recorded "finished" — pulumi treats them as zombie operations.

**Procedure:**

```bash
# 1. Cancel any stuck lock
pulumi cancel --stack <STACK> --yes

# 2. Confirm pending operations exist
pulumi stack export --stack <STACK> | python3 -c "
import json, sys
d = json.load(sys.stdin)
ops = d['deployment'].get('pending_operations', [])
print(f'Pending: {len(ops)}')
for o in ops: print(f'  {o[\"type\"]}: {o[\"resource\"][\"urn\"]}')"

# 3. Strip pending operations (state-only edit)
pulumi stack export --stack <STACK> > /tmp/state-with-pending.json
python3 -c "
import json
d = json.load(open('/tmp/state-with-pending.json'))
n = len(d['deployment'].get('pending_operations', []))
d['deployment']['pending_operations'] = []
json.dump(d, open('/tmp/state-no-pending.json','w'))
print(f'Stripped {n} pending operations')"

# 4. Import cleaned state
pulumi stack import --stack <STACK> --file /tmp/state-no-pending.json

# 5. Refresh to reconcile remaining drift
pulumi refresh --stack <STACK> --yes --skip-preview
```

Verify: `pulumi stack export --stack <STACK> | python3 -c "import json,sys; print(len(json.load(sys.stdin)['deployment'].get('pending_operations',[])))"` returns `0`.

## Scenario B: duplicate URN in state

Symptom: `pulumi state delete <urn> --force` fails with "Resource URN ambiguously referred to multiple resources."

**Cause:** A replace+update sequence left two state entries with the same URN — typically when `import:` was passed inconsistently across runs.

**Procedure:**

```bash
# 1. Identify duplicates
pulumi stack export --stack <STACK> | python3 -c "
import json, sys
from collections import Counter
d = json.load(sys.stdin)
urns = Counter(r['urn'] for r in d['deployment']['resources'])
for urn, cnt in urns.items():
    if cnt > 1:
        print(f'DUP {cnt}x: {urn}')"

# 2. Filter ALL entries of the duplicate URN out of state (cleaner than picking one)
TARGET_URN='urn:pulumi:dev::loopzero-infra::cloudflare:index/dnsRecord:DnsRecord::loopzero-tunnel-dns'
pulumi stack export --stack <STACK> > /tmp/before.json
python3 -c "
import json, sys
d = json.load(open('/tmp/before.json'))
before = len(d['deployment']['resources'])
d['deployment']['resources'] = [r for r in d['deployment']['resources'] if r.get('urn') != '$TARGET_URN']
after = len(d['deployment']['resources'])
sys.stderr.write(f'Removed {before - after} duplicate entries\\n')
json.dump(d, sys.stdout)" > /tmp/after.json 2>&1

# 3. Verify JSON is valid (Python script puts stderr to stdout sometimes — separate streams)
python3 -c "import json; json.load(open('/tmp/after.json'))" && echo "JSON OK"

# 4. Import
pulumi stack import --stack <STACK> --file /tmp/after.json

# 5. pulumi up — pulumi recreates the resource if still wired in code
pulumi up --stack <STACK> --yes --skip-preview
```

**Caveat:** if the resource still exists in cloud (orphan), pulumi will fail to create with conflict. Either delete the cloud orphan first OR adopt via `import:` directive in source code (`infra.ts` pattern, see ADR-0015).

## Scenario C: state references resource that was destroyed externally

Symptom: `pulumi up` fails with `error: [ERROR] DELETE ...: 404 Not Found ... "Record does not exist"`.

**Cause:** Someone (you, another operator, automation) destroyed the cloud resource via `doctl`, `curl`, dashboard, etc. Pulumi state still tracks it.

**Procedure:**

```bash
# 1. pulumi refresh marks externally-destroyed resources as gone in state
pulumi refresh --stack <STACK> --yes --skip-preview

# 2. Re-run up — pulumi will create the resource fresh
pulumi up --stack <STACK> --yes --skip-preview
```

**If refresh doesn't clear the entry** (rare, happens with replacements mid-flight):

```bash
# Force-delete from state with --target-replace skip
pulumi state delete --stack <STACK> '<urn>' --target-dependents
```

## Scenario D: Pulumi backend lock from killed process

Symptom: `pulumi up` aborts with `error: the stack is currently locked by 1 lock(s).`

**Cause:** Previous `pulumi up` was killed (SIGKILL) before releasing the S3 lock file.

**Procedure:**

```bash
# Always safe — pulumi cancel ignores 404 if lock already gone
pulumi cancel --stack <STACK> --yes
```

## Scenario E: state import overwrites with stale data

Symptom: Import "completes" but `pulumi up` then wants to delete most resources (`-` prefix on most lines).

**Cause:** The state file you imported was older or corrupted. Pulumi sees vast diff = thinks user wants destroy.

**Procedure:**

```bash
# IMMEDIATE rollback: re-import the pre-repair backup
pulumi cancel --stack <STACK> --yes
pulumi stack import --stack <STACK> --file ${HOME}/.loopzero/backups/state-pre-repair-${TS}.json

# Verify
pulumi stack export --stack <STACK> | python3 -c "
import json, sys
d = json.load(sys.stdin)
print(f'Resources: {len(d[\"deployment\"][\"resources\"])}')"
```

If pre-repair backup is also bad: walk back through `${HOME}/.loopzero/backups/state-pre-up-*.json` files (these are taken before each `pulumi up`).

## Anti-patterns (DO NOT)

- `rm /path/to/.pulumi/locks/*.json` — bypasses pulumi's safety; multi-writer corruption
- `pulumi state delete --force <urn>` against duplicate URNs without first filtering — picks one randomly
- Editing state JSON manually without `python3 -c "import json"` validation — silent corruption
- Running `pulumi up` while another `pulumi up` is killed but lock still held
- Backups in `/tmp` (Security veto from MEET-2026-04-27-060)
- `pulumi stack rm <STACK> --force --yes` "to start fresh" — destroys all state including non-recoverable references

## Recovery from total state loss

If `loopzero-dev-pulumi-state` bucket is wiped and no local backup exists:

```bash
# Option 1: restore from another operator's local ${HOME}/.loopzero/backups/
# Option 2: re-bootstrap stack with adopt-or-create (`core/adopt-or-create.ts`)
#          handles VPC + Registry + CF resources gracefully
# Option 3: manual `pulumi import` for each cloud resource — slow but correct
```

ADR-0015 mandates adopt-or-create for stateful primitives precisely so total state loss is recoverable without losing data. Greenfield bootstrap with existing cloud = acceptable disaster recovery path.

## References

- ADR-0015 — Pre-bootstrap Adopt-or-Create Policy
- MEET-2026-04-27-060 — original incident yielding 3 ad-hoc state surgeries
- Pulumi docs: https://www.pulumi.com/docs/iac/concepts/state-and-backends/
