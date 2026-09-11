---
type: design-drawing
title: D2 — where the trust boundaries sit
date: 2026-09-10
status: draft
---

# D2 — Where customer code stops

```mermaid
flowchart TB
    subgraph managed["DOOR 1 — OUR CELL, shared (the solo developer)"]
        subgraph ns_a["namespace loopzero-a-proj-prod · PSA restricted"]
            app_a[Agent A]
        end
        subgraph ns_b["namespace loopzero-b-proj-prod · PSA restricted"]
            app_b[Agent B]
        end
        kernel["shared nodes, shared kernel — no runtimeClass"]
        app_a -.-> kernel
        app_b -.-> kernel
    end
    subgraph customer["DOOR 2 — CUSTOMER AWS ACCOUNT (privacy)"]
        subgraph untrusted["UNTRUSTED — the customer agentic app"]
            app[Application workload]
        end
        ops["Operators — app, tenant, preview, platform"]
        app -.->|NetworkPolicy| ops
    end
    subgraph platform["LOOPZERO CONTROL PLANE — ours, both doors"]
        api[Control API]
        pg[(Postgres)]
        minio[(MinIO)]
    end
    ops -->|ClusterCredential| api
    api --> pg
    api --> minio
    app -->|"agent memory, execution record"| api
    app_a --> api
    app_b --> api
```

## Two doors, and they are not isolated the same way

**Decided 2026-09-11** — [`../decisions/two-doors-managed-and-byoc.md`](../decisions/two-doors-managed-and-byoc.md).
This drawing carried only door 2 until then, which made the platform look like one shape.

| | what separates two CUSTOMERS |
|---|---|
| door 1 · managed | a namespace label and PodSecurity `restricted`. **Same kernel, same nodes** |
| door 2 · BYOC | an AWS account. Nothing is shared |

The control plane is ours on both, so the boundary that moves is the workload's, not the API's.

**Door 1 shares a kernel, and that is decided** —
[`../decisions/shared-kernel-on-the-managed-door.md`](../decisions/shared-kernel-on-the-managed-door.md),
2026-09-11. `runtimeClass` appears in zero files and will stay that way: no gVisor, no Kata, no
Firecracker.

The pod is already at the ceiling of what Kubernetes offers without touching the kernel —
`runAsNonRoot`, uid 1000, `seccompProfile: RuntimeDefault`, `readOnlyRootFilesystem`,
`allowPrivilegeEscalation=false`, no service-account token, and PSA `restricted` on the namespace.
What remains is the shared kernel, and the decision is to carry it, because door 1 runs **the image
she built from her code** rather than code generated at runtime.

That is where Heroku, Railway, Supabase and PlanetScale sit, and it is the line Vercel draws in its
own documentation by shipping a Firecracker-backed product BESIDE its platform rather than inside
it. The exit condition is written in the decision: reopen the day LoopZero executes agent-generated code
as a platform capability.
