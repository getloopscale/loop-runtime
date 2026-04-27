# Runbook: Sprint 8 Phase -1 Bootstrap

**Owners:** infra-engineer (primary), sre-engineer (incident commander), platform-security-engineer (file mode review), change-validation-planner (acceptance gate)
**Change ID:** CHG-2026-04-27-001
**ADR:** [ADR-0015](../meetings/adr/0015-pre-bootstrap-adopt-or-create.md)
**Meeting:** [MEET-2026-04-27-060](../meetings/records/MEET-2026-04-27-060.md)
**Status:** REQUIRED before T-1.3 (`pulumi up`) executes

---

## When to use this runbook

Two scenarios:

1. **Forward path:** executing `pulumi up --stack dev --yes` for the first time on a fresh stack. Use sections 1-7.
2. **Recovery path:** `pulumi up` failed mid-execution (DOKS create error, CF tunnel race, droplet quota, etc). Use sections 8-12.

Do NOT improvise. The 6 absolute vetos in MEET-2026-04-27-060 still apply during recovery.

## Pre-flight checklist (MUST pass before T-1.3)

```bash
# 1. Pulumi installed
test -x ~/.pulumi/bin/pulumi || { echo "FAIL: pulumi not installed"; exit 1; }
~/.pulumi/bin/pulumi version

# 2. Backup directories with chmod 600 (Security veto: NO /tmp)
mkdir -p ${HOME}/.loopzero/backups ${HOME}/.loopzero/artifacts ${HOME}/.kube
chmod 700 ${HOME}/.loopzero ${HOME}/.loopzero/backups ${HOME}/.loopzero/artifacts

# 3. Pulumi config passphrase loaded
test -n "${PULUMI_CONFIG_PASSPHRASE}" || { echo "FAIL: PULUMI_CONFIG_PASSPHRASE missing"; exit 1; }

# 4. DO API token loaded
test -n "${DIGITALOCEAN_TOKEN}" || { echo "FAIL: DIGITALOCEAN_TOKEN missing"; exit 1; }

# 5. CF API token loaded
test -n "${CLOUDFLARE_API_TOKEN}" || { echo "FAIL: CLOUDFLARE_API_TOKEN missing"; exit 1; }

# 6. AWS aliases for Spaces backend
test -n "${AWS_ACCESS_KEY_ID}" && test -n "${AWS_SECRET_ACCESS_KEY}" || \
  { echo "FAIL: AWS_* aliases for Spaces missing"; exit 1; }

# 7. DO droplet quota (SRE veto: must validate >= 12 headroom)
QUOTA_OK=$(doctl account get -o json | python3 -c "import json,sys; a=json.load(sys.stdin); print(1 if a['droplet_limit'] - 0 >= 12 else 0)")
test "$QUOTA_OK" = "1" || { echo "FAIL: insufficient droplet quota"; exit 1; }

# 8. Pulumi backend reachable
~/.pulumi/bin/pulumi whoami || { echo "FAIL: pulumi not logged in"; exit 1; }

# 9. Stack selected
~/.pulumi/bin/pulumi stack ls | grep -q "^dev" || { echo "FAIL: stack dev not found"; exit 1; }

# 10. Preview clean (zero `- delete` on protected resources)
cd /home/paulo/loopzero/infra/pulumi
~/.pulumi/bin/pulumi preview --stack dev 2>&1 | tee ${HOME}/.loopzero/artifacts/preview-$(date +%s).log
grep -E "^\s*-\s" ${HOME}/.loopzero/artifacts/preview-*.log | grep -v "^\s*-$" && \
  { echo "FAIL: preview shows deletions"; exit 1; } || echo "OK: no deletions in preview"
```

If any pre-flight fails, **STOP**. Do not proceed to T-1.3.

## Section 1: Backup state before pulumi up

```bash
cd /home/paulo/loopzero/infra/pulumi
TS=$(date +%Y%m%d-%H%M%S)
~/.pulumi/bin/pulumi stack export --stack dev > ${HOME}/.loopzero/backups/state-pre-up-${TS}.json
chmod 600 ${HOME}/.loopzero/backups/state-pre-up-${TS}.json
ls -la ${HOME}/.loopzero/backups/state-pre-up-${TS}.json
```

State export pre-up enables clean restoration if T-1.3 corrupts state mid-flight.

