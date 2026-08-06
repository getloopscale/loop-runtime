---
title: "CI Runbook — T0.2 Cenário B (Argo Workflows + Argo Events)"
generated: { by: process:t0.2-plan-implementation, at: 2026-05-27 }
name: ci-runbook
description: Operator runbook for activating + operating LoopZero CI (T0.2 Cenário B — Argo Workflows + Argo Events). Covers activation, daily operation, troubleshooting, rollback to Cenário C.
type: operational-runbook
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: T0.2 plan implementation
---

# CI Runbook — T0.2 Cenário B (Argo Workflows + Argo Events)

> **Purpose:** Activate + operate LoopZero's PR-time enforcement CI without code-diving.
>
> **Per ADR-2026-05-27-T0.2-RATIFIED** (sponsor-ratified via `MEET-2026-05-27-batch-sponsor-approvals`).

## TL;DR

```bash
# Pre-flight: cluster + dependencies ready
task doctor   # 41/0

# Activate (sequence matters)
helmfile -e standalone-multi sync --selector name=argo-ci-namespace
helmfile -e standalone-multi sync --selector name=argo-events
helmfile -e standalone-multi sync --selector name=loopzero-ci-workflows

# Configure Gitea webhook (1× per repo)
# - URL: https://<eventsource-svc>/gitea-webhook
# - Secret: kubectl get secret -n argo-ci gitea-webhook-secret -o jsonpath='{.data.secret}' | base64 -d
# - Events: pull_request

# Done. Open a PR and watch.
kubectl get workflows -n argo-ci --watch
```

## Activation

### Step 1 — Pre-flight checks

| Check | Command | Expected |
|-------|---------|----------|
| Build cluster reachable | `kubectl --context k3d-loopzero-build get nodes` | All Ready |
| Argo Workflows running | `kubectl get pods -n argo -l app.kubernetes.io/name=argo-workflows-workflow-controller` | 1/1 Running |
| Gitea reachable | `curl -s http://gitea-http.loopzero-system.svc.cluster.local:3000/api/v1/version` | JSON response |
| ESO operational | `kubectl get externalsecrets -A` | All `Synced` |

### Step 2 — Create required Secrets

**Gitea webhook signing secret** (HMAC SHA256):
```bash
# Generate a strong random secret
SECRET=$(openssl rand -hex 32)

# Create via ESO ExternalSecret (operator preferred) OR direct kubectl
kubectl create secret generic gitea-webhook-secret \
  --namespace=argo-ci \
  --from-literal=secret="$SECRET"

# Note this value — needed in Gitea webhook UI (Step 4).
echo "WEBHOOK_SECRET=$SECRET" >> /tmp/ci-activation-secrets
```

**Gitea API token for status posting:**
```bash
# In Gitea UI: User Settings → Applications → Generate New Token
# Required scopes: repo (full), admin:repo_hook
TOKEN="<paste-token>"

kubectl create secret generic gitea-api-token \
  --namespace=argo-ci \
  --from-literal=token="$TOKEN"
```

### Step 3 — Helmfile sync sequence

**Order matters** — dependencies enforce themselves via Helmfile `needs:`, but explicit selector ordering also helps:

```bash
# 1. Namespace + RBAC + NetworkPolicy (blocks everything else)
helmfile -e standalone-multi sync --selector name=argo-ci-namespace

# 2. Argo Events controller + EventBus
helmfile -e standalone-multi sync --selector name=argo-events
# Verify EventBus pods Running before proceeding:
kubectl wait --for=condition=Ready pod -n argo-events -l controller=jetstream -A --timeout=300s

# 3. CI workflows (WorkflowTemplate + EventSource + Sensor + CronWorkflows)
helmfile -e standalone-multi sync --selector name=loopzero-ci-workflows
```

### Step 4 — Configure Gitea webhook (per repo)

In Gitea UI for the `loopzero` repo:

1. **Settings → Webhooks → Add Webhook → Gitea**
2. **Target URL:** `https://<eventsource-svc>.argo-ci.svc.cluster.local:12000/gitea-webhook`
   - For external Gitea (not in-cluster): operator creates Traefik Ingress
