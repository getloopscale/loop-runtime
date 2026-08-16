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
| `loopzero-platform` | **os três de uma vez** | é o guarda-chuva — ver abaixo |

## O guarda-chuva instala três planos num cluster, e isso é o desenho

`charts/loopzero-platform` declara 12 dependências que atravessam **core**, **build** e **cell**, e as
instala como **um único release Helm**. O ADR decidiu que isso é **composição single-cluster
deliberada**, não descuido — e a limitação está escrita no próprio chart
(`charts/loopzero-platform/README.md` e a anotação `loopzero.io/topology` no `Chart.yaml`).

**O que se perde ao usá-lo:** a separação de planos que os quatro helmfiles constroem. O plano de
build deixa de ser um cluster próprio; o isolamento entre control plane e workload some; e um
`helm uninstall` alcança os três de uma vez.

**Quem o usa hoje: ninguém em produção.** Os consumidores medidos são todos de teste ou de template
(`taskfiles/helm.yml:32,:37,:42,:108`, `infra/tests/helm/loopzero-platform/defaults_test.go`,
`infra/tests/helm/envparity/envparity_test.go:51`). O bootstrap do ArgoCD referencia
**sub-charts individualmente, nunca o guarda-chuva** — os únicos `path: charts/*` em
`infra/argocd-bootstrap/apps/` são quatro Applications, uma por operator.

Se ele deve continuar existindo é a pergunta do **`B-073`**, e ela é diferente desta: aqui está
decidido *o que ele é*; lá se decide *se vale mantê-lo*.

## Manutenção

Uma linha nova entra com a **prova** ou com a palavra **inferido**. Um índice que apaga a diferença
entre as duas é pior que não existir, porque quem lê passa a acreditar que tudo foi verificado.
