---
type: decision
title: Cell control planes are EKS via AWSManagedControlPlane
date: 2026-09-11
status: stable
disposition: target-state — the CAPI ADR is PROPOSED and nothing here authorises building
supersedes_nothing: true
---

# Cell control planes are EKS via AWSManagedControlPlane

**Decision:** a cell's control plane is **EKS**, provisioned by CAPA through
`AWSManagedControlPlane`. Nodes come from `MachineDeployment` + `AWSMachine`.
**`AWSManagedMachinePool` is NOT used.**

## The row that decided it, and it was the one nobody had checked

The specialist recommending this flagged one unverified row as the one that would flip the
answer: *"If EKS's oldest supported minor is newer than what our 8 CRDs and 4 operators are
tested against, the managed control plane stops being the cheap answer."*

Checked 2026-09-11 against the AWS EKS standard-support page:

```
EKS standard support   1.36 · 1.35 · 1.34        (1.33 ended 2026-07-29)
our libraries          k8s.io/api v0.36.0   →  1.36
```

**It does not bite.** EKS already offers 1.36 and that is exactly where our libraries sit. The
risk was EKS running ahead of what we test; it is not.

## What this costs, stated rather than discovered

**`AWSManagedMachinePool` — EKS managed node groups — is still experimental in CAPA**, behind
`EXP_MACHINE_POOL`. The stable path for nodes is `AWSMachine`, and that **brings AMIs back**.
CAPA's community AMIs are documented as **non-production**; production means running
image-builder per Kubernetes minor per region.

So the managed control plane removes etcd, backup/restore and control-plane patching. It does
**not** remove the node image pipeline. A decision that claimed otherwise would be wrong within
one quarter.

## Alternatives rejected

| | Why not |
|---|---|
| **Self-managed control plane via CAPA** | Buys nothing here and costs an AMI pipeline for the control plane too — ~150 EC2 instances and etcd backup/restore at fifty clusters |
| **Terraform-direct EKS** | Re-creates the second provisioning mechanism that ADR § D15 rejects by name, and doubles the upgrade matrix and the DR path |

**Both still need ClusterClass at that count.** With plain CAPI resources, fifty clusters means
fifty sets of Cluster, ControlPlane, MachineDeployment and templates.

## The number that stops this being sized wrongly

**Fifty clusters do not come from tenant growth.** At `maxPerCell=5000`, fifty thousand tenants
is **10 cells**. The fiftieth cluster comes from BYOC accounts and regions. Sizing the CAPI
machinery off tenant count produces a bottleneck that does not exist — the same error that once
inflated this project's Helm release count tenfold.

## Found while verifying, and it is ours rather than AWS's

**The dev runtime is four minors behind our own libraries, and below EKS support entirely.**

```
k3d          rancher/k3s:v1.32.13   →  1.32
libraries    k8s.io/api v0.36.0     →  1.36
EKS standard oldest                 →  1.34
```

Repository Rule 10 says *"local equals production — same Helm charts, same operators, same build
pipeline"*, and the Kubernetes version is not in that equality today. `task setup` raises a
cluster on a version AWS no longer offers. Registered as a backlog item rather than fixed here.

**One 1.36 break was checked and we are clean:** `StrictIPCIDRValidation` is enabled by default
and rejects CIDR with leading zeros or ambiguous semantics. Every CIDR in `charts/*/values.yaml`,
`infra/helm/*/values.yaml*` and the helmfile environments was parsed — **none is non-canonical**.

## What is NOT verified

**Whether EKS needs a feature gate in CAPA v2.13.0.** Two primary pages disagree: the EKS
**index** page still lists `EKS=true` as required; the **enabling** page says default-on. The
more specific page wins on the usual reading, and **this must be confirmed against the v2.13.0
tag** before anyone writes a manifest. It is a build-time detail, not a decision input — and it
is exactly the kind of thing someone trips over on day one.

**The governing ADR is `PROPOSED`.** `docs/decisions/2026-08-07-capi-byoc-cluster-lifecycle-adr.md`
set a T3 meeting for 2026-08-17 with **no ratification record**, and its own text says no item
authorises implementation until the status is `RATIFIED`. This document records the target. It
does not release construction.

## Related

- [`byoc-as-the-only-mode-has-no-precedent.md`](byoc-as-the-only-mode-has-no-precedent.md) — the scope this serves
- [`../product/objectives.md`](../product/objectives.md) — OBJ-3, whose metric this mechanism must meet
- [`../architecture/paved-road.md`](../architecture/paved-road.md) — where the cell appears in the drawing
