# Roadmap: loopzero.yml como Contrato Declarativo

> Data: 2026-03-25 (atualizado 2026-03-26)
> Status: Milestones 1-5 completos, M6-M7 implementados (pendente validacao DigitalOcean)
> Escopo: Evolucao de deteccao heuristica para contrato estrutural via loopzero.yml

---

## Contexto

Pipeline atual de deteccao e build:

```
loopzero init → heuristica (monorepo + framework + app type) → gera loopzero.yaml
loopzero deploy → parseia loopzero.yaml → upload bundle → API valida (superficial) → submete Workflow
```

Pipeline alvo:

```
loopzero init → heuristica (somente aqui) → gera loopzero.yml v2 completo
loopzero deploy → parseia loopzero.yml → valida estrito → normaliza AppUnit[] → BuildPlan → Workflow por app
```

### Problemas atuais

| Problema | Arquivo(s) | Impacto |
|---|---|---|
| Deteccao de framework por leitura de arquivos (fragil) | `cli/src/detection/framework.ts` | Falso positivo/negativo em frameworks nao cobertos |
| Inferencia de tipo por nome de diretorio | `cli/src/commands/init.ts` (`inferAppType`) | `worker` no nome → tipo worker (heuristica fragil) |
| Fallback silencioso para Node quando framework vazio | `api/internal/argo/languages.go` (`ResolveStrategy`) | Build com estrategia errada sem aviso |
| Sem validacao de path no filesystem | `cli/src/commands/deploy.ts` | Deploy prossegue com path inexistente |
| Schema raso — sem build strategy, sem env de build | `cli/src/adapters/loopzero-yaml.ts`, `api/internal/source/validator.go` | Pipeline infere tudo, nada e explicito |
| Sem conceito formal de build plan | `api/internal/services/build_orchestrator.go` | WorkflowBuilder recebe dados crus, resolve inline |

### Componentes impactados

| Componente | Arquivo(s) principal(is) | Tipo de mudanca |
|---|---|---|
| Schema loopzero.yml | `cli/src/adapters/loopzero-yaml.ts`, `api/internal/source/validator.go` | Campos novos |
| CLI validacao | `cli/src/commands/deploy.ts` | Validacao estrita |
| CLI init | `cli/src/commands/init.ts` | Gerar schema v2 |
| Modelo AppUnit | `cli/src/adapters/` (novo), `api/internal/services/` (novo) | Tipo novo |
| Build Plan | `api/internal/services/` (novo) | Tipo novo |
| WorkflowBuilder | `api/internal/argo/workflow_builder.go` | Receber BuildPlan |
| LanguageStrategies | `api/internal/argo/languages.go` | Remover fallback |
| Deteccao | `cli/src/detection/framework.ts`, `cli/src/detection/monorepo.ts` | Confinar ao init |

---

## Milestone 1 — Schema loopzero.yml v2

**Objetivo:** Definir o schema completo do loopzero.yml com todos os campos necessarios para build deterministico por app.

**Done:** Schema existe como tipo TypeScript no CLI e struct Go na API, com documentacao de cada campo, e testes unitarios validam serializacao/deserializacao de todos os campos.

- **Tarefa 1.1 — Definir tipo BuildStrategy com valores permitidos**
  - Acao: Criar enum/union `BuildStrategy` com valores `dockerfile` (Dockerfile existente), `buildpack` (auto-gerado por framework), `custom` (script de build do usuario). Adicionar ao tipo `LoopZeroYamlApp` no CLI (`cli/src/adapters/loopzero-yaml.ts`) e a struct `AppConfig` na API (`api/internal/source/validator.go`).
  - Done: Tipo `BuildStrategy` existe em ambos CLI e API. `parseLoopZeroYaml("apps:\n  api:\n    build: dockerfile")` retorna `{ build: "dockerfile" }`. `ValidateLoopZeroYAML` aceita os 3 valores e rejeita qualquer outro.