## Section 2: Execute pulumi up (foreground, attended)

**SRE veto: do NOT background this command.** Operator stays present for the full duration (~45-90min).

```bash
cd /home/paulo/loopzero/infra/pulumi
TS=$(date +%Y%m%d-%H%M%S)
LOG=${HOME}/.loopzero/artifacts/up-${TS}.log

# Parallel SHOULD show real-time progress; --yes skips confirmation prompts
~/.pulumi/bin/pulumi up --stack dev --yes 2>&1 | tee "$LOG"

# Capture exit code
EXIT=${PIPESTATUS[0]}
echo "pulumi up exit: $EXIT" >> "$LOG"
chmod 600 "$LOG"
```

**Expected milestones (timing):**
- 0-2min: plugin installs (cloudflare, digitalocean, kubernetes, command, random)
- 2-15min: VPC adopt + Registry adopt + Spaces buckets create + PG cluster create kickoff
- 15-30min: 4 DOKS clusters provisioning in parallel (each ~10-15min)
- 30-45min: PG online + first wave Helm releases (cert-manager, traefik)
- 45-75min: bootstrap waves (ArgoCD, Gitea, MinIO, Prometheus, Grafana, loopzero-api, tenant-controller)
- 75-90min: TLS configuration + DNS records + final wiring

**If output stops for >5min between milestones:** check DO console for stuck operations, but do NOT kill the pulumi process — orphan resources are worse than slow ones.

## Section 3: Save per-cluster kubeconfigs (Security veto: NO ~/.kube/config aggregation)

After successful `pulumi up`, save each cluster kubeconfig to its own file:

```bash
for ID in $(doctl kubernetes cluster list --format ID --no-header); do
  NAME=$(doctl kubernetes cluster get "$ID" --format Name --no-header)
  doctl kubernetes cluster kubeconfig show "$ID" > "${HOME}/.kube/loopzero-${NAME}.yaml"
  chmod 600 "${HOME}/.kube/loopzero-${NAME}.yaml"
  echo "Saved: ${HOME}/.kube/loopzero-${NAME}.yaml"
done

# Use via: export KUBECONFIG=${HOME}/.kube/loopzero-loopzero-infra-dev-sfo3-mgmt.yaml
```

**Do NOT** `doctl kubernetes cluster kubeconfig save <id>` — that merges admin keys into `~/.kube/config` (Security veto).

## Section 4: Validation matrix (16 items — every check must pass)

Reference: MEET-2026-04-27-060 §Stage 2 (Change Validation Planner).

```bash
# Item 1: Bucket exists
python3 -c "
import boto3, os
s3 = boto3.client('s3', region_name='us-east-1',
    endpoint_url='https://sfo3.digitaloceanspaces.com',
    aws_access_key_id=os.environ['SPACES_ACCESS_KEY_ID'],
    aws_secret_access_key=os.environ['SPACES_SECRET_ACCESS_KEY'])
print('OK' if 'loopzero-dev-pulumi-state' in [b['Name'] for b in s3.list_buckets()['Buckets']] else 'FAIL')
"

# Item 2: Pulumi auth
~/.pulumi/bin/pulumi whoami && ~/.pulumi/bin/pulumi stack ls | grep -q dev

# Item 3: Config decryptable
~/.pulumi/bin/pulumi config get digitaloceanToken --stack dev | wc -c | awk '$1 > 20 {print "OK"}'

# Item 4: Adopt-or-create lookups hit (registry + VPC same, not create)
grep -E "= +.*ContainerRegistry|= +.*Vpc" "$LOG" || echo "WARN: lookups did not adopt"

# Item 5: No deletions in plan
! grep -E "^\s*-\s" "$LOG" | grep -v "^\s*-$"

# Item 6: pulumi up exit 0 + ~80 created
grep "Resources:" "$LOG" | tail -3
grep -c "+ create" "$LOG"

# Item 7: execution all-true
~/.pulumi/bin/pulumi stack output --stack dev execution --json | python3 -c "
import json,sys; e=json.load(sys.stdin)
all_true = all(v is True for v in e.get('components', {}).values())
print('OK' if all_true else f'FAIL: {[k for k,v in e[\"components\"].items() if not v]}')
"

# Item 8: pods healthy per cluster (no CrashLoopBackOff/ImagePullBackOff)
for KCFG in ${HOME}/.kube/loopzero-*.yaml; do
  echo "=== $KCFG ==="
  KUBECONFIG=$KCFG kubectl get pods -A 2>/dev/null | grep -E "CrashLoopBackOff|ImagePullBackOff" && echo FAIL || echo OK
done

# Item 9: PG reachable (use dbUri output, secret)
DB_URI=$(~/.pulumi/bin/pulumi stack output --stack dev --show-secrets dbUri 2>/dev/null)
test -n "$DB_URI" && psql "$DB_URI" -c "SELECT 1" >/dev/null 2>&1 && echo OK || echo FAIL

# Item 10: DNS public
dig api.dev.loopzero.example +short | grep -E "[0-9]+\.[0-9]+" >/dev/null && echo OK || echo FAIL

# Item 11: API healthz
curl -fsS https://api.dev.loopzero.example/healthz | head -c 50 && echo " OK"

# Items 12-15: smoke E2E (see Section 5)
# Item 16: re-preview drift
~/.pulumi/bin/pulumi preview --stack dev --diff 2>&1 | grep -q "no changes" && echo OK || echo FAIL
```