3. **HTTP Method:** POST
4. **POST Content Type:** application/json
5. **Secret:** value from `/tmp/ci-activation-secrets` (Step 2)
6. **Trigger On:** Custom Events → check "Pull Request"
7. **Active:** ✓
8. **Save**

Click "Test Delivery" — should receive HTTP 200.

### Step 5 — Set up branch protection

In Gitea UI:

1. **Repository Settings → Branches → Add Rule**
2. **Branch Name Pattern:** `develop` (or `main`)
3. **Require status checks to pass before merging:** ✓
4. **Status checks that must pass:** `quality` (per ADR D4 fixed name)
5. **Save**

PRs cannot be merged until the `quality` check posts success.

## Daily operation

### View CI runs

```bash
# All recent CI workflows
kubectl get workflows -n argo-ci --sort-by=.metadata.creationTimestamp

# Watch live
kubectl get workflows -n argo-ci --watch

# Get details
kubectl describe workflow -n argo-ci <workflow-name>

# Argo UI (cluster port-forward)
kubectl port-forward -n argo svc/argo-server 2746:2746
# Then: open https://localhost:2746
```

### Retry a failed CI run

Easiest path: in Gitea PR UI, click "Re-run checks" — Argo will re-fire on next webhook (push empty commit OR re-open PR).

Manual path:
```bash
# Find failed workflow
kubectl get workflows -n argo-ci -l loopzero.io/ci-pr-number=<N>

# Resubmit
kubectl create -f - <<EOF
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: pr-<N>-retry-
  namespace: argo-ci
spec:
  workflowTemplateRef:
    name: quality-gate
  arguments:
    parameters:
      - name: pr-number
        value: "<N>"
      - name: commit-sha
        value: "<sha>"
EOF
```

### CronWorkflows

| CronWorkflow | Schedule | Purpose |
|--------------|----------|---------|
| `drill-watchdog` | every 6h | Detect orphan drill stacks burning $/hr |
| `weekly-soak` | Sunday 02:00 UTC | Drill prod-identical L2 scale soak |
| `coverage-report` | Monday 09:00 UTC | Weekly coverage delta report |
| `cosign-deadline` | daily 00:00 UTC | Check cosign cutover deadline (ADR-2026-05-12) |

View next scheduled runs:
```bash
kubectl get cronworkflows -n argo-ci
```

Manually trigger:
```bash
argo cron create-now -n argo-ci <cron-name>
```

## Troubleshooting

### Webhook receives event but no Workflow fires

**Symptom:** Gitea webhook UI shows "Recent Deliveries" green, but `kubectl get workflows -n argo-ci` empty.

**Diagnosis:**
```bash
# Check EventSource pod logs
kubectl logs -n argo-ci -l eventsource-name=gitea-webhook --tail=50

# Check Sensor pod logs
kubectl logs -n argo-ci -l sensor-name=pr-trigger --tail=50

# Check EventBus (NATS) health
kubectl get pods -n argo-events -l controller=jetstream
kubectl logs -n argo-events -l controller=jetstream --tail=30
```

**Common causes:**

| Cause | Fix |
|-------|-----|
| HMAC signature mismatch | Verify `gitea-webhook-secret` value matches Gitea webhook UI secret |
| EventBus pod down | Restart NATS: `kubectl delete pod -n argo-events -l controller=jetstream` |
| Sensor RBAC missing | Verify `loopzero-ci-runner` RoleBinding allows `argoproj.io/workflows:create` |
| Event filter rejects payload | Check Sensor logs for "filter expression false"; verify `body.action` in payload matches filter list |

### Workflow fires but status not posted to Gitea PR

**Symptom:** Workflow completes (Succeeded/Failed) but PR check stuck in "pending" state.

**Diagnosis:**
```bash
# Check onExit handler pod logs
kubectl logs -n argo-ci -l workflow.argoproj.io/name=<workflow-name>,workflows.argoproj.io/template=post-status

# Check API token validity
kubectl get secret -n argo-ci gitea-api-token -o jsonpath='{.data.token}' | base64 -d | \
  xargs -I {} curl -s -H "Authorization: token {}" http://gitea-http.loopzero-system.svc.cluster.local:3000/api/v1/user
```

