# Fixed replicas for the control plane, autoscaling for tenant workloads

- **Status:** PROPOSED
- **Date:** 2026-09-07
- **Tier:** T2 (control-plane capacity posture)
- **Source:** infrastructure system-design audit, finding SD-01.4

## Context

The manifest renderer emits an HPA — or a KEDA `ScaledObject` when `minReplicas` is 0 — for every
tenant application (`api/internal/manifests/renderer_helpers.go:105-116`). The platform that runs
them has none: **zero `autoscaling:` blocks and zero HPA or ScaledObject templates across `charts/`
and `infra/helm/`**. Every platform replica count is a fixed literal — loopzero-api 2, each of the four
operators 2, loopzero-cloud 2, argocd 3, traefik 2, loki 1.

The audit did not call this wrong. It called it **undecided**: nothing in the repository states what
2 replicas of loopzero-api is sized to serve, so the number is indistinguishable from a default nobody
revisited.

Two facts sharpen the question:

- `docs/operations/loopzero-capsim.mjs` models the AWS target at 50,000 tenants and sizes this tier at
  **8 replicas × 350 rps**. Nothing moves 2 toward 8 on its own.
- `charts/loopzero-api/values.yaml` sets `loopzeroKedaEnabled: "false"` while KEDA is installed on the cell
  clusters — so the platform ships the autoscaler it does not use on itself.

## Decision

**Keep fixed replicas for the control plane.** Record it as a choice with its reasons, and record
what would change it.

## Why

1. **Blast radius.** The control plane holds the environment lock, the placement transaction and the
   GitOps push. More replicas under load means more concurrent holders of those, and the failure
   modes of contention are worse than the failure mode of a queue.
2. **The load is not request-shaped.** Capacity here is already modelled in tenants and concurrent
   builds — which is what `SRE-07.1` argues for over QPS, because requests differ in cost. An HPA on
   CPU would autoscale on the wrong signal.
3. **Cost predictability.** A control plane that scales with tenant count turns an infrastructure
   line item into a variable one, with no measured relationship driving it.
4. **The measurement to justify a threshold does not exist.** No QPS or per-replica throughput
   figure is declared for this tier (SD-02.1). An HPA needs a target value; setting one today would
   be picking a number and calling it capacity.

## Alternatives rejected

1. **HPA on CPU for loopzero-api and the operators.** Rejected on (2) and (4): wrong signal, and no
   measured threshold to set it to. Reconsider once the load suites produce a per-replica figure.
2. **KEDA scale-to-zero for the operators.** Rejected outright: a controller that is not running is
   not reconciling, and the reconcile loop is the product.
3. **Raise the fixed counts toward the capsim numbers (2 → 8).** Rejected today: it would provision
   for a load nobody has measured arriving. The right trigger is the measurement, not the model.

## What would change this decision

Any one of:

- A measured per-replica throughput figure for loopzero-api that shows 2 replicas at 500m CPU saturating
  before the declared SLO degrades.
- Sustained tenant counts approaching `maxTenantsPerCell` (5000) in any cell.
- A `deploy_latency` p95 burn traceable to control-plane saturation rather than to the build plane.

## Consequences

- `loopzero-capsim.mjs` keeps modelling 8 replicas as a **target**, which is correct for a target model.
  The comment there now says so explicitly, so the gap between model and deployment reads as
  "someone must act" rather than as a contradiction.
- The topology-spread and PDB work done alongside this ADR is what makes fixed replicas survivable:
  2 replicas that cannot land on one node is a meaningfully different posture from 2 that can.

## Related

- Audit finding SD-01.4 and its companion SD-02.1 — `system-design-output/final_report.md`
- Spread constraints enabled the same day: `charts/loopzero-api/values.yaml`, locked by
  `infra/tests/charts/loopzero_api_resilience_defaults_test.go`
