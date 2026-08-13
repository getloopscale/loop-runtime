# decisions

## Documentos

* [Evaluations: External Tooling — Deep Review 2026-05-21 Phase 8](2026-05-21-external-tooling-evaluations.md)
* [ADR: Naming Conventions — Deep Review 2026-05-21 Ratification](2026-05-21-naming-conventions-adr.md)
* [Live CA Fetch from `kube-root-ca.crt` on ClusterCredential Reconcile](2026-05-25-live-ca-fetch-clustercredential-adr.md)
* [Render schema.gen.sql via golang-migrate replay against ephemeral PostgreSQL 16; preserve golang-migrate as canonical migrator](2026-05-29-schema-gen-sql-render.md)
* [Adopt sqlc as canonical database access layer (replaces hand-rolled pgx)](2026-05-29-sqlc-adoption.md)
* [sqlc migration rollback playbook (operator-witnessed criteria + atomic rollback mechanism)](2026-05-29-sqlc-migration-rollback.md)
* [ADR D6 — Hot-path complexity decomposition pattern](2026-06-11-hot-path-complexity-decomposition.md)
* [ADR D3 — `pkg/domain.DefaultPermanentEnv` canonical constant](2026-06-11-pkg-domain-default-permanent-env.md)
* [ADR D2 — `pkg/domain.EnvRef` Value Object](2026-06-11-pkg-domain-env-ref-value-object.md)
* [ADR D4 — Plan tier canonical resolution pattern](2026-06-11-plan-tier-canonical-resolution.md)
* [ADR D1 — Tilt-owned YAML → Helm ConfigMap + Secret mount canonical pattern](2026-06-11-tilt-yaml-configmap-mount.md)
* [ADR D5 — Wiring triad pillar-(b) test contract](2026-06-11-wiring-triad-pillar-b-test-contract.md)
* [ADR: Deploy config model — services.json is the immutable app-shape; secrets + scaling are operational commands](2026-06-15-deploy-config-model-services-json-app-shape.md)
* [ADR 2026-06-15 — LoopZeroCloud deploy supports only the fixed LoopZeroKit stack (python + node)](2026-06-15-deploy-fixed-loopzerokit-stack.md)
* [J-06 Overview backend: read-model boundaries + price source of truth](2026-07-03-j06-overview-backend-read-model-boundaries.md)
* [Cluster API como ciclo de vida de cluster para BYOC multi-cloud e multi-país](2026-08-07-capi-byoc-cluster-lifecycle-adr.md) — **PROPOSED** (T3, aguarda reunião)

---

## Decisões da camada de dados — migradas para `loopzero-data-cells`

Em **2026-08-13** foram criadas seis decisões sobre a camada de dados dos produtos (shards, backup,
escala, resiliência). Elas viviam aqui e **foram movidas** para o repositório
[`loopzero-data-cells`](../../../loopzero-data-cells/docs/decisions/index.md) quando ele nasceu, porque o
sistema que decidem tem repositório próprio.

| Decisão | Onde está | Toca este repositório? |
|---|---|---|
| CloudNativePG como operator dos shards (motor, storage, failover) | `loopzero-data-cells` | o chart `loopzero-data-cell` nasce em `loopzero/charts/` |
| Onde o código do cell de dados mora | `loopzero-data-cells` | idem |
| O plano de controle de dados como sistema separado | `loopzero-data-cells` | não |
| Escala horizontal, headroom e backup | `loopzero-data-cells` | não |
| **Guardrail de `DELETE` estendido a `postgresql.cnpg.io`** | `loopzero-data-cells` | **sim — a mudança é em `charts/loopzero-admission-policies`**, e é pré-requisito bloqueante do primeiro shard |
| Política de resiliência da camada de dados | `loopzero-data-cells` | as métricas de banco ausentes são trabalho daqui (`#287`) |

**Duas decisões deste repositório são pressupostas por elas** e permanecem aqui:
[`capi-byoc-cluster-lifecycle`](2026-08-07-capi-byoc-cluster-lifecycle-adr.md) (§ D3, D12, D15) e
[`cell-cluster-unification`](2026-08-09-cell-cluster-unification-adr.md).
