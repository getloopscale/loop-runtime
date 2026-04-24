# ADR-0012: CloudNativePG Replaces bitnami/postgresql in Self-Hosted Installer

**Status:** ACCEPTED
**Date:** 2026-04-24
**Origin:** Async T2 approval in pre-release phase (CHG-2026-04-24-INSTALLER-CNPG)
**Change ID:** CHG-2026-04-24-INSTALLER-CNPG
**Deciders:** Platform Architect (async), SRE (async via ADR review)
**Supersedes:** None.

---

## Context

The self-hosted installer (`loopzero platform install`) was pinned to `bitnami/postgresql` v16.6.1 as its bundled database. On 2026-04-24 the installer failed at Step 2/10 (prerequisites) on a fresh k3d cluster with:

```
Warning  Failed     20m (x5 over 23m)  kubelet  Failed to pull image
  "docker.io/bitnami/postgresql:17.4.0-debian-12-r13": not found
```

### Root cause

In **September 2025** Bitnami (acquired by VMware / Broadcom) removed public images from `docker.io/bitnami/*`. Community-maintained images moved to `docker.io/bitnamilegacy/*`; supported/paid images moved to `docker.io/bitnamisecure/*`. Existing charts pinned to Bitnami versions without image-registry overrides broke permanently. The self-hosted install of LoopZero was one of them.

Cascade impact in the install:
1. `bitnami/postgresql` → ImagePullBackOff (timeout 5 min)
2. `gitea-charts/gitea` subcharts (`postgresql-ha`, `redis-cluster`, `valkey`) → same regression
3. `ory/kratos` + `ory/hydra` → pre-install timeout (depend on PG that never came up)
4. Steps 3–10 of the 10-step install never reached

### Why not just point to `bitnamilegacy/*`

Fast, 30 LoC, but it re-binds the product to the **same vendor policy** that caused the regression, only on a community-maintained tier with no SLA. Bitnamilegacy can be discontinued unilaterally at any time. This is the class of supply-chain risk CLAUDE.md §16 and Rule 10 ("local equals production") explicitly warn against.

## Decision

Replace `bitnami/postgresql` with **CloudNativePG (CNPG)** operator:

- **Operator chart:** `cnpg/cloudnative-pg` → installs the controller in `cnpg-system` namespace
- **Instance chart:** `cnpg/cluster` → creates a `Cluster` CR in `loopzero-system` that provisions the actual PostgreSQL pods

Gitea moves to **SQLite + memory cache + level queue** (HA subcharts disabled). Gitea in LoopZero is a GitOps mirror (internal desired-state store), not user-facing traffic — SQLite is more than sufficient and eliminates all Bitnami subchart dependencies.

## Consequences

### Positive
- **Zero Bitnami dependency** in the installer.
- **CNCF Graduated** (Feb 2024) — mature governance, production users at HBO Max, IBM, Volkswagen.
- **HA by default** via streaming replication (set `cluster.instances: 3` for production profile).
- **Automatic failover** (< 30 s RTO) via Kubernetes services `<cluster>-rw`, `<cluster>-ro`, `<cluster>-r`.
- **Password generated randomly** per install (in secret `<cluster>-app`) — removes hardcoded `loopzero:loopzero` credential in install defaults.
- **Backup/restore** integrated via Barman (phase 3 — not wired yet, CRD support exists).

### Negative / Risks
- **+1 CRD namespace** (`postgresql.cnpg.io`) in `cnpg-system` — new operator to monitor.
- **+1 Helm release** in Step 2 — install time grows ~30 s (operator + cluster ready).
- **Installer must wait for `Cluster` CR to reach `Cluster in healthy state`** before constructing `DATABASE_URL` — new synchronization step. Bootstrap extracts password from `<cluster>-app` secret, not from Helm `--set`.
- **Migration path for existing installs** (non-issue in pre-release: no customer installs exist).

### Neutral
- `config.database.external=true` path (BYO PostgreSQL — RDS, Cloud SQL, DO Managed PG) is unchanged. Customers at FAANG scale should still point to managed PG; CNPG is for the self-hosted demo/test/dev flow.

## Rejected alternatives

**A. Override `image.repository=bitnamilegacy/postgresql`.** Rejected: re-binds to vendor policy that just fired; community tier has no SLA.

**B. Write our own `loopzero-postgres` chart wrapping Cluster CR.** Rejected: YAGNI. `cnpg/cluster` already templates the CR with all knobs we need.

**C. Apply `Cluster` CR via `kubectl apply` in a dedicated step.** Rejected: breaks the "list of Helm releases" pattern, increases installer code. `cnpg/cluster` chart keeps the abstraction uniform.

## Scope & validation

