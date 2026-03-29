# ESO Behavioral Contract Map — LoopZero AppSecret Wiring

This document maps ESO behavioral contracts to LoopZero's AppSecret feature requirements.
Contracts are derived from ESO's own test suite and verified against the reference implementation.

---

## Contract 1: ExternalSecret creates K8s Secret on successful sync

**ID**: `contract_sync_creates_secret`
**Status**: validated
**Source evidence**: `src_secret_sync_basic_create`, `src_secret_sync_hash_annotation`

### What this means for LoopZero

When LoopZero's AppSecretController creates an ExternalSecret CR in a tenant namespace:
1. ESO creates a K8s Secret with the secret data within `requeue_interval` (default 1s in tests, 10m in production)
2. The Secret is annotated with `external-secrets.io/data-hash` — do not strip this annotation
3. The ExternalSecret status shows `Ready=True` — LoopZero's AppSecretController should propagate this to AppSecret status

### Preconditions
- ClusterSecretStore or SecretStore exists and is valid
- Remote secret key exists in the provider backend
- ExternalSecret CR is created in the target namespace

### Postconditions
- K8s Secret exists in the ExternalSecret's namespace
- `Secret.data` contains the synced key-value pairs
- `Secret.metadata.annotations[external-secrets.io/data-hash]` is set
- `ExternalSecret.status.conditions[Ready].status == True`

### Invariants
- Secret name = `ExternalSecret.spec.target.name` (or ExternalSecret name if unset)
- Secret has ownerReference to the ExternalSecret (unless CreationPolicy=Orphan/Merge/None)

---

## Contract 2: Provider errors set Ready=False condition with retry

**ID**: `contract_error_sets_condition`
**Status**: validated
**Source evidence**: `src_secret_sync_provider_error`, `src_secret_sync_store_missing`, `src_secret_sync_store_constructor_error`

### What this means for LoopZero

Any sync failure is surfaced via ExternalSecret.status.conditions. LoopZero's AppSecretController should:
1. Watch ExternalSecret conditions and mirror the Ready status to AppSecret.status
2. Never treat ESO errors as permanent — ESO retries automatically
3. Surface the `Reason` field to the user (mapped to product language, not internal terms)

### Failure modes and their reasons

| Failure | ESO Reason | User-facing message |
|---------|-----------|---------------------|
| Store does not exist | `SecretSyncedError` | "Secret configuration references a missing secret store. Check your store configuration." |
| Provider returned error | `SecretSyncedError` | "Secret provider returned an error. The system will retry automatically." |
| Store constructor failed | `SecretSyncedError` | "Secret store authentication failed. Verify store credentials." |
| Invalid provider config | `InvalidProviderConfig` | "Secret store configuration is invalid." |

### Recovery behavior
ESO automatically transitions `Ready` from `False` back to `True` when the error clears, without any manual intervention. This is tested by `src_secret_sync_provider_error`.

---

## Contract 3: RefreshInterval controls when upstream changes propagate

**ID**: `contract_refresh_interval_controls_rotation`
**Status**: validated
**Source evidence**: `src_secret_sync_refresh_interval`, `src_secret_sync_refresh_interval_zero`, `src_secret_sync_refresh_logic_interval_elapsed`

### What this means for LoopZero

The `refreshInterval` in ExternalSecret.spec is the main knob for secret rotation latency.

| `refreshInterval` | Behavior |
|-------------------|---------|
| `0` | Sync-once: secret is created once and never updated. |
| Positive duration (e.g. `1h`) | ESO polls the provider every `refreshInterval` and updates the K8s Secret if the value changed. |

### Recommended LoopZero defaults
- AppSecret default: `1h` (balance between freshness and API load)
- High-rotation secrets (API tokens): `15m`
- Immutable secrets (TLS certs managed elsewhere): `0`