**Common causes:**

| Cause | Fix |
|-------|-----|
| `gitea-api-token` revoked | Regenerate in Gitea UI + update Secret |
| Repo path mismatch | Verify `REPO_PATH` env var or default `loopzero/loopzero` matches actual repo |
| Network policy blocks egress | Verify NetworkPolicy allows egress to gitea-http svc (Step 1 chart enables this) |
| Status posted but wrong context name | Confirm `STATUS_CONTEXT` env = `quality` (D4 fixed) |

### CronWorkflow not firing on schedule

**Diagnosis:**
```bash
kubectl describe cronworkflow -n argo-ci <name>
```

**Common causes:**

| Cause | Fix |
|-------|-----|
| `suspend: true` set | Patch: `kubectl patch cronworkflow -n argo-ci <name> --type=merge -p '{"spec":{"suspend":false}}'` |
| Previous run still active (Forbid policy) | Check `kubectl get workflows -n argo-ci -l workflows.argoproj.io/cron-workflow=<name>` |
| Argo Workflows controller down | Restart: `kubectl rollout restart deploy -n argo argo-workflows-workflow-controller` |

### Quality gate fails locally but passes in CI (or vice versa)

**Most likely:** `loopzero-ops` image in DOCR is stale.

```bash
# Rebuild + push
task images:build:loopzero-ops
task images:push:loopzero-ops

# Trigger workflow refresh
kubectl rollout restart deploy -n argo argo-workflows-workflow-controller
```

## Rollback to Cenário C (GHA SaaS CI-only)

Per ADR-2026-05-27-T0.2-RATIFIED, Cenário C remains documented fallback.
Rollback procedure (~30min):

### Step 1 — Disable Cenário B

```bash
# Uninstall loopzero-ci-workflows + argo-events + argo-ci-namespace
helmfile -e standalone-multi destroy --selector name=loopzero-ci-workflows
helmfile -e standalone-multi destroy --selector name=argo-events
helmfile -e standalone-multi destroy --selector name=argo-ci-namespace
```

### Step 2 — Activate Cenário C POC files (preserved per ADR D5)

```bash
# Rename POC files (preserved by ADR D5)
mv infra/helm/gitea/values-actions-poc.yaml.disabled infra/helm/gitea/values-actions-poc.yaml
mv .gitea/workflows/quality.yml.example .gitea/workflows/quality.yml

# OR migrate to GHA SaaS path:
mv .gitea/workflows/quality.yml.example .github/workflows/quality.yml
```

### Step 3 — Update sponsor decision record

Document rationale: why Cenário B execution failed, what's being tried instead.
Append ASYNC AMENDMENT to `MEET-2026-05-27-batch-sponsor-approvals`.

### Step 4 — Re-configure Gitea webhook OR re-litigate GHA SaaS

Cenário A (Gitea Actions): webhook stays, just register runner
Cenário C (GHA SaaS): sponsor reverses 2026-05-23 operator decision

## Related

- ADR: `.claude/knowledge-base/decisions/2026-05-27-T0.2-cenario-b-ratified.md`
- Meeting: `.claude/knowledge-base/meetings/MEET-2026-05-27-batch-sponsor-approvals.md`
- Plan: [`docs/plans/2026-05-27-t0.2-cenario-b-argo-workflows-ci-poc-plan.md`](../plans/2026-05-27-t0.2-cenario-b-argo-workflows-ci-poc-plan.md)
- AS-IS architecture: [`docs/architecture/ci-cd/argo-workflows-build-pipeline.md`](../architecture/ci-cd/argo-workflows-build-pipeline.md)
- PROPOSED architecture: [`docs/architecture/ci-cd/diff/real-ci-active.md`](../architecture/ci-cd/diff/real-ci-active.md)
- Cenário C fallback files (preserved):
  - `infra/helm/gitea/values-actions-poc.yaml.disabled`
  - `.gitea/workflows/quality.yml.example`