- **Tarefa 1.2 — Adicionar campo dockerfile ao schema**
  - Acao: Adicionar campo opcional `dockerfile: string` ao tipo `LoopZeroYamlApp` (CLI) e `AppConfig` (API). Quando `build: dockerfile`, este campo aponta para o Dockerfile relativo ao `path` da app. Default: `Dockerfile`.
  - Done: `parseLoopZeroYaml` retorna `dockerfile` quando presente. `ValidateLoopZeroYAML` aceita campo `dockerfile` e ignora quando `build != dockerfile`.

- **Tarefa 1.3 — Adicionar campo env ao schema**
  - Acao: Adicionar campo opcional `env: Record<string, string>` ao tipo `LoopZeroYamlApp` (CLI) e `Env map[string]string` ao `AppConfig` (API). Valores sao variaveis de ambiente de build (nao runtime).
  - Done: `parseLoopZeroYaml` retorna mapa `env` quando presente. `ValidateLoopZeroYAML` valida que keys seguem padrao `^[A-Z_][A-Z0-9_]*$`.

- **Tarefa 1.4 — Adicionar campo ignore ao schema raiz**
  - Acao: Adicionar campo opcional `ignore: string[]` ao tipo `LoopZeroYaml` (CLI) e `LoopZeroYAML` (API). Lista de glob patterns de diretorios que nao sao apps deployaveis (substitui hardcoded `PACKAGE_DIR_PATTERNS`).
  - Done: `parseLoopZeroYaml` retorna array `ignore`. `ValidateLoopZeroYAML` aceita campo `ignore` como lista de strings.

- **Tarefa 1.5 — Tornar type obrigatorio no schema v2**
  - Acao: Remover inferencia implicita de `type` no API. Quando `type` esta ausente no loopzero.yml, o CLI preenche durante `init` (inferencia no momento da geracao, nao no momento do deploy). O API rejeita `type` vazio.
  - Done: `ValidateLoopZeroYAML` retorna erro `"app 'X' must specify type: server, worker, or frontend"` quando `type` e vazio. CLI `init` sempre gera `type` explicito.

- **Tarefa 1.6 — Escrever testes de serializacao round-trip**
  - Acao: Escrever teste no CLI que gera loopzero.yml via `generateYaml()`, parseia o resultado via `parseLoopZeroYaml()`, e verifica que todos os campos (incluindo `build`, `dockerfile`, `env`, `ignore`) sobrevivem ao round-trip. Escrever teste equivalente no API com `yaml.Marshal` -> `ValidateLoopZeroYAML`.
  - Done: Testes de round-trip passam no CLI (Jest) e no API (Go testing). Todos os campos do schema v2 preservados.

---

## Milestone 2 — Validacao Estrita no CLI

**Objetivo:** O CLI falha com mensagem explicita e exit code correto quando o loopzero.yml e invalido, paths nao existem, ou tipos sao incompativeis.

**Done:** `loopzero deploy` com config invalida retorna exit code 1, mensagem descrevendo o erro exato, e `--json` retorna campo `error` com `field` e `message`. Nenhum deploy prossegue com config invalida.

- **Tarefa 2.1 — Validar existencia de path no filesystem**
  - Acao: No `loopzero deploy`, apos parsear loopzero.yml, iterar sobre cada app e verificar que `path` resolve para um diretorio existente relativo ao loopzero.yml. Se nao existir, retornar exit code 1 com mensagem: `"App '{name}': path '{path}' does not exist"`.
  - Done: `loopzero deploy` em projeto com `apps.api.path: apps/nonexistent` retorna exit code 1 e mensagem contendo `"path 'apps/nonexistent' does not exist"`. Teste unitario cobre este cenario.

- **Tarefa 2.2 — Validar combinacao type + port**
  - Acao: No CLI, antes de upload, validar: `type: server` exige `port > 0`; `type: worker` rejeita `port` (erro se presente); `type: frontend` aceita `port` opcional. Retornar exit code 1 com mensagem especifica por violacao.
  - Done: Testes unitarios cobrem 4 cenarios: server sem port (erro), server com port (ok), worker com port (erro), frontend sem port (ok). Cada erro tem mensagem identificando app name e violacao.

