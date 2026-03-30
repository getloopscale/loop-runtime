# ESO Test Catalog — Harvested for LoopZero AppSecret Wiring

Generated from: `/home/paulo/loopzero/referencias/external-secrets`
Relevant to: LoopZero AppSecret CRD + ESO ClusterSecretStore wiring (ESO/Secrets E2E validation)

---

## Summary

| Category | Count |
|----------|-------|
| Source tests harvested | 17 |
| Domains covered | secret-sync, secret-rotation, error_recovery, cluster-secret-store |
| Behavioral contracts extracted | 6 |
| Golden (portable) tests written | 5 |

---

## Source Tests

### Domain: secret-sync

| ID | Test Name | File | Type | Key Behavior |
|----|-----------|------|------|--------------|
| `src_secret_sync_basic_create` | `externalsecret_creates_k8s_secret_on_sync` | `pkg/controllers/externalsecret/externalsecret_controller_test.go` | integration | ExternalSecret → K8s Secret created, owner watch triggers recreation on manual delete |
| `src_secret_sync_hash_annotation` | `synced_secret_has_data_hash_annotation` | same | integration | Synced Secret gets `external-secrets.io/data-hash` annotation for tamper detection |
| `src_secret_sync_hash_change_triggers_refresh` | `tampered_hash_annotation_triggers_immediate_resync` | same | integration | Corrupted hash triggers resync even if refreshInterval has not elapsed |
| `src_secret_sync_deletion_policy_delete` | `deletion_policy_delete_removes_secret_when_provider_secret_gone` | same | integration | DeletionPolicy=Delete → K8s Secret deleted when provider returns NoSecretErr |
| `src_secret_sync_deletion_policy_retain` | `deletion_policy_retain_keeps_secret_when_provider_secret_gone` | same | integration | DeletionPolicy=Retain → K8s Secret preserved when provider returns NoSecretErr |
| `src_secret_sync_orphan_policy` | `orphan_creation_policy_retains_secret_after_external_secret_deleted` | same | integration | CreationPolicy=Orphan → Secret not deleted when ExternalSecret is deleted |

### Domain: secret-rotation

| ID | Test Name | File | Type | Key Behavior |
|----|-----------|------|------|--------------|
| `src_secret_sync_refresh_interval` | `secret_value_refreshed_when_provider_changes_within_refresh_interval` | same | integration | Upstream value change propagated to K8s Secret within refreshInterval |
| `src_secret_sync_refresh_interval_zero` | `refresh_interval_zero_disables_rotation` | same | integration | refreshInterval=0 → K8s Secret never updated after initial sync |
| `src_secret_sync_refresh_logic_interval_elapsed` | `should_refresh_when_refresh_interval_has_passed` | same | unit | shouldRefresh() returns true when interval elapsed |

### Domain: error_recovery

| ID | Test Name | File | Type | Key Behavior |
|----|-----------|------|------|--------------|
| `src_secret_sync_provider_error` | `provider_error_sets_not_ready_condition_then_recovers` | same | integration | Provider GetSecret error → Ready=False; recovery → Ready=True auto |
| `src_secret_sync_store_missing` | `missing_secret_store_sets_error_condition` | same | integration | ExternalSecret referencing nonexistent store → Ready=False, SecretSyncedError |
| `src_secret_sync_store_constructor_error` | `store_constructor_error_sets_error_condition` | same | integration | Store provider constructor failure → Ready=False, SecretSyncedError |
| `src_secretstore_invalid_provider` | `invalid_provider_config_sets_invalid_condition_on_store` | `pkg/controllers/secretstore/common_test.go` | integration | SecretStore with invalid provider config → ReasonInvalidProviderConfig |

### Domain: cluster-secret-store

| ID | Test Name | File | Type | Key Behavior |
|----|-----------|------|------|--------------|
| `src_secret_sync_cluster_store` | `cluster_secret_store_syncs_secret_cross_namespace` | `externalsecret_controller_test.go` | integration | ClusterSecretStore with no conditions allows any namespace to sync |
| `src_secret_sync_cluster_store_namespace_condition` | `cluster_secret_store_namespace_condition_blocks_wrong_namespace` | same | integration | ClusterSecretStore with namespace condition blocks non-matching namespace |
| `src_ces_deploys_to_matching_namespaces` | `cluster_external_secret_creates_external_secret_in_matching_namespaces` | `pkg/controllers/clusterexternalsecret/clusterexternalsecret_controller_test.go` | integration | CES with namespace selector creates ExternalSecret in each matching ns |
| `src_ces_cleans_up_deselected_namespaces` | `cluster_external_secret_removes_external_secret_from_deselected_namespace` | same | integration | When namespace loses matching label, CES deletes ExternalSecret from it |

---

## Test Framework Details

### ESO Test Infrastructure

- **Framework**: Ginkgo v2 + Gomega
- **Environment**: `controller-runtime/pkg/envtest` (etcd + kube-apiserver, no real cluster)
- **Fake Provider**: `runtime/testing/fake.Client` — injectable provider that returns predefined values; controllable error injection via `WithGetSecret(data, err)`, `WithGetSecretMap(...)`, `WithNew(...)`
- **Metrics**: Prometheus CounterVec/GaugeVec for `sync_calls_total`, `sync_calls_error`, `external_secret_condition`, `reconcile_duration`
- **Timeouts**: `timeout = 10s`, `interval = 250ms` for Eventually/Consistently assertions

### ESO Controller Under Test

`pkg/controllers/externalsecret.Reconciler` with:
- `ClusterSecretStoreEnabled: true`
- `RequeueInterval: 1s`
- `MaxConcurrentReconciles: 1` (Serial tests)
- `SecretClient`: separate cached client for managed secrets (disabled for Secret/ConfigMap)

### Fake Provider Registration

The test suite uses a global `init()` function to register the fake provider:
```go
func init() {
    esv1.ForceRegister(&fake.Client{}, &esv1.SecretStoreProvider{AWS: &esv1.AWSProvider{...}})
}
```
This means tests can reference AWS SecretStore specs but the fake provider intercepts all calls.

---

## What LoopZero Needs

LoopZero's AppSecret wiring requires these ESO primitives:

1. **ClusterSecretStore** (cluster-scoped) — points at mgmt cluster via Kubernetes provider
2. **ExternalSecret** (per tenant namespace) — created by LoopZero's AppSecretController when user runs `loopzero secret set`
3. **Namespace conditions** on ClusterSecretStore — enforce that only authorized tenant namespaces can sync
4. **refreshInterval** — configurable per AppSecret; recommended 1h default

The Fake provider in tests substitutes for the real Kubernetes provider (mgmt cluster K8s Secrets). The behavioral contracts are provider-agnostic.
