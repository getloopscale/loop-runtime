# Exceção de securityContext — `argo-events`

**Data:** 2026-08-12 · **Status:** ACCEPTED · **Item:** `B-045` (bala 3)
**Revisar até:** 2026-11-09

## Por que este chart não declara os quatro campos

Ele **não tem pod spec própria**. `Chart.yaml` declara uma dependência do chart upstream
`argo-events` 2.4.4 (pinado, com Renovate subindo patches), e este diretório é um *wrapper*: ele
fornece valores, não templates de contêiner.

Endurecer aqui não é adicionar `securityContext` a um template nosso — é sobrescrever a árvore de
valores do upstream, cujo esquema muda entre versões menores. Feito às cegas, um `values` que o
upstream renomeia deixa de aplicar **em silêncio**, e o chart volta a rodar sem endurecimento com o
gate verde. Esse modo de falha é pior que a lacuna: ele a esconde.

## O que esta exceção NÃO diz

Não diz que o argo-events deve rodar privilegiado. Diz que o endurecimento dele é um trabalho de
**overlay de valores contra um esquema upstream versionado**, que precisa ser verificado contra o
render real — e que fazê-lo sem essa verificação produz garantia falsa.

## Condição de saída

Ao subir a versão do chart upstream: ler o `values.yaml` dele, mapear os campos de `securityContext`
que ele expõe, aplicá-los aqui, e confirmar no `helm template` que saem no manifesto renderizado.
Confirmado isso, este arquivo é **deletado** — não atualizado.