- **Tarefa 2.3 — Validar build vs dockerfile consistencia**
  - Acao: Quando `build: dockerfile`, verificar que o arquivo referenciado por `dockerfile` (ou default `Dockerfile`) existe no `path` da app. Quando `build: buildpack`, verificar que `framework` e um dos valores reconhecidos. Retornar exit code 1 com mensagem especifica.
  - Done: `loopzero deploy` com `build: dockerfile` e Dockerfile ausente retorna exit code 1 e mensagem `"App '{name}': Dockerfile not found at '{path}/Dockerfile'"`. Teste unitario cobre ambos os cenarios.

- **Tarefa 2.4 — Validar campos desconhecidos no YAML**
  - Acao: No `parseLoopZeroYaml`, detectar campos nao reconhecidos no nivel raiz e no nivel de app. Emitir warning (nao erro) listando campos desconhecidos: `"Unknown field 'apps.api.replicas' -- ignored"`. Em `--json`, incluir array `warnings`.
  - Done: YAML com campo `apps.api.replicas: 3` gera warning visivel. `--json` inclui `"warnings": ["Unknown field 'apps.api.replicas'"]`. Teste unitario valida.

- **Tarefa 2.5 — Validar no modo CI**
  - Acao: Quando `CI=true`, erros de validacao devem sair em formato estruturado (sem cor, sem spinner). `--json` retorna `{ "ok": false, "errors": [...] }`. Exit code 1.
  - Done: `CI=true loopzero deploy --json` com config invalida retorna JSON com array `errors` contendo `{ "field", "message" }` para cada violacao. Teste unitario cobre.

---

## Milestone 3 — Modelo Normalizado AppUnit

**Objetivo:** Criar tipo interno AppUnit que representa uma app completamente resolvida — todos os campos obrigatorios preenchidos, pronta para gerar um build plan.

**Done:** Tipo AppUnit existe no CLI e no API. Toda app no pipeline passa por normalizacao para AppUnit antes de qualquer operacao. Testes unitarios cobrem normalizacao de cada combinacao valida de campos.

- **Tarefa 3.1 — Definir tipo AppUnit no CLI**
  - Acao: Criar interface `AppUnit` em `cli/src/adapters/app-unit.ts` com campos obrigatorios: `name: string`, `type: 'server' | 'worker' | 'frontend'`, `path: string`, `framework: string`, `port: number | null`, `build: 'dockerfile' | 'buildpack' | 'custom'`, `dockerfile: string | null`, `env: Record<string, string>`. Nenhum campo e opcional.
  - Done: Interface `AppUnit` existe, exportada, e todos os campos sao non-optional (exceto `port` e `dockerfile` que sao `| null`).

- **Tarefa 3.2 — Implementar funcao normalizeApp() no CLI**
  - Acao: Criar funcao `normalizeApp(name: string, raw: LoopZeroYamlApp): AppUnit` que preenche defaults: `build` default `buildpack` quando `framework` e reconhecido, `dockerfile` quando framework e `custom`. `type` default inferido de `framework` (frontends -> `frontend`, resto -> `server`). `env` default `{}`.
  - Done: `normalizeApp("api", { path: "apps/api", framework: "express", port: 3000 })` retorna `AppUnit` com `type: "server"`, `build: "buildpack"`, `env: {}`. Teste unitario cobre 6 combinacoes.

- **Tarefa 3.3 — Definir tipo AppUnit no API (Go)**
  - Acao: Criar struct `AppUnit` em `api/internal/services/app_unit.go` com campos: `Name`, `Type`, `Path`, `Framework`, `Port`, `Build`, `Dockerfile`, `Env`. Substituir uso de `AppSpec` no `BuildOrchestrator` por `AppUnit`.
  - Done: Struct `AppUnit` existe. `BuildOrchestrator.SubmitBuild` recebe `[]AppUnit` em vez de `[]AppSpec`. Codigo compila e testes existentes passam.

