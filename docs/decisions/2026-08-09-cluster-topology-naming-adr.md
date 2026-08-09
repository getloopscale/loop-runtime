# ADR — Nomenclatura de topologia de clusters

**Data:** 2026-08-09
**Status:** **ACCEPTED** para `mgmt`→`core` e `vault`→`secrets`; **PROPOSED** para a metade `cell`
**Tier:** T2+ (`.claude/rules/pre-release-governance.md`) — toca operações e o caminho crítico de deploy
**Sponsor:** CTO
**Emenda a:** `docs/decisions/2026-08-07-capi-byoc-cluster-lifecycle-adr.md` (PROPOSED, T3), onde o
ciclo de vida de cluster já está desenhado. Este ADR não compete com aquele: acrescenta o vocabulário.

## Contexto

Três defeitos de nomenclatura, medidos em 2026-08-09 sobre a árvore `26ba1340`
(`.claude/knowledge-base/discoveries/opportunities/cluster-topology-naming-opportunity.md`):

1. **O cluster de tenants tem três nomes.** `loopzero-workload` na config k3d, `loopzero-runtime-2` no segundo,
   `runtime` no helmfile e nos secrets — e `cell` no código Go, que é o vocabulário do domínio.
2. **`mgmt` não é um cluster de gestão.** Ele roda 55 releases, contra 28 no de tenants, 11 no de build
   e 8 no de secrets, incluindo `loopzero-api`, `loopzero-cloud`, os 4 operators, Ory, Gitea, ArgoCD, Postgres e
   Harbor. `B-002` mediu que ele é SPOF **total** do plano de controle. O nome faz o time raciocinar
   sobre ele como infraestrutura de suporte descartável.
3. **O cluster `vault` roda OpenBao.** `infra/helmfile/vault.helmfile.yaml.gotmpl` usa o chart
   `openbao-helm` do OpenBao org, binário `bao`. O cluster, o namespace e o helmfile carregam a marca de
   um produto da HashiCorp que não é usado.

## Decisão

| Atual | Novo | Status |
|---|---|---|
| `mgmt` / `loopzero-mgmt` | **`core`** | ACCEPTED |
| `vault` / `loopzero-vault` / `vault-system` | **`secrets`** / `secrets-system` | ACCEPTED |
| `build` / `loopzero-build` | **inalterado** | ACCEPTED |
| *(cluster de CAPI, ainda inexistente)* | **`fleet`** | ACCEPTED — nasce com o nome, sem trabalho de rename |
| `workload` + `runtime` (cluster de tenants) | `cell-N` | **PROPOSED** — ver § A metade bloqueada |
| `mgmt` como nome reciclável | **quarentena** | ACCEPTED |

## Rationale e alternativas rejeitadas

### `core`

O cluster **é** o produto, não a gestão dele. As alternativas rejeitadas descrevem função de suporte —
exatamente o erro que `mgmt` comete:

- **`control`** — rejeitada. Descreve papel, e o papel é metade da verdade: ele não só controla, ele
  hospeda o produto inteiro.
- **`platform`** — rejeitada. Genérico ao ponto de não distinguir este cluster dos outros três, que
  também são plataforma.
- **`ops`** — rejeitada por descrever suporte, e ainda colide com `loopzero-ops`, o binário de qualidade.

### `secrets`

O nome descreve o que o cluster guarda e **sobrevive à troca do produto** — se o OpenBao for substituído
amanhã, `secrets` continua correto.

- **`bao`** — rejeitada. Trocaria uma marca de terceiro por outra, repetindo o defeito que se corrige.
  O produto continua sendo nomeado `openbao`/`bao` **onde ele é o produto**; o que muda é o nome do
  cluster e do namespace.

### `build` não muda

Já diz exatamente o que é, e numera bem se houver um segundo. Renomear seria mudança sem ganho, e a
medição registrou o baseline para provar que ele ficou intocado: 657 ocorrências de `loopzero-build` em 253
arquivos, 122 de `build-cluster` em 69.

### `fleet` para o cluster de CAPI, e `mgmt` em quarentena

O cluster de gerenciamento de ciclo de vida (AM5, `loopzero-workspace/ROADMAP_AWS.md:79`, hoje `[ ]`) nasce
como **`fleet`**. Não se recicla `mgmt` para ele, ainda que o CAPI chame o dele de *management cluster*:
um nome que acabou de mudar de dono fica ambíguo em todo log, runbook e ticket antigo por meses, e a
ambiguidade cai justamente sobre quem está lendo um incidente antigo sob pressão.

## A metade bloqueada — `cell`

O mapa original propunha `loopzero-workload` → `cell-1` e `loopzero-runtime-2` → `cell-2`. **A medição o
refutou.** `infra/helmfile/scripts/bootstrap-cell-cc.sh:20-22` mostra que `cell-1` e `cell-2` já existem
como ids de cell, e que `cell-default` **e** `cell-1` apontam para o mesmo cluster `loopzero-workload` —
relação 2:1. Nomear o cluster `cell-1` faria o nome do cluster coincidir com o de uma das duas cells que
rodam nele: o mesmo defeito, uma camada acima.

O CTO decidiu resolver pela raiz — unificar cell↔cluster em 1:1, aposentando `cell-default`. Isso é
**migração de dados, não rename**, e virou `B-005`, T3. A medição de `B-005`
(`.claude/knowledge-base/discoveries/opportunities/cell-cluster-unification-opportunity.md`) mostrou por
quê: `cell-default` não é um valor, é uma **sentinela** no trigger `populate_cell_id_from_tenant`
(`api/internal/db/migrations/000001_initial_schema.up.sql:66-74`), tratada como equivalente a `NULL` e
significando *"ainda não resolvido, herde do tenant"*, armada em quatro tabelas.

Esta metade fica `PROPOSED` até a reunião T3 do `B-005` decidir quem passa a portar esse sentido.

## Consequências

**Aceitas:**

- 1.388 ocorrências vivas mudam em 312 arquivos. O `cluster-rename-guard` (T0.1) é o revisor real desse
  volume: prova que nenhuma das 70 linhas classificadas genéricas foi tocada.
- O namespace `vault-system` → `secrets-system` é **recurso vivo**: recriação e migração de cargas, com
  ordem de aplicação declarada no plano.
- Documentação histórica **preserva os nomes antigos** — 147 dos 351 arquivos que o grep acusa são audit
  trail, e reescrevê-los destruiria a única evidência do que era verdade na data
  (`.claude/rules/audit-trail-rotation.md`).
- `loopzero-workspace` carrega 111 ocorrências em 25 arquivos e fica como followup naquele repositório.
- Os clusters DigitalOcean vivos (`do-sfo3-loopzero-infra-dev-sfo3-{mgmt,runtime,...}`) **não** são
  renomeados: são da plataforma legada até o cutover AM8, e renomear cluster vivo é migração.

**Rejeitada:** adiar o rename até depois do AM5. A janela é agora porque os clusters AWS ainda não
existem — feito hoje, eles nascem com o nome certo. Feito depois, são quatro clusters vivos a migrar.

## Referências

- Medição: `.claude/knowledge-base/discoveries/opportunities/cluster-topology-naming-opportunity.md`
- Plano: `.claude/knowledge-base/plans/cluster-rename-core-secrets-plan.md`
- SPOF do cluster central: `.claude/knowledge-base/discoveries/opportunities/core-cluster-spof-opportunity.md`
- Metade bloqueada: `.claude/knowledge-base/discoveries/opportunities/cell-cluster-unification-opportunity.md`
- Gatilho: `.claude/knowledge-base/plans/helm-topology-index-plan.md:198-200`
