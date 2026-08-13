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
| CloudNativePG como operator dos shards (motor, storage, failover) | `loopzero-data-cells` | não |
| Onde o código do cell de dados mora | `loopzero-data-cells` | não — **revisado em 2026-08-13**, ver abaixo |
| O plano de controle de dados como sistema separado | `loopzero-data-cells` | não |
| Escala horizontal, headroom e backup | `loopzero-data-cells` | não |
| Guardrail de `DELETE` estendido a `postgresql.cnpg.io` | `loopzero-data-cells` | **sim, mas só uma vez:** a lista de apiGroups do `charts/loopzero-admission-policies` passa a vir de `values`, e o perímetro é declarado por quem compõe a cell |
| Política de resiliência da camada de dados | `loopzero-data-cells` | possivelmente nada — ver `#287` abaixo |
| **Acoplamento entre repositórios** | `loopzero-data-cells` | **sim — `infra/helmfile/cell.helmfile.yaml.gotmpl`**, ver abaixo |

### O corte de 2026-08-13: este repositório é o plano de **aplicações**, e metade do que ele guarda é substrato

O owner reformulou a fronteira: **o `loopzero` é o plano de aplicações** (build, deploy, workloads) e o
`loopzero-data-cells` é o plano de **dados** (memory, rag, lens, trust, promptly, skills). O que os dois
precisam — cell, vínculo tenant↔cell, `ClusterCredential`, composição de cell, guardrail de `DELETE` —
**não pertence a nenhum dos dois.** É substrato, e ele mora aqui por sedimentação: nasceu no único
repositório que existia.

O `CLAUDE.md` deste repositório declara *"THIS REPO IS THE ENGINE — API, operators, shared Go libs,
Helm charts, infra"*. **Metade dessa lista é substrato**, medido em 2026-08-13:

| Peça | Natureza |
|---|---|
| `infra/helmfile/{core,build,cell,secrets}.helmfile` | substrato |
| `charts/loopzero-admission-policies` | substrato |
| `operators/api/v1/clustercredential_types.go` | substrato |
| `charts/loopzero-crds` → `clustercredentials.yaml` | substrato (as outras 7 CRDs são plano de aplicações) |
| `infra/k3d/*.yaml` — os 5 clusters de dev | substrato |
| `api/internal/placement/engine.go` — tenant → cell | **indeciso**, deliberadamente |

O destino é o `loopzero-cluster-management`. **A decisão está em
[`loopzero-data-cells/docs/decisions/2026-08-13-substrate-plane-adr.md`](../../../loopzero-data-cells/docs/decisions/2026-08-13-substrate-plane-adr.md)**
— fora deste repositório porque a cadeia de raciocínio nasceu lá; migra para o
`loopzero-cluster-management` quando aquele repositório for montado.

**Três coisas que a reunião T3 precisa absorver, e nenhuma é confortável:**

1. **O dev loop deste repositório passa a depender do substrato.** `task setup` sobe 4 clusters k3d a
   partir de arquivos que sairiam daqui — e o repositório de destino **hoje não tem código**. A ordem
   correta é montar o destino primeiro e migrar o helmfile por último.
2. **`api/internal/placement/engine.go` fica marcado como indeciso de propósito.** A simetria diz que
   ele acompanha; ele roda em transação `SERIALIZABLE` com `pg_advisory_xact_lock` contra este banco,
   e a linha canônica é `tenants.cell_id`. Extraí-lo mexe em transação, schema e bootstrap ao mesmo
   tempo — medir antes, não decidir por simetria.
3. **Existem duas decisões de placement sobre o mesmo tenant e ninguém decidiu se são co-locadas** —
   este repositório decide *tenant → cell*, o `loopzero-data-cells` decide *tenant → shard*. Se não forem
   co-locadas, toda query de um tenant atravessa cluster.

### A revisão anterior de 2026-08-13: este repositório e o `loopzero-data-cells` não se referenciam

O desenho original punha o chart `loopzero-data-cell` em `loopzero/charts/` e o requisito do guardrail como
mudança de fonte aqui. Isso, somado ao `ClusterCredential` que o `loopzero-data-cells` lê, formava um
**ciclo** entre os dois repositórios. O ADR `cross-repo-coupling` (lá) o desfez, e a régua é:

> Artefato publicado e versionado é integração. Árvore de fontes de outro repositório é dependência.

Consequências **para cá**, e são pequenas:

- O chart `loopzero-data-cell` **não** vem para `charts/`. Nasce lá e é publicado no mesmo registry.
- O `charts/loopzero-admission-policies` ganha a lista de apiGroups protegidos em `values` — mudança
  única, que fecha a porta para o próximo requisito externo chegar como PR.
- **`infra/helmfile/cell.helmfile.yaml.gotmpl` referencia `../helm/argo-rollouts` e
  `../helm/admission-policies` por caminho relativo**, enquanto consome os charts de terceiros por
  versão pinada. Os componentes de fora estão mais desacoplados que os de dentro, e enquanto for
  assim a composição de uma cell só roda de dentro desta árvore. **Trocar por coordenada OCI é a
  única peça acionável hoje** — não muda comportamento e não depende de ratificação.
- O `#287` continua sendo trabalho daqui, mas **deixou de ser afirmado como bloqueio da camada de
  dados**: aquelas quatro métricas são do banco deste engine, e os shards são CNPG, que exporta as
  suas. A origem nunca foi medida.

**Duas decisões deste repositório são pressupostas por elas** e permanecem aqui:
[`capi-byoc-cluster-lifecycle`](2026-08-07-capi-byoc-cluster-lifecycle-adr.md) (§ D3, D12, D15) e
[`cell-cluster-unification`](2026-08-09-cell-cluster-unification-adr.md).