- **Tarefa 3.4 — Implementar funcao NormalizeApp() no API (Go)**
  - Acao: Criar funcao `NormalizeApp(name string, raw AppConfig) (AppUnit, error)` em `api/internal/services/app_unit.go`. Mesma logica de defaults da tarefa 3.2. Retorna erro se combinacao e invalida (server sem port, worker com port).
  - Done: `NormalizeApp("api", AppConfig{Path: "apps/api", Framework: "express", Port: 3000})` retorna `AppUnit` com `Build: "buildpack"`. Teste unitario cobre mesmas 6 combinacoes do CLI.

- **Tarefa 3.5 — Substituir AppSpec por AppUnit no payload CLI -> API**
  - Acao: Alterar payload de `POST /api/v1/builds` para enviar `AppUnit` (com campos `build`, `dockerfile`, `env`) em vez de `AppSpec`. Atualizar handler `BuildHandler.Create` para deserializar `AppUnit`. Manter backward compatibility: se `build` ausente, tratar como `buildpack`.
  - Done: CLI envia `AppUnit` no body. API deserializa corretamente. Deploy end-to-end funciona com novo payload. Teste de integracao cobre.

---

## Milestone 4 — Build Plan

**Objetivo:** Criar modelo intermediario BuildPlan que traduz AppUnit[] em instrucoes concretas para o pipeline — uma por app, com estrategia de build resolvida.

**Done:** Tipo BuildPlan existe no API. WorkflowBuilder recebe BuildPlan em vez de WorkflowSpec.Apps. Testes unitarios cobrem geracao de plan para cada estrategia de build.

- **Tarefa 4.1 — Definir tipo BuildPlan e AppBuildPlan**
  - Acao: Criar struct `BuildPlan` em `api/internal/services/build_plan.go` contendo `[]AppBuildPlan`. Cada `AppBuildPlan` contem: `AppName`, `ImageTag`, `DockerfilePath` (resolvido — absoluto no workspace), `ContextPath`, `BuildArgs map[string]string`, `CacheKey`, `LanguageStrategy string`.
  - Done: Structs existem, compilam, e um teste unitario instancia um `BuildPlan` com 2 apps de estrategias diferentes.

- **Tarefa 4.2 — Implementar funcao CreateBuildPlan()**
  - Acao: Criar funcao `CreateBuildPlan(units []AppUnit, tenantID, projectID, commitSHA, registryURL string) BuildPlan` em `api/internal/services/build_plan.go`. Para cada AppUnit: resolver `DockerfilePath` (se `build: dockerfile`, usar `{path}/{dockerfile}`; se `buildpack`, gerar path para Dockerfile auto-gerado), resolver `ImageTag` como `{registryURL}/{tenantID}/{projectID}/{appName}:{commitSHA}`, resolver `LanguageStrategy` via `ResolveStrategy(framework).Name()`.
  - Done: `CreateBuildPlan` com 2 apps retorna plan com 2 entries. Teste unitario valida `ImageTag`, `DockerfilePath`, e `LanguageStrategy` para cada app.

- **Tarefa 4.3 — Adaptar WorkflowBuilder.Build() para receber BuildPlan**
  - Acao: Alterar assinatura de `WorkflowBuilder.Build(spec WorkflowSpec)` para `WorkflowBuilder.Build(plan BuildPlan, meta WorkflowMeta)` onde `WorkflowMeta` contem `BuildID`, `TenantID`, `Namespace`, `CorrelationID`. O builder itera sobre `plan.Apps` em vez de `spec.Apps`.
  - Done: `WorkflowBuilder.Build` aceita `BuildPlan`. Todos os testes existentes do `WorkflowBuilder` adaptados e passando. Workflow CR gerado e identico ao anterior para mesmos inputs.

- **Tarefa 4.4 — Mover groupAppsByLanguage para operar sobre AppBuildPlan**
  - Acao: Refatorar `groupAppsByLanguage` para agrupar `[]AppBuildPlan` por campo `LanguageStrategy` em vez de chamar `ResolveStrategy` internamente. A resolucao de estrategia acontece no `CreateBuildPlan`, nao no `WorkflowBuilder`.
  - Done: `groupAppsByLanguage` aceita `[]AppBuildPlan`. Nao chama mais `ResolveStrategy`. Testes passam.

