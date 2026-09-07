# ADR — Nomenclatura de topologia de clusters

**Data:** 2026-08-09
**Status:** ACCEPTED with 2026-09-04 dated freeze on 8 named survivors (see § Exception list) — originally ACCEPTED for `mgmt`→`core` and `vault`→`secrets`; the `cell` half landed in code by 2026-08-17 (see § A metade bloqueada — cell); the remaining unrenamed occurrences are frozen AS-IS per the sponsor delegation of 2026-09-04 (`BACKLOG.md § B-001` `decided_by`, `.claude/rules/decision-delegation.txt`); the 3 rows that carried the open trigger `sponsor decision` were resolved on 2026-09-06 (see § Decision on the domain term, filed as `B-174`), leaving only `AM8` and `never — …` triggers outstanding
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

**How to read this section.** Each row names path, identifier, resource kind (from the closed set `live ArgoCD Application` / `live DOKS cluster` / `sponsor design decision` / `historical preservation`), revisit trigger (from the closed set `AM8` / `sponsor decision` / `never — domain term` / `never — historical`), and a one-sentence `why:` in the row itself so no citation-only reference is needed. The exception list is **NOT** extended without a delegation-authorised amendment: a new occurrence of an old identifier NOT in this list is drift, not a survivor.

| Path | Identifier | Kind | Revisit trigger | Why |
|---|---|---|---|---|
| `infra/argocd-bootstrap/apps/cell-cluster-issuer.yaml` | `runtime-cluster-issuer` (`name:`) | live ArgoCD Application | AM8 | why: this is the ArgoCD Application object's `.metadata.name`; renaming means `argocd app delete` then re-create, which loses sync state and reconciliation history. The rename waits for AM8 because the DOKS re-provisioning re-creates it anyway. |
| `infra/argocd-bootstrap/validate-sync-waves.py` | `("runtime-traefik", "runtime-cluster-issuer")` (pair in tuple list) | live ArgoCD Application | AM8 | why: companion validator that pins the Application name from the sibling row above; must move in lockstep with that row so the pair does not diverge mid-cutover. |
| `infra/pulumi-drill/src/index.ts` | `${doProject}-runtime-cluster` (`clusterName:`, L172) | live DOKS cluster | AM8 | why: this is the live DigitalOcean Kubernetes cluster's `clusterName`; renaming means destroy and re-create a running production cluster, which is what the delegation's "rename no live resource" clause explicitly forbids. Pulumi is legacy AS-IS until the AM8 AWS re-platform per `CLAUDE.md`. |
| `infra/pulumi-shared/__tests__/contracts.test.ts` | `runtimeClusterName: "loopzero-runtime"` (L99) | live DOKS cluster | AM8 | why: the structural contract test that pins the stack-output key produced by the row above. It asserts the shape the live DOKS cluster publishes, so it can only change when that cluster's name changes — at AM8, in lockstep with `pulumi-drill/src/index.ts`. Renaming the assertion alone would make the test pass against a name the infrastructure does not emit. |
| `operators/internal/argocd/real_provisioner.go` | `single-runtime-cluster` (comment L465) | sponsor design decision | `never — domain term` (was `sponsor decision`; resolved 2026-09-06, see § Decision on the domain term) | why: this is the one row of the three that genuinely carries both senses on one line — a topology (`single-runtime-cluster`, an install with exactly one runtime cluster) beside a literal ArgoCD Secret name (`"runtime-argocd-cluster"`). Renaming either would make the comment describe an install shape or a Secret that does not exist. Resolved 2026-09-06: the domain term stays and the comment now states which sense each occurrence carries. |
| `operators/internal/preview/preview_controller.go` | `runtime-cluster` (comment L697) | sponsor design decision | `never — domain term` (was `sponsor decision`; resolved 2026-09-06, see § Decision on the domain term) | why: **not** the same trap as the row above, as the 2026-09-06 re-measurement found: this line carries the domain sense alone and names no cluster. Its real ambiguity is with Go's `runtime` package, which the file imports (L21) and uses for `*runtime.Scheme` (L78). Resolved 2026-09-06: the domain term stays and the comment now says so. |
| `operators/internal/tenant/tenant_controller.go` | `cell→runtime-cluster mapping` (comment L85) | sponsor design decision | `never — domain term` (was `sponsor decision`; resolved 2026-09-06, see § Decision on the domain term) | why: also the domain sense alone, not both. The design call this row escalated — migrate to `cell→cell-cluster`, or keep the term — was **decided on 2026-09-06 in favour of keeping it**: a cell maps *to* a runtime cluster, so `cell→cell-cluster` is circular and would erase the mapping the sentence names. The comment now records that rejection. |
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
that ships alongside the AM8 cluster rename. The trigger `sponsor decision` is **no longer in use**: the three rows that
carried it were closed on 2026-09-06 by § Decision on the domain term below, filed as `B-174`. It is
kept in the closed set above so the rows' `was` annotation stays readable. For the row with trigger `never — historical`: no revisit; the row documents a preserved
artefact.

