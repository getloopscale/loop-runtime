---
type: design-drawing
title: D2 — where the trust boundaries sit
date: 2026-09-10
status: draft
---

# D2 — Where customer code stops

```mermaid
flowchart TB
    subgraph customer["CUSTOMER AWS ACCOUNT — cell"]
        subgraph untrusted["UNTRUSTED — the customer agentic app"]
            app[Application workload]
        end
        ops["Operators — app, tenant, preview, platform"]
        app -.->|NetworkPolicy| ops
    end
    subgraph platform["LOOPZERO PLATFORM"]
        api[Control API]
        pg[(Postgres)]
        minio[(MinIO)]
    end
    ops -->|ClusterCredential| api
    api --> pg
    api --> minio
    app -->|"agent memory, execution record"| api
```

## Measured

- **The cell runs inside the customer's own AWS account** — `PIECE-9` states it:
  *"bringing a cell into existence inside the customer's AWS account… reproducibly from
  a declared ClusterClass."* The trust boundary is therefore an account boundary, not
  only a namespace one.
- `NetworkPolicy` and `securityContext` are exercised by chart tests —
  `charts/loopzero-api/tests/networkpolicy_test.yaml`,
  `charts/loopzero-api/tests/security-context_test.yaml`,
  `charts/loopzero-tenant-operator/tests/networkpolicy_test.yaml`.
- `ClusterCredential` is one of the eight CRDs, which is how a cell authenticates
  rather than a shared secret.

## Decided

- **The customer's code is the Application workload, and nothing else.** Operators,
  API and storage are the platform's.
- **The credential crossing the boundary is per-cell**, carried by a CRD rather than
  configured by hand.

## Open — for the sign-off to resolve

- **Egress from the customer's agentic app.** No allowlist was found. An agent that
  can reach anything is a different product than one that cannot.
- **Can one tenant's app observe another's?** `tenant-operator` exists; whether the
  isolation is namespace, node or cell was not established from the charts.

## Related

- [`states.md`](states.md) — D1, the phases that cross this boundary
- [`sequence.md`](sequence.md) — D3, the calls that cross it
- [`sign-off.md`](sign-off.md) — the open questions this drawing leaves