- **Tarefa 4.5 — Integrar CreateBuildPlan no BuildOrchestrator**
  - Acao: No `BuildOrchestrator.SubmitBuild`, chamar `CreateBuildPlan(units, ...)` e passar o resultado para `WorkflowBuilder.Build(plan, meta)`. Remover construcao inline de `WorkflowSpec.Apps`.
  - Done: `BuildOrchestrator` usa `CreateBuildPlan`. Deploy end-to-end funciona. `BuildPlan` e persistido junto com o `Build` record no banco (campo JSON `build_plan`).

---

## Milestone 5 — Pipeline Per-App

**Objetivo:** Cada app no BuildPlan gera um step de build independente no Workflow, com Dockerfile resolvido do plan (nao inferido no pipeline).

**Done:** Workflow CR gerado contem um step de build de imagem por app, usando DockerfilePath e ContextPath do AppBuildPlan. Monorepo com 3 apps gera 3 steps de build de imagem independentes no DAG.

- **Tarefa 5.1 — Gerar Dockerfile path a partir do AppBuildPlan**
  - Acao: No `WorkflowBuilder`, ao gerar template de build de imagem (Kaniko/BuildKit), usar `appPlan.DockerfilePath` como `--dockerfile` arg em vez de gerar `Dockerfile.{appName}` inline. Quando `build: buildpack`, o Dockerfile e gerado pelo step `dockerfile-{lang}` e salvo no path indicado pelo plan.
  - Done: Template de build de imagem usa `DockerfilePath` do plan. Teste unitario valida que arg `--dockerfile` contem o path do plan, nao um path hardcoded.

- **Tarefa 5.2 — Usar ContextPath do plan para context de build**
  - Acao: No template de build de imagem, usar `appPlan.ContextPath` como `--context` arg em vez de `/workspace` fixo. Para monorepo, context e `/workspace/{appPath}`. Para single-app, context e `/workspace`.
  - Done: Template com app `path: apps/api` gera `--context /workspace/apps/api`. Teste unitario valida para single-app e monorepo.

- **Tarefa 5.3 — Injetar BuildArgs do plan no template de build**
  - Acao: No template de build de imagem, iterar sobre `appPlan.BuildArgs` e gerar `--build-arg KEY=VALUE` para cada entrada. `BuildArgs` inclui entries do campo `env` do `AppUnit` mais entries geradas pelo sistema (`COMMIT_SHA`, `APP_NAME`).
  - Done: `BuildPlan` com `env: {"NODE_ENV": "production"}` gera template com `--build-arg NODE_ENV=production`. Teste unitario valida.

- **Tarefa 5.4 — Eliminar fallback silencioso em ResolveStrategy**
  - Acao: Remover fallback `framework == "" -> NodeStrategy` de `ResolveStrategy`. Quando `LanguageStrategy` e vazio ou nao reconhecido, retornar erro em vez de fallback. O `CreateBuildPlan` ja garante que `LanguageStrategy` e resolvido antes de chegar ao builder.
  - Done: `ResolveStrategy("")` retorna erro `"unknown framework ''"`. Nenhum fallback silencioso. Testes ajustados.

- **Tarefa 5.5 — Validar pipeline com monorepo multi-linguagem**
  - Acao: Escrever teste unitario do `WorkflowBuilder` com `BuildPlan` contendo 3 apps: Node (express), Go (gin), Python (fastapi). Validar que DAG contem 3 grupos de steps de linguagem e 3 steps de build de imagem independentes.
  - Done: Teste passa. DAG contem `install-deps-node`, `install-deps-go`, `install-deps-python` e 3 steps `build-image-{app}` com dependencies corretas.

---

## Milestone 6 — Deteccao Baseada em Contrato

**Objetivo:** Substituir heuristicas frageis de deteccao por deteccao baseada em contrato: presenca de loopzero.yml e o modo primario, e modo automatico usa project markers (nao leitura de conteudo de arquivos).

**Done:** `loopzero init` gera loopzero.yml v2 completo. `loopzero deploy` nao executa `detectFramework` nem `detectMonorepoTool` — usa exclusivamente o loopzero.yml parseado.

