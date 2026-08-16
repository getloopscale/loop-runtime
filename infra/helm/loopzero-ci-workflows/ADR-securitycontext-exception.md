# Exceção de securityContext — `loopzero-ci-workflows`

**Data:** 2026-08-12 · **Status:** ACCEPTED · **Item:** `B-045` (bala 3)
**Revisar até:** 2026-11-09

## O que este chart é, medido

Ele **tem** contêineres reais — e uma medição anterior deste item afirmou o contrário, por procurar
`containers:` no plural quando o Argo usa `container:` no singular. São dois:

- `workflowtemplate-quality-gate.yaml:69` — `loopzero-ops`, que roda os quality gates
- `workflowtemplate-quality-gate.yaml:89` — `alpine/curl`, digest-pinado (#232)

Mais os quatro CronWorkflows que os invocam. A classificação `NON-CONFORMANT` do gate está
**correta**; não é falso positivo.

## Por que a exceção, e não o endurecimento, nesta fatia

Três dos quatro campos são seguros de adicionar sem verificação (`runAsNonRoot`,
`allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`). O quarto não é:
**`readOnlyRootFilesystem: true` quebra qualquer contêiner que escreva na raiz**, e o `loopzero-ops`
escreve — ele compila e roda gates. Fechá-lo exige montar um `emptyDir` em `/tmp` e **verificar que
os quatro CronWorkflows continuam passando**, o que é validação LIVE contra o cluster de build.

O `.claude/rules/pre-release-governance.md` classifica mudança em workload em execução como T2:
reunião **antes** do código. E o `cleanroom-up-live-test-mandate.md` exige Cleanroom + LIVE para
validá-la. Nenhum dos dois está disponível nesta sessão.

Adicionar os três campos fáceis e deixar o quarto sem menção seria a pior das opções: o gate ficaria
verde por `PARTIAL` sem que ninguém soubesse que o campo que faltava é o que exige trabalho.

## Condição de saída

Uma fatia própria, com reunião T2 e validação LIVE: `emptyDir` em `/tmp`, os quatro campos nos dois
contêineres, e os quatro CronWorkflows verificados no cluster de build. Confirmado, este arquivo é
**deletado**.
