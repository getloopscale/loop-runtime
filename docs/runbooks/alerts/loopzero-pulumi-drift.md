# Runbook: LoopZeroPulumiDrift

**Severity:** Warning
**Component:** Pulumi infrastructure stack
**SLO impact:** Infrastructure consistency, GitOps source of truth

## Alert expression

```
loopzero_pulumi_drift_resources_total > 0
```

Triggered when the daily drift-detection workflow finds resources whose actual state differs from the declared Pulumi state.

## What this means

Someone (or something) modified infrastructure outside Pulumi: manual `kubectl edit`, console click, or another automation. The Pulumi state is now out of sync with reality. Next `pulumi up` may revert valid changes or fail.

## Triage (first 5 min)

1. Open the GH Actions run for `infra-drift.yml`:
   ```
   gh run list --workflow=infra-drift.yml --limit=1
   gh run view <run-id> --log
   ```
2. Read the drift report (Pulumi outputs).
3. Identify which resources changed and who changed them (audit log + git blame on related repos).

## Diagnose

| Pattern | Likely cause |
|---------|--------------|
| K8s resource diff | manual `kubectl edit` or `kubectl patch` |
| DigitalOcean diff | Console click — node count, DB size |
| Cloudflare diff | Manual zone change, page rule edit |
| ArgoCD app diff | Customer environment with weird settings (expected for tenant Apps) |

## Mitigate

**Decision tree:**

1. **Drift is intentional and should be in code:**
   - Update Pulumi source (`infra/pulumi/`) to match.
   - Run `pulumi preview` to confirm no further drift.
   - Open PR.

2. **Drift is accidental, code is correct:**
   - Run `pulumi up` to reconcile (re-apply declared state).
   - Verify the manual change was not load-bearing.

3. **Drift is for tenant runtime resources:**
   - Expected — ArgoCD-managed Apps appear as drift in Pulumi if Pulumi imports the namespace.
   - Adjust drift detection scope to ignore namespaces matching `loopzero-*`.

**Never** run `pulumi up` blindly without preview.

## Verify recovery

- Next `infra-drift.yml` run reports zero drift
- Pulumi state matches GitOps source

## Escalation

If drift is in `kube-system` or platform-critical resources: page platform-architect immediately. Treat as security incident if attribution unknown.

## Related

- Workflow: `.github/workflows/infra-drift.yml`
- Pulumi source: `infra/pulumi/src/`
- CLAUDE.md: "ArgoCD skip-reconcile annotation lifecycle" pattern
