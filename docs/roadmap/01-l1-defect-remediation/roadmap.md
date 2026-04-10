# 01 — L1 Defect Remediation

**Phase:** A (Foundation)
**Complexity:** 3/5
**Dependencies:** None — can start immediately
**Unblocks:** L1 at 100% success, all subsequent items
**Cost delta:** $0
**Governance:** Pre-approved (MEET-046). No T2/T3 gate. Implement immediately via TDD. Validate on DigitalOcean.

---

## Problem

L1 load testing (2026-04-09) exposed 13 defects at 10 synthetic tenants. The system has a **10% build failure rate** from PVC multi-attach. Non-leader API pods get killed by liveness probes before leader election completes. The manifest gate blocks ALL new tenant deploys via substring matching. Without fixing these, every load level above L1 is built on a broken foundation.

Evidence: `infra/tests/load/CORRECTIONS.txt`

## Scope

| Correction | Root Cause | Fix |
|---|---|---|
| **D/F** — Probe kills non-leader | Leader election takes 60s, liveness kills at 45s | Add startupProbe to API Deployment in Pulumi. Start HTTP server BEFORE leader election. |
| **G** — Manifest gate blocks new tenants ✅ (2026-04-10) | `strings.Contains("kind: ClusterIssuer")` matches `issuerRef.kind` inside Certificate CRs | YAML-document-aware validation: parse apiVersion+kind at top level, not substring match. Implemented in `api/internal/gitops/manifest_gate.go` using `yaml.v3` decoder. Forbidden top-level kinds: Namespace, ClusterRole, ClusterRoleBinding, ClusterIssuer, CustomResourceDefinition. 9 unit tests passing. |
| **I** — PVC multi-attach (10% failure) | Argo Workflows schedules steps on different nodes, DO Block Storage is RWO | nodeAffinity in loopzero-forge WorkflowTemplate to pin steps to same node |
| **J** — loopzero-packs-runner bash error | v0.1.0 bash syntax error in Rust template | Already fixed (v0.2.0 Go binary). Ensure Pulumi references v0.2.0. |
| **K** — Build cluster undersized | 2 small nodes, PVC contention | Build node class: small → medium, autoscale 2→5. Codify in Pulumi. |
| **H** — Image cache on DOKS | `:dev` tag not re-pulled from cache | Use SHA-based image tags (already done by CI). Document as policy. |
| **B** — schema_migrations corruption | `migrate force 0` on production DB | Document: NEVER run `migrate force` on production. Add CI check. |
| **L** — Fixture path issue | Subdirectory path in loopzero.yaml vs flat fixture | Fix load test fixture to match production structure |
| **E** — GitOps push empty repo | New tenants missing ArgoCD repo credentials | Verify EnsureTenant creates Gitea permissions + ArgoCD repo-creds |
| **M** — Promtail Pulumi crash | gRPC provider crash on runtime cluster | Removed from Pulumi. Track: upgrade pulumi-kubernetes when stable. |

## Acceptance Criteria

1. **L1 passes at 100%.** 10 tenants, 10 concurrent deploys, 10/10 SUCCEEDED, 0 FAILED.
2. **Non-leader pods survive 30+ minutes.** 2 API replicas deployed, non-leader pod shows 0 restarts after 30 minutes.
3. **Manifest gate is YAML-aware.** Unit test: Certificate with `issuerRef.kind: ClusterIssuer` passes. Top-level ClusterIssuer blocks.
4. **PVC multi-attach < 1%.** 50 builds on 2-node cluster, fewer than 1 PVC scheduling failure.
5. **All corrections codified.** Zero manual interventions needed to re-run L1 from scratch.

## Required Tests

| Test | Type | Exists? | Location |
|---|---|---|---|
| `TestManifestGate_AllowsCertificateWithClusterIssuerRef` | Unit | ✅ Implemented (2026-04-10) | `api/internal/gitops/manifest_gate_test.go` |
| `TestManifestGate_BlocksTopLevelClusterIssuer` | Unit | ✅ Implemented (2026-04-10) | same |
| `TestManifestGate_AllowsCommonTenantResources` | Unit | ✅ Implemented (2026-04-10) | same |
| `TestManifestGate_BlocksTopLevelNamespace` | Unit | ✅ Implemented (2026-04-10) | same |
| `TestManifestGate_BlocksTopLevelClusterRole` | Unit | ✅ Implemented (2026-04-10) | same |
| `TestManifestGate_BlocksMixedWithForbidden` | Unit | ✅ Implemented (2026-04-10) | same |
| `TestManifestGate_TolerantOfEmptyDocuments` | Unit | ✅ Implemented (2026-04-10) | same |
| `TestManifestGate_RejectsMalformedYAML` | Unit | ✅ Implemented (2026-04-10) | same |
| `TestL1_10Tenants` (re-run) | Load | **Yes — exists** | `infra/tests/load/cell_scale_test.go` |
| `TestL1_WebhookBurst` | Load | **Yes — exists** | same |

## SLO Targets

| SLI | Target |
|---|---|
| Deploy success rate (10 tenants) | 100% |
| Deploy p99 latency | < 600s |
| API pod restart rate | 0 restarts/hour |
| Leader failover time | < 90s |

## Rollback Plan

Each correction is independent. Revert the specific commit. For infra changes (Correction K): `pulumi config set buildNodeClass s-2vcpu-4gb && pulumi up`.

## DoD

```bash
# 1. L1 load test passes at 100%
LOOPZERO_LOAD_TEST=1 go test ./infra/tests/load/ -v -run TestL1_10Tenants -timeout 30m 2>&1 | grep "FAIL"
# No FAIL lines

# 2. Non-leader pod stable
kubectl get pods -l app=loopzero-api -n loopzero-system -o jsonpath='{range .items[*]}{.metadata.name} restarts={.status.containerStatuses[0].restartCount}{"\n"}{end}'
# All restarts=0

# 3. Manifest gate tests pass
cd api && go test ./internal/services/ -run TestManifestGate -v

# 4. Zero manual fixes needed
# Re-run L1 from a clean state — no kubectl patch, no psql, no manual image push
```

## External Dependencies

- **loopzero-forge** (external repo `github.com/getloopscale/loopzero-forge`): PVC nodeAffinity fix requires a change there. Track separately.