### Tamper detection
Even with `refreshInterval > 0`, ESO detects if the `data-hash` annotation is modified or the Secret data is tampered with. In that case, resync is immediate. This prevents drift between the K8s Secret and the upstream provider.

---

## Contract 4: ClusterSecretStore namespace conditions enforce tenant isolation

**ID**: `contract_cluster_store_namespace_isolation`
**Status**: validated
**Source evidence**: `src_secret_sync_cluster_store`, `src_secret_sync_cluster_store_namespace_condition`

### What this means for LoopZero

The `ClusterSecretStore` on the mgmt cluster MUST have namespace conditions configured. Without them, any namespace can read any tenant's secrets.

### Recommended LoopZero configuration

```yaml
apiVersion: external-secrets.io/v1
kind: ClusterSecretStore
metadata:
  name: loopzero-mgmt-store
spec:
  conditions:
    - namespaceSelector:
        matchLabels:
          loopzero.io/tenant: "{{.TenantID}}"  # Must match tenant namespace label
  provider:
    kubernetes:
      server:
        url: https://mgmt-cluster:6443
        caBundle: ...
      auth:
        serviceAccount:
          name: eso-reader
          namespace: external-secrets
      remoteNamespace: "{{.TenantID}}-secrets"  # Namespace containing AppSecret source secrets
```

### Condition evaluation (from ESO source)
- Multiple conditions are OR-evaluated (at least one must match)
- Each condition evaluates Namespaces (string list) OR NamespaceSelector (label selector), not both
- Non-matching → `SecretSyncedError` condition set immediately (no K8s Secret created)

---

## Contract 5: DeletionPolicy controls K8s Secret lifecycle

**ID**: `contract_deletion_policy_controls_secret_lifecycle`
**Status**: validated
**Source evidence**: `src_secret_sync_deletion_policy_delete`, `src_secret_sync_deletion_policy_retain`, `src_secret_sync_orphan_policy`

### What this means for LoopZero

When the user runs `loopzero secret delete` or deprovisioning occurs:

| Scenario | Recommended DeletionPolicy | Result |
|----------|---------------------------|--------|
| AppSecret explicitly deleted by user | `Delete` | K8s Secret removed when upstream gone |
| Tenant deprovisioned | Namespace deletion cascades | All Secrets in namespace are deleted |
| AppSecret should persist after ESO removal | `Retain` | K8s Secret survives |

### LoopZero recommendation
Use the default (`Retain`) for safety. Explicit cleanup should be handled by LoopZero's deprovisioning flow (delete the namespace), not by DeletionPolicy.

---

## Contract 6: Owner-watch triggers immediate Secret recreation

**ID**: `contract_owner_watch_immediate_recreation`
**Status**: validated
**Source evidence**: `src_secret_sync_basic_create`

### What this means for LoopZero

ESO registers a watch on the owned K8s Secret. If the Secret is accidentally deleted (e.g. `kubectl delete secret`), it is immediately recreated WITHOUT waiting for the next refresh cycle. This is self-healing behavior.

**Important**: This requires the ExternalSecret to use `CreationPolicy=Owner` (the default). If LoopZero ever creates ExternalSecrets with `CreationPolicy=Orphan`, this self-healing property is lost.

### LoopZero implication
LoopZero should always use `CreationPolicy=Owner` (the default) unless there is an explicit reason to orphan the secret. This is the safe default.

---

## Non-Portable Contracts (implementation details, not for LoopZero)

The following contracts exist in ESO's test suite but are NOT relevant to LoopZero's wiring:

- Template engine (v1/v2) — LoopZero does not use ESO templates
- Generator references (Fake, ClusterGenerator) — LoopZero uses direct provider refs
- Merge creation policy — LoopZero does not share K8s Secrets between multiple ExternalSecrets
- DataFrom/Find operations — LoopZero uses individual key references only
- Prometheus metric counters — LoopZero should monitor ExternalSecret conditions, not ESO internal metrics
