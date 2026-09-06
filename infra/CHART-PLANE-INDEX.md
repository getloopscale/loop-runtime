# Índice chart → plano

Qual plano cada chart de `charts/` pertence, e **como isso foi provado**. Executa a terceira
bala do `B-048` e a consequência do ADR
[`2026-08-12-umbrella-chart-plane-separation`](../docs/wiki/decisions/2026-08-12-umbrella-chart-plane-separation.md).

## Por que este arquivo existe

O caminho de desenvolvimento gasta **quatro helmfiles** construindo a separação de planos
(`infra/helmfile/{core,build,cell-0,secrets}.helmfile.yaml.gotmpl`). Sem um índice, a única forma
de responder *"em que plano isto roda?"* era abrir os quatro e procurar — e a resposta errada não
falha, ela instala no cluster errado.

## Como ler a coluna Prova

A atribuição de plano é **medível**, e a medição tem um degrau que não pode ser apagado:

- **provado** — um helmfile declara `chart: ../../charts/<nome>`, então a linha fala do **mesmo
  artefato** que este índice.
- **inferido** — o plano vem do nome, não de uma declaração. O repositório tem **dois charts com o
  mesmo nome em árvores diferentes** (`infra/helm/` para bootstrap, `charts/` para OCI — `CLAUDE.md`
  § Helm chart dual system, item `B-056`), e uma linha `chart: ../helm/<nome>` prova o plano do
  chart de *bootstrap*, não deste. Confundir os dois foi o erro que a v1.1 do ADR corrigiu.

## O índice

| Chart | Plano | Prova |
|---|---|---|
| `loopzero-crds` | core | **provado** — `infra/helmfile/core.helmfile.yaml.gotmpl:173` |
| `loopzero-tenant-operator` | core | **provado** — `core.helmfile.yaml.gotmpl:551` |
| `loopzero-app-operator` | core | **provado** — `core.helmfile.yaml.gotmpl:576` |
| `loopzero-preview-operator` | core | **provado** — `core.helmfile.yaml.gotmpl:596` |
| `loopzero-platform-operator` | core | **provado** — `core.helmfile.yaml.gotmpl:616` |
| `loopzero-build-cwts` | **build** | **provado** — `build.helmfile.yaml.gotmpl:70-72` |
| `loopzero-api` | core (esperado) | **inferido** — nenhum helmfile declara `chart: ../../charts/loopzero-api` |
| `loopzero-cloud` | core (esperado) | **inferido** — idem |
| `loopzero-admission-policies` | core (esperado) | **inferido** — idem |
| `loopzero-monitoring` | — | **não declarado em lugar nenhum**; não há contraparte em `infra/helm/` |
| `loopzero-dashboard` | — | **não declarado em lugar nenhum**; idem |
| `loopzero-common` | — | biblioteca (`type: library`), não instala nada por si |
| ~~`loopzero-platform`~~ | — | **removido em 2026-08-19** — ver abaixo |

## O guarda-chuva foi REMOVIDO em 2026-08-19, e por que

`charts/loopzero-platform` declarava 12 dependências atravessando **core**, **build** e **cell**,
instaladas como um único release. O `B-048` decidiu que isso era composição single-cluster
**deliberada**, não descuido.

**Ele foi removido porque não instalava.** Medido em 2026-08-19: `helm template` sai com
`rc=1` e **zero linhas** — sem profile, com `profiles/dev.yaml` e com `profiles/prod.yaml`.
Os `values.schema.json` dos subcharts recusam `global` e `enabled`, que são exatamente as
chaves que uma composição guarda-chuva injeta.

Quebrado desde `1ce505758` (**2026-06-19**), dois meses, sem ninguém notar — porque nenhum
gate o renderizava. Não estava no bootstrap do ArgoCD nem no matrix de publicação, e o único
caminho de install era um alvo manual de taskfile, também quebrado.

**A decisão do `B-048` não morreu com ele:** vive no ADR
`docs/wiki/decisions/2026-08-12-umbrella-chart-plane-separation` e no histórico do git. O que
saiu foi a implementação não-funcional.

**Consequência medida, para quem for decidir depois:** `loopzero-monitoring` e `loopzero-dashboard`
tinham o guarda-chuva como **único** consumidor declarado. Com ele fora, ficam sem nenhum.
Removê-los é decisão distinta, e o `B-073` não a pediu.

Registro: `MEET-ASYNC-AMENDMENT-2026-08-19-remove-umbrella-chart.md`.

## Manutenção

Uma linha nova entra com a **prova** ou com a palavra **inferido**. Um índice que apaga a diferença
entre as duas é pior que não existir, porque quem lê passa a acreditar que tudo foi verificado.