- **Tarefa 6.1 — Reescrever loopzero init para gerar schema v2**
  - Acao: Alterar `InitCommand.execute()` para gerar loopzero.yml com campos `build`, `type`, `env`, `ignore` e `dockerfile` (quando detectado). A deteccao existente e usada apenas durante `init` para popular o YAML inicial. O output gerado e o schema v2 completo.
  - Done: `loopzero init` em monorepo Turborepo com app Express gera loopzero.yml contendo `build: buildpack`, `type: server`, `env: {}`. Teste unitario valida output do YAML gerado.

- **Tarefa 6.2 — Remover detectFramework do fluxo de deploy**
  - Acao: No `loopzero deploy`, remover chamada a `detectFramework` e `resolveAppType`. Toda informacao vem do loopzero.yml parseado -> normalizado para `AppUnit[]`. Se loopzero.yml nao existe, retornar exit code 1 com mensagem: `"No loopzero.yml found. Run: loopzero init"`.
  - Done: `loopzero deploy` sem loopzero.yml retorna exit code 1 e mensagem contendo `"loopzero init"`. `detectFramework` nao e chamada em nenhum path de deploy. Teste unitario cobre.

- **Tarefa 6.3 — Remover inferAppType do fluxo de deploy**
  - Acao: Deletar chamadas a `inferAppType` em `deploy.ts`. O `type` vem do loopzero.yml (obrigatorio no schema v2). Se ausente, `normalizeApp` aplica default baseado em `framework` (mesma regra do `init`), mas com warning.
  - Done: `grep "inferAppType" cli/src/commands/deploy.ts` retorna zero resultados. Teste unitario valida que deploy usa `type` do YAML.

- **Tarefa 6.4 — Simplificar deteccao de monorepo para init only**
  - Acao: Mover `detectMonorepoTool` para ser chamada exclusivamente por `InitCommand`. Adicionar comentario no modulo: "Monorepo detection runs only during `loopzero init`. At deploy time, `loopzero.yml` is the source of truth."
  - Done: `grep -r "detectMonorepo" cli/src/commands/deploy.ts` retorna zero resultados. Funcao permanece em `detection/monorepo.ts` para uso pelo `init`.

- **Tarefa 6.5 — Adicionar comando loopzero validate**
  - Acao: Criar comando `loopzero validate` que parseia loopzero.yml, executa todas as validacoes da Milestone 2, e retorna exit code 0 (valido) ou 1 (invalido). Output: lista de erros ou `"loopzero.yml is valid (N apps)"`. Suporta `--json`.
  - Done: `loopzero validate` com config valida retorna exit code 0 e mensagem `"loopzero.yml is valid (2 apps)"`. Com config invalida, retorna exit code 1 e lista de erros. `--json` retorna `{ "valid": true/false, "errors": [], "apps": N }`. Testes unitarios cobrem ambos cenarios.

---

## Milestone 7 — Modo Dual: Automatico + Explicito

**Objetivo:** Suportar dois modos operacionais limpos — automatico (defaults do init) e explicito (config manual no loopzero.yml) — sem ambiguidade sobre qual modo esta ativo.

**Done:** `loopzero deploy` opera em um unico path de codigo independente do modo. O loopzero.yml e obrigatorio. O modo automatico significa "YAML gerado pelo init sem edicao manual". O modo explicito significa "YAML editado pelo usuario". Ambos passam pela mesma validacao e normalizacao.

- **Tarefa 7.1 — Adicionar campo version ao schema loopzero.yml**
  - Acao: Adicionar campo obrigatorio `version: 1` ao tipo `LoopZeroYaml` (CLI) e `LoopZeroYAML` (API). Validacao rejeita quando ausente ou diferente de `1`. Permite evolucao futura do schema sem quebrar configs existentes.
  - Done: `ValidateLoopZeroYAML` sem campo `version` retorna erro `"version is required (current: 1)"`. `parseLoopZeroYaml` parseia campo `version`. Testes cobrem.

