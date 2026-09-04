# ADR — Nomenclatura de topologia de clusters

**Data:** 2026-08-09
**Status:** ACCEPTED with 2026-09-04 dated freeze on 8 named survivors (see § Exception list) — originally ACCEPTED for `mgmt`→`core` and `vault`→`secrets`; the `cell` half landed in code by 2026-08-17 (see § A metade bloqueada — cell); the remaining unrenamed occurrences are frozen AS-IS per the sponsor delegation of 2026-09-04 (`BACKLOG.md § B-001` `decided_by`, `.claude/rules/decision-delegation.txt`)
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

O cluster de gerenciamento de ciclo de vida (AM5, `ROADMAP_AWS.md:79` no umbrella `loopzero-workspace`, hoje `[ ]`) nasce
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

## Exception list — 2026-09-04 dated freeze

The 2026-08-17 re-measurement of B-001 identified 8 remaining occurrences of the old cluster identifiers. Re-measured on 2026-09-04 at HEAD `7649170a` over the executable tree (`cmd/ api/ operators/ pkg/ infra/ charts/ taskfiles/ tools/`): **all 8 are still on disk**, one per row below. Per the sponsor delegation recorded in `BACKLOG.md § B-001` `decided_by` (governed by `.claude/rules/decision-delegation.txt`, commit `7565db99c`), the 8 survivors below are **frozen AS-IS** — rename no live resource, drop the cleanroom bullet, and record the freeze here so a reader who greps for the old identifier lands on the row that explains it.

**How to read this section.** Each row names path, identifier, resource kind (from the closed set `live ArgoCD Application` / `live DOKS cluster` / `sponsor design decision` / `historical preservation`), revisit trigger (from the closed set `AM8` / `sponsor decision` / `never — historical`), and a one-sentence `why:` in the row itself so no citation-only reference is needed. The exception list is **NOT** extended without a delegation-authorised amendment: a new occurrence of an old identifier NOT in this list is drift, not a survivor.

| Path | Identifier | Kind | Revisit trigger | Why |
|---|---|---|---|---|
| `infra/argocd-bootstrap/apps/cell-cluster-issuer.yaml` | `runtime-cluster-issuer` (`name:`) | live ArgoCD Application | AM8 | why: this is the ArgoCD Application object's `.metadata.name`; renaming means `argocd app delete` then re-create, which loses sync state and reconciliation history. The rename waits for AM8 because the DOKS re-provisioning re-creates it anyway. |
| `infra/argocd-bootstrap/validate-sync-waves.py` | `("runtime-traefik", "runtime-cluster-issuer")` (pair in tuple list) | live ArgoCD Application | AM8 | why: companion validator that pins the Application name from the sibling row above; must move in lockstep with that row so the pair does not diverge mid-cutover. |
| `infra/pulumi-drill/src/index.ts` | `${doProject}-runtime-cluster` (`clusterName:`, L172) | live DOKS cluster | AM8 | why: this is the live DigitalOcean Kubernetes cluster's `clusterName`; renaming means destroy and re-create a running production cluster, which is what the delegation's "rename no live resource" clause explicitly forbids. Pulumi is legacy AS-IS until the AM8 AWS re-platform per `CLAUDE.md`. |
| `infra/pulumi-shared/__tests__/contracts.test.ts` | `runtimeClusterName: "loopzero-runtime"` (L99) | live DOKS cluster | AM8 | why: the structural contract test that pins the stack-output key produced by the row above. It asserts the shape the live DOKS cluster publishes, so it can only change when that cluster's name changes — at AM8, in lockstep with `pulumi-drill/src/index.ts`. Renaming the assertion alone would make the test pass against a name the infrastructure does not emit. |
| `operators/internal/argocd/real_provisioner.go` | `single-runtime-cluster` (comment L465) | sponsor design decision | sponsor decision | why: the identifier is both a historical cluster name and a Go domain-term describing the single-runtime-cluster legacy path. The item's own text mandates escalating this to the sponsor rather than renaming inside a slice, because a text rename here would either lie about the code's actual shape or force a code-shape change that is not documentation. |
| `operators/internal/preview/preview_controller.go` | `runtime-cluster` (comment L697) | sponsor design decision | sponsor decision | why: same trap as the row above — the term describes the domain concept `every runtime-cluster` in a comment about the preview controller's cell mapping. A textual rename would decouple the comment from the concept it names. |
| `operators/internal/tenant/tenant_controller.go` | `cell→runtime-cluster mapping` (comment L85) | sponsor design decision | sponsor decision | why: same trap; the comment describes the `ClusterCredentials` resolution as "cell→runtime-cluster mapping". The design call is whether to migrate the domain vocabulary here to `cell→cell-cluster` or keep the current term. Escalated. |
| `taskfiles/k3d.yml` | `loopzero-mgmt` (comment ~L134-137) | historical preservation | never — historical | why: the comment describes the historical bug where `loopzero-mgmt` was inexistent while `core-cluster.yaml` was expected — the audit trail of a fixed defect. DoD-1 of B-001 explicitly preserves "documentos históricos" (historical documents), and rewriting this comment would erase the record of what was true before. |

