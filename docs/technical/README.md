# Technical Reference

Este diretório define a referência técnica de SOTA (state of the art) para os módulos principais do LoopZero.

O objetivo não é repetir implementação linha por linha. O objetivo é registrar:

- qual é a responsabilidade de cada módulo;
- quais domínios ele cobre;
- quais pastas são a fonte de verdade;
- qual é o padrão de engenharia esperado para considerar o módulo "SOTA" no contexto do LoopZero;
- quais invariantes e checklists devem guiar mudanças futuras.

## Mapa Por Pasta

| Pasta do repositório | Referência técnica |
|---|---|
| `cli/` | [docs/technical/cli/README.md](./cli/README.md) |
| `api/` | [docs/technical/api/README.md](./api/README.md) |
| `operators/` | [docs/technical/operators/README.md](./operators/README.md) |
| `cloud/` | [docs/technical/cloud/README.md](./cloud/README.md) |
| `infra/` | [docs/technical/infra/README.md](./infra/README.md) |
| `charts/` | [docs/technical/charts/README.md](./charts/README.md) |
| `pkg/` | [docs/technical/pkg/README.md](./pkg/README.md) |

## Critério SOTA Adotado

No LoopZero, um módulo é considerado SOTA quando atende simultaneamente estes critérios:

1. Tem responsabilidade clara e fronteiras explícitas.
2. Expõe contratos estáveis para o consumidor seguinte.
3. Tem observabilidade suficiente para operação e debug.
4. É testável em isolamento e em fluxo integrado.
5. Tolera concorrência, retry e falhas parciais sem corromper estado.
6. Evita acoplamento acidental com detalhes internos de outro plano do sistema.
7. Mantém paridade entre local, CI e produção.

## Barra De Validação

No LoopZero, teste de integração é necessário, mas não suficiente.

Para considerar um módulo realmente SOTA em uma PaaS como esta, a validação precisa subir por camadas:

1. Testes unitários validam invariantes locais.
2. Testes de integração validam boundaries internos do módulo.
3. Testes production-parity validam o comportamento entre componentes reais.
4. Para fluxos críticos, a plataforma precisa de evidência em ambiente real, dogfood ou equivalente operacional.

Regra prática:

- se um módulo governa deploy, auth, billing, bootstrap, runtime, cells, policies ou observabilidade da plataforma, apenas testes de integração não bastam.
- a evidência final precisa incluir execução com dependências reais ou um ambiente parity alto o suficiente para provar o comportamento sistêmico.

## Domínios Transversais

Os documentos por pasta usam estes domínios recorrentes:

- `deploy`: fluxo principal de build, release e publicação.
- `identity`: autenticação, autorização, sessão e billing/account context.
- `git/source`: origem de código, integração com GitHub e materialização do source bundle.
- `runtime`: estado desejado e estado observado da aplicação.
- `preview`: ambientes efêmeros por PR.
- `network/edge`: domínios, roteamento, CDN, TLS e Cloudflare.
- `control-plane`: operadores, APIs internas e reconciliação.
- `observability`: métricas, tracing, logs e sinais de saúde.
- `platform/bootstrap`: criação e evolução da plataforma base.

## Como Usar

- Antes de editar um módulo, leia a referência correspondente.
- Se uma mudança altera fronteira de domínio, atualize o documento do módulo afetado.
- Se uma responsabilidade migrar de pasta, atualize também este índice.
