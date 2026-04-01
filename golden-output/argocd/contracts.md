# ArgoCD Behavioral Contracts for LoopZero

These contracts define the behavior LoopZero depends on from ArgoCD, derived from
the ArgoCD reference test suite at `referencias/argo-cd`.

---

## Contract 1: Webhook Payload Format

**ID**: `contract_argocd_sync_callback_payload`
**Domain**: sync-callbacks
**Type**: interface

### Payload Schema

```json
{
  "app_name":     "<argocd-application-name>",
  "sync_status":  "Synced | OutOfSync",
  "health_status": "Healthy | Progressing | Degraded | Missing | Unknown | Suspended",
  "revision":     "<git-commit-sha>"
}
```

### Invariants

1. `app_name` is the ArgoCD Application resource name, format `{tenantID}-{environment}`.
2. `sync_status` is `Synced` only when all resources match the desired git state.
3. `health_status` is `Healthy` only when all non-hook resources pass their health checks.
4. `revision` is the git commit SHA that was synced. May be empty string if not configured in trigger.
5. LoopZero MUST require `sync_status=Synced` AND `health_status=Healthy` before activating a release. Either alone is insufficient.

### Failure Modes

- ArgoCD sends intermediate notifications (Progressing, OutOfSync) — LoopZero must ignore these.
- Multiple notifications for the same sync event — handler must be idempotent.
- `revision` field may be absent in older ArgoCD versions — treat missing/empty as "skip revision check".
- Network retry by ArgoCD notification controller — always return HTTP 200.

---

## Contract 2: Application Name Convention

**ID**: `contract_argocd_app_name_format`
**Domain**: sync-callbacks
**Type**: naming

### Format

```
{tenantID}-{environment}
```

Examples: `tn-loopzero-production`, `acme-staging`, `my-tenant-dev-preview`

### Parsing Rule

Split on the **last** hyphen. Everything before the last hyphen is `tenantID`; the last segment is `environment`.

This allows tenantIDs that contain hyphens (e.g., `tn-loopzero`).

### Valid / Invalid

| Input | Valid | tenantID | environment |
|-------|-------|----------|-------------|
| `acme-staging` | yes | `acme` | `staging` |
| `tn-loopzero-production` | yes | `tn-loopzero` | `production` |
| `my-tenant-with-dashes-production` | yes | `my-tenant-with-dashes` | `production` |
| `` (empty) | no | — | — |
| `nohyphen` | no | — | — |
| `-leading` | no | — | — |

---

## Contract 3: Application Health Status Semantics

**ID**: `contract_argocd_health_statuses`
**Domain**: application-health
**Type**: enum

### Status Definitions

| Status | Meaning | LoopZero Action |
|--------|---------|-------------|
| `Healthy` | All non-hook resources passing health checks | Activate PENDING release |
| `Progressing` | Resources are being applied or rolling out | Wait — do not activate |
| `Degraded` | One or more non-hook resources failed | Do not activate — emit sync_failed |
| `Missing` | Expected resources not present in cluster | Do not activate — still applying |
| `Unknown` | Health cannot be determined | Do not activate — wait |
| `Suspended` | Intentionally paused | Do not activate |

### Health Aggregation Rules (from `controller/health_test.go`)

1. If ANY non-hook resource is Degraded → application is Degraded.
2. If ALL resources are Missing (Target declared, no Live) → application is Missing.
3. If zero resources → application is Healthy.
4. Hook resources (annotated with `argocd.argoproj.io/hook`) are excluded from aggregation.
5. A failed job that is a sync hook does NOT cause Degraded.
6. A Degraded child Application DOES propagate to parent.
7. A Missing or Unknown child Application does NOT propagate to parent.

---

## Contract 4: Idempotency

**ID**: `contract_argocd_sync_completion_idempotency`
**Domain**: sync-callbacks
**Type**: behavior

### Guarantees Required by LoopZero

1. `POST /internal/argocd-callback` must return HTTP 200 regardless of activation success/failure.
2. Calling `OnSyncCompleted` when no PENDING releases exist is a no-op (returns nil error).
3. Calling `OnSyncCompleted` when the release is already ACTIVE: the state machine prevents double-activation, no duplicate events emitted.
4. Concurrent calls for the same app+revision: `withEnvironmentLock` serializes activation.

### Why This Matters

ArgoCD notification controller has retry logic. If LoopZero returns HTTP 500, ArgoCD retries
the same notification. LoopZero's handler MUST always return 200 to prevent infinite retry loops.
Activation failures are handled asynchronously by the ReleaseConvergenceLoop fallback.

---

## Derived Design Decisions

### Why SyncCallbackActivator uses last-hyphen split

ArgoCD Application names are user-visible resource names. LoopZero tenant IDs come
from GitHub org slugs (e.g., `my-org`) and environments can be any string (e.g.,
`production`, `staging`, `preview`). Since environments never contain hyphens
by convention, splitting on the last hyphen is unambiguous. The tenantID may
contain hyphens from the org name.

### Why revision check is optional (not hard failure)

ArgoCD's notification trigger template may or may not include the revision field.
Some configurations send empty revision. Rather than blocking activation, LoopZero
skips the revision check when either the revision field is empty OR the release
has no recorded GitCommit. The fallback ReleaseConvergenceLoop independently
validates the deployed state.

### Why non-completion callbacks return HTTP 200

ArgoCD sends notifications for ALL trigger conditions, not just Synced+Healthy.
Intermediate states (Progressing, OutOfSync) are valid trigger outputs. LoopZero
filters them at the handler level and returns 200 to acknowledge receipt without
activation. This prevents ArgoCD from interpreting the filter as an error.