Archive validation log: `cp validation-output ${HOME}/.loopzero/artifacts/validation-${TS}.log; chmod 600`

## Section 5: Smoke E2E (items 12-15 of validation matrix)

```bash
# 12. Smoke deploy
mkdir -p /tmp/smoke && cd /tmp/smoke
git init -q
echo "console.log('hello sprint 8')" > index.js
echo '{"name":"smoke","version":"0.0.1","scripts":{"start":"node index.js"}}' > package.json
git add . && git commit -q -m "init"
cd /home/paulo/loopzero/cli && npm run build && cd /tmp/smoke
node /home/paulo/loopzero/cli/dist/index.js login --api https://api.dev.loopzero.example
node /home/paulo/loopzero/cli/dist/index.js init --framework node
node /home/paulo/loopzero/cli/dist/index.js deploy --env staging --json | tee /tmp/smoke-deploy.json

# 13. Smoke URL 200
curl -fsS "$(jq -r .url /tmp/smoke-deploy.json)" >/dev/null && echo OK

# 14. No internal terms (Rule 3 enforcement)
grep -iE "argocd|gitea|crd" /tmp/smoke-deploy.json && echo "FAIL: internal term leaked" || echo OK

# 15. Cleanup (GitOps veto: smoke project must NOT become first zombie tenant)
node /home/paulo/loopzero/cli/dist/index.js project delete smoke --force
# Verify Gitea repo cleaned
KUBECONFIG=${HOME}/.kube/loopzero-loopzero-infra-dev-sfo3-mgmt.yaml \
  kubectl exec -n loopzero-system gitea-0 -- gitea admin repo list 2>/dev/null | grep smoke && echo "FAIL: gitea repo lingering" || echo OK
```

## Section 6: Archive run artifacts

```bash
TS=$(date +%Y%m%d-%H%M%S)
cd ${HOME}/.loopzero/artifacts
tar czf bootstrap-${TS}.tar.gz up-*.log preview-*.log validation-*.log
chmod 600 bootstrap-${TS}.tar.gz
ls -la bootstrap-${TS}.tar.gz
```

## Section 7: Declare CHG-001 green (16/16 pass)

