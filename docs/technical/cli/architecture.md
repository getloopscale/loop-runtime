# CLI Architecture

## Escopo

`cli/` é a interface primária do produto. Seu papel é transformar intenção do operador em chamadas consistentes ao control plane, preservando UX humana e automação por contrato.

## Mapa Por Pasta

| Pasta | Responsabilidade principal | Domínio |
|---|---|---|
| `cli/src/index.ts` | bootstrap, parsing global, wiring e lifecycle do processo | runtime da CLI |
| `cli/src/register/` | composição dos grupos de comandos | command topology |
| `cli/src/commands/` | execução de casos de uso por comando | deploy, project, team, env, preview, platform |
| `cli/src/api/` | client HTTP, SSE, retry, auth refresh | transporte |
| `cli/src/auth/` | device flow, token refresh, abertura de browser | identity |
| `cli/src/output/` | formatação humana, JSON, prompts, renderer de deploy | UX/output |
| `cli/src/config/` | resolução de URL, config local, token store | config/state |
| `cli/src/detection/` | detecção de framework e monorepo | local project intelligence |
| `cli/src/adapters/` | source bundle, git, loopzero.yaml, validação local | source/config |
| `cli/src/tracing/` | spans locais e propagação para API | observability |
| `cli/src/installer/` | bootstrap/instalação de plataforma | platform/bootstrap |
| `cli/tests/unit/` | contratos locais e comportamento isolado | test |
| `cli/tests/integration/` | contratos fim a fim no boundary do processo | test |

## Domínios Funcionais

### `command-runtime`

- Define o boundary do processo `loopzero`.
- Controla parse, help, version, wiring global e cleanup.
- É a camada que deve assegurar comportamento consistente em invocação interativa e por script.

Arquivos âncora:

- `cli/src/index.ts`
- `cli/src/register/*.ts`

### `deploy-runtime`

- Faz pré-validação local, empacotamento de source, upload, submissão de build e stream de progresso.
- Deve falhar cedo em configuração inválida e falhar tarde apenas para erros realmente remotos.

Arquivos âncora:

- `cli/src/commands/deploy.ts`
- `cli/src/commands/deploy-helpers.ts`
- `cli/src/output/deploy-renderer.ts`
- `cli/src/adapters/source-bundle.ts`

### `identity`

- Guarda e renova credenciais.
- Separa token store local de fluxo de autenticação remoto.
- Deve funcionar tanto com config persistida quanto com `LOOPZERO_TOKEN` em CI.

Arquivos âncora:

- `cli/src/auth/device-flow.ts`
- `cli/src/auth/token-refresh.ts`
- `cli/src/config/store.ts`

### `transport`

- Encapsula HTTP, retry, correlation e SSE.
- Não deve vazar detalhes de fetch/headers para comandos.

Arquivos âncora:

- `cli/src/api/client.ts`
- `cli/src/api/sse-client.ts`

### `output-contracts`

- Separa saída humana de saída automatizável.
- Centraliza padrões de formatação, prompts e erros.

Arquivos âncora:

- `cli/src/output/formatter.ts`
- `cli/src/output/error-message.ts`
- `cli/src/output/prompt-confirm.ts`

### `local-project-intelligence`

- Entende o workspace local antes de chamar backend.
- Deve detectar configuração ausente, inválida ou incompleta.

Arquivos âncora:

- `cli/src/adapters/loopzero-yaml.ts`
- `cli/src/adapters/validate-config.ts`
- `cli/src/detection/framework.ts`
- `cli/src/detection/monorepo.ts`

## Fronteiras Arquiteturais

### O que pertence à CLI

- Parse de argumentos.
- UX local, help e mensagens acionáveis.
- Pré-validação de config e input.
- Persistência local de credenciais.
- Renderização de progresso.
- Retry de transporte, desde que sem reinventar regra de negócio.

### O que não pertence à CLI

- Regra canônica de autorização.
- Verdade final sobre estado de deploy.
- Controle de concorrência de ambiente.
- Persistência operacional.
- Decisão final de admissibilidade de release.

## Regra De Ouro

A CLI pode antecipar erro do usuário. A CLI não pode inventar verdade de sistema.
