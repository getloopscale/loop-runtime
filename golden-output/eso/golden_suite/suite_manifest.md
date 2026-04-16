# Golden Suite Manifest: ESO AppSecret Sync

## Source Reference

External Secrets Operator (ESO) controller tests:
- `referencias/external-secrets/pkg/controllers/externalsecret/externalsecret_controller_test.go`
- `referencias/external-secrets/pkg/controllers/externalsecret/suite_test.go`

## LoopZero Implementation Under Test

| File | Role |
|------|------|
| `api/internal/k8s/secret_k8s_manager.go` | Creates K8s Secret on mgmt + ExternalSecret CR on runtime |
| `api/internal/routes/secrets.go` | HTTP handlers for `loopzero secret set/list/delete` |
| `operators/api/v1/appsecret_types.go` | AppSecret CRD with Synced/RotationCurrent conditions |
| `operators/internal/conditions/types.go` | Typed condition constants |

## ESO Patterns Harvested

| ESO Pattern | Where in ESO | LoopZero Adaptation |
|-------------|-------------|-----------------|
| `testCase` struct with `checkCondition` + `checkSecret` | `externalsecret_controller_test.go:79-94` | Table-driven tests with explicit field assertions per contract |
| `makeDefaultTestcase` factory | `externalsecret_controller_test.go:278-328` | `FakeSecretSyncer.CreateSecret()` produces full SyncResult |
| `isSecretValid` table-driven tests | `externalsecret_controller_test.go:108-201` | Contract 1-2: validate naming, namespace, data shape |
| SecretStore/ClusterSecretStore reference | ESO ExternalSecret.spec.secretStoreRef | Contract 8: always "loopzero-mgmt-store" ClusterSecretStore |
| creationPolicy (Owner/Orphan/Merge) | ESO ExternalSecret.spec.target.creationPolicy | Contract 5: always "Owner" for cleanup guarantee |
| refreshInterval | ESO ExternalSecret.spec.refreshInterval | Contract 6: 30s convergence window |
| Namespace isolation via ClusterSecretStore conditions | ESO ClusterSecretStore.spec.conditions.namespaceSelector | Contract 3: tenant labels on both mgmt Secret and ExternalSecret |
| Ready condition (True/False with reason) | ESO ExternalSecret.status.conditions | Contract 9: Synced condition maps to ESO's Ready |
| envtest suite bootstrap | `suite_test.go:58-129` | Not used (golden tests are specification-level, not envtest) |

## Behavioral Contracts (10)

| # | Contract | Test Function | ESO Equivalent |
|---|----------|---------------|----------------|
| 1 | `loopzero secret set` creates K8s Secret on mgmt in platform-system | `TestSecretSync_CreatesK8sSecretOnMgmtCluster` | ExternalSecret creates target Secret |
| 2 | ExternalSecret CR is created on runtime with correct store ref + data mapping | `TestSecretSync_CreatesExternalSecretOnRuntimeCluster` | ExternalSecret with SecretStore ref |
| 3 | Both mgmt Secret and ExternalSecret carry tenant isolation labels | `TestSecretSync_LabelsEnforceTenantIsolation` | ClusterSecretStore namespace conditions |
| 4 | Delete removes both mgmt Secret and runtime ExternalSecret | `TestSecretSync_DeletionRemovesBothResources` | ExternalSecret deletion + deletionPolicy |
| 5 | CreationPolicy is always "Owner" (cleanup on delete) | `TestSecretSync_OwnerCreationPolicyIsAlwaysSet` | creationPolicy=Owner ownerReference |
| 6 | RefreshInterval is 30s (eventual consistency window) | `TestSecretSync_RefreshIntervalIsConfigured` | refreshInterval on ExternalSecret |
| 7 | Runtime namespace follows loopzero-{tenant}-{env} convention | `TestSecretSync_NamespaceNamingConvention` | Namespace-scoped ExternalSecret |
| 8 | All ExternalSecrets reference ClusterSecretStore "loopzero-mgmt-store" | `TestSecretSync_AlwaysReferencesClusterSecretStore` | secretStoreRef.kind=ClusterSecretStore |
| 9 | Synced condition is set to True on success | `TestSecretSync_SyncedConditionOnSuccess` | Ready condition on ExternalSecret |
| 10 | Target secret name follows user-secrets-{tenant}-{project}-{env}-{app} | `TestSecretSync_TargetSecretNameConvention` | target.name on ExternalSecret |

## Multi-Cluster Architecture

```
loopzero secret set DATABASE_URL=postgres://...
    |
    v
[API] SecretHandler.Set()
    |
    +---> [PG] secret_store.Set()         -- source of truth
    |
    +---> [K8s mgmt] Secret in platform-system
    |         name: secret-{tenant}-{app}-{name}
    |         labels: loopzero.io/tenant, loopzero.io/project, loopzero.io/app
    |
    +---> [K8s runtime] ExternalSecret in loopzero-{tenant}-{env}
              secretStoreRef: loopzero-mgmt-store (ClusterSecretStore)
              remoteRef.key: secret-{tenant}-{app}-{name}
              target.name: user-secrets-{tenant}-{project}-{env}-{app}
              creationPolicy: Owner
              refreshInterval: 30s
                  |
                  v (ESO controller)
              [K8s runtime] Secret: user-secrets-{tenant}-{project}-{env}-{app}
                  |
                  v (pod envFrom)
              [Pod] env var DATABASE_URL available
```

## How to Run

```bash
cd /home/paulo/loopzero/golden-output/eso/golden_suite/portable_tests
go test -v -count=1 ./...
```

These tests are self-contained with no external dependencies (no envtest, no ESO
CRDs, no K8s binaries). They validate behavioral contracts using a FakeSecretSyncer
that mirrors the real `SecretK8sManager` naming and labeling logic.

## Not Covered (Future Work)

| Gap | Reason |
|-----|--------|
| ESO controller reconciliation (actual ESO running) | Golden tests are specification-level; E2E with live ESO is in `infra/tests/e2e/` |
| Missing store reference error condition | Requires envtest with ESO CRDs installed |
| Secret rotation via RotationConfig | AppSecret rotation controller not yet implemented |
| Multi-key secrets (multiple data mappings per ExternalSecret) | Current implementation creates one ExternalSecret per secret key |
| ClusterSecretStore namespaceSelector enforcement | Infrastructure concern validated at deploy time, not in unit tests |

## Mapping to LoopZero Sprint

These tests are pre-conditions for the **ESO/Secrets E2E validation** item from the Production Readiness plan:

> Phase: ESO/Secrets -- External Secrets Operator E2E validation pending

Running these golden tests validates the wiring contracts. The next step is E2E
validation against a real cluster with ESO installed and the Kubernetes provider
configured to read from the mgmt cluster.