**Correction of 2026-09-04 — the list is 8 rows, not 7.** An earlier pass on this same day dropped the
`pulumi-shared` row and recorded it as *"delivered by deletion"*. That was a misread of the path, not a
measurement: the B-001 2026-08-17 table writes the pair as `infra/pulumi-drill/src/index.ts` **+**
`pulumi-shared/__tests__/contracts.test.ts`, where the second path is relative to `infra/`, and the pass
resolved it as `infra/pulumi-drill/pulumi-shared/…`, which never existed in this repository
(`git log --diff-filter=D -- 'infra/pulumi-drill/pulumi-shared/*'` is empty). The file is
`infra/pulumi-shared/__tests__/contracts.test.ts`, it is present at HEAD, and it carries the identifier
on line 99. Re-measured at `7649170a` over `cmd/ api/ operators/ pkg/ infra/ charts/ taskfiles/ tools/`:

```
$ git grep -n 'loopzero-workload\|workload-cluster\|loopzero-runtime\|runtime-cluster\|loopzero-mgmt' \
    -- cmd api operators pkg infra charts taskfiles tools | wc -l
8
```

Eight lines, eight rows above, one-to-one. The count the item recorded on 2026-08-17 was right; the
correction was to the path, and it is written here rather than silently applied because a survivor
declared dead is the one kind of exception nobody re-checks.

**Scope of the grep that measures this list.** The DoD-1 pattern as originally written also matches
`loopzero-core` and `core-cluster` — the names this ADR *introduced*. At HEAD those two account for 261 of
the 267 executable-tree hits, so the literal DoD-1 grep can no longer be read as a survivor count. The
list above is measured with the **old** identifiers only. This is a property of the DoD text, not drift:
DoD-1 was written before the rename landed and was superseded by the delegation of 2026-09-04.

**Revisit protocol.** For rows with trigger `AM8`: closed by the ADR amendment (or a superseding ADR)
that ships alongside the AM8 cluster rename. For rows with trigger `sponsor decision`: closed by an ADR
recording the design decision the escalation was for, filed as its own `B-NNN` — **no such item exists
today**, so this trigger currently names a condition with no owner; whoever opens it should say so in
that item. For the row with trigger `never — historical`: no revisit; the row documents a preserved
artefact.

## Referências

- Medição: `.claude/knowledge-base/discoveries/opportunities/cluster-topology-naming-opportunity.md`
- Plano: `.claude/knowledge-base/plans/cluster-rename-core-secrets-plan.md`
- SPOF do cluster central: `.claude/knowledge-base/discoveries/opportunities/core-cluster-spof-opportunity.md`
- Metade bloqueada: `.claude/knowledge-base/discoveries/opportunities/cell-cluster-unification-opportunity.md`
- Gatilho: `.claude/knowledge-base/plans/helm-topology-index-plan.md:198-200`