## Decision on the domain term (2026-09-06)

**Decision: `runtime` stays the domain term.** The three `sponsor design decision` rows above move to
trigger `never — domain term`. This section is the ADR that the revisit protocol asked for, filed as
`B-174`.

The escalation asked one question. Those three sites use `runtime` both as a cluster's name and as the
domain term for the class of cluster that runs tenant workloads; either the cluster name moves or the
domain term does. Measured at HEAD `cadadb9c9` on 2026-09-06:

**1. The cluster name is not reachable by a rename — it lives in running systems, not only here.**

| Where | What | Cost of renaming |
|---|---|---|
| `infra/pulumi-drill/src/index.ts:172` | `clusterName: ${doProject}-runtime-cluster` — a live DOKS cluster | destroy and re-create a running cluster |
| `infra/pulumi-shared/__tests__/contracts.test.ts:99` | `runtimeClusterName: "loopzero-runtime"` — pins the stack output that cluster publishes | the assertion would stop matching what the infrastructure emits |
| `infra/argocd-bootstrap/apps/cell-cluster-issuer.yaml:4` | a live ArgoCD Application's `.metadata.name` | `argocd app delete` then re-create, losing sync state |
| `infra/pulumi-platform/src/index.ts:187,193` → `components/bootstrap-shared.ts:172` | stamps `loopzero.io/cluster-role: runtime` onto a live Namespace | mutating a label on a running object |
| `infra/argocd-bootstrap/apps/cell-*.yaml` (12 files, L8) | `loopzero.io/cluster-role: runtime` on live Applications | the same, twelve times |

The sponsor delegation of 2026-09-04 says *rename no live resource*. Every row above is a live resource,
which is why the four already in the exception list carry trigger `AM8`. That half of the question was
therefore already answered before the escalation was written; only the domain term was ever open.

The `loopzero.io/cluster-role: runtime` rows are **not** drift under the rule stated above, and their absence
from the exception list is not an omission: that list is measured with the old *cluster identifiers*
(`loopzero-runtime`, `runtime-cluster`, …), which a bare label value does not match. They are recorded here
because they are where the escalation's answer actually lives.

**2. The replacement the exception rows floated is wrong on the merits.** The `tenant_controller.go` row
proposed migrating the vocabulary to `cell→cell-cluster`. A cell and a runtime cluster are not the same
thing: `2026-08-09-cell-cluster-unification-adr.md` records `cell-1` as *"Maps to runtime-1 … cluster"*
and treats the dev-environment conflation of those two identities as the defect it is separating.
`cell→cell-cluster` is circular — it deletes the mapping the sentence exists to name.

**3. The cost of moving the domain term is 149 identifiers across 22 files** — `RuntimeClientFactory`
(63), `RuntimeClient` (50), `RuntimeFactory` (36), `runtimeClientForPreview` (2), measured over
`operators api pkg cmd`; most are exported API surface. `2026-05-21-naming-conventions-adr.md:100`
already points the other way: its deferred rename is `runtime2ClusterName` → `runtimeCell2ClusterName`,
which keeps `runtime` and qualifies it.

**What this changes in the code.** Keeping the word does not by itself fix the ambiguity the item was
opened for, so each of the three sites now states which sense it carries, in the comment itself. Writing
those notes corrected the item's own framing: only `real_provisioner.go` carries both senses on one line
(`single-runtime-cluster`, a topology, beside `"runtime-argocd-cluster"`, a literal Secret name).
`preview_controller.go:697` and `tenant_controller.go:85` carry the domain sense **alone** — their real
collision is with Go's `runtime` package, which both files import and use for `*runtime.Scheme` within a
few lines of the comment.

The notes are worded so they do not themselves match the survivor grep. Quoting the term while defining
it would have raised that grep from 8 to 11 and planted three false positives in a detector whose whole
job is to flag new occurrences.

**Out of scope.** The cluster name and the `loopzero.io/cluster-role: runtime` label value are unchanged and
stay on trigger `AM8`.

## Referências

- Medição: `.claude/knowledge-base/discoveries/opportunities/cluster-topology-naming-opportunity.md`
- Plano: `.claude/knowledge-base/plans/cluster-rename-core-secrets-plan.md`
- SPOF do cluster central: `.claude/knowledge-base/discoveries/opportunities/core-cluster-spof-opportunity.md`
- Metade bloqueada: `.claude/knowledge-base/discoveries/opportunities/cell-cluster-unification-opportunity.md`
- Gatilho: `.claude/knowledge-base/plans/helm-topology-index-plan.md:198-200`