**Files touched:**
- `cli/src/installer/prerequisites.ts` — swap `bitnami/postgresql` for CNPG operator + cluster, disable Gitea HA subcharts, add SQLite config.
- `cli/src/installer/bootstrap.ts` — new `extractPostgresCredentials()` reads secret `<cluster>-app`.
- `cli/src/commands/platform.ts` — wire dynamic `DATABASE_URL` from secret into `tokens` passed to `installPlatform`.

**Validation:**
- Unit: existing `npm test` suite (1059 tests) must still pass. No new unit tests needed — the change is at integration boundary.
- E2E: `loopzero platform install -f /tmp/test-values.yaml` on fresh k3d must reach Step 10/10 APPROVED. Followed by `loopzero login` + `loopzero init` + `loopzero deploy` on a hello-world app.

## Supply-chain roadmap (separate ADR)

This ADR addresses the immediate regression. A follow-up ADR should cover:
- Image mirroring to customer registry (DOCR) at install time
- Admission policy (Kyverno) rejecting non-mirrored images
- SBOM generation (syft) + CVE scanning (trivy) in build pipeline
- Cosign attestation verification for third-party deps

These are tracked in Sprint 6 remainder and the "Supply chain hardening" backlog.

---

## BYO vs LoopZero-internal — Responsibility Boundary

A related architectural clarification surfaced while designing this change:
**the installer must not blur the line between resources the customer provides
and resources the LoopZero product owns.** CNPG is a *dev/test convenience*,
not a production story.

### The rule

| Layer | Resource | Who provisions in production | Who provisions in dev |
|---|---|---|---|
| **Customer plane (BYO)** | Kubernetes cluster | Customer (DOKS, EKS, GKE, AKS) | k3d / kind / minikube |
| | PostgreSQL | Customer (DO Managed PG, RDS, Cloud SQL, Aurora) | CNPG embedded |
| | Object storage | Customer (Spaces, S3, GCS) | MinIO embedded |
| | Container registry | Customer (DOCR, ECR, Harbor) | k3d-registry local |
| **LoopZero plane (internal)** | Gitea | Internal to LoopZero (GitOps mirror) | Internal to LoopZero |
| | ArgoCD | Internal to LoopZero (convergence) | Internal to LoopZero |
| | LoopZero operators (api/controller/tenant/preview) | Internal to LoopZero | Internal to LoopZero |
| **Optional** | Ory Kratos / Hydra | Bundled OR external IdP | Bundled |

**The rule:** anything that is **customer state** (DB, object storage, cluster,
registry) is **BYO**. Anything that is **LoopZero product state** (Gitea, ArgoCD,
the operators) is **LoopZero-internal**.

### Why this matters

Without this separation, a customer could accidentally run
`profile: production + database.external: false` and the installer would
provision **CNPG in a single-replica Pod on a production cluster** with no
backup strategy, no cross-AZ HA, no customer RPO/RTO commitments, and no
DR runbook. The system would run — and look green — until the node rotated
or the PVC disappeared, at which point the customer loses all of LoopZero's
operational state silently.

The same anti-pattern would exist for MinIO: a single Pod holding every
customer's source bundles and build artifacts is not a production story.

### Enforcement (T1 follow-up)

Add a fail-fast check in `cli/src/commands/platform.ts` at the top of
`platformInstall()`:

```ts
if (config.profile === 'production') {
  if (!config.database.external || !config.database.url) {
    writeError('Production profile requires external PostgreSQL.');
    writeError('  Set database.external: true and database.url in values.yaml.');
    writeError('  Use DO Managed PostgreSQL, RDS, Cloud SQL, or equivalent.');
    writeError('  Embedded CNPG is supported only for profile: minimal (dev/test).');
    return 1;
  }
  if (!config.storage.accessKey || !config.storage.secretKey) {
    writeError('Production profile requires external object storage credentials.');
    writeError('  Set storage.accessKey + storage.secretKey (DO Spaces, S3, GCS).');
    return 1;
  }
}
```

This keeps the installer honest about what it can and cannot deliver. It
does not break the dev/test path (profile=minimal still auto-provisions
CNPG + MinIO as today) but makes accidental production misuse a loud error
instead of a silent one.

### What this ADR decides vs. defers

**Decides now (in this ADR scope):**
- CNPG replaces bitnami/postgresql in the minimal profile.
- Gitea switches to SQLite for the minimal profile.
- Credentials flow through the CNPG-generated secret rather than hardcoded.

**Defers to a follow-up T1 change (new ADR / CHG):**
- Fail-fast validation above.
- README documentation of the BYO responsibility table.
- Same pattern applied to storage (reject MinIO embedded when
  `profile: production`).
- `auth.type=external` recommendation for production (Kratos/Hydra
  are already optional via `auth.type=bundled | external`).

The follow-up is intentionally separate so this ADR stays narrow on the
Bitnami regression and the follow-up can be reviewed on its own merits
(UX of error messages, wording of README guidance, exact YAML contract).