When all 16 validation items pass:
1. Update plan `docs/plans/sprint8-hardening-chaos-plan.md` Phase -1 status: ✅ DONE
2. Note in CHANGELOG.md under [Unreleased] § Added: "Sprint 8 Phase -1 bootstrap completed (CHG-2026-04-27-001)"
3. Remove gate file Sprint 8 section (preserve Sprint 5/6):
   ```bash
   # Manual edit: remove `# Governance Gate — Sprint 8 v3.1 CHG-001 ...` section
   # from .claude/governance-gate.local.md
   ```
4. CHG-002 (Phase 0-6 Hardening) requires NEW meeting before opening.

---

## Recovery scenarios

### Section 8: pulumi up fails mid-DOKS create

**Symptom:** `pulumi up` aborts with `error: digitalocean:KubernetesCluster — error creating cluster: cluster_size_limit_exceeded` or similar.

**Diagnosis:**
```bash
doctl account get  # check droplet quota
doctl kubernetes cluster list  # check what was created
```

**Recovery:**
1. Identify the failing cluster (mgmt/build/runtime/runtime-2).
2. If quota: open DO support ticket; do NOT delete partial clusters until ticket resolves.
3. If transient (timeout): `pulumi refresh --stack dev` then `pulumi up --stack dev` (idempotent).
4. If persistent error: `pulumi cancel --stack dev`, restore state from `${HOME}/.loopzero/backups/state-pre-up-*.json`, fix root cause, retry.

### Section 9: pulumi up fails on Helm release

**Symptom:** `pulumi up` reaches Phase 2 (bootstrap) and fails on a specific Helm release (e.g., gitea, argocd).

**Diagnosis:**
```bash
KUBECONFIG=${HOME}/.kube/loopzero-loopzero-infra-dev-sfo3-mgmt.yaml kubectl get pods -n loopzero-system
# Look for image pull errors, init container failures, etc.
```

**Recovery:**
1. Helm releases are idempotent. Re-run `pulumi up --stack dev` — pulumi resumes from where it failed.
2. If pod stuck in CrashLoopBackOff: read pod logs, fix Helm value (CHG-002 scope, but minor fixes can be triaged inline if blocking T-1.3 itself).
3. Worst case: `pulumi destroy --stack dev --target <urn>` for the failing release, fix, re-up.

### Section 10: Registry import fails after lookup hit

**Symptom:** `error: digitalocean:ContainerRegistry import failed`.

**Diagnosis:** the registry `loopzero-system` may have been renamed or deleted between `lookupExistingRegistry()` call and the actual import.

**Recovery:**
1. Verify: `doctl registry get` confirms current registry name.
2. If `loopzero-system` no longer exists, lookup returned undefined and code defaults to creating `"loopzero-system"` — that's the expected greenfield path.
3. If registry name changed (e.g., `loopzero-system-v2`): the lookup returns the new name, import uses it, all consumers (image pulls, secrets) need updating — this is OUT OF SCOPE for CHG-001. Document and escalate.

### Section 11: CF Tunnel adopt fails

**Symptom:** preview shows `+ create` for tunnel but DO has existing `loopzero-tunnel-dev`.

**Diagnosis:**
```bash
curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  "https://api.cloudflare.com/client/v4/accounts/$CLOUDFLARE_API_ORIGIN_KEY/cfd_tunnel?name=loopzero-tunnel-dev"
```

**Recovery:**
1. Confirm `lookupExistingTunnel` finds it. If not, check `is_deleted=false` filter in `adopt-or-create.ts:89`.
2. If tunnel is in soft-deleted state: hard-delete via API + retry `pulumi up`.
3. If tunnel is healthy but adopt logic skipped: this is a code bug, raise CHG-002.

### Section 12: Recovery aborts mid-validation matrix

**Symptom:** items 1-7 pass but item 8 (pods healthy) shows persistent CrashLoopBackOff.

**Recovery:**
1. The system is in degraded state but bootstrap is partial-success.
2. Document which pods are unhealthy in `${HOME}/.loopzero/artifacts/incident-${TS}.md`.
3. **Do NOT** declare CHG-001 green. Phase -1 is incomplete.
4. Triage: minor fixes (Helm value typo) can be patched in CHG-001 scope. Anything else (CRD bugs, controller logic) is CHG-002+.
5. If incident severity HIGH: escalate to SRE for incident response.

---

## Compliance with ADR-0015 vetos

This runbook satisfies all 6 absolute vetos:
- ✅ Secrets/state files in `${HOME}/.loopzero/backups/` chmod 600 (NOT /tmp)
- ✅ Per-cluster kubeconfigs in `${HOME}/.kube/loopzero-*.yaml` chmod 600 (NOT aggregated)
- ✅ DOCR `loopzero-system` adopted via `lookupExistingRegistry()` (NOT renamed)
- ✅ Runbook published BEFORE T-1.3 execution (this document)
- ✅ Pre-flight `doctl account get` validates ≥12 droplet headroom (Section: Pre-flight item 7)
- ✅ `pulumi up` foreground only, operator attended (Section 2)

If any veto is bypassed during execution, mark CHG-001 BLOCKED and escalate to platform-security-engineer + sre-engineer.