- **Tarefa 7.2 — Migrar configs existentes para schema v2**
  - Acao: No `loopzero deploy`, quando loopzero.yml e parseado e nao contem campos v2 (`version`, `build`), emitir warning: `"loopzero.yml uses legacy format. Run: loopzero init --force to upgrade"`. Aplicar defaults v2 internamente (backward compat), mas logar warning.
  - Done: Deploy com YAML sem `version` funciona (backward compat) e emite warning visivel. Deploy com `version: 1` nao emite warning. Teste unitario cobre ambos.

- **Tarefa 7.3 — Documentar formato loopzero.yml v2 completo**
  - Acao: Atualizar secao de loopzero.yaml na spec (sec. 26 quando existir) com schema completo, exemplos de single-app, monorepo, worker, frontend, custom Dockerfile, e todas as regras de validacao. Se a secao nao existe, criar em `docs/`.
  - Done: Documentacao existe com exemplos para: single app server, monorepo 3 apps, worker sem port, frontend, custom Dockerfile. Cada campo tem descricao, tipo, default, e regras de validacao.

- **Tarefa 7.4 — Remover resolveAppType do deploy**
  - Acao: No `cli/src/commands/deploy.ts`, remover qualquer chamada residual a `resolveAppType` ou inferencia de tipo. O deploy recebe `AppUnit[]` ja normalizados — tipo resolvido, build resolvido, port validado.
  - Done: `grep -r "resolveAppType" cli/src/commands/` retorna zero resultados. Testes passam.

- **Tarefa 7.5 — Teste end-to-end: single app, monorepo, custom Dockerfile**
  - Acao: Escrever 3 testes de integracao no CLI: (1) deploy de single app com `build: buildpack`, (2) deploy de monorepo com 2 apps (server + worker), (3) deploy de app com `build: dockerfile` e Dockerfile custom. Cada teste valida que o payload enviado ao API contem `AppUnit[]` corretos.
  - Done: 3 testes passam. Payload para API contem `build`, `type`, `dockerfile`, `env` para cada app.

- **Tarefa 7.6 — Validar no ambiente DigitalOcean (dev)**
  - Acao: Executar deploy de monorepo com 2+ apps no ambiente dev (DigitalOcean) usando loopzero.yml v2. Validar: YAML parseado -> `AppUnit[]` -> `BuildPlan` -> Workflow CR -> build por app -> imagens no registry -> ArgoCD sync -> apps acessiveis.
  - Done: Deploy completa no ambiente DigitalOcean. Cada app tem imagem distinta no DO Container Registry. Apps respondem nas URLs esperadas.

---

## Decisoes arquiteturais

| Decisao | Justificativa |
|---|---|
| **Schema v2 aditivo** (campos novos, nenhum renomeado) | Backward compatibility. Configs existentes continuam funcionando com defaults aplicados e warning. |
| **AppUnit como fronteira de normalizacao** | Antes do AppUnit: parsing e validacao. Depois: tudo deterministico. Elimina bugs de "heuristica errou e propagou silenciosamente". |
| **Deteccao confinada ao init** | Preserva DX de "zero config" no onboarding sem custo de fragilidade em producao. Heuristicas rodam uma vez, resultado e persistido no YAML. |
| **BuildPlan como intermediario** | Separa "o que o usuario quer" (AppUnit) de "como o pipeline executa" (BuildPlan). WorkflowBuilder nao precisa saber sobre loopzero.yml. |
| **loopzero.yml obrigatorio no deploy** | Elimina ambiguidade. Sem YAML = erro explicito com sugestao de `loopzero init`. Nenhum path de deploy infere nada. |
| **version field desde o inicio** | Permite evolucao futura do schema sem breaking change. Version 1 e o contrato atual. |

## Ordem de dependencias

```
M1 (Schema) ──> M2 (Validacao) ──> M3 (AppUnit) ──> M4 (BuildPlan) ──> M5 (Pipeline)
                                         │
                                         └──> M6 (Deteccao) ──> M7 (Modo Dual)
```

M1-M5 e o caminho critico. M6-M7 podem iniciar apos M3 em paralelo com M4-M5.
